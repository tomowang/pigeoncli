package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/core/compose"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

var errBoom = errors.New("boom")

// dispatch runs msg through m.Update and returns the result as an App,
// for message types press() (which only builds tea.KeyMsg) can't send.
func dispatch(t *testing.T, m App, msg tea.Msg) (App, tea.Cmd) {
	t.Helper()
	model, cmd := m.Update(msg)
	return model.(App), cmd
}

func TestDraftsEqual(t *testing.T) {
	base := compose.Draft{To: []string{"a@example.com"}, Subject: "Hi", Body: "body"}
	same := compose.Draft{To: []string{"a@example.com"}, Subject: "Hi", Body: "body"}
	if !draftsEqual(base, same) {
		t.Fatal("identical drafts compared unequal")
	}

	cases := []compose.Draft{
		{To: []string{"b@example.com"}, Subject: "Hi", Body: "body"},
		{To: []string{"a@example.com"}, Subject: "Bye", Body: "body"},
		{To: []string{"a@example.com"}, Subject: "Hi", Body: "different"},
		{To: []string{"a@example.com"}, Subject: "Hi", Body: "body", Cc: []string{"c@example.com"}},
		{To: []string{"a@example.com"}, Subject: "Hi", Body: "body", Attachments: []compose.Attachment{{Filename: "a.txt", Data: []byte("x")}}},
	}
	for i, c := range cases {
		if draftsEqual(base, c) {
			t.Errorf("case %d: %+v compared equal to %+v", i, base, c)
		}
	}

	// InReplyTo/References are deliberately not part of the comparison.
	withRefs := base
	withRefs.InReplyTo, withRefs.References = "id@example.com", "id@example.com"
	if !draftsEqual(base, withRefs) {
		t.Fatal("InReplyTo/References should not affect draftsEqual")
	}
}

func TestCurrentDraftMatchesWidgetValues(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeTo.SetValue("bob@example.com, carol@example.com")
	m.composeBcc.SetValue("secret@example.com")
	m.composeSubject.SetValue("Subject line")
	m.composeBody.SetValue("Body text")

	got := m.currentDraft()
	if len(got.To) != 2 || got.To[0] != "bob@example.com" || got.To[1] != "carol@example.com" {
		t.Fatalf("To = %v", got.To)
	}
	if len(got.Bcc) != 1 || got.Bcc[0] != "secret@example.com" {
		t.Fatalf("Bcc = %v", got.Bcc)
	}
	if got.Subject != "Subject line" || got.Body != "Body text" {
		t.Fatalf("Subject/Body = %q/%q", got.Subject, got.Body)
	}
}

func TestEnterComposeResetsDraftStateAndBumpsGen(t *testing.T) {
	m := newComposeTestApp(t)
	firstGen := m.draftGen
	m.draftRef = compose.DraftRef{FolderPath: "Drafts", UID: 9}
	m.draftLastSaved = compose.Draft{Subject: "stale"}
	m.draftSaving = true

	draft := compose.Draft{To: []string{"x@example.com"}, Subject: "New"}
	m = m.enterCompose(draft, composeFieldTo)

	if m.draftGen == firstGen {
		t.Fatal("enterCompose should bump draftGen")
	}
	if m.draftRef.UID != 0 || m.draftSaving {
		t.Fatalf("enterCompose should reset draftRef/draftSaving, got ref=%+v saving=%v", m.draftRef, m.draftSaving)
	}
	if !draftsEqual(m.draftInitial, draft) {
		t.Fatalf("draftInitial = %+v, want %+v", m.draftInitial, draft)
	}
}

func TestCancelComposeSkipsSaveWhenUnchanged(t *testing.T) {
	m := newComposeTestApp(t) // enterCompose with To: bob@example.com, nothing else touched
	m, cmd := press(m, "esc")
	if cmd != nil {
		t.Fatal("esc on an untouched draft should not trigger a save")
	}
	if m.composing {
		t.Fatal("esc should close the compose pane")
	}
}

