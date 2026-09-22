// Package compose is a core domain/service layer package. It exposes
// Service, which builds new/reply/reply-all/forward drafts (quoting or a
// forwarded-message header block, signature insertion, attachments read
// from disk via NewAttachmentFromFile or carried over from the original on
// a forward) and sends them via internal/smtp, including any Bcc addresses
// as SMTP-only recipients that never appear in the built message's
// headers. Used by internal/cli, internal/tui, and (later) internal/httpapi
// so reply/quoting logic lives in exactly one place.
//
// Implemented in Phase 4.
package compose
