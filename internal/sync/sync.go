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
	// WindowCount caps how many of a folder's most recent messages get
	// fully synced the first time that folder is synced (or resynced
	// after a UIDVALIDITY change); 0 disables windowing and syncs full
	// history, as before this field existed.
	WindowCount int
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
	priorByPath := make(map[string]sqlite.FolderRow, len(existing))
	for _, f := range existing {
		priorByPath[f.Path] = f
	}

	for _, rf := range remoteFolders {
		result := FolderProgress{Path: rf.Path}
		total, unread, err := syncFolder(ctx, db, cl, accountID, rf, priorByPath[rf.Path], a.WindowCount)
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

func syncFolder(ctx context.Context, db *sqlite.DB, cl *imap.Client, accountID int64, rf imap.Folder, prior sqlite.FolderRow, windowCount int) (total, unread int, err error) {
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

	uidValidity, uidNext, _, highestModSeq, err := cl.SelectFolder(ctx, rf.Path)
	if err != nil {
		return 0, 0, fmt.Errorf("select: %w", err)
	}

	uidValidityChanged := prior.UIDValidity != 0 && prior.UIDValidity != uidValidity
	if uidValidityChanged {
		if err := db.ClearFolderMessages(ctx, folderID); err != nil {
			return 0, 0, fmt.Errorf("clear stale cache after uidvalidity change: %w", err)
		}
	}

	// CONDSTORE fast path: ask the server for only what changed since our
	// last-recorded HIGHESTMODSEQ, instead of every UID+flags in the
	// folder. Requires the server to have handed back a usable baseline
	// last time (prior.ModSeq != 0), CONDSTORE still negotiated this
	// session (highestModSeq != 0), and no UIDVALIDITY reset just now —
	// otherwise fall back to the full listing.
	useCondStore := prior.ModSeq != 0 && highestModSeq != 0 && !uidValidityChanged

	var changedFlags map[uint32][]string
	if useCondStore {
		changedFlags, err = cl.FetchChangedUIDsAndFlags(ctx, prior.ModSeq)
	} else {
		changedFlags, err = cl.FetchUIDsAndFlags(ctx)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("fetch uids/flags: %w", err)
	}
	localFlags, err := db.ListMessageFlags(ctx, folderID)
	if err != nil {
		return 0, 0, fmt.Errorf("load cached flags: %w", err)
	}

	// The local cache starts empty either on this folder's very first
	// sync, or right after a UIDVALIDITY reset just cleared it above. In
	// both cases any sync-horizon carried over from prior is meaningless
	// (a UIDVALIDITY change renumbers UIDs entirely), so it's recomputed
	// from scratch: windowCount>0 caps this sync to the most recent
	// windowCount messages, 0 disables windowing (syncs everything, the
	// pre-windowing behavior). A folder that's already been synced keeps
	// whatever horizon it committed to previously, regardless of the
	// current windowCount setting.
	horizon := prior.SyncHorizonUID
	if freshCache := prior.UIDValidity == 0 || uidValidityChanged; freshCache {
		horizon = 0
		if windowCount > 0 {
			horizon = windowHorizon(changedFlags, windowCount)
		}
	}

	var newUIDs []uint32
	flagUpdates := make(map[uint32][]string)
	for uid, flags := range changedFlags {
		if lf, ok := localFlags[uid]; ok {
			if !flagsEqual(lf, flags) {
				flagUpdates[uid] = flags
			}
		} else if horizon == 0 || uid >= horizon {
			newUIDs = append(newUIDs, uid)
		}
		// else: below the sync horizon — intentionally left uncached
		// until a future "load more" widens the window. Without this
		// check, every subsequent sync would treat these old UIDs as
		// "new" forever, since they're never added to the local cache.
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

	// CHANGEDSINCE never reports expunges (that needs QRESYNC's VANISHED
	// response, which isn't available here — see ListUIDs), so deletion
	// reconciliation always needs the full live-UID set. Under the
	// CONDSTORE path that means one extra round trip, but UID SEARCH ALL's
	// response is a handful of compressed ranges, not one line per
	// message, so it stays cheap even for large folders.
	var remoteUIDs []uint32
	if useCondStore {
		remoteUIDs, err = cl.ListUIDs(ctx)
		if err != nil {
			return 0, 0, fmt.Errorf("list uids: %w", err)
		}
	} else {
		remoteUIDs = make([]uint32, 0, len(changedFlags))
		for uid := range changedFlags {
			remoteUIDs = append(remoteUIDs, uid)
		}
	}

	if err := db.DeleteMessagesNotIn(ctx, folderID, remoteUIDs); err != nil {
		return 0, 0, fmt.Errorf("reconcile deletions: %w", err)
	}

	return db.UpdateFolderSyncState(ctx, folderID, uidValidity, uidNext, highestModSeq, horizon)
}

// windowHorizon returns the UID cutoff that keeps only the count most
// recent messages in remote — UID order is monotonic per RFC 3501 and
// used here as a recency proxy, avoiding a separate date-based IMAP
// search. Messages with UID below the returned value are left out of the
// sync. 0 means no cutoff: remote has count or fewer messages, so there's
// nothing to window.
func windowHorizon(remote map[uint32][]string, count int) uint32 {
	if len(remote) <= count {
		return 0
	}
	uids := make([]uint32, 0, len(remote))
	for uid := range remote {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
	return uids[count-1]
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
