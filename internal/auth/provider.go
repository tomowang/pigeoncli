package auth

import (
	"context"
	"fmt"

	"github.com/emersion/go-sasl"
)

// Provider supplies a SASL client for authenticating an account against its
// IMAP and SMTP servers. Both go-imap's Client.Authenticate and go-smtp's
// Client.Auth accept a sasl.Client, so a single interface covers both
// protocols. PasswordProvider was v1's only implementation; GoogleProvider
// (see google.go) is the second, added without changing internal/imap or
// internal/smtp.
type Provider interface {
	IMAPSASLClient(ctx context.Context) (sasl.Client, error)
	SMTPSASLClient(ctx context.Context) (sasl.Client, error)
}

// Account auth_type values, as stored in config.Account.AuthType.
const (
	AuthTypePassword = "password"
	AuthTypeGoogle   = "google"
)

// NewProvider builds the Provider for authType (defaulting to
// AuthTypePassword when empty, matching config.Account's zero value).
// Every internal/core/* service that dials IMAP/SMTP goes through this
// instead of constructing a PasswordProvider or GoogleProvider directly,
// so adding a new auth type only means adding a case here.
func NewProvider(authType, username, accountSlug string) (Provider, error) {
	switch authType {
	case "", AuthTypePassword:
		return PasswordProvider{Username: username, AccountSlug: accountSlug}, nil
	case AuthTypeGoogle:
		return GoogleProvider{Username: username, AccountSlug: accountSlug}, nil
	default:
		return nil, fmt.Errorf("unknown auth type %q", authType)
	}
}

// PasswordProvider authenticates using a username/password pair, with the
// password read from the OS keyring on demand.
type PasswordProvider struct {
	Username    string
	AccountSlug string
}

func (p PasswordProvider) IMAPSASLClient(ctx context.Context) (sasl.Client, error) {
	return p.saslClient()
}

func (p PasswordProvider) SMTPSASLClient(ctx context.Context) (sasl.Client, error) {
	return p.saslClient()
}

func (p PasswordProvider) saslClient() (sasl.Client, error) {
	password, err := GetPassword(p.AccountSlug)
	if err != nil {
		return nil, fmt.Errorf("credentials for %q: %w", p.AccountSlug, err)
	}
	return sasl.NewPlainClient("", p.Username, password), nil
}
