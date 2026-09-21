package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/core/account"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

// newTriageApp returns an App showing INBOX (uids 1..3, only uid 2 read) of
// account "work", with focus on the message list. No services are wired, so
// commands returned by key handlers must not be executed.
func newTriageApp(t *testing.T) App {
	t.Helper()
	m := newApp(context.Background(), nil, nil, nil, nil, nil, nil)
	m.width, m.height = 100, 30
	m.layout()
	m.selectedAccount = account.Account{Slug: "work"}
	m.selectedFolder = "INBOX"
	m.focus = focusMessages
	m.folders.SetItems([]list.Item{
		folderItem(folder.Folder{Path: "INBOX", Name: "INBOX", SpecialUse: `\Inbox`}),
		folderItem(folder.Folder{Path: "Archive", Name: "Archive", SpecialUse: `\Archive`}),
		folderItem(folder.Folder{Path: "Trash", Name: "Trash", SpecialUse: `\Trash`}),
	})
	m.messages.SetItems([]list.Item{
		messageItem(message.Message{UID: 1, Subject: "one"}),
		messageItem(message.Message{UID: 2, Subject: "two", Flags: []string{`\Seen`}}),
		messageItem(message.Message{UID: 3, Subject: "three"}),
	})
	return m
}

func press(m App, k string) (App, tea.Cmd) {
	var msg tea.KeyMsg
	switch k {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	model, cmd := m.Update(msg)
	return model.(App), cmd
}

func uids(m App) []uint32 {
	var out []uint32
	for _, it := range m.messages.Items() {
		out = append(out, it.(messageItem).UID)
	}
	return out
}

func TestArchiveKeyRemovesRowImmediately(t *testing.T) {
	m := newTriageApp(t)

	m, cmd := press(m, "e")
	if cmd == nil {
		t.Fatal("archive returned no command")
	}
	if got := uids(m); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("rows after archive = %v, want [2 3]", got)
	}
	if !m.busy {
		t.Fatal("archive should mark the app busy while the server call runs")
	}
}

func TestArchiveKeyClosesOpenViewer(t *testing.T) {
	m := newTriageApp(t)
	vm := message.Message{UID: 1, Subject: "one"}
	m.viewingMsg = &vm

	m, _ = press(m, "e")
	if m.viewingMsg != nil {
		t.Fatal("viewer stayed open after archiving the open message")
	}
	if got := uids(m); len(got) != 2 {
		t.Fatalf("rows after archive = %v", got)
	}
}

func TestTriageKeysDoNothingOutsideMessageList(t *testing.T) {
	m := newTriageApp(t)
	m.focus = focusFolders

	m, cmd := press(m, "e")
	if cmd != nil || len(uids(m)) != 3 {
		t.Fatal("e archived a message while the folder pane had focus")
	}
}

func TestToggleReadAndStarUpdateListImmediately(t *testing.T) {
	m := newTriageApp(t)

	// UID 1 is unread: u marks it read.
	m, cmd := press(m, "u")
	if cmd == nil {
		t.Fatal("u returned no command")
	}
	first := message.Message(m.messages.Items()[0].(messageItem))
	if !first.IsRead() {
		t.Fatalf("flags after u = %v, want read", first.Flags)
	}
	if strings.Contains(messageItem(first).Title(), "* ") {
		t.Fatalf("title %q still shows the unread marker", messageItem(first).Title())
	}

	m, _ = press(m, "u")
	if message.Message(m.messages.Items()[0].(messageItem)).IsRead() {
		t.Fatal("second u should mark the message unread again")
	}

	m, _ = press(m, "*")
	starred := m.messages.Items()[0].(messageItem)
	if !message.Message(starred).IsStarred() || !strings.Contains(starred.Title(), "★") {
		t.Fatalf("flags after * = %v, title %q", starred.Flags, starred.Title())
	}
	m, _ = press(m, "*")
	if message.Message(m.messages.Items()[0].(messageItem)).IsStarred() {
		t.Fatal("second * should remove the star")
	}
}

