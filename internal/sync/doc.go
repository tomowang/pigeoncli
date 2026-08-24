// Package sync reconciles a folder's local sqlite cache with its IMAP
// state: full re-sync on UIDVALIDITY change, incremental UID fetch plus a
// flag-refresh pass otherwise, and deletion reconciliation via UID SEARCH.
// It reports progress over a channel consumed by internal/tui's
// self-re-arming tea.Cmd pattern.
//
// Implemented in Phase 2.
package sync
