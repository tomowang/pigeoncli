package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tomowang/pigeoncli/internal/core/compose"
)

// startReply builds a reply (or reply-all) draft for the message currently
// being viewed and switches into the compose pane. All reply/quoting/
// signature logic lives in core/compose.Service.NewReply — this just
// renders the resulting Draft into the compose widgets. Focus starts on
// the body, since To/Cc/Subject are already filled in.
func (m App) startReply(replyAll bool) (tea.Model, tea.Cmd) {
	if m.viewingMsg == nil {
		return m, nil
	}
	draft := m.composeSvc.NewReply(m.ctx, m.selectedAccount, *m.viewingMsg, m.viewBody, replyAll)
	m.viewingMsg = nil
	nm := m.enterCompose(draft, composeFieldBody)
	return nm, draftAutosaveTickCmd(nm.draftGen)
}

// startForward builds a forward draft for the message currently being
// viewed and switches into the compose pane, focused on To since (unlike a
// reply) nothing is addressed yet. All subject/header-block/attachment
// logic lives in core/compose.Service.NewForward.
func (m App) startForward() (tea.Model, tea.Cmd) {
	if m.viewingMsg == nil {
		return m, nil
	}
	draft := m.composeSvc.NewForward(m.ctx, m.selectedAccount, *m.viewingMsg, m.viewBody)
	m.viewingMsg = nil
	nm := m.enterCompose(draft, composeFieldTo)
	return nm, draftAutosaveTickCmd(nm.draftGen)
}

// startNewMessage builds an empty draft (just the account's default
// signature, if any) and switches into the compose pane. Focus starts on
// To, since there's nothing pre-filled to edit.
func (m App) startNewMessage() (tea.Model, tea.Cmd) {
	draft := m.composeSvc.NewMessage(m.ctx, m.selectedAccount)
	nm := m.enterCompose(draft, composeFieldTo)
	return nm, draftAutosaveTickCmd(nm.draftGen)
}

// enterCompose renders draft into fresh compose widgets, focuses field,
// and switches the view into compose mode.
func (m App) enterCompose(draft compose.Draft, field composeField) App {
	m.composeTo = textinput.New()
	m.composeTo.Prompt = "To: "
	m.composeTo.SetValue(strings.Join(draft.To, ", "))

	m.composeCc = textinput.New()
	m.composeCc.Prompt = "Cc: "
	m.composeCc.SetValue(strings.Join(draft.Cc, ", "))

	m.composeBcc = textinput.New()
	m.composeBcc.Prompt = "Bcc: "
	m.composeBcc.SetValue(strings.Join(draft.Bcc, ", "))

	m.composeSubject = textinput.New()
	m.composeSubject.Prompt = "Subject: "
	m.composeSubject.SetValue(draft.Subject)

	m.composeBody = textarea.New()
	m.composeBody.SetValue(draft.Body)

	m.composeInReplyTo = draft.InReplyTo
	m.composeReferences = draft.References
	m.composeAttachments = draft.Attachments
	m.attachingFile = false

	// A fresh session: no saved copy yet, and draftInitial is what "no
	// edits made" means for it — see draftBaseline. startEditDraft
	// overrides draftRef/draftLastSaved right after this call, for a
	// session that resumes an already-saved draft instead.
	m.draftGen++
	m.draftInitial = draft
	m.draftRef = compose.DraftRef{}
	m.draftLastSaved = compose.Draft{}
	m.draftSaving = false

	m.composing = true
	m = m.clearStatus()
	m.composeField = field
	switch field {
	case composeFieldTo:
		m.composeTo.Focus()
	case composeFieldCc:
		m.composeCc.Focus()
	case composeFieldBcc:
		m.composeBcc.Focus()
	case composeFieldSubject:
		m.composeSubject.Focus()
	case composeFieldBody:
		m.composeBody.Focus()
	}

	m.layout()
	return m
}

// updateComposing handles input while the compose pane is active. Global
// keys (send/cancel/next-field) are intercepted first; everything else —
// including non-key messages like cursor-blink ticks — goes to whichever
// widget currently has focus.
func (m App) updateComposing(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.attachingFile {
		return m.updateAttachingFile(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return m.cancelCompose()
		case "tab":
			return m.cycleComposeField(), nil
		case "ctrl+s":
			return m.sendCompose()
		case "ctrl+g":
			return m.beginAttachFile(), nil
		case "ctrl+r":
			return m.removeLastAttachment()
		case "ctrl+o":
			return m.saveDraftNow()
		}
	}

	var cmd tea.Cmd
	switch m.composeField {
	case composeFieldTo:
		m.composeTo, cmd = m.composeTo.Update(msg)
	case composeFieldCc:
		m.composeCc, cmd = m.composeCc.Update(msg)
	case composeFieldBcc:
		m.composeBcc, cmd = m.composeBcc.Update(msg)
	case composeFieldSubject:
		m.composeSubject, cmd = m.composeSubject.Update(msg)
	case composeFieldBody:
		m.composeBody, cmd = m.composeBody.Update(msg)
	}
	return m, cmd
}