func TestToggleReadInViewerKeepsViewerAndUpdatesIt(t *testing.T) {
	m := newTriageApp(t)
	vm := message.Message{UID: 2, Subject: "two", Flags: []string{`\Seen`}}
	m.viewingMsg = &vm

	m, _ = press(m, "u")
	if m.viewingMsg == nil {
		t.Fatal("toggling read closed the viewer")
	}
	if m.viewingMsg.IsRead() {
		t.Fatal("viewer's message should now be unread")
	}
	if message.Message(m.messages.Items()[1].(messageItem)).IsRead() {
		t.Fatal("list row for the open message should now be unread")
	}
}

func TestDeleteInTrashAsksBeforePurging(t *testing.T) {
	m := newTriageApp(t)
	m.selectedFolder = "Trash"

	m, cmd := press(m, "d")
	if cmd != nil || !m.confirmingPurge {
		t.Fatalf("d in Trash: confirming=%v cmd=%v, want a confirmation prompt and no command", m.confirmingPurge, cmd != nil)
	}
	if len(uids(m)) != 3 {
		t.Fatal("row removed before the purge was confirmed")
	}
	if !strings.Contains(m.View(), "Permanently delete") {
		t.Fatalf("view doesn't show the confirmation:\n%s", m.View())
	}

	// Anything but y cancels.
	m, cmd = press(m, "x")
	if !m.confirmingPurge || cmd != nil {
		t.Fatal("an unrelated key should leave the prompt open")
	}
	m, _ = press(m, "n")
	if m.confirmingPurge || len(uids(m)) != 3 {
		t.Fatal("n should cancel without deleting")
	}

	m, _ = press(m, "d")
	m, cmd = press(m, "y")
	if m.confirmingPurge || cmd == nil || len(uids(m)) != 2 {
		t.Fatalf("y should purge: confirming=%v cmd=%v rows=%v", m.confirmingPurge, cmd != nil, uids(m))
	}
}

func TestDeleteOutsideTrashMovesWithoutConfirming(t *testing.T) {
	m := newTriageApp(t)

	m, cmd := press(m, "d")
	if m.confirmingPurge || cmd == nil || len(uids(m)) != 2 {
		t.Fatalf("d in INBOX: confirming=%v cmd=%v rows=%v", m.confirmingPurge, cmd != nil, uids(m))
	}
}

func TestMoveKeyOpensPickerWithoutCurrentFolder(t *testing.T) {
	m := newTriageApp(t)

	m, _ = press(m, "m")
	if !m.pickingFolder {
		t.Fatal("m did not open the folder picker")
	}
	var paths []string
	for _, it := range m.folderPicker.Items() {
		paths = append(paths, it.(folderItem).Path)
	}
	if strings.Join(paths, ",") != "Archive,Trash" {
		t.Fatalf("picker folders = %v, want Archive and Trash (not the current INBOX)", paths)
	}
	if !strings.Contains(m.View(), "Move to") {
		t.Fatal("view doesn't show the picker")
	}

	m, _ = press(m, "esc")
	if m.pickingFolder || len(uids(m)) != 3 {
		t.Fatal("esc should close the picker without moving anything")
	}

	m, _ = press(m, "m")
	m, cmd := press(m, "enter")
	if m.pickingFolder || cmd == nil || len(uids(m)) != 2 {
		t.Fatalf("enter should move the selected message: picking=%v cmd=%v rows=%v", m.pickingFolder, cmd != nil, uids(m))
	}
}

func TestUndoWithNothingToUndo(t *testing.T) {
	m := newTriageApp(t)
	m, cmd := press(m, "U")
	if cmd == nil {
		t.Fatal("expected the status auto-clear command")
	}
	if m.status.text != "Nothing to undo." {
		t.Fatalf("status = %q", m.status.text)
	}
}

