package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// FolderRow is a folder as cached locally.
type FolderRow struct {
	ID          int64
	AccountID   int64
	Path        string
	Name        string
	Delimiter   string
	SpecialUse  string
	UIDValidity uint32
	UIDNext     uint32
	ModSeq      uint64
	// SyncHorizonUID is the oldest UID this folder has committed to
	// keeping synced, once an initial-sync window has been applied. 0
	// means no horizon — every remote message is expected to be cached.
	SyncHorizonUID uint32
	UnreadCount    int
	TotalCount     int
}

// UpsertFolder inserts or updates a folder's identity fields (name,
// delimiter, special-use), returning its internal id. Sync state
// (uidvalidity/uidnext/counts) is updated separately via
// UpdateFolderSyncState once a sync pass completes.
func (db *DB) UpsertFolder(ctx context.Context, accountID int64, path, name, delimiter, specialUse string) (int64, error) {
	_, err := db.ExecContext(ctx, `
		INSERT INTO folders (account_id, path, name, delimiter, special_use)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(account_id, path) DO UPDATE SET
			name = excluded.name,
			delimiter = excluded.delimiter,
			special_use = excluded.special_use
	`, accountID, path, name, delimiter, specialUse)
	if err != nil {
		return 0, fmt.Errorf("upsert folder %q: %w", path, err)
	}

	var id int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM folders WHERE account_id = ? AND path = ?", accountID, path).Scan(&id); err != nil {
		return 0, fmt.Errorf("load folder id for %q: %w", path, err)
	}
	return id, nil
}

// FolderID looks up the local row id for the folder at path under
// accountID. It reports false if the folder has never been synced.
func (db *DB) FolderID(ctx context.Context, accountID int64, path string) (int64, bool, error) {
	var id int64
	err := db.QueryRowContext(ctx, "SELECT id FROM folders WHERE account_id = ? AND path = ?", accountID, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("load folder id for %q: %w", path, err)
	}
	return id, true, nil
}

// ListFolders returns all folders for accountID, ordered by path.
func (db *DB) ListFolders(ctx context.Context, accountID int64) ([]FolderRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, account_id, path, name, delimiter, special_use, uidvalidity, uidnext, mod_seq, sync_horizon_uid, unread_count, total_count
		FROM folders WHERE account_id = ? ORDER BY path
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FolderRow
	for rows.Next() {
		var f FolderRow
		var delim, specialUse sql.NullString
		if err := rows.Scan(&f.ID, &f.AccountID, &f.Path, &f.Name, &delim, &specialUse,
			&f.UIDValidity, &f.UIDNext, &f.ModSeq, &f.SyncHorizonUID, &f.UnreadCount, &f.TotalCount); err != nil {
			return nil, fmt.Errorf("scan folder: %w", err)
		}
		f.Delimiter = delim.String
		f.SpecialUse = specialUse.String
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpdateFolderSyncState records a folder's post-sync UIDVALIDITY/UIDNEXT/
// MODSEQ/sync-horizon, recomputes its message counts from the messages
// table, and returns those counts so callers (e.g. sync progress
// reporting) don't need to track them separately as messages are
// upserted/deleted. Note that total/unread reflect what's cached locally,
// which is only the whole mailbox when syncHorizonUID is 0 — otherwise
// older messages below the horizon are intentionally left uncached.
func (db *DB) UpdateFolderSyncState(ctx context.Context, folderID int64, uidValidity, uidNext uint32, modSeq uint64, syncHorizonUID uint32) (total, unread int, err error) {
	if _, err := db.ExecContext(ctx, `
		UPDATE folders SET
			uidvalidity = ?,
			uidnext = ?,
			mod_seq = ?,
			sync_horizon_uid = ?,
			total_count = (SELECT COUNT(*) FROM messages WHERE folder_id = ?),
			unread_count = (SELECT COUNT(*) FROM messages WHERE folder_id = ? AND seen = 0),
			last_synced_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, uidValidity, uidNext, modSeq, syncHorizonUID, folderID, folderID, folderID); err != nil {
		return 0, 0, fmt.Errorf("update folder sync state: %w", err)
	}

	if err := db.QueryRowContext(ctx, "SELECT total_count, unread_count FROM folders WHERE id = ?", folderID).
		Scan(&total, &unread); err != nil {
		return 0, 0, fmt.Errorf("load folder counts: %w", err)
	}
	return total, unread, nil
}

// ClearFolderMessages deletes all cached messages for a folder — used when
// UIDVALIDITY changes and the local cache must be rebuilt from scratch.
func (db *DB) ClearFolderMessages(ctx context.Context, folderID int64) error {
	if _, err := db.ExecContext(ctx, "DELETE FROM messages WHERE folder_id = ?", folderID); err != nil {
		return fmt.Errorf("clear folder messages: %w", err)
	}
	return nil
}
