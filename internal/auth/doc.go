// Package auth stores and retrieves account credentials via the OS keyring
// and defines the Provider interface (IMAPCredentials, SMTPAuth) that
// internal/imap and internal/smtp depend on. PasswordProvider authenticates
// with a username/password pair; GoogleProvider authenticates a Gmail
// account with an OAuth2 token (see google.go for the interactive login
// flow), both behind the same interface so callers never change. Use
// NewProvider to build the right one from an account's AuthType rather
// than constructing either directly.
//
// Implemented in Phase 1; GoogleProvider added alongside Gmail support.
package auth
