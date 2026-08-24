// Package compose is a core domain/service layer package. It exposes
// Service, which builds new/reply/reply-all drafts (quoting, signature
// insertion) and sends them via internal/smtp. Used by internal/cli,
// internal/tui, and (later) internal/httpapi so reply/quoting logic lives
// in exactly one place.
//
// Implemented in Phase 4.
package compose
