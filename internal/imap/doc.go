// Package imap is a thin wrapper over emersion/go-imap/v2 (imapclient):
// dialing, login via internal/auth.Provider, folder listing, message
// fetch, and IDLE. Consumed only by internal/sync and internal/core/*,
// never directly by internal/tui or internal/cli.
//
// Implemented starting in Phase 1 (dial/login for `account test`),
// extended in Phase 2 (folder listing, header fetch for sync), and Phase 3
// (raw body fetch for message read).
package imap
