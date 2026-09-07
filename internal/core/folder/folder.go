package folder

import (
	"context"
	"fmt"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
	"github.com/tomowang/pigeoncli/internal/sync"
)

// Folder is the folder DTO used by internal/cli, internal/tui, and (later)
// internal/httpapi.
type Folder struct {
	Path        string
	Name        string
	SpecialUse  string
	UnreadCount int
	TotalCount  int
}

// Progress reports the outcome of syncing one folder.
type Progress = sync.FolderProgress

// Service manages folder listing and syncing for accounts, backed by the
// local sqlite cache.
type Service struct {
	db *sqlite.DB
}

// NewService creates a Service backed by db.
func NewService(db *sqlite.DB) *Service {
	return &Service{db: db}
}

// List returns the cached folders for accountSlug, ordered by path. It
// reads only from the local cache — call Sync first to populate it.
func (s *Service) List(ctx context.Context, accountSlug string) ([]Folder, error) {
	accountID, ok, err := s.db.AccountID(ctx, accountSlug)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("account %q has not been synced yet", accountSlug)
	}

	rows, err := s.db.ListFolders(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]Folder, len(rows))
	for i, r := range rows {
		out[i] = Folder{Path: r.Path, Name: r.Name, SpecialUse: r.SpecialUse, UnreadCount: r.UnreadCount, TotalCount: r.TotalCount}
	}
	return out, nil
}

// Sync connects to the IMAP server described by cfg (using its keyring
// credentials) and syncs its folders and message headers into the local
// cache. windowCount caps how many of a folder's most recent messages get
// fully synced the first time that folder is synced (0 disables
// windowing, syncing full history); see internal/sync's doc comment for
// details. If onProgress is non-nil, it's called once per folder.
func (s *Service) Sync(ctx context.Context, cfg config.Account, windowCount int, onProgress func(Progress)) error {
	provider, err := auth.NewProvider(cfg.AuthType, cfg.Username, cfg.Slug)
	if err != nil {
		return err
	}
	a := sync.Account{
		Slug:        cfg.Slug,
		Email:       cfg.Email,
		DisplayName: cfg.DisplayName,
		IMAP:        cfg.IMAP,
		Provider:    provider,
		WindowCount: windowCount,
	}
	return sync.SyncAccount(ctx, s.db, a, onProgress)
}

// Watch opens a dedicated IMAP IDLE connection to cfg's server for
// folderPath and blocks until ctx is canceled, calling onUpdate (from a
// background goroutine — see sync.WatchFolder) whenever the server
// reports an unsolicited change to that folder. It never touches the
// local cache itself; callers should trigger a Sync in response to
// onUpdate. A nil error means ctx was canceled normally; any other error
// means live updates were lost (e.g. the server doesn't support IDLE, or
// the connection dropped) and callers should fall back to polling rather
// than retrying immediately.
func (s *Service) Watch(ctx context.Context, cfg config.Account, folderPath string, onUpdate func()) error {
	provider, err := auth.NewProvider(cfg.AuthType, cfg.Username, cfg.Slug)
	if err != nil {
		return err
	}
	return sync.WatchFolder(ctx, cfg.IMAP, provider, folderPath, onUpdate)
}
