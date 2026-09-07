package account

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"

	"github.com/tomowang/pigeoncli/internal/auth"
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

func TestAddGoogleReauthorizeAndRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	svc := NewService(path)
	ctx := context.Background()

	a := Account{
		Slug:     "gmail",
		Email:    "me@gmail.com",
		Username: "me@gmail.com",
	}
	a.IMAP, a.SMTP = GmailServerConfig()

	tok := &oauth2.Token{AccessToken: "access-1", RefreshToken: "refresh-1", Expiry: time.Now().Add(time.Hour)}
	if err := svc.AddGoogle(ctx, a, tok); err != nil {
		t.Fatalf("AddGoogle: %v", err)
	}

	got, err := svc.Get(ctx, "gmail")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AuthType != AuthTypeGoogle {
		t.Fatalf("AuthType = %q, want %q", got.AuthType, AuthTypeGoogle)
	}

	stored, err := auth.GetOAuthToken("gmail")
	if err != nil {
		t.Fatalf("GetOAuthToken: %v", err)
	}
	if stored.AccessToken != "access-1" {
		t.Fatalf("stored access token = %q, want access-1", stored.AccessToken)
	}

	newTok := &oauth2.Token{AccessToken: "access-2", RefreshToken: "refresh-2", Expiry: time.Now().Add(time.Hour)}
	if err := svc.ReauthorizeGoogle(ctx, "gmail", newTok); err != nil {
		t.Fatalf("ReauthorizeGoogle: %v", err)
	}
	stored, err = auth.GetOAuthToken("gmail")
	if err != nil {
		t.Fatalf("GetOAuthToken after reauthorize: %v", err)
	}
	if stored.AccessToken != "access-2" {
		t.Fatalf("stored access token = %q, want access-2", stored.AccessToken)
	}

	if err := svc.Remove(ctx, "gmail"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := auth.GetOAuthToken("gmail"); err == nil {
		t.Fatalf("expected oauth token to be deleted after Remove")
	}
}

func TestAddRejectsGoogleAuthType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	svc := NewService(path)
	ctx := context.Background()

	a := Account{
		Slug:     "gmail",
		Email:    "me@gmail.com",
		Username: "me@gmail.com",
		AuthType: AuthTypeGoogle,
	}
	a.IMAP, a.SMTP = GmailServerConfig()

	if err := svc.Add(ctx, a, "irrelevant"); err == nil {
		t.Fatalf("expected error adding a google account via Add")
	}
}

func TestReauthorizeGoogleRejectsPasswordAccount(t *testing.T) {
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

	if err := svc.ReauthorizeGoogle(ctx, "work", &oauth2.Token{AccessToken: "x"}); err == nil {
		t.Fatalf("expected error reauthorizing a password account")
	}
}
