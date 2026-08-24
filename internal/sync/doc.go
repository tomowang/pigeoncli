// Package sync reconciles a folder's local sqlite cache with its IMAP
// state: it clears the cache on UIDVALIDITY change, re-fetches all message
// headers in the folder, upserts them, and deletes any cached message
// whose UID is no longer present server-side. Progress is reported via a
// callback for now; if a future phase needs streaming into the TUI's
// self-re-arming tea.Cmd pattern, that can wrap the same callback.
//
// Implemented in Phase 2.
package sync
