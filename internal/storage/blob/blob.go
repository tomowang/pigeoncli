// Package blob stores raw message bodies on disk, outside sqlite, so the
// cache database stays small and fast to query. Files are addressed by the
// message's internal account/folder ids and UID — not a content hash —
// which is simpler and, unlike the folder path or account slug, always
// safe to use directly as a filesystem path.
package blob

import (
	"fmt"
	"os"
	"path/filepath"
)

// Store is an on-disk cache of raw message bodies.
type Store struct {
	dir string
}

// DefaultDir returns the default blob cache directory
// ($XDG_CACHE_HOME/pigeon/blobs, or the OS equivalent).
func DefaultDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}
	return filepath.Join(dir, "pigeon", "blobs"), nil
}

// NewStore creates a Store rooted at dir. The directory is created lazily
// on first Write.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Ref returns the reference (a relative path) under which a message's raw
// body is stored. This value is what's persisted in the messages table's
// raw_ref column.
func (s *Store) Ref(accountID, folderID int64, uid uint32) string {
	return filepath.Join(fmt.Sprintf("%d", accountID), fmt.Sprintf("%d", folderID), fmt.Sprintf("%d.eml", uid))
}

// Write saves data under ref, creating parent directories as needed.
func (s *Store) Write(ref string, data []byte) error {
	path := filepath.Join(s.dir, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create blob dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write blob %s: %w", ref, err)
	}
	return nil
}

// Read loads the data stored under ref.
func (s *Store) Read(ref string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, ref))
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", ref, err)
	}
	return data, nil
}