func TestCancelComposeSavesWhenDirty(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeSvc = compose.NewService(nil, nil)
	m.composeSubject.SetValue("Changed my mind")

	m, cmd := press(m, "esc")
	if cmd == nil {
		t.Fatal("esc on an edited draft should trigger a background save")
	}
	if m.composing {
		t.Fatal("esc should close the compose pane immediately regardless of the save")
	}
}

func TestSaveDraftNowShowsStatusAndUpdatesStateOnSuccess(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeSvc = compose.NewService(nil, nil)
	m.composeSubject.SetValue("Draft in progress")

	m, cmd := press(m, "ctrl+o")
	if cmd == nil || m.status.text != "Saving draft…" || !m.draftSaving {
		t.Fatalf("cmd=%v status=%q saving=%v", cmd != nil, m.status.text, m.draftSaving)
	}

	ref := compose.DraftRef{FolderPath: "Drafts", UID: 42}
	saved := m.currentDraft()
	m, cmd = dispatch(t, m, draftSaveResultMsg{gen: m.draftGen, ref: ref, draft: saved, manual: true})
	if cmd == nil {
		t.Fatal("expected the status auto-clear command")
	}
	if m.draftSaving || m.status.sev != sevSuccess || m.draftRef != ref {
		t.Fatalf("saving=%v status=%+v ref=%+v", m.draftSaving, m.status, m.draftRef)
	}
	if !draftsEqual(m.draftLastSaved, saved) {
		t.Fatalf("draftLastSaved = %+v, want %+v", m.draftLastSaved, saved)
	}
}

func TestSaveDraftNowReportsErrorButAutosaveStaysSilent(t *testing.T) {
	m := newComposeTestApp(t)
	gen := m.draftGen

	model, _ := m.handleDraftSaveResult(draftSaveResultMsg{gen: gen, manual: true, err: errBoom})
	m = model.(App)
	if m.status.sev != sevError || !strings.Contains(m.status.text, "boom") {
		t.Fatalf("manual failure status = %+v", m.status)
	}

	m2 := newComposeTestApp(t)
	model2, cmd := m2.handleDraftSaveResult(draftSaveResultMsg{gen: m2.draftGen, manual: false, err: errBoom})
	m2 = model2.(App)
	if cmd != nil || m2.status.text != "" {
		t.Fatalf("autosave failure should be silent, got status=%q cmd=%v", m2.status.text, cmd != nil)
	}
}

func TestSaveDraftResultIgnoresStaleGen(t *testing.T) {
	m := newComposeTestApp(t)
	m.draftGen = 5
	model, cmd := m.handleDraftSaveResult(draftSaveResultMsg{gen: 4, ref: compose.DraftRef{FolderPath: "Drafts", UID: 1}, manual: true})
	got := model.(App)
	if cmd != nil || got.draftRef.UID != 0 {
		t.Fatalf("a stale-gen result should be ignored entirely: cmd=%v ref=%+v", cmd != nil, got.draftRef)
	}
}

func TestDraftAutosaveTickIgnoredWhenNotComposingOrStaleGen(t *testing.T) {
	m := newComposeTestApp(t)
	m.composing = false
	if _, cmd := m.handleDraftAutosaveTick(draftAutosaveTickMsg{gen: m.draftGen}); cmd != nil {
		t.Fatal("a tick after compose closed should not reschedule")
	}

	m2 := newComposeTestApp(t)
	if _, cmd := m2.handleDraftAutosaveTick(draftAutosaveTickMsg{gen: m2.draftGen - 1}); cmd != nil {
		t.Fatal("a stale-gen tick should not reschedule")
	}
}

func TestDraftAutosaveTickSkipsWhenClean(t *testing.T) {
	m := newComposeTestApp(t) // untouched since enterCompose
	model, cmd := m.handleDraftAutosaveTick(draftAutosaveTickMsg{gen: m.draftGen})
	got := model.(App)
	if cmd == nil {
		t.Fatal("a clean draft should still reschedule the next tick")
	}
	if got.draftSaving {
		t.Fatal("a clean draft should not start a save")
	}
}

