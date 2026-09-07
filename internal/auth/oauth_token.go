package auth

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// oauthTokenSuffix distinguishes an OAuth2 token's keyring entry from a
// same-slug password entry (see SetPassword), so an account can't
// accidentally collide between auth types across an auth-type change.
const oauthTokenSuffix = ".oauth2"

// SetOAuthToken stores tok (as JSON, including its refresh token) in the OS
// keyring under the given account slug.
func SetOAuthToken(accountSlug string, tok *oauth2.Token) error {
	data, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("encode oauth2 token for %q: %w", accountSlug, err)
	}
	if err := keyring.Set(serviceName, accountSlug+oauthTokenSuffix, string(data)); err != nil {
		return fmt.Errorf("store oauth2 token for %q: %w", accountSlug, err)
	}
	return nil
}

// GetOAuthToken retrieves the OAuth2 token stored for the given account
// slug.
func GetOAuthToken(accountSlug string) (*oauth2.Token, error) {
	data, err := keyring.Get(serviceName, accountSlug+oauthTokenSuffix)
	if err != nil {
		return nil, fmt.Errorf("load oauth2 token for %q: %w", accountSlug, err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(data), &tok); err != nil {
		return nil, fmt.Errorf("decode oauth2 token for %q: %w", accountSlug, err)
	}
	return &tok, nil
}

// DeleteOAuthToken removes the OAuth2 token stored for the given account
// slug. It is not an error if no token was stored.
func DeleteOAuthToken(accountSlug string) error {
	if err := keyring.Delete(serviceName, accountSlug+oauthTokenSuffix); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete oauth2 token for %q: %w", accountSlug, err)
	}
	return nil
}
