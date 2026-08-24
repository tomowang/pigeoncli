package auth

import (
	"context"
	"fmt"

	"github.com/emersion/go-sasl"
)

// Provider supplies a SASL client for authenticating an account against its
// IMAP and SMTP servers. Both go-imap's Client.Authenticate and go-smtp's
// Client.Auth accept a sasl.Client, so a single interface covers both
// protocols. v1 ships only PasswordProvider; adding OAuth2 later means
// adding another implementation of this interface (e.g. one producing an
// XOAUTH2/OAUTHBEARER sasl.Client) — internal/imap and internal/smtp never
// change.
type Provider interface {
	IMAPSASLClient(ctx context.Context) (sasl.Client, error)
	SMTPSASLClient(ctx context.Context) (sasl.Client, error)
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
