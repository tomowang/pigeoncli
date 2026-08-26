package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// SignatureRow is a saved signature. AccountSlug is "" for the global
// default signature (used by an account that has none of its own).
type SignatureRow struct {
	ID          int64
	AccountSlug string
	Name        string
	BodyPlain   string
	IsDefault   bool
}

func nullableSlug(slug string) any {
	if slug == "" {
		return nil
	}
	return slug
}

// AddSignature creates a signature scoped to accountSlug ("" for the
// global default signature). If isDefault is true, any existing default
// in the same scope is cleared first.
func (db *DB) AddSignature(ctx context.Context, accountSlug, name, body string, isDefault bool) (int64, error) {
	if isDefault {
		if err := db.clearDefaultSignature(ctx, accountSlug); err != nil {
			return 0, err
		}
	}
	res, err := db.ExecContext(ctx, `
		INSERT INTO signatures (account_slug, name, body_plain, is_default, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, nullableSlug(accountSlug), name, body, boolToInt(isDefault))
	if err != nil {
		return 0, fmt.Errorf("add signature: %w", err)
	}
	return res.LastInsertId()
}

// UpdateSignature replaces the name/body of an existing signature.
func (db *DB) UpdateSignature(ctx context.Context, id int64, name, body string) error {
	if _, err := db.ExecContext(ctx, `
		UPDATE signatures SET name = ?, body_plain = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, name, body, id); err != nil {
		return fmt.Errorf("update signature %d: %w", id, err)
	}
	return nil
}

// RemoveSignature deletes a signature by id.
func (db *DB) RemoveSignature(ctx context.Context, id int64) error {
	if _, err := db.ExecContext(ctx, "DELETE FROM signatures WHERE id = ?", id); err != nil {
		return fmt.Errorf("remove signature %d: %w", id, err)
	}
	return nil
}

// SetDefaultSignature makes signature id the default within its own scope
// (global, or the account it belongs to), clearing any previous default
// in that same scope.
func (db *DB) SetDefaultSignature(ctx context.Context, id int64) error {
	var accountSlug sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT account_slug FROM signatures WHERE id = ?", id).Scan(&accountSlug); err != nil {
		return fmt.Errorf("load signature %d: %w", id, err)
	}
	if err := db.clearDefaultSignature(ctx, accountSlug.String); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "UPDATE signatures SET is_default = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id); err != nil {
		return fmt.Errorf("set default signature %d: %w", id, err)
	}
	return nil
}

func (db *DB) clearDefaultSignature(ctx context.Context, accountSlug string) error {
	var err error
	if accountSlug == "" {
		_, err = db.ExecContext(ctx, "UPDATE signatures SET is_default = 0 WHERE account_slug IS NULL")
	} else {
		_, err = db.ExecContext(ctx, "UPDATE signatures SET is_default = 0 WHERE account_slug = ?", accountSlug)
	}
	if err != nil {
		return fmt.Errorf("clear default signature: %w", err)
	}
	return nil
}

// ListSignatures returns every signature scoped to accountSlug plus every
// global signature, account-specific first.
func (db *DB) ListSignatures(ctx context.Context, accountSlug string) ([]SignatureRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, account_slug, name, body_plain, is_default FROM signatures
		WHERE account_slug = ? OR account_slug IS NULL
		ORDER BY (account_slug IS NULL), id
	`, accountSlug)
	if err != nil {
		return nil, fmt.Errorf("list signatures: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanSignatureRows(rows)
}

// ListAllSignatures returns every signature, across every account and
// global, account-specific ones first.
func (db *DB) ListAllSignatures(ctx context.Context) ([]SignatureRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, account_slug, name, body_plain, is_default FROM signatures
		ORDER BY (account_slug IS NULL), account_slug, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list all signatures: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanSignatureRows(rows)
}

func scanSignatureRows(rows *sql.Rows) ([]SignatureRow, error) {
	var out []SignatureRow
	for rows.Next() {
		var r SignatureRow
		var slug sql.NullString
		var isDefault int
		if err := rows.Scan(&r.ID, &slug, &r.Name, &r.BodyPlain, &isDefault); err != nil {
			return nil, fmt.Errorf("scan signature: %w", err)
		}
		r.AccountSlug = slug.String
		r.IsDefault = isDefault != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// DefaultSignature resolves the signature that should be inserted into a
// new draft for accountSlug: that account's own default if it has one,
// otherwise the global default, otherwise none.
func (db *DB) DefaultSignature(ctx context.Context, accountSlug string) (SignatureRow, bool, error) {
	rows, err := db.ListSignatures(ctx, accountSlug)
	if err != nil {
		return SignatureRow{}, false, err
	}
	var global *SignatureRow
	for i := range rows {
		if !rows[i].IsDefault {
			continue
		}
		if rows[i].AccountSlug == accountSlug {
			return rows[i], true, nil
		}
		if rows[i].AccountSlug == "" && global == nil {
			global = &rows[i]
		}
	}
	if global != nil {
		return *global, true, nil
	}
	return SignatureRow{}, false, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
