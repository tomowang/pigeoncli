package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/core/logs"
)

func newLogTestApp(entries int) App {
	m := App{
		theme:       themes[defaultThemeName],
		width:       80,
		height:      20,
		logViewport: viewport.New(0, 0),
	}
	for i := 0; i < entries; i++ {
		m.statusHistory = append(m.statusHistory, statusEntry{text: fmt.Sprintf("entry %d", i), at: time.Now()})
	}
	m.openLog()
	return m
}

func TestLogOverlayScrollsInsteadOfClosing(t *testing.T) {
	m := newLogTestApp(60)

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyPgDown},
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
		{Type: tea.KeyPgUp},
	} {
		model, _ := m.updateLog(key)
		m = model.(App)
		if !m.showLog {
			t.Fatalf("%v closed the log overlay; want it to scroll", key)
		}
	}

	model, _ := m.updateLog(tea.KeyMsg{Type: tea.KeyPgDown})
	m = model.(App)
	if m.logViewport.YOffset == 0 {
		t.Fatal("pgdown did not scroll the log viewport")
	}
}

func TestLogOverlayClosesOnEsc(t *testing.T) {
	m := newLogTestApp(3)
	model, _ := m.updateLog(tea.KeyMsg{Type: tea.KeyEsc})
	if model.(App).showLog {
		t.Fatal("esc did not close the log overlay")
	}
}

func writeTestLogFile(t *testing.T, n int) *logs.Service {
	t.Helper()
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"time":"2026-01-02T03:04:05Z","level":"INFO","msg":"old %d"}`+"\n", i)
	}
	path := filepath.Join(t.TempDir(), "pigeon.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return logs.NewService(path)
}

// runCmd executes cmd and feeds its message back through Update, like the
// bubbletea runtime would.
func runCmd(t *testing.T, m App, cmd tea.Cmd) App {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	model, _ := m.Update(cmd())
	return model.(App)
}

func TestLogOverlayLazyLoadsOlderRecords(t *testing.T) {
	svc := writeTestLogFile(t, 500)
	cur, err := svc.End()
	if err != nil {
		t.Fatal(err)
	}
	m := newLogTestApp(3)
	m.logsSvc, m.logCursor, m.logExhausted = svc, cur, false

	// Opening with only 3 short session entries leaves the viewport at its
	// end, so the first page is fetched right away.
	m.openLog()
	if len(m.logOlder) != 0 {
		t.Fatal("older records were loaded before the overlay asked for them")
	}
	m, cmd := m.loadOlderLogCmd()
	m = runCmd(t, m, cmd)
	if len(m.logOlder) != logPageSize || m.logOlder[0].Message != "old 499" {
		t.Fatalf("first page: %d records, first %q; want %d starting at old 499", len(m.logOlder), m.logOlder[0].Message, logPageSize)
	}

	// The page now overflows the viewport; at the top no further load fires.
	m.logViewport.GotoTop()
	model, _ := m.updateLog(tea.KeyMsg{Type: tea.KeyUp})
	m = model.(App)
	if m.logLoading {
		t.Fatal("scrolling at the top started a load")
	}

	// Reaching the bottom pulls in the next page.
	m.logViewport.GotoBottom()
	model, _ = m.updateLog(tea.KeyMsg{Type: tea.KeyDown})
	m = model.(App)
	if !m.logLoading {
		t.Fatal("reaching the bottom did not start a load")
	}
	m.logLoading = false // the batch wraps the load; re-issue it directly
	m, load := m.loadOlderLogCmd()
	m = runCmd(t, m, load)
	if len(m.logOlder) != 2*logPageSize || m.logOlder[logPageSize].Message != "old 299" {
		t.Fatalf("second page: %d records, next %q; want %d continuing at old 299", len(m.logOlder), m.logOlder[logPageSize].Message, 2*logPageSize)
	}
	if m.logExhausted {
		t.Fatal("marked exhausted with records remaining")
	}
}

func TestLogOverlayExhaustsAtFileStart(t *testing.T) {
	svc := writeTestLogFile(t, 5)
	cur, _ := svc.End()
	m := newLogTestApp(1)
	m.logsSvc, m.logCursor, m.logExhausted = svc, cur, false
	m.openLog()

	m, cmd := m.loadOlderLogCmd()
	m = runCmd(t, m, cmd)
	if len(m.logOlder) != 5 || !m.logExhausted || m.logCursor != 0 {
		t.Fatalf("got %d records, exhausted=%v, cursor=%d; want 5, true, 0", len(m.logOlder), m.logExhausted, m.logCursor)
	}
	if _, cmd := m.loadOlderLogCmd(); cmd != nil {
		t.Fatal("kept loading after reaching the start of the file")
	}
}
