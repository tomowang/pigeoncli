// Package compose is a core domain/service layer package. It exposes
// Service, which builds new/reply/reply-all/forward drafts (quoting or a
// forwarded-message header block, signature insertion, attachments read
// from disk via NewAttachmentFromFile or carried over from the original on
// a forward) and sends them via internal/smtp, including any Bcc addresses
// as SMTP-only recipients that never appear in the built message's
// headers.
//
// SaveDraft/LoadDraft/DiscardDraft (drafts.go) persist an in-progress
// Draft to the account's IMAP Drafts folder over internal/imap instead —
// APPEND a new \Draft-flagged copy (deleting the previous one, if any, so
// repeated saves of the same draft don't pile up duplicates), reconstruct
// a Draft from a saved copy (including Bcc, via mime.DraftBcc, since Bcc
// never round-trips through the normal message-sync path), and remove a
// saved copy once it's been sent or explicitly discarded.
//
// Used by internal/cli, internal/tui, and (later) internal/httpapi so
// reply/quoting/draft logic lives in exactly one place.
//
// Implemented in Phase 4.
package compose
