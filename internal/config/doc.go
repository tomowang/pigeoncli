// Package config loads and saves pigeon's non-secret configuration
// (TOML), including account connection settings (host, port, TLS mode,
// username, folder settings). Secrets (passwords, tokens) never live here —
// see internal/auth for credential storage.
//
// Implemented in Phase 1.
package config
