package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// TLSMode selects how a connection to a mail server is secured.
type TLSMode string

const (
	TLSModeTLS      TLSMode = "tls"      // implicit TLS (IMAPS/SMTPS)
	TLSModeSTARTTLS TLSMode = "starttls" // plaintext connect, then STARTTLS
	TLSModeNone     TLSMode = "none"     // no encryption
)

// Valid reports whether m is one of the known TLS modes.
func (m TLSMode) Valid() bool {
	switch m {
	case TLSModeTLS, TLSModeSTARTTLS, TLSModeNone:
		return true
	}
	return false
}

// ServerConfig holds connection settings for an IMAP or SMTP server.
type ServerConfig struct {
	Host string  `toml:"host"`
	Port int     `toml:"port"`
	TLS  TLSMode `toml:"tls"`
}

// Account holds non-secret settings for one mail account. Secrets
// (passwords, tokens) are never stored here — see internal/auth.
type Account struct {
	Slug        string       `toml:"slug"`
	Email       string       `toml:"email"`
	DisplayName string       `toml:"display_name,omitempty"`
	Username    string       `toml:"username"`
	AuthType    string       `toml:"auth_type"`
	IMAP        ServerConfig `toml:"imap"`
	SMTP        ServerConfig `toml:"smtp"`
}

// Config is the root of pigeon's TOML config file.
type Config struct {
	Accounts []Account `toml:"accounts"`
}

// DefaultPath returns the default config file path
// ($XDG_CONFIG_HOME/pigeon/config.toml, or the OS equivalent).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(dir, "pigeon", "config.toml"), nil
}

// Load reads the config file at path. A missing file is not an error: it
// returns an empty Config so first-run flows (e.g. `account add`) can
// create the file on Save.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

// Save writes the config file at path, creating parent directories as
// needed. The file is written with 0600 permissions, since it sits
// alongside (though never contains) sensitive account details.
func (c *Config) Save(path string) error {
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// Account returns the account with the given slug, if any.
func (c *Config) Account(slug string) (Account, bool) {
	for _, a := range c.Accounts {
		if a.Slug == slug {
			return a, true
		}
	}
	return Account{}, false
}

// UpsertAccount adds a new account, or replaces the existing one with the
// same slug.
func (c *Config) UpsertAccount(a Account) {
	for i, existing := range c.Accounts {
		if existing.Slug == a.Slug {
			c.Accounts[i] = a
			return
		}
	}
	c.Accounts = append(c.Accounts, a)
}

// RemoveAccount deletes the account with the given slug. It reports
// whether an account was removed.
func (c *Config) RemoveAccount(slug string) bool {
	for i, a := range c.Accounts {
		if a.Slug == slug {
			c.Accounts = append(c.Accounts[:i], c.Accounts[i+1:]...)
			return true
		}
	}
	return false
}
