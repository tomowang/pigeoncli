package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// SearchResultRow is one FTS5 header-search hit, joined with its folder
// path so the caller can open the message directly without a second query.
type SearchResultRow struct {
	MessageRow
	FolderPath string
}

// SearchMessages runs a full-text search over subject/from/to/cc headers
// for accountID's messages, ranked by FTS5's built-in relevance rank. An
// empty (or whitespace-only) query returns no results rather than matching
// everything.
func (db *DB) SearchMessages(ctx context.Context, accountID int64, query string, limit int) ([]SearchResultRow, error) {
	ftsQ := ftsQuery(query)
	if ftsQ == "" {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT m.uid, m.message_id, m.in_reply_to, m.subject, m.from_name, m.from_addr,
		       m.to_addrs, m.cc_addrs, m.date, m.flags, m.size, f.path
		FROM messages_fts
		JOIN messages m ON m.id = messages_fts.rowid
		JOIN folders f ON f.id = m.folder_id
		WHERE messages_fts MATCH ? AND m.account_id = ?
		ORDER BY rank
		LIMIT ?
	`, ftsQ, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}
	defer rows.Close()

	var out []SearchResultRow
	for rows.Next() {
		var r SearchResultRow
		var toJSON, ccJSON, flagsJSON string
		var date sql.NullTime
		if err := rows.Scan(&r.UID, &r.MessageID, &r.InReplyTo, &r.Subject, &r.FromName, &r.FromAddr,
			&toJSON, &ccJSON, &date, &flagsJSON, &r.Size, &r.FolderPath); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		r.Date = date.Time
		if err := json.Unmarshal([]byte(toJSON), &r.ToAddrs); err != nil {
			return nil, fmt.Errorf("decode to_addrs for uid=%d: %w", r.UID, err)
		}
		if err := json.Unmarshal([]byte(ccJSON), &r.CcAddrs); err != nil {
			return nil, fmt.Errorf("decode cc_addrs for uid=%d: %w", r.UID, err)
		}
		if err := json.Unmarshal([]byte(flagsJSON), &r.Flags); err != nil {
			return nil, fmt.Errorf("decode flags for uid=%d: %w", r.UID, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ftsQuery builds a safe FTS5 MATCH expression from free-form user input:
// each whitespace-separated term becomes a quoted prefix match, ANDed
// together. Quoting every term means FTS5 query-syntax characters in the
// input ("*:-()  etc.) are treated as literal text, not query operators —
// never pass raw user input straight to MATCH.
func ftsQuery(input string) string {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return ""
	}
	terms := make([]string, len(fields))
	for i, f := range fields {
		terms[i] = `"` + strings.ReplaceAll(f, `"`, `""`) + `"*`
	}
	return strings.Join(terms, " AND ")
}
