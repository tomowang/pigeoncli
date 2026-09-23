package tui

import (
	"bytes"
	"fmt"
	"log/slog"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/core/compose"
)

// draftAutosaveInterval is how often the compose pane, while open, checks
// whether the draft has changed since it was last saved and — if so —
// saves it to the Drafts folder. It's a fixed period rather than a
// per-keystroke debounce: simpler, and frequent enough to protect against
// a crash without generating an IMAP round trip on every keystroke.
const draftAutosaveInterval = 20 * time.Second

// draftAutosaveTickMsg drives the periodic autosave check. gen ties it to
// the compose session that scheduled it (see App.draftGen), so a stale
// tick from a session that has since closed — or been replaced by a new
// one — is dropped rather than autosaving into the wrong draft.
type draftAutosaveTickMsg struct{ gen int }

func draftAutosaveTickCmd(gen int) tea.Cmd {
	return tea.Tick(draftAutosaveInterval, func(time.Time) tea.Msg {
		return draftAutosaveTickMsg{gen: gen}
	})
}

// draftSaveResultMsg reports the outcome of a save triggered by the
// autosave tick, ctrl+o, or closing compose with unsaved changes (esc).
// manual distinguishes ctrl+o (worth a status line either way) from the
// other two (silent unless it fails).
type draftSaveResultMsg struct {
	gen    int
	ref    compose.DraftRef
	draft  compose.Draft
	manual bool
	err    error
}

// draftDiscardedMsg reports the outcome of removing a sent draft's saved
// copy — best-effort, so its only real consequence is what it logs on
// failure.
type draftDiscardedMsg struct{ err error }

// currentDraft builds a compose.Draft from the compose pane's current
// widget values. sendCompose, the autosave tick, and the manual/close-
// triggered saves all build the draft this same way, so they all save or
// send exactly what's on screen.
func (m App) currentDraft() compose.Draft {
	return compose.Draft{
		To:          splitAddrs(m.composeTo.Value()),
		Cc:          splitAddrs(m.composeCc.Value()),
		Bcc:         splitAddrs(m.composeBcc.Value()),
		Subject:     m.composeSubject.Value(),
		Body:        m.composeBody.Value(),
		InReplyTo:   m.composeInReplyTo,
		References:  m.composeReferences,
		Attachments: m.composeAttachments,
	}
}

// draftBaseline is what the current compose session's draft is compared
// against to decide whether there's anything worth saving: the content as
// of the last successful save, or — before the first save — the content
// enterCompose started with (draftInitial), so opening a reply/forward/
// resumed draft and closing it untouched doesn't autosave a copy of
// something the user never actually edited.
func (m App) draftBaseline() compose.Draft {
	if m.draftRef.UID != 0 {
		return m.draftLastSaved
	}
	return m.draftInitial
}

// draftsEqual reports whether a and b would produce the same saved draft.
// InReplyTo/References aren't compared: they're fixed for a compose
// session (never user-editable), so they can't be what changed.
func draftsEqual(a, b compose.Draft) bool {
	if !slices.Equal(a.To, b.To) || !slices.Equal(a.Cc, b.Cc) || !slices.Equal(a.Bcc, b.Bcc) {
		return false
	}
	if a.Subject != b.Subject || a.Body != b.Body {
		return false
	}
	if len(a.Attachments) != len(b.Attachments) {
		return false
	}
	for i := range a.Attachments {
		if a.Attachments[i].Filename != b.Attachments[i].Filename || !bytes.Equal(a.Attachments[i].Data, b.Attachments[i].Data) {
			return false
		}
	}
	return true
}

// saveDraftCmd saves draft to the Drafts folder, replacing the current
// session's previous saved copy (m.draftRef) if there is one.
func (m App) saveDraftCmd(draft compose.Draft, manual bool) tea.Cmd {
	ctx, svc, cfg, prev, gen := m.ctx, m.composeSvc, m.selectedAccount, m.draftRef, m.draftGen
	return func() tea.Msg {
		ref, err := svc.SaveDraft(ctx, cfg, draft, prev)
		return draftSaveResultMsg{gen: gen, ref: ref, draft: draft, manual: manual, err: err}
	}
}

// discardDraftCmd permanently deletes ref's saved copy — called once its
// draft has actually been sent.
func (m App) discardDraftCmd(ref compose.DraftRef) tea.Cmd {
	ctx, svc, cfg := m.ctx, m.composeSvc, m.selectedAccount
	return func() tea.Msg {
		return draftDiscardedMsg{err: svc.DiscardDraft(ctx, cfg, ref)}
	}
}

// handleDraftAutosaveTick is the top-level App.Update case for
// draftAutosaveTickMsg: no-op (and don't reschedule) if the session that
// scheduled it has ended; otherwise save if the draft has changed since
// the last save, and always reschedule the next tick.
func (m App) handleDraftAutosaveTick(msg draftAutosaveTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.draftGen || !m.composing {
		return m, nil
	}
	next := draftAutosaveTickCmd(m.draftGen)
	if m.draftSaving || draftsEqual(m.currentDraft(), m.draftBaseline()) {
		return m, next
	}
	m.draftSaving = true
	return m, tea.Batch(m.saveDraftCmd(m.currentDraft(), false), next)
}

// handleDraftSaveResult is the top-level App.Update case for
// draftSaveResultMsg.
func (m App) handleDraftSaveResult(msg draftSaveResultMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.draftGen {
		return m, nil // a different (or no) compose session is active now
	}
	m.draftSaving = false
	if msg.err != nil {
		slog.Warn("save draft failed", "manual", msg.manual, "err", msg.err)
		if msg.manual {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("save draft: %v", msg.err), sevError)
			return m, cmd
		}
		return m, nil
	}
	m.draftRef = msg.ref
	m.draftLastSaved = msg.draft
	if msg.manual {
		var cmd tea.Cmd
		m, cmd = m.setStatus("Draft saved.", sevSuccess)
		return m, cmd
	}
	return m, nil
}

// handleDraftDiscarded is the top-level App.Update case for
// draftDiscardedMsg: best-effort, so a failure is only logged, not shown —
// the message has already been sent by that point.
func (m App) handleDraftDiscarded(msg draftDiscardedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		slog.Warn("discard sent draft's saved copy failed", "err", msg.err)
	}
	return m, nil
}

// startEditDraft resumes editing the currently viewed message as a draft
// (only meaningful when it lives in the Drafts folder — see the "c" key in
// updateViewing), reconstructed via compose.Service.LoadDraft. Unlike a
// fresh compose session, its baseline is the draft as loaded, and future
// saves replace this same copy rather than creating a new one.
func (m App) startEditDraft() (tea.Model, tea.Cmd) {
	if m.viewingMsg == nil {
		return m, nil
	}
	ref := compose.DraftRef{FolderPath: m.selectedFolder, UID: m.viewingMsg.UID}
	draft := m.composeSvc.LoadDraft(*m.viewingMsg, m.viewBody)
	m.viewingMsg = nil
	nm := m.enterCompose(draft, composeFieldBody)
	nm.draftRef = ref
	nm.draftLastSaved = draft
	return nm, draftAutosaveTickCmd(nm.draftGen)
}
