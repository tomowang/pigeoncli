// Package sync reconciles a folder's local sqlite cache with its IMAP
// state: it clears the cache on UIDVALIDITY change, then does a cheap
// UID+flags fetch for the whole folder and diffs it against the local
// cache — new UIDs get a full envelope fetch, already-cached UIDs (whose
// headers are immutable) only get their flags refreshed if changed, and
// any cached message whose UID is no longer present server-side is
// deleted. Progress is reported via a callback for now; if a future phase
// needs streaming into the TUI's self-re-arming tea.Cmd pattern, that can
// wrap the same callback.
//
// Implemented in Phase 2.
package sync
