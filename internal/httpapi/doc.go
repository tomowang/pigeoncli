// Package httpapi will expose pigeon's core services (internal/core/*)
// over HTTP, as a third frontend alongside internal/tui and internal/cli.
// It must depend only on internal/core — never on internal/imap,
// internal/smtp, or internal/storage directly.
//
// Not yet implemented; reserved by the Phase 0 scaffold.
package httpapi
