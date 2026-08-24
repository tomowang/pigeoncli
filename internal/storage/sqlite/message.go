package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// MessageHeader is the set of header fields synced eagerly for every
// message (bodies are fetched lazily — see Phase 3).
type MessageHeader struct {
	UID       uint32
	MessageID string
	InReplyTo string
	Subject   string
	FromName  string
	FromAddr  string
	ToAddrs   []string
	CcAddrs   []string
	Date      time.Time
	Flags     []string
	Size      int64
}

func (h MessageHeader) seen() bool {
	for _, f := range h.Flags {
		if f == `\Seen` {
			return true
		}
	}
	return false
}

// UpsertMessageHeaders inserts or updates header rows for a batch of
// messages in one folder.
func (db *DB) UpsertMessageHeaders(ctx context.Context, accountID, folderID int64, headers []MessageHeader) error {
	if len(headers) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert headers: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO messages (
			account_id, folder_id, uid, message_id, in_reply_to, subject,
			from_name, from_addr, to_addrs, cc_addrs, date, flags, seen, size, synced_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(folder_id, uid) DO UPDATE SET
			message_id = excluded.message_id,
			in_reply_to = excluded.in_reply_to,
			subject = excluded.subject,
			from_name = excluded.from_name,
			from_addr = excluded.from_addr,
			to_addrs = excluded.to_addrs,
			cc_addrs = excluded.cc_addrs,
			date = excluded.date,
			flags = excluded.flags,
			seen = excluded.seen,
			size = excluded.size,
			synced_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert headers: %w", err)
	}
	defer stmt.Close()

	for _, h := range headers {
		toJSON, err := json.Marshal(h.ToAddrs)
		if err != nil {
			return fmt.Errorf("encode to_addrs for uid=%d: %w", h.UID, err)
		}
		ccJSON, err := json.Marshal(h.CcAddrs)
		if err != nil {
			return fmt.Errorf("encode cc_addrs for uid=%d: %w", h.UID, err)
		}
		flagsJSON, err := json.Marshal(h.Flags)
		if err != nil {
			return fmt.Errorf("encode flags for uid=%d: %w", h.UID, err)
		}
		seen := 0
		if h.seen() {
			seen = 1
		}
		if _, err := stmt.ExecContext(ctx, accountID, folderID, h.UID, h.MessageID, h.InReplyTo, h.Subject,
			h.FromName, h.FromAddr, string(toJSON), string(ccJSON), h.Date, string(flagsJSON), seen, h.Size,
		); err != nil {
			return fmt.Errorf("upsert header uid=%d: %w", h.UID, err)
		}
	}

	return tx.Commit()
}

// DeleteMessagesNotIn removes cached messages in folderID whose UID isn't
// in keepUIDs — used to reconcile server-side deletions/expunges.
func (db *DB) DeleteMessagesNotIn(ctx context.Context, folderID int64, keepUIDs []uint32) error {
	keep := make(map[uint32]struct{}, len(keepUIDs))
	for _, u := range keepUIDs {
		keep[u] = struct{}{}
	}

	rows, err := db.QueryContext(ctx, "SELECT uid FROM messages WHERE folder_id = ?", folderID)
	if err != nil {
		return fmt.Errorf("list cached uids: %w", err)
	}
	var stale []uint32
	for rows.Next() {
		var uid uint32
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return fmt.Errorf("scan cached uid: %w", err)
		}
		if _, ok := keep[uid]; !ok {
			stale = append(stale, uid)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(stale) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete stale messages: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "DELETE FROM messages WHERE folder_id = ? AND uid = ?")
	if err != nil {
		return fmt.Errorf("prepare delete stale messages: %w", err)
	}
	defer stmt.Close()

	for _, uid := range stale {
		if _, err := stmt.ExecContext(ctx, folderID, uid); err != nil {
			return fmt.Errorf("delete stale uid=%d: %w", uid, err)
		}
	}

	return tx.Commit()
}

// MessageRow is a cached message's header fields, as read back for display.
type MessageRow struct {
	UID       uint32
	MessageID string
	InReplyTo string
	Subject   string
	FromName  string
	FromAddr  string
	ToAddrs   []string
	CcAddrs   []string
	Date      time.Time
	Flags     []string
	Size      int64
}

// ListMessages returns up to limit cached messages for folderID, most
// recent first.
func (db *DB) ListMessages(ctx context.Context, folderID int64, limit int) ([]MessageRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT uid, message_id, in_reply_to, subject, from_name, from_addr, to_addrs, cc_addrs, date, flags, size
		FROM messages WHERE folder_id = ? ORDER BY date DESC, uid DESC LIMIT ?
	`, folderID, limit)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var out []MessageRow
	for rows.Next() {
		var m MessageRow
		var toJSON, ccJSON, flagsJSON string
		var date sql.NullTime
		if err := rows.Scan(&m.UID, &m.MessageID, &m.InReplyTo, &m.Subject, &m.FromName, &m.FromAddr,
			&toJSON, &ccJSON, &date, &flagsJSON, &m.Size); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		m.Date = date.Time
		if err := json.Unmarshal([]byte(toJSON), &m.ToAddrs); err != nil {
			return nil, fmt.Errorf("decode to_addrs for uid=%d: %w", m.UID, err)
		}
		if err := json.Unmarshal([]byte(ccJSON), &m.CcAddrs); err != nil {
			return nil, fmt.Errorf("decode cc_addrs for uid=%d: %w", m.UID, err)
		}
		if err := json.Unmarshal([]byte(flagsJSON), &m.Flags); err != nil {
			return nil, fmt.Errorf("decode flags for uid=%d: %w", m.UID, err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MessageBodyState reports whether a message's raw body has already been
// fetched and cached, and if so, the blob store reference it was cached
// under.
func (db *DB) MessageBodyState(ctx context.Context, folderID int64, uid uint32) (rawRef string, bodySynced bool, err error) {
	var ref sql.NullString
	var synced bool
	err = db.QueryRowContext(ctx, "SELECT raw_ref, body_synced FROM messages WHERE folder_id = ? AND uid = ?", folderID, uid).
		Scan(&ref, &synced)
	if err != nil {
		return "", false, fmt.Errorf("load body state for uid=%d: %w", uid, err)
	}
	return ref.String, synced, nil
}

// SetMessageBodyCached records that a message's raw body has been fetched
// and cached under rawRef.
func (db *DB) SetMessageBodyCached(ctx context.Context, folderID int64, uid uint32, rawRef string) error {
	_, err := db.ExecContext(ctx, "UPDATE messages SET raw_ref = ?, body_synced = 1 WHERE folder_id = ? AND uid = ?",
		rawRef, folderID, uid)
	if err != nil {
		return fmt.Errorf("set body cached for uid=%d: %w", uid, err)
	}
	return nil
}
