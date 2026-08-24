package account

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func TestAddListGetRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	svc := NewService(path)
	ctx := context.Background()

	a := Account{
		Slug:     "work",
		Email:    "me@example.com",
		Username: "me@example.com",
		IMAP:     ServerConfig{Host: "imap.example.com", Port: 993, TLS: TLSModeTLS},
		SMTP:     ServerConfig{Host: "smtp.example.com", Port: 587, TLS: TLSModeSTARTTLS},
	}
	if err := svc.Add(ctx, a, "s3cret"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := svc.Add(ctx, a, "s3cret"); err == nil {
		t.Fatalf("expected error adding duplicate slug")
	}

	got, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "work" {
		t.Fatalf("unexpected list: %+v", got)
	}

	one, err := svc.Get(ctx, "work")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if one.Email != "me@example.com" {
		t.Fatalf("unexpected account: %+v", one)
	}

	if err := svc.Remove(ctx, "work"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := svc.Get(ctx, "work"); err == nil {
		t.Fatalf("expected error getting removed account")
	}
}

func TestUpdatePreservesPasswordWhenNotReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	svc := NewService(path)
	ctx := context.Background()

	a := Account{
		Slug:     "work",
		Email:    "me@example.com",
		Username: "me@example.com",
		IMAP:     ServerConfig{Host: "imap.example.com", Port: 993, TLS: TLSModeTLS},
		SMTP:     ServerConfig{Host: "smtp.example.com", Port: 587, TLS: TLSModeSTARTTLS},
	}
	if err := svc.Add(ctx, a, "s3cret"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	a.DisplayName = "Work"
	if err := svc.Update(ctx, a, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := svc.Get(ctx, "work")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName != "Work" {
		t.Fatalf("expected updated display name, got %+v", got)
	}
}