// beginAttachFile switches into the attach-file path prompt, a lightweight
// mode within compose (not a top-level App mode): updateComposing routes
// here first while m.attachingFile is set, and viewCompose renders the
// prompt in place of the compose fields.
func (m App) beginAttachFile() App {
	m.attachFileInput = textinput.New()
	m.attachFileInput.Prompt = "Attach file: "
	m.attachFileInput.CursorEnd()
	m.attachFileInput.Focus()
	m.attachingFile = true
	m = m.clearStatus()
	m.layout()
	return m
}

// updateAttachingFile handles input while the attach-file prompt is active.
func (m App) updateAttachingFile(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m.handleCtrlC()
		case "esc":
			m.attachingFile = false
			m = m.clearStatus()
			return m, nil
		case "enter":
			path := strings.TrimSpace(m.attachFileInput.Value())
			m.attachingFile = false
			if path == "" {
				return m, nil
			}
			att, err := compose.NewAttachmentFromFile(path)
			if err != nil {
				var cmd tea.Cmd
				m, cmd = m.setStatus(fmt.Sprintf("attach %s: %v", path, err), sevError)
				return m, cmd
			}
			m.composeAttachments = append(m.composeAttachments, att)
			m.layout() // the body shrinks by one line once an attachments line is shown
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("Attached %s (%s).", att.Filename, humanSize(int64(len(att.Data)))), sevSuccess)
			return m, cmd
		}
	}
	var cmd tea.Cmd
	m.attachFileInput, cmd = m.attachFileInput.Update(msg)
	return m, cmd
}

// removeLastAttachment drops the most recently attached file from the
// draft being composed.
func (m App) removeLastAttachment() (App, tea.Cmd) {
	if len(m.composeAttachments) == 0 {
		return m.setStatus("No attachments to remove.", sevInfo)
	}
	removed := m.composeAttachments[len(m.composeAttachments)-1]
	m.composeAttachments = m.composeAttachments[:len(m.composeAttachments)-1]
	m.layout()
	return m.setStatus(fmt.Sprintf("Removed %s.", removed.Filename), sevInfo)
}

func (m App) cycleComposeField() App {
	m.composeTo.Blur()
	m.composeCc.Blur()
	m.composeBcc.Blur()
	m.composeSubject.Blur()
	m.composeBody.Blur()

	m.composeField = (m.composeField + 1) % composeFieldCount
	switch m.composeField {
	case composeFieldTo:
		m.composeTo.Focus()
	case composeFieldCc:
		m.composeCc.Focus()
	case composeFieldBcc:
		m.composeBcc.Focus()
	case composeFieldSubject:
		m.composeSubject.Focus()
	case composeFieldBody:
		m.composeBody.Focus()
	}
	return m
}

func (m App) sendCompose() (tea.Model, tea.Cmd) {
	draft := m.currentDraft()
	cfg := m.selectedAccount
	ctx := m.ctx
	svc := m.composeSvc

	var cmd tea.Cmd
	m, cmd = m.setStatus("Sending...", sevInfo)
	return m, tea.Batch(cmd, func() tea.Msg {
		return sendResultMsg{err: svc.Send(ctx, cfg, draft)}
	})
}

// cancelCompose closes the compose pane. If the draft has changed since it
// was last saved (or, if never saved, since the session started — see
// draftBaseline), it's saved to the Drafts folder first so the work isn't
// lost; the save happens in the background and the pane closes right away
// either way.
func (m App) cancelCompose() (tea.Model, tea.Cmd) {
	current := m.currentDraft()
	dirty := !draftsEqual(current, m.draftBaseline())
	m.composing = false
	m = m.clearStatus()
	if !dirty {
		return m, nil
	}
	return m, m.saveDraftCmd(current, false)
}

// saveDraftNow is ctrl+o: save the draft right now, reporting success or
// failure in the status bar either way (unlike the silent autosave tick).
func (m App) saveDraftNow() (tea.Model, tea.Cmd) {
	if m.draftSaving {
		return m.setStatus("Already saving…", sevInfo)
	}
	m.draftSaving = true
	var cmd tea.Cmd
	m, cmd = m.setStatus("Saving draft…", sevInfo)
	return m, tea.Batch(cmd, m.saveDraftCmd(m.currentDraft(), true))
}

func splitAddrs(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (m App) viewCompose() string {
	if m.attachingFile {
		return m.attachFileInput.View() + "\n" + m.footer()
	}
	fields := lipgloss.JoinVertical(lipgloss.Left,
		m.composeTo.View(),
		m.composeCc.View(),
		m.composeBcc.View(),
		m.composeSubject.View(),
		m.composeBody.View(),
	)
	if line := m.composeAttachmentsLine(); line != "" {
		fields = lipgloss.JoinVertical(lipgloss.Left, fields, line)
	}
	return fields + "\n" + m.footer()
}

// composeAttachmentsLine renders a one-line summary of the files attached
// to the draft being composed, or "" if there are none.
func (m App) composeAttachmentsLine() string {
	if len(m.composeAttachments) == 0 {
		return ""
	}
	parts := make([]string, len(m.composeAttachments))
	for i, a := range m.composeAttachments {
		parts[i] = fmt.Sprintf("%s (%s)", a.Filename, humanSize(int64(len(a.Data))))
	}
	return "Attachments: " + strings.Join(parts, "   ") + "   (ctrl+r: remove last)"
}
