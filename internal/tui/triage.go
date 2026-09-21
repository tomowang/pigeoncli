package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tomowang/pigeoncli/internal/core/account"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

// triageKind names an archive/delete/move/flag action, for status text and
// logging.
type triageKind int

const (
	triageArchive triageKind = iota
	triageDelete
	triageMove
	triagePurge
	triageUndo
	triageFlag
)

func (k triageKind) failVerb() string {
	switch k {
	case triageArchive:
		return "archive"
	case triageDelete:
		return "delete"
	case triageMove:
		return "move"
	case triagePurge:
		return "delete permanently"
	case triageUndo:
		return "undo"
	default:
		return "update message"
	}
}

func (k triageKind) busyText() string {
	switch k {
	case triageArchive:
		return "Archiving…"
	case triageDelete:
		return "Deleting…"
	case triageMove:
		return "Moving…"
	case triagePurge:
		return "Deleting permanently…"
	default:
		return "Working…"
	}
}

func (k triageKind) done(n int, dest string) string {
	msgs := "message"
	if n != 1 {
		msgs = fmt.Sprintf("%d messages", n)
	}
	switch k {
	case triageArchive:
		return "Archived " + msgs + "."
	case triageDelete:
		return "Moved " + msgs + " to Trash."
	case triageMove:
		return "Moved " + msgs + " to " + dest + "."
	case triagePurge:
		return "Permanently deleted " + msgs + "."
	case triageUndo:
		return "Restored " + msgs + "."
	default:
		return "Updated."
	}
}

// triageResultMsg reports the outcome of an archive, delete, move, purge,
// undo, or flag action.
type triageResultMsg struct {
	kind    triageKind
	count   int
	dest    string // destination folder path, for moves
	moved   []message.MoveResult
	account account.Account
	err     error
}

type moveFunc func(ctx context.Context, svc *message.Service, cfg account.Account, refs []message.Ref) ([]message.MoveResult, error)

// triageTarget is the message the triage keys act on: the open message, or
// else the selection in the message list.
func (m App) triageTarget() (message.Message, bool) {
	if m.viewingMsg != nil {
		return *m.viewingMsg, true
	}
	if m.focus == focusMessages {
		if it, ok := m.messages.SelectedItem().(messageItem); ok {
			return message.Message(it), true
		}
	}
	return message.Message{}, false
}

// handleTriageKey handles the archive/delete/move/read/star/undo keys. It's
// only called when a message is open or the message list has focus, and
// reports whether key was one of them.
func (m App) handleTriageKey(key string) (App, tea.Cmd, bool) {
	switch key {
	case "e", "d", "m", "u", "*":
	case "U":
		nm, cmd := m.undoCmd()
		return nm, cmd, true
	default:
		return m, nil, false
	}

	target, ok := m.triageTarget()
	if !ok {
		return m, nil, true
	}
	switch key {
	case "e":
		nm, cmd := m.beginMove(triageArchive, target, "", func(ctx context.Context, svc *message.Service, cfg account.Account, refs []message.Ref) ([]message.MoveResult, error) {
			return svc.Archive(ctx, cfg, refs)
		})
		return nm, cmd, true
	case "d":
		if m.currentFolderSpecialUse() == `\Trash` {
			// Already in the Trash: the only "delete" left is permanent, so
			// ask first.
			m.confirmingPurge = true
			m.triageMsg = target
			return m, nil, true
		}
		nm, cmd := m.beginMove(triageDelete, target, "", func(ctx context.Context, svc *message.Service, cfg account.Account, refs []message.Ref) ([]message.MoveResult, error) {
			return svc.Delete(ctx, cfg, refs)
		})
		return nm, cmd, true
	case "m":
		nm := m.beginFolderPicker(target)
		return nm, nil, true
	case "u":
		nm, cmd := m.toggleFlag(target, message.FlagSeen)
		return nm, cmd, true
	default: // "*"
		nm, cmd := m.toggleFlag(target, message.FlagFlagged)
		return nm, cmd, true
	}
}

// removeMessageItem drops uid from the message list right away, so the UI
// doesn't wait on the network. If the action fails, the list is reloaded
// from the (unchanged) cache and the row comes back.
func (m App) removeMessageItem(uid uint32) App {
	for i, it := range m.messages.Items() {
		if mi, ok := it.(messageItem); ok && mi.UID == uid {
			m.messages.RemoveItem(i)
			break
		}
	}
	return m
}

