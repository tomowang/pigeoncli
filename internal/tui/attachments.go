package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/core/message"
)

// attachmentSavedMsg reports the outcome of saving one attachment to disk.
type attachmentSavedMsg struct {
	path string
	err  error
}

// attachmentsLine renders a one-line summary of the currently viewed
// message's attachments, or "" if it has none.
func (m App) attachmentsLine() string {
	if m.viewingMsg == nil || len(m.viewBody.Attachments) == 0 {
		return ""
	}
	parts := make([]string, len(m.viewBody.Attachments))
	for i, a := range m.viewBody.Attachments {
		parts[i] = fmt.Sprintf("[%d] %s (%s)", a.Index+1, a.Filename, humanSize(a.Size))
	}
	return "Attachments: " + strings.Join(parts, "   ") + "   (a: save)"
}

// humanSize formats n bytes as a short human-readable size (e.g. "128 KiB").
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// beginAttachmentPicker switches into the attachment-picker overlay, listing
// every attachment on the currently viewed message.
func (m App) beginAttachmentPicker() App {
	items := make([]list.Item, len(m.viewBody.Attachments))
	for i, a := range m.viewBody.Attachments {
		items[i] = attachmentItem(a)
	}
	m.attachmentPicker.SetItems(items)
	m.pickingAttachment = true
	m = m.clearStatus()
	return m
}

// beginSaveAttachment switches into the save-path prompt for att, prefilled
// with a sensible default destination.
func (m App) beginSaveAttachment(att message.Attachment) App {
	m.pendingAttachment = att
	m.saveAttachmentInput = textinput.New()
	m.saveAttachmentInput.Prompt = "Save to: "
	m.saveAttachmentInput.SetValue(defaultAttachmentPath(att.Filename))
	m.saveAttachmentInput.CursorEnd()
	m.saveAttachmentInput.Focus()
	m.savingAttachment = true
	m = m.clearStatus()
	m.layout()
	return m
}

// defaultAttachmentPath suggests ~/Downloads/filename, falling back to just
// filename (relative to the current directory) if the home directory can't
// be resolved.
func defaultAttachmentPath(filename string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filename
	}
	return filepath.Join(home, "Downloads", filename)
}

func (m App) saveAttachmentCmd(uid uint32, folderPath string, index int, destPath string) tea.Cmd {
	ctx, svc, cfg := m.ctx, m.messageSvc, m.selectedAccount
	return func() tea.Msg {
		err := svc.SaveAttachment(ctx, cfg, folderPath, uid, index, destPath)
		return attachmentSavedMsg{path: destPath, err: err}
	}
}

// updateAttachmentPicker handles input while the attachment picker is shown
// (only reached when a message has more than one attachment).
func (m App) updateAttachmentPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m.handleCtrlC()
		case "q":
			m.quitting = true
			return m, nil
		case "esc":
			m.pickingAttachment = false
			return m, nil
		case "enter":
			if item, ok := m.attachmentPicker.SelectedItem().(attachmentItem); ok {
				m.pickingAttachment = false
				return m.beginSaveAttachment(message.Attachment(item)), nil
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.attachmentPicker, cmd = m.attachmentPicker.Update(msg)
	return m, cmd
}

// updateSavingAttachment handles input while the save-path prompt is
// active.
func (m App) updateSavingAttachment(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			m.savingAttachment = false
			m = m.clearStatus()
			return m, nil
		case "enter":
			if m.viewingMsg == nil {
				m.savingAttachment = false
				return m, nil
			}
			destPath := m.saveAttachmentInput.Value()
			index := m.pendingAttachment.Index
			uid := m.viewingMsg.UID
			folderPath := m.selectedFolder
			m.savingAttachment = false
			var cmd tea.Cmd
			m, cmd = m.setStatus("Saving...", sevInfo)
			return m, tea.Batch(cmd, m.saveAttachmentCmd(uid, folderPath, index, destPath))
		}
	}
	var cmd tea.Cmd
	m.saveAttachmentInput, cmd = m.saveAttachmentInput.Update(msg)
	return m, cmd
}

func (m App) viewAttachmentPicker() string {
	header := m.theme.ViewHeader.Render("Attachments — enter: save · esc: back")
	return header + "\n" + m.attachmentPicker.View() + "\n" + m.footer()
}

func (m App) viewSaveAttachment() string {
	return m.saveAttachmentInput.View() + "\n" + m.footer()
}
