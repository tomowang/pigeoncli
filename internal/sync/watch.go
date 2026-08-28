package sync

import (
	"context"
	"fmt"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/imap"
)

// WatchFolder opens a dedicated IMAP connection to imapCfg, selects
// folderPath, and blocks in IMAP IDLE (RFC 2177) until ctx is canceled,
// calling onUpdate (from a background goroutine — callers must not touch
// non-thread-safe state directly from it) every time the server reports
// an unsolicited change to the mailbox (new mail, flag change, expunge).
//
// This is a live-update hint, not a sync: WatchFolder never touches the
// local cache itself. Callers should trigger a real Sync in response to
// onUpdate — the existing UID-diff/CONDSTORE sync path in this package
// stays the source of truth for what actually changed.
//
// A nil error return means ctx was canceled normally. Any other error
// (including the server not supporting IDLE) means the caller lost live
// updates and may want to fall back to periodic polling instead of
// retrying immediately.
func WatchFolder(ctx context.Context, imapCfg config.ServerConfig, provider auth.Provider, folderPath string, onUpdate func()) error {
	cl, err := imap.DialClientWithUpdates(ctx, imap.DialOptions{Host: imapCfg.Host, Port: imapCfg.Port, TLS: imapCfg.TLS}, provider, onUpdate)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = cl.Close() }()
	defer cl.WatchContext(ctx)()

	if !cl.SupportsIdle() {
		return fmt.Errorf("server does not support IMAP IDLE")
	}

	if _, _, _, _, err := cl.SelectFolder(ctx, folderPath); err != nil {
		return fmt.Errorf("select %q: %w", folderPath, err)
	}

	if err := cl.Idle(); err != nil {
		// WatchContext force-closes the connection when ctx is canceled,
		// which surfaces here as a connection error rather than a clean
		// return — that's the expected, silent shutdown path, not a
		// real failure.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("idle: %w", err)
	}
	return nil
}
