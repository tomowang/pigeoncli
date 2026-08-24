package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// UpsertAccount inserts or updates the local account mirror row for slug,
// returning its internal id. Connection settings (host/port/TLS) are not
// mirrored here — the TOML config file stays their single source of truth.
func (db *DB) UpsertAccount(ctx context.Context, slug, email, displayName string) (int64, error) {
	_, err := db.ExecContext(ctx, `
		INSERT INTO accounts (slug, email, display_name, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(slug) DO UPDATE SET
			email = excluded.email,
			display_name = excluded.display_name,
			updated_at = CURRENT_TIMESTAMP
	`, slug, email, displayName)
	if err != nil {
		return 0, fmt.Errorf("upsert account %q: %w", slug, err)
	}

	var id int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM accounts WHERE slug = ?", slug).Scan(&id); err != nil {
		return 0, fmt.Errorf("load account id for %q: %w", slug, err)
	}
	return id, nil
}

// AccountID looks up the local row id for the account with the given slug.
// It reports false if the account has never been synced (and so has no
// local mirror row yet).
func (db *DB) AccountID(ctx context.Context, slug string) (int64, bool, error) {
	var id int64
	err := db.QueryRowContext(ctx, "SELECT id FROM accounts WHERE slug = ?", slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("load account id for %q: %w", slug, err)
	}
	return id, true, nil
}

// SetAccountSyncedNow updates the account's last_synced_at to now.
func (db *DB) SetAccountSyncedNow(ctx context.Context, accountID int64) error {
	if _, err := db.ExecContext(ctx, "UPDATE accounts SET last_synced_at = CURRENT_TIMESTAMP WHERE id = ?", accountID); err != nil {
		return fmt.Errorf("update account sync time: %w", err)
	}
	return nil
}
