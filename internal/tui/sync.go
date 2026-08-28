package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/settings"
)

// defaultSyncInterval is used until settingsLoadedMsg arrives with the
// configured value.
const defaultSyncInterval = 5 * time.Minute

// defaultInitialSyncWindow mirrors config.SyncConfig's default and is used
// until settingsLoadedMsg arrives with the configured value.
const defaultInitialSyncWindow = 1000

// settingsLoadedMsg reports the user's config-file preferences, loaded once
// at startup.
type settingsLoadedMsg struct {
	settings settings.Settings
	err      error
}

// syncTickMsg fires the periodic background-sync timer.
type syncTickMsg struct{}

// syncProgressMsg reports one folder's sync outcome, streamed from
// startSyncCmd via App.syncCh.
type syncProgressMsg struct {
	progress folder.Progress
}

// syncDoneMsg reports that a sync (manual or background) has finished
// syncing every folder.
type syncDoneMsg struct {
	err error
}

func (m App) loadSettingsCmd() tea.Cmd {
	return func() tea.Msg {
		s, err := m.settingsSvc.Get(m.ctx)
		return settingsLoadedMsg{settings: s, err: err}
	}
}

// startSyncCmd runs a full account sync in the background, pushing one
// folder.Progress per folder into ch and closing it when done. Paired with
// listenSyncProgressCmd, this is the "channel plus a self-re-arming
// tea.Cmd" pattern AGENTS.md documents for long-running streams, so Update
// never blocks on a channel read directly.
func (m App) startSyncCmd(cfg config.Account, ch chan<- folder.Progress) tea.Cmd {
	ctx, svc, windowCount := m.ctx, m.folderSvc, m.initialSyncWindow
	return func() tea.Msg {
		err := svc.Sync(ctx, cfg, windowCount, func(p folder.Progress) { ch <- p })
		close(ch)
		return syncDoneMsg{err: err}
	}
}

// listenSyncProgressCmd reads one event off ch. Update re-issues it after
// each syncProgressMsg so the drain continues until ch is closed, at which
// point the read returns !ok and this stops re-arming itself.
func listenSyncProgressCmd(ch <-chan folder.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return syncProgressMsg{progress: p}
	}
}

// tickCmd re-arms the periodic background-sync timer.
func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return syncTickMsg{} })
}
