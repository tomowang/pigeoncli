package account

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

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

// AuthType values for Account.AuthType.
const (
	AuthTypePassword = auth.AuthTypePassword
	AuthTypeGoogle   = auth.AuthTypeGoogle
)

// GmailServerConfig returns Gmail's well-known IMAP/SMTP connection
// settings, so a caller adding a Google-authenticated account can default
// to these instead of asking the user to type imap.gmail.com/
// smtp.gmail.com by hand.
func GmailServerConfig() (imapCfg, smtpCfg ServerConfig) {
	return ServerConfig{Host: "imap.gmail.com", Port: 993, TLS: TLSModeTLS},
		ServerConfig{Host: "smtp.gmail.com", Port: 587, TLS: TLSModeSTARTTLS}
}

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

// Add creates a new password-authenticated account: its settings are
// written to the config file and its password is stored in the OS
// keyring. It fails if the slug is already in use. Use AddGoogle instead
// for a Gmail/OAuth2 account.
func (s *Service) Add(ctx context.Context, a Account, password string) error {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return err
	}
	if _, exists := cfg.Account(a.Slug); exists {
		return fmt.Errorf("account %q already exists", a.Slug)
	}
	if a.AuthType == "" {
		a.AuthType = AuthTypePassword
	}
	if a.AuthType != AuthTypePassword {
		return fmt.Errorf("account auth type %q: use AddGoogle instead of Add", a.AuthType)
	}
	if err := auth.SetPassword(a.Slug, password); err != nil {
		return err
	}
	cfg.UpsertAccount(a)
	return cfg.Save(s.configPath)
}

// AuthorizeGoogle runs Google's interactive OAuth2 loopback flow (see
// auth.LoginGoogle): it calls onURL with the consent-screen URL to show
// the user, best-effort opens it in a browser too, and blocks until the
// user finishes (or cancels) sign-in. The returned token isn't persisted
// yet — pass it to AddGoogle for a new account or ReauthorizeGoogle for
// an existing one.
func (s *Service) AuthorizeGoogle(ctx context.Context, onURL func(url string)) (*oauth2.Token, error) {
	return auth.LoginGoogle(ctx, onURL)
}

// AddGoogle creates a new Google-authenticated (Gmail) account: its
// settings are written to the config file and token is stored in the OS
// keyring. It fails if the slug is already in use. Obtain token via
// AuthorizeGoogle first.
func (s *Service) AddGoogle(ctx context.Context, a Account, token *oauth2.Token) error {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return err
	}
	if _, exists := cfg.Account(a.Slug); exists {
		return fmt.Errorf("account %q already exists", a.Slug)
	}
	a.AuthType = AuthTypeGoogle
	if err := auth.SetOAuthToken(a.Slug, token); err != nil {
		return err
	}
	cfg.UpsertAccount(a)
	return cfg.Save(s.configPath)
}

// ReauthorizeGoogle replaces the stored OAuth2 token for an existing
// Google-authenticated account (e.g. after its refresh token was revoked,
// or to grant a changed scope) without otherwise touching the account's
// config.
func (s *Service) ReauthorizeGoogle(ctx context.Context, slug string, token *oauth2.Token) error {
	a, err := s.Get(ctx, slug)
	if err != nil {
		return err
	}
	if a.AuthType != AuthTypeGoogle {
		return fmt.Errorf("account %q does not use google auth", slug)
	}
	return auth.SetOAuthToken(slug, token)
}

// Update replaces the settings of an existing account. If password is
// non-nil, the account's stored password is replaced too (this only
// applies to a password-authenticated account; use ReauthorizeGoogle for
// a Google one); otherwise existing credentials are left untouched.
func (s *Service) Update(ctx context.Context, a Account, password *string) error {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return err
	}
	if _, exists := cfg.Account(a.Slug); !exists {
		return fmt.Errorf("account %q not found", a.Slug)
	}
	if a.AuthType == "" {
		a.AuthType = AuthTypePassword
	}
	if password != nil {
		if a.AuthType != AuthTypePassword {
			return fmt.Errorf("account %q does not use password auth", a.Slug)
		}
		if err := auth.SetPassword(a.Slug, *password); err != nil {
			return err
		}
	}
	cfg.UpsertAccount(a)
	return cfg.Save(s.configPath)
}

// Remove deletes an account's config entry and any stored credentials
// (password or OAuth2 token, whichever it used).
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
	if err := auth.DeleteOAuthToken(slug); err != nil {
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

	provider, err := auth.NewProvider(a.AuthType, a.Username, a.Slug)
	if err != nil {
		return err
	}

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
