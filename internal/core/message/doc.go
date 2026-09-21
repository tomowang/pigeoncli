// Package message is a core domain/service layer package. It exposes
// Service, the message API used by internal/cli, internal/tui, and (later)
// internal/httpapi:
//
//   - reading: cached header listing, search, related messages, lazy body
//     fetch-and-cache, plaintext rendering, attachments;
//   - triage: Move, Archive, Delete, Purge and Undo, plus SetFlags (and the
//     MarkRead/MarkUnread/Star/Unstar shortcuts) for flag changes.
//
// Triage calls take []Ref, so a whole selection — even one spanning
// folders — is handled over a single IMAP connection. Each action is applied
// on the server first and then mirrored into the local cache, including the
// folder unread/total counts.
package message