// beginMove starts fn on msg in the currently selected folder. The row leaves
// the list and any open viewer closes immediately.
func (m App) beginMove(kind triageKind, msg message.Message, dest string, fn moveFunc) (App, tea.Cmd) {
	refs := []message.Ref{{FolderPath: m.selectedFolder, UID: msg.UID}}
	m = m.removeMessageItem(msg.UID)
	if m.viewingMsg != nil {
		m.viewingMsg = nil
		m.layout()
	}

	ctx, svc, cfg := m.ctx, m.messageSvc, m.selectedAccount
	run := func() tea.Msg {
		moved, err := fn(ctx, svc, cfg, refs)
		return triageResultMsg{kind: kind, count: len(refs), dest: dest, moved: moved, account: cfg, err: err}
	}
	m, busy := m.startBusy(kind.busyText())
	return m, tea.Batch(busy, run)
}

// beginPurge permanently deletes msg (after the user confirmed).
func (m App) beginPurge(msg message.Message) (App, tea.Cmd) {
	refs := []message.Ref{{FolderPath: m.selectedFolder, UID: msg.UID}}
	m = m.removeMessageItem(msg.UID)
	if m.viewingMsg != nil {
		m.viewingMsg = nil
		m.layout()
	}
	ctx, svc, cfg := m.ctx, m.messageSvc, m.selectedAccount
	run := func() tea.Msg {
		err := svc.Purge(ctx, cfg, refs)
		return triageResultMsg{kind: triagePurge, count: len(refs), account: cfg, err: err}
	}
	m, busy := m.startBusy(triagePurge.busyText())
	return m, tea.Batch(busy, run)
}

// withFlag returns flags with f set (on) or cleared (!on).
func withFlag(flags []string, f message.Flag, on bool) []string {
	out := make([]string, 0, len(flags)+1)
	for _, x := range flags {
		if !strings.EqualFold(x, string(f)) {
			out = append(out, x)
		}
	}
	if on {
		out = append(out, string(f))
	}
	return out
}

// toggleFlag flips f on msg. The list (and open viewer) update immediately;
// the server call follows, and a reload afterwards reflects what really
// happened.
func (m App) toggleFlag(msg message.Message, f message.Flag) (App, tea.Cmd) {
	on := !msg.Has(f)
	flags := withFlag(msg.Flags, f, on)

	for i, it := range m.messages.Items() {
		if mi, ok := it.(messageItem); ok && mi.UID == msg.UID {
			mi.Flags = flags
			m.messages.SetItem(i, mi)
			break
		}
	}
	if m.viewingMsg != nil && m.viewingMsg.UID == msg.UID {
		vm := *m.viewingMsg
		vm.Flags = flags
		m.viewingMsg = &vm
	}

	refs := []message.Ref{{FolderPath: m.selectedFolder, UID: msg.UID}}
	ctx, svc, cfg := m.ctx, m.messageSvc, m.selectedAccount
	run := func() tea.Msg {
		var add, remove []message.Flag
		if on {
			add = []message.Flag{f}
		} else {
			remove = []message.Flag{f}
		}
		err := svc.SetFlags(ctx, cfg, refs, add, remove)
		return triageResultMsg{kind: triageFlag, count: 1, account: cfg, err: err}
	}
	return m, run
}

// undoCmd reverses the most recent archive/delete/move.
func (m App) undoCmd() (App, tea.Cmd) {
	if len(m.lastMoved) == 0 {
		return m.setStatus("Nothing to undo.", sevInfo)
	}
	moved, cfg := m.lastMoved, m.lastMovedAccount
	ctx, svc := m.ctx, m.messageSvc
	run := func() tea.Msg {
		err := svc.Undo(ctx, cfg, moved)
		return triageResultMsg{kind: triageUndo, count: len(moved), account: cfg, err: err}
	}
	m, busy := m.startBusy("Undoing…")
	return m, tea.Batch(busy, run)
}

// undoable reports whether every result in moved has a known new UID, so
// there's something worth offering an undo for.
func undoable(moved []message.MoveResult) bool {
	return len(moved) > 0 && !slices.ContainsFunc(moved, func(r message.MoveResult) bool { return r.Dest.UID == 0 })
}

