// Package settings exposes user-facing preferences (background sync
// interval, theme) read from the config file, as a core service so
// internal/tui can consume them without importing internal/config directly.
package settings

import (
	"context"
	"time"

	"github.com/tomowang/pigeoncli/internal/config"
)

// Settings holds the user preferences internal/tui reads at startup.
type Settings struct {
	SyncInterval time.Duration
	Theme        string
}

// Service reads Settings from the config file. A Service is cheap to
// create; it re-reads the config file on every call, matching the pattern
// used by internal/core/account.Service.
type Service struct {
	configPath string
}

// NewService creates a Service backed by the config file at configPath.
func NewService(configPath string) *Service {
	return &Service{configPath: configPath}
}

// Get returns the current settings, applying defaults for anything unset.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return Settings{}, err
	}
	return Settings{SyncInterval: cfg.Sync.Interval(), Theme: cfg.UI.Theme}, nil
}
