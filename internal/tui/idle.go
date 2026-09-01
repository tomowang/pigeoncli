package tui

import (
	"context"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/core/account"
)

// idleUpdateMsg reports that the currently-watched folder changed on the
// server (new mail, flag change, expunge) — a hint to sync soon rather
// than wait for the next background-sync timer tick.
type idleUpdateMsg struct{}

// idleStoppedMsg reports that the IDLE watcher for accountSlug/folderPath
// ended, so Update can tell whether this was an intentional stop (the
// user switched away from that account/folder — a newer watcher already
// superseded it) or an actual failure worth surfacing.
type idleStoppedMsg struct {
	accountSlug string
	folderPath  string
	err         error
}

// startIdleCmd opens a dedicated IMAP IDLE connection to cfg for
// folderPath and blocks until ctx is canceled or the connection errors,
// pushing to ch (non-blocking; a pending signal is enough, no need to
// queue more) on every unsolicited server update. Paired with
// listenIdleCmd, this is the same "channel plus a self-re-arming tea.Cmd"
// pattern used for sync progress (see sync.go), so Update never blocks on
// a channel read directly.
func (m App) startIdleCmd(ctx context.Context, cfg account.Account, folderPath string, ch chan<- struct{}) tea.Cmd {
	svc := m.folderSvc
	return func() tea.Msg {
		err := svc.Watch(ctx, cfg, folderPath, func() {
			select {
			case ch <- struct{}{}:
			default:
			}
		})
		slog.Debug("idle watcher goroutine returned", "account", cfg.Slug, "folder", folderPath, "err", err)
		return idleStoppedMsg{accountSlug: cfg.Slug, folderPath: folderPath, err: err}
	}
}

// listenIdleCmd reads one signal off ch, re-issuing itself so the drain
// continues until ch is closed or abandoned (watcher stopped/superseded).
func listenIdleCmd(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-ch
		if !ok {
			return nil
		}
		return idleUpdateMsg{}
	}
}

// restartIdleCmd stops any IDLE watcher currently in effect and starts a
// new one for cfg/folderPath, unless one is already running for that
// exact pair. Passing an empty cfg.Slug or folderPath just stops
// watching (e.g. no folder selected yet).
func (m App) restartIdleCmd(cfg account.Account, folderPath string) (App, tea.Cmd) {
	if m.idleAccount == cfg.Slug && m.idleFolder == folderPath && m.idleCancel != nil {
		return m, nil
	}
	if m.idleCancel != nil {
		m.idleCancel()
	}
	m.idleAccount = cfg.Slug
	m.idleFolder = folderPath
	if cfg.Slug == "" || folderPath == "" {
		m.idleCancel = nil
		m.idleCh = nil
		return m, nil
	}

	ctx, cancel := context.WithCancel(m.ctx)
	m.idleCancel = cancel
	m.idleCh = make(chan struct{}, 1)
	return m, tea.Batch(m.startIdleCmd(ctx, cfg, folderPath, m.idleCh), listenIdleCmd(m.idleCh))
}
