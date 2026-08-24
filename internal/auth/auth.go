package auth

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const serviceName = "pigeon"

// SetPassword stores password in the OS keyring under the given account
// slug.
func SetPassword(accountSlug, password string) error {
	if err := keyring.Set(serviceName, accountSlug, password); err != nil {
		return fmt.Errorf("store password for %q: %w", accountSlug, err)
	}
	return nil
}

// GetPassword retrieves the password stored for the given account slug.
func GetPassword(accountSlug string) (string, error) {
	password, err := keyring.Get(serviceName, accountSlug)
	if err != nil {
		return "", fmt.Errorf("load password for %q: %w", accountSlug, err)
	}
	return password, nil
}

// DeletePassword removes the password stored for the given account slug.
// It is not an error if no password was stored.
func DeletePassword(accountSlug string) error {
	if err := keyring.Delete(serviceName, accountSlug); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete password for %q: %w", accountSlug, err)
	}
	return nil
}
