package settings

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomowang/pigeoncli/internal/config"
)

func TestGetDefaultsWhenConfigFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	got, err := NewService(path).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	want := Settings{SyncInterval: 5 * time.Minute, InitialSyncWindow: 1000, Theme: ""}
	if got != want {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
}

func TestGetReadsConfiguredValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &config.Config{
		Sync: config.SyncConfig{IntervalMinutes: 15, InitialWindowCount: 200},
		UI:   config.UIConfig{Theme: "dark"},
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := NewService(path).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	want := Settings{SyncInterval: 15 * time.Minute, InitialSyncWindow: 200, Theme: "dark"}
	if got != want {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
}

func TestGetInitialWindowDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &config.Config{Sync: config.SyncConfig{InitialWindowCount: -1}}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := NewService(path).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.InitialSyncWindow != 0 {
		t.Fatalf("InitialSyncWindow = %d, want 0 (disabled)", got.InitialSyncWindow)
	}
}
