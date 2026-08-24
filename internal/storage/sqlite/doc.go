// Package sqlite provides the modernc.org/sqlite-backed storage layer:
// connection setup, an embedded go:embed migration runner, and CRUD for
// accounts, folders, messages, and signatures. Consumed only by
// internal/core/* and internal/sync.
//
// Implemented in Phase 2.
package sqlite