func moved(uid uint32) []message.MoveResult {
	return []message.MoveResult{{
		Src:             message.Ref{FolderPath: "INBOX", UID: uid},
		Dest:            message.Ref{FolderPath: "Archive", UID: uid + 100},
		DestUIDValidity: 1,
	}}
}

func TestTriageResultRemembersMoveForUndo(t *testing.T) {
	m := newTriageApp(t)
	m.busy = true

	model, _ := m.handleTriageResult(triageResultMsg{kind: triageArchive, count: 1, moved: moved(1), account: m.selectedAccount})
	m = model.(App)
	if m.busy || len(m.lastMoved) != 1 {
		t.Fatalf("busy=%v lastMoved=%v", m.busy, m.lastMoved)
	}
	if !strings.Contains(m.status.text, "Archived") || !strings.Contains(m.status.text, "U: undo") {
		t.Fatalf("status = %q, want an archived message with the undo hint", m.status.text)
	}

	// A move the server gave no new UID for can't be undone, so don't offer it.
	m = newTriageApp(t)
	noUID := []message.MoveResult{{Src: message.Ref{FolderPath: "INBOX", UID: 1}, Dest: message.Ref{FolderPath: "Archive"}}}
	model, _ = m.handleTriageResult(triageResultMsg{kind: triageArchive, count: 1, moved: noUID, account: m.selectedAccount})
	if strings.Contains(model.(App).status.text, "undo") {
		t.Fatalf("status %q offers an undo that can't work", model.(App).status.text)
	}
}

func TestTriageResultErrorReportsAndReloads(t *testing.T) {
	m := newTriageApp(t)
	m.busy = true

	model, cmd := m.handleTriageResult(triageResultMsg{kind: triageDelete, count: 1, err: message.ErrNoTrashFolder})
	m = model.(App)
	if m.busy || m.status.sev != sevError || !strings.Contains(m.status.text, "no Trash folder") {
		t.Fatalf("busy=%v status=%+v", m.busy, m.status)
	}
	if cmd == nil {
		t.Fatal("a failed action must still reload the list so the optimistic edit rolls back")
	}

	m = newTriageApp(t)
	model, _ = m.handleTriageResult(triageResultMsg{kind: triageArchive, count: 1, err: errors.New("boom")})
	if got := model.(App).status.text; got != "archive: boom" {
		t.Fatalf("status = %q", got)
	}
	if len(model.(App).lastMoved) != 0 {
		t.Fatal("a failed move must not become undoable")
	}
}

func TestUndoResultClearsUndoState(t *testing.T) {
	m := newTriageApp(t)
	m.lastMoved = moved(1)

	// A failed undo stays available for a retry.
	model, _ := m.handleTriageResult(triageResultMsg{kind: triageUndo, err: errors.New("offline")})
	if len(model.(App).lastMoved) != 1 {
		t.Fatal("failed undo dropped the undo state")
	}

	model, _ = m.handleTriageResult(triageResultMsg{kind: triageUndo, count: 1})
	m = model.(App)
	if len(m.lastMoved) != 0 || !strings.Contains(m.status.text, "Restored") {
		t.Fatalf("lastMoved=%v status=%q", m.lastMoved, m.status.text)
	}
	if m.syncCh == nil {
		t.Fatal("undo should trigger a sync so the restored message reappears")
	}
}

func TestWithFlag(t *testing.T) {
	got := withFlag([]string{`\Seen`, `\Flagged`}, message.FlagSeen, false)
	if len(got) != 1 || got[0] != `\Flagged` {
		t.Fatalf("removing: %v", got)
	}
	got = withFlag([]string{`\seen`}, message.FlagSeen, true)
	if len(got) != 1 || got[0] != `\Seen` {
		t.Fatalf("adding over a differently-cased duplicate: %v", got)
	}
}
