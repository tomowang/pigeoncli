package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// severity classifies a status message for color-coding in the status bar
// and the L log overlay.
type severity int

const (
	sevInfo severity = iota
	sevSuccess
	sevError
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

// viewLog renders the full-screen scrollable status history overlay,
// following the same "press any key to close" pattern as viewHelp.
func (m App) viewLog() string {
	if len(m.statusHistory) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).Render("pigeon — log\n\nNo messages yet.\n\nPress any key to close.")
	}

	lines := make([]string, 0, len(m.statusHistory)+3)
	lines = append(lines, "pigeon — log", "")
	for i := len(m.statusHistory) - 1; i >= 0; i-- {
		e := m.statusHistory[i]
		lines = append(lines, m.theme.StatusStyle(e.sev).Render(fmt.Sprintf(" %s ", e.at.Format("15:04:05")))+" "+e.text)
	}
	lines = append(lines, "", "Press any key to close.")
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}
