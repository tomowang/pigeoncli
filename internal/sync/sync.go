package sync

import (
	"context"
	"fmt"
	"sort"
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
	defer func() { _ = cl.Close() }()
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

	// Cheap first pass: just UIDs and flags for the whole folder. Diffed
	// against what's already cached, this tells us which UIDs are new
	// (need a full envelope fetch) vs. already known (headers are
	// immutable per UID, so only a changed flag set needs writing back).
	remoteFlags, err := cl.FetchUIDsAndFlags(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch uids/flags: %w", err)
	}
	localFlags, err := db.ListMessageFlags(ctx, folderID)
	if err != nil {
		return 0, 0, fmt.Errorf("load cached flags: %w", err)
	}

	var newUIDs []uint32
	flagUpdates := make(map[uint32][]string)
	for uid, flags := range remoteFlags {
		if lf, ok := localFlags[uid]; ok {
			if !flagsEqual(lf, flags) {
				flagUpdates[uid] = flags
			}
		} else {
			newUIDs = append(newUIDs, uid)
		}
	}

	if len(newUIDs) > 0 {
		headers, err := cl.FetchHeaders(ctx, newUIDs)
		if err != nil {
			return 0, 0, fmt.Errorf("fetch new headers: %w", err)
		}
		rows := make([]sqlite.MessageHeader, len(headers))
		for i, h := range headers {
			rows[i] = sqlite.MessageHeader{
				UID: h.UID, MessageID: h.MessageID, InReplyTo: h.InReplyTo, References: h.References, Subject: h.Subject,
				FromName: h.FromName, FromAddr: h.FromAddr, ToAddrs: h.ToAddrs, CcAddrs: h.CcAddrs,
				Date: h.Date, Flags: h.Flags, Size: h.Size,
			}
		}
		if err := db.UpsertMessageHeaders(ctx, accountID, folderID, rows); err != nil {
			return 0, 0, fmt.Errorf("store new headers: %w", err)
		}
	}

	if err := db.UpdateMessageFlags(ctx, folderID, flagUpdates); err != nil {
		return 0, 0, fmt.Errorf("update flags: %w", err)
	}

	remoteUIDs := make([]uint32, 0, len(remoteFlags))
	for uid, flags := range remoteFlags {
		remoteUIDs = append(remoteUIDs, uid)
		if !seen(flags) {
			unread++
		}
	}
	total = len(remoteFlags)

	if err := db.DeleteMessagesNotIn(ctx, folderID, remoteUIDs); err != nil {
		return 0, 0, fmt.Errorf("reconcile deletions: %w", err)
	}
	if err := db.UpdateFolderSyncState(ctx, folderID, uidValidity, uidNext); err != nil {
		return 0, 0, fmt.Errorf("update sync state: %w", err)
	}

	return total, unread, nil
}

// flagsEqual reports whether a and b contain the same flags, ignoring
// order.
func flagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

func seen(flags []string) bool {
	for _, f := range flags {
		if f == `\Seen` {
			return true
		}
	}
	return false
}
