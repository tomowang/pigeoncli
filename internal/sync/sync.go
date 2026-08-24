package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/imap"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

// FolderProgress reports the outcome of syncing one folder, for callers
// that want to print/stream progress.
type FolderProgress struct {
	Path        string
	TotalCount  int
	UnreadCount int
	Err         error
}

// Account is the minimal account information the sync engine needs: its
// local slug/display metadata, IMAP connection settings, and credentials.
type Account struct {
	Slug        string
	Email       string
	DisplayName string
	IMAP        config.ServerConfig
	Provider    auth.Provider
}

// SyncAccount connects to a's IMAP server, lists its folders, and syncs
// message headers for each folder into db. If onProgress is non-nil, it's
// called once per folder as syncing completes (successfully or not);
// syncing continues with the remaining folders after a per-folder error.
func SyncAccount(ctx context.Context, db *sqlite.DB, a Account, onProgress func(FolderProgress)) error {
	cl, err := imap.DialClient(ctx, imap.DialOptions{Host: a.IMAP.Host, Port: a.IMAP.Port, TLS: a.IMAP.TLS}, a.Provider)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer cl.Close()
	defer cl.WatchContext(ctx)()

	accountID, err := db.UpsertAccount(ctx, a.Slug, a.Email, a.DisplayName)
	if err != nil {
		return fmt.Errorf("mirror account: %w", err)
	}

	remoteFolders, err := cl.ListFolders(ctx)
	if err != nil {
		return fmt.Errorf("list folders: %w", err)
	}

	existing, err := db.ListFolders(ctx, accountID)
	if err != nil {
		return fmt.Errorf("load existing folder state: %w", err)
	}
	priorUIDValidity := make(map[string]uint32, len(existing))
	for _, f := range existing {
		priorUIDValidity[f.Path] = f.UIDValidity
	}

	for _, rf := range remoteFolders {
		result := FolderProgress{Path: rf.Path}
		total, unread, err := syncFolder(ctx, db, cl, accountID, rf, priorUIDValidity[rf.Path])
		if err != nil {
			result.Err = fmt.Errorf("sync %q: %w", rf.Path, err)
		} else {
			result.TotalCount = total
			result.UnreadCount = unread
		}
		if onProgress != nil {
			onProgress(result)
		}
	}

	return db.SetAccountSyncedNow(ctx, accountID)
}

func syncFolder(ctx context.Context, db *sqlite.DB, cl *imap.Client, accountID int64, rf imap.Folder, priorUIDValidity uint32) (total, unread int, err error) {
	name := rf.Path
	if rf.Delim != "" {
		if idx := strings.LastIndex(rf.Path, rf.Delim); idx >= 0 {
			name = rf.Path[idx+len(rf.Delim):]
		}
	}

	folderID, err := db.UpsertFolder(ctx, accountID, rf.Path, name, rf.Delim, rf.SpecialUse)
	if err != nil {
		return 0, 0, fmt.Errorf("upsert folder: %w", err)
	}

	uidValidity, uidNext, _, err := cl.SelectFolder(ctx, rf.Path)
	if err != nil {
		return 0, 0, fmt.Errorf("select: %w", err)
	}

	if priorUIDValidity != 0 && priorUIDValidity != uidValidity {
		if err := db.ClearFolderMessages(ctx, folderID); err != nil {
			return 0, 0, fmt.Errorf("clear stale cache after uidvalidity change: %w", err)
		}
	}

	headers, err := cl.FetchAllHeaders(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch headers: %w", err)
	}

	rows := make([]sqlite.MessageHeader, len(headers))
	uids := make([]uint32, len(headers))
	for i, h := range headers {
		rows[i] = sqlite.MessageHeader{
			UID: h.UID, MessageID: h.MessageID, InReplyTo: h.InReplyTo, Subject: h.Subject,
			FromName: h.FromName, FromAddr: h.FromAddr, ToAddrs: h.ToAddrs, CcAddrs: h.CcAddrs,
			Date: h.Date, Flags: h.Flags, Size: h.Size,
		}
		uids[i] = h.UID
		if !seen(h.Flags) {
			unread++
		}
	}
	total = len(rows)

	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, rows); err != nil {
		return 0, 0, fmt.Errorf("store headers: %w", err)
	}
	if err := db.DeleteMessagesNotIn(ctx, folderID, uids); err != nil {
		return 0, 0, fmt.Errorf("reconcile deletions: %w", err)
	}
	if err := db.UpdateFolderSyncState(ctx, folderID, uidValidity, uidNext); err != nil {
		return 0, 0, fmt.Errorf("update sync state: %w", err)
	}

	return total, unread, nil
}

func seen(flags []string) bool {
	for _, f := range flags {
		if f == `\Seen` {
			return true
		}
	}
	return false
}
