package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tomowang/pigeoncli/internal/core/logs"
)

// severity classifies a status message for color-coding in the status bar
// and the L log overlay.
type severity int

const (
	sevInfo severity = iota
	sevSuccess
	sevError
	sevWarn // only used for WARN records in the log overlay; nothing sets it as the live status yet
)

// statusEntry is one status-bar message, kept in App.statusHistory so it's
// still visible (via the log overlay) after being overwritten by the next
// event.
type statusEntry struct {
	text string
	sev  severity
	at   time.Time
}

// statusHistoryCap bounds App.statusHistory so a long session doesn't grow
// it unboundedly.
const statusHistoryCap = 100

// statusClearMsg auto-clears an info/success status after a delay. gen is
// compared against App.statusGen so a stale tick (scheduled by an earlier
// setStatus call) can't wipe out a status set after it.
type statusClearMsg struct{ gen int }

// setStatus records text as the current status and appends it to history.
// info/success messages auto-clear after a few seconds (via statusClearMsg);
// error messages persist until the next setStatus call so they aren't
// missed.
func (m App) setStatus(text string, sev severity) (App, tea.Cmd) {
	m.statusGen++
	m.status = statusEntry{text: text, sev: sev, at: time.Now()}
	m.statusHistory = append(m.statusHistory, m.status)
	if over := len(m.statusHistory) - statusHistoryCap; over > 0 {
		m.statusHistory = m.statusHistory[over:]
	}
	if m.showLog {
		m.refreshLog()
	}

	if sev == sevError {
		return m, nil
	}
	gen := m.statusGen
	return m, tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return statusClearMsg{gen: gen}
	})
}

// startBusy marks a one-off network op as in flight and sets text as the
// status, like setStatus, but — unlike a plain info status — the spinner it
// starts keeps the status bar showing text until stopBusy is called, even
// past setStatus's 4-second auto-clear. Use this instead of setStatus for
// ops (opening a message, jumping to related messages) whose duration
// depends on network/server latency and so can plausibly outlast 4 seconds;
// otherwise the status bar goes blank while the op is still running and the
// UI looks stuck. Pair with tea.Batch(cmd, <the op's tea.Cmd>).
func (m App) startBusy(text string) (App, tea.Cmd) {
	spinnerAlreadyTicking := m.syncCh != nil || m.busy
	m.busy = true
	m, cmd := m.setStatus(text, sevInfo)
	if spinnerAlreadyTicking {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.syncSpinner.Tick)
}

// stopBusy ends the in-flight marker set by startBusy. Call it from the
// message handler that receives the op's result, on every branch (success,
// empty, and error alike).
func (m App) stopBusy() App {
	m.busy = false
	return m
}

// clearStatus resets the status bar to its zero value (footer() then renders
// it as a blank row above the shortcuts line) without touching history —
// this isn't a message worth logging, just "nothing pending anymore".
func (m App) clearStatus() App {
	m.status = statusEntry{}
	return m
}

// logChromeLines is the number of rows viewLog spends on everything but the
// scrollable viewport: the Padding(1, 2) top/bottom rows, plus the title and
// its blank line above, plus the blank line and hint below. layoutLog uses
// this to size the viewport so the two stay in sync.
const logChromeLines = 6

// openLog shows the log overlay, scrolled to the top (newest entry).
func (m *App) openLog() {
	m.showLog = true
	m.layoutLog()
	m.refreshLog()
	m.logViewport.GotoTop()
}

// layoutLog sizes the log viewport to the window and re-wraps its content.
func (m *App) layoutLog() {
	m.logViewport.Width = max(m.width-4, 1) // Padding(1, 2) on both sides
	m.logViewport.Height = max(m.height-logChromeLines, 1)
	m.refreshLog()
}

// logTimeFormat is the timestamp layout for every row of the log overlay, so
// this session's entries line up with the older ones from the log file.
const logTimeFormat = "2006-01-02 15:04:05"

// logPageSize is how many older log-file records each lazy load fetches.
const logPageSize = 200

// logPageMsg carries one page of older log-file records back to Update.
type logPageMsg struct {
	entries []logs.Entry
	next    logs.Cursor
	err     error
}

// loadOlderLogCmd starts fetching the next page of log-file records if the
// log overlay is scrolled to its end and there is more history to load. It
// is called on open and after every scroll, so older records are only read
// from disk once the user actually reaches them.
func (m App) loadOlderLogCmd() (App, tea.Cmd) {
	if m.logsSvc == nil || m.logLoading || m.logExhausted || !m.logViewport.AtBottom() {
		return m, nil
	}
	m.logLoading = true
	svc, cur := m.logsSvc, m.logCursor
	return m, func() tea.Msg {
		entries, next, err := svc.Before(cur, logPageSize)
		return logPageMsg{entries: entries, next: next, err: err}
	}
}

// refreshLog rebuilds the log viewport's content, wrapped to the viewport
// width: this session's status history newest first, then the older records
// loaded so far from the log file. The scroll offset is kept (SetContent
// clamps it), so new entries arriving while the overlay is open don't snap
// the view back to the top.
func (m *App) refreshLog() {
	if len(m.statusHistory) == 0 && len(m.logOlder) == 0 {
		if m.logExhausted {
			m.logViewport.SetContent("No messages yet.")
		} else {
			m.logViewport.SetContent("Loading…")
		}
		return
	}

	lines := make([]string, 0, len(m.statusHistory)+len(m.logOlder)+1)
	for i := len(m.statusHistory) - 1; i >= 0; i-- {
		e := m.statusHistory[i]
		lines = append(lines, m.theme.StatusStyle(e.sev).Render(fmt.Sprintf(" %s ", e.at.Format(logTimeFormat)))+" "+e.text)
	}
	if len(m.logOlder) > 0 {
		lines = append(lines, lipgloss.NewStyle().Faint(true).Render("── earlier, from the log file ──"))
	}
	for _, e := range m.logOlder {
		sev, text := sevInfo, e.Message
		if e.Attrs != "" {
			text += "  " + e.Attrs
		}
		if e.Level != "" && e.Level != "INFO" {
			text = "[" + e.Level + "] " + text
		}
		switch e.Level {
		case "ERROR":
			sev = sevError
		case "WARN":
			sev = sevWarn
		}
		lines = append(lines, m.theme.StatusStyle(sev).Render(fmt.Sprintf(" %s ", e.Time.Local().Format(logTimeFormat)))+" "+text)
	}
	m.logViewport.SetContent(lipgloss.NewStyle().Width(m.logViewport.Width).Render(strings.Join(lines, "\n")))
}

// updateLog handles input while the log overlay is open: esc/enter/L close
// it, and everything else (arrows, j/k, pgup/pgdown, space, u/d, mouse wheel)
// is delegated to the viewport for scrolling.
func (m App) updateLog(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m.handleCtrlC()
		case "q":
			m.quitting = true
			return m, nil
		case "esc", "enter", "L":
			m.showLog = false
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.logViewport, cmd = m.logViewport.Update(msg)
	m, loadCmd := m.loadOlderLogCmd()
	return m, tea.Batch(cmd, loadCmd)
}

// viewLog renders the full-screen scrollable status history overlay.
func (m App) viewLog() string {
	lines := []string{"pigeon — log", "", m.logViewport.View(), "", "↑/↓ pgup/pgdn: scroll · esc: close · q: quit"}
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}