func TestDraftAutosaveTickSavesWhenDirty(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeSvc = compose.NewService(nil, nil)
	m.composeBody.SetValue("typed something")

	model, cmd := m.handleDraftAutosaveTick(draftAutosaveTickMsg{gen: m.draftGen})
	got := model.(App)
	if cmd == nil || !got.draftSaving {
		t.Fatalf("cmd=%v saving=%v, want a save kicked off", cmd != nil, got.draftSaving)
	}
}

func TestSendResultDiscardsDraftRefOnSuccess(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeSvc = compose.NewService(nil, nil)
	m.draftRef = compose.DraftRef{FolderPath: "Drafts", UID: 3}

	got, cmd := dispatch(t, m, sendResultMsg{})
	if got.draftRef.UID != 0 {
		t.Fatalf("draftRef = %+v, want cleared once sent", got.draftRef)
	}
	if cmd == nil {
		t.Fatal("expected a batch of commands including the discard")
	}
}

func TestSendResultLeavesDraftRefAloneWhenNeverSaved(t *testing.T) {
	m := newComposeTestApp(t)
	got, _ := dispatch(t, m, sendResultMsg{})
	if got.draftRef.UID != 0 {
		t.Fatalf("draftRef = %+v, want still zero", got.draftRef)
	}
}

func TestHandleDraftDiscardedNeverErrorsTheUI(t *testing.T) {
	m := newComposeTestApp(t)
	model, cmd := m.handleDraftDiscarded(draftDiscardedMsg{err: errBoom})
	got := model.(App)
	if cmd != nil || got.status.sev == sevError {
		t.Fatalf("a failed discard should be silent to the user, got status=%+v cmd=%v", got.status, cmd != nil)
	}
}

func TestStartEditDraftLoadsFieldsAndSetsBaseline(t *testing.T) {
	msg := message.Message{UID: 5, ToAddrs: []string{"someone@example.com"}, Subject: "Unfinished"}
	body := message.Body{PlainText: "so far so good"}
	m := newViewingTestApp(t, msg, body)
	m.selectedFolder = "Drafts"
	m.folders.SetItems([]list.Item{folderItem(folder.Folder{Path: "Drafts", Name: "Drafts", SpecialUse: `\Drafts`})})

	model, cmd := m.updateViewing(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	got := model.(App)
	if cmd == nil {
		t.Fatal("expected the autosave-tick command")
	}
	if !got.composing || got.viewingMsg != nil {
		t.Fatalf("composing=%v viewingMsg=%v", got.composing, got.viewingMsg)
	}
	if got.composeTo.Value() != "someone@example.com" || got.composeSubject.Value() != "Unfinished" {
		t.Fatalf("To=%q Subject=%q", got.composeTo.Value(), got.composeSubject.Value())
	}
	if got.composeBody.Value() != "so far so good" {
		t.Fatalf("Body = %q", got.composeBody.Value())
	}
	wantRef := compose.DraftRef{FolderPath: "Drafts", UID: 5}
	if got.draftRef != wantRef {
		t.Fatalf("draftRef = %+v, want %+v", got.draftRef, wantRef)
	}
	if !draftsEqual(got.draftInitial, got.draftLastSaved) {
		t.Fatalf("draftInitial and draftLastSaved should match on resume: %+v vs %+v", got.draftInitial, got.draftLastSaved)
	}
}

func TestCKeyDoesNothingOutsideDraftsFolder(t *testing.T) {
	msg := message.Message{UID: 1, Subject: "Ordinary"}
	m := newViewingTestApp(t, msg, message.Body{})
	m.selectedFolder = "INBOX"

	model, cmd := m.updateViewing(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	got := model.(App)
	if cmd != nil || got.composing || got.viewingMsg == nil {
		t.Fatalf("c outside Drafts should be a no-op: cmd=%v composing=%v viewingMsg=%v", cmd != nil, got.composing, got.viewingMsg)
	}
}
