package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps a connection to pigeon's local sqlite cache.
type DB struct {
	*sql.DB
}

// DefaultPath returns the default sqlite cache path
// ($XDG_CACHE_HOME/pigeon/pigeon.db, or the OS equivalent).
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}
	return filepath.Join(dir, "pigeon", "pigeon.db"), nil
}

// Open opens (creating if needed) the sqlite database at path and applies
// any pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// modernc.org/sqlite serializes access per *sql.DB internally; capping
	// at one open connection avoids SQLITE_BUSY from concurrent writers
	// without needing WAL-mode/busy-timeout tuning for a single-user cache.
	sqlDB.SetMaxOpenConns(1)

	if _, err := sqlDB.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	db := &DB{DB: sqlDB}
	if err := db.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) migrate(ctx context.Context) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}

		var applied bool
		if err := db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d (%s): %w", version, name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("invalid migration filename %q: %w", name, err)
	}
	return version, nil
}
