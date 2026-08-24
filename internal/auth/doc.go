// Package auth stores and retrieves account credentials via the OS keyring
// and defines the Provider interface (IMAPCredentials, SMTPAuth) that
// internal/imap and internal/smtp depend on. v1 ships only a
// PasswordProvider; an OAuth2Provider can be added later behind the same
// interface without changing callers.
//
// Implemented in Phase 1.
package auth