func (m App) handleTriageResult(msg triageResultMsg) (tea.Model, tea.Cmd) {
	m = m.stopBusy()

	// Whether it worked or not, the cache is the source of truth: reload so
	// optimistic edits either settle or roll back.
	reload := tea.Batch(
		m.loadMessagesCmd(m.selectedAccount.Slug, m.selectedFolder),
		m.loadFoldersCmd(m.selectedAccount.Slug),
	)

	if len(msg.moved) > 0 && msg.kind != triageUndo {
		m.lastMoved = msg.moved
		m.lastMovedAccount = msg.account
	}

	var status, extra tea.Cmd
	if msg.err != nil {
		slog.Error("triage action failed", "action", msg.kind.failVerb(), "err", msg.err)
		text := fmt.Sprintf("%s: %v", msg.kind.failVerb(), msg.err)
		if errors.Is(msg.err, message.ErrNoTrashFolder) {
			text = "delete: this account has no Trash folder"
		}
		m, status = m.setStatus(text, sevError)
		return m, tea.Batch(status, reload)
	}

	slog.Info("triage action done", "action", msg.kind.failVerb(), "count", msg.count)
	text := msg.kind.done(msg.count, msg.dest)
	switch msg.kind {
	case triageArchive, triageDelete, triageMove:
		if undoable(msg.moved) {
			text += " U: undo."
		}
	case triageUndo:
		// The restored messages have fresh UIDs in their original folder and
		// aren't cached until that folder syncs, so sync now.
		m.lastMoved = nil
		if m.syncCh == nil && m.selectedAccount.Slug != "" {
			ch := make(chan folder.Progress)
			m.syncCh = ch
			extra = tea.Batch(m.startSyncCmd(m.selectedAccount, ch), listenSyncProgressCmd(ch), m.syncSpinner.Tick)
		}
	}
	m, status = m.setStatus(text, sevSuccess)
	return m, tea.Batch(status, reload, extra)
}

// --- permanent-delete confirmation ---

func (m App) updateConfirmPurge(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c":
		return m.handleCtrlC()
	case "y", "Y":
		m.confirmingPurge = false
		return m.beginPurge(m.triageMsg)
	case "n", "N", "esc":
		m.confirmingPurge = false
	}
	return m, nil
}

func (m App) viewConfirmPurge() string {
	subject := m.triageMsg.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	box := m.theme.ActivePane.Render(fmt.Sprintf("Permanently delete %q?\nThis can't be undone. (y/n)", subject))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// --- move-to-folder picker ---

// beginFolderPicker opens the folder picker for msg, listing every folder of
// the selected account except the one msg is already in.
func (m App) beginFolderPicker(msg message.Message) App {
	var items []list.Item
	for _, it := range m.folders.Items() {
		if f, ok := it.(folderItem); ok && f.Path != m.selectedFolder {
			items = append(items, f)
		}
	}
	if len(items) == 0 {
		m, _ = m.setStatus("No other folders to move to.", sevInfo)
		return m
	}
	m.folderPicker.SetItems(items)
	m.folderPicker.ResetFilter()
	m.folderPicker.Select(0)
	m.pickingFolder = true
	m.triageMsg = msg
	m = m.clearStatus()
	return m
}

func (m App) updatePickingFolder(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && m.folderPicker.FilterState() != list.Filtering {
		switch key.String() {
		case "ctrl+c":
			return m.handleCtrlC()
		case "q":
			m.quitting = true
			return m, nil
		case "esc":
			if m.folderPicker.FilterState() == list.FilterApplied {
				break // let the list clear its filter first
			}
			m.pickingFolder = false
			return m, nil
		case "enter":
			item, ok := m.folderPicker.SelectedItem().(folderItem)
			if !ok {
				return m, nil
			}
			m.pickingFolder = false
			dest := item.Path
			return m.beginMove(triageMove, m.triageMsg, item.Name, func(ctx context.Context, svc *message.Service, cfg account.Account, refs []message.Ref) ([]message.MoveResult, error) {
				return svc.Move(ctx, cfg, refs, dest)
			})
		}
	}
	var cmd tea.Cmd
	m.folderPicker, cmd = m.folderPicker.Update(msg)
	return m, cmd
}

func (m App) viewFolderPicker() string {
	header := m.theme.ViewHeader.Render("Move to — enter: move · /: filter · esc: cancel")
	return header + "\n" + m.folderPicker.View() + "\n" + m.footer()
}
