// Package account is a core domain/service layer package. It exposes
// Service, the account management API used by internal/cli, internal/tui,
// and (later) internal/httpapi. It depends on internal/config, internal/auth,
// and internal/storage/sqlite, but callers of Service never see those types
// directly.
//
// Implemented starting in Phase 1 (config/keyring CRUD) and extended in
// Phase 2 (sqlite-backed account mirror).
package account
