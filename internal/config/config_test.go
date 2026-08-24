package config

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Accounts) != 0 {
		t.Fatalf("expected no accounts, got %d", len(cfg.Accounts))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	cfg := &Config{Accounts: []Account{{
		Slug: "work", Email: "me@example.com", Username: "me@example.com", AuthType: "password",
		IMAP: ServerConfig{Host: "imap.example.com", Port: 993, TLS: TLSModeTLS},
		SMTP: ServerConfig{Host: "smtp.example.com", Port: 587, TLS: TLSModeSTARTTLS},
	}}}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Slug != "work" {
		t.Fatalf("round-trip mismatch: %+v", got.Accounts)
	}
	if got.Accounts[0].IMAP.TLS != TLSModeTLS {
		t.Fatalf("TLS mode mismatch: %v", got.Accounts[0].IMAP.TLS)
	}
}

func TestUpsertAndRemoveAccount(t *testing.T) {
	cfg := &Config{}
	cfg.UpsertAccount(Account{Slug: "a", Email: "a@example.com"})
	cfg.UpsertAccount(Account{Slug: "a", Email: "a2@example.com"})
	if len(cfg.Accounts) != 1 || cfg.Accounts[0].Email != "a2@example.com" {
		t.Fatalf("expected upsert to replace, got %+v", cfg.Accounts)
	}

	if !cfg.RemoveAccount("a") {
		t.Fatalf("expected RemoveAccount to report removal")
	}
	if len(cfg.Accounts) != 0 {
		t.Fatalf("expected account removed, got %+v", cfg.Accounts)
	}
	if cfg.RemoveAccount("missing") {
		t.Fatalf("expected RemoveAccount to report no-op for missing slug")
	}
}
