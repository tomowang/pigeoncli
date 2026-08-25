package account

import (
	"context"
	"fmt"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/imap"
	"github.com/tomowang/pigeoncli/internal/smtp"
)

// Account, ServerConfig, and TLSMode are plain data (no protocol-library
// types), so they double as the DTOs internal/cli and internal/tui consume
// directly from this package.
type (
	Account      = config.Account
	ServerConfig = config.ServerConfig
	TLSMode      = config.TLSMode
)

const (
	TLSModeTLS      = config.TLSModeTLS
	TLSModeSTARTTLS = config.TLSModeSTARTTLS
	TLSModeNone     = config.TLSModeNone
)

// Service manages mail accounts: their config-file settings and their
// keyring-stored credentials. A Service is cheap to create; it re-reads the
// config file on every call rather than caching it, since pigeon's CLI
// commands are short-lived processes.
type Service struct {
	configPath string
}

// NewService creates a Service backed by the config file at configPath.
func NewService(configPath string) *Service {
	return &Service{configPath: configPath}
}

// List returns all configured accounts.
func (s *Service) List(ctx context.Context) ([]Account, error) {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return nil, err
	}
	return cfg.Accounts, nil
}

// Get returns the account with the given slug.
func (s *Service) Get(ctx context.Context, slug string) (Account, error) {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return Account{}, err
	}
	a, ok := cfg.Account(slug)
	if !ok {
		return Account{}, fmt.Errorf("account %q not found", slug)
	}
	return a, nil
}

// Validate checks that a has the fields required to save and connect to an
// account: a slug, an email, IMAP/SMTP hosts, and valid TLS modes. It's
// shared by internal/cli and internal/tui so both surfaces reject the same
// incomplete input before it reaches Add/Update.
func Validate(a Account) error {
	if a.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if a.Email == "" {
		return fmt.Errorf("email is required")
	}
	if a.IMAP.Host == "" {
		return fmt.Errorf("IMAP host is required")
	}
	if a.SMTP.Host == "" {
		return fmt.Errorf("SMTP host is required")
	}
	if !a.IMAP.TLS.Valid() {
		return fmt.Errorf("invalid IMAP TLS mode %q (want tls, starttls, or none)", a.IMAP.TLS)
	}
	if !a.SMTP.TLS.Valid() {
		return fmt.Errorf("invalid SMTP TLS mode %q (want tls, starttls, or none)", a.SMTP.TLS)
	}
	return nil
}

// Add creates a new account: its settings are written to the config file
// and its password is stored in the OS keyring. It fails if the slug is
// already in use.
func (s *Service) Add(ctx context.Context, a Account, password string) error {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return err
	}
	if _, exists := cfg.Account(a.Slug); exists {
		return fmt.Errorf("account %q already exists", a.Slug)
	}
	if a.AuthType == "" {
		a.AuthType = "password"
	}
	if err := auth.SetPassword(a.Slug, password); err != nil {
		return err
	}
	cfg.UpsertAccount(a)
	return cfg.Save(s.configPath)
}

// Update replaces the settings of an existing account. If password is
// non-nil, the account's stored password is replaced too; otherwise the
// existing password is left untouched.
func (s *Service) Update(ctx context.Context, a Account, password *string) error {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return err
	}
	if _, exists := cfg.Account(a.Slug); !exists {
		return fmt.Errorf("account %q not found", a.Slug)
	}
	if a.AuthType == "" {
		a.AuthType = "password"
	}
	if password != nil {
		if err := auth.SetPassword(a.Slug, *password); err != nil {
			return err
		}
	}
	cfg.UpsertAccount(a)
	return cfg.Save(s.configPath)
}

// Remove deletes an account's config entry and its stored password.
func (s *Service) Remove(ctx context.Context, slug string) error {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return err
	}
	if !cfg.RemoveAccount(slug) {
		return fmt.Errorf("account %q not found", slug)
	}
	if err := auth.DeletePassword(slug); err != nil {
		return err
	}
	return cfg.Save(s.configPath)
}

// TestConnection dials and authenticates against both the IMAP and SMTP
// servers configured for the account, then disconnects. It reports the
// first failure it finds.
func (s *Service) TestConnection(ctx context.Context, slug string) error {
	a, err := s.Get(ctx, slug)
	if err != nil {
		return err
	}

	provider := auth.PasswordProvider{Username: a.Username, AccountSlug: a.Slug}

	imapOpts := imap.DialOptions{Host: a.IMAP.Host, Port: a.IMAP.Port, TLS: a.IMAP.TLS}
	if err := imap.TestLogin(ctx, imapOpts, provider); err != nil {
		return fmt.Errorf("imap: %w", err)
	}

	smtpOpts := smtp.DialOptions{Host: a.SMTP.Host, Port: a.SMTP.Port, TLS: a.SMTP.TLS}
	if err := smtp.TestLogin(ctx, smtpOpts, provider); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}

	return nil
}
