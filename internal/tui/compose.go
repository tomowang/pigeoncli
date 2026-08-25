package tui

import (
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
	return m.enterCompose(draft, composeFieldBody), nil
}

// startNewMessage builds an empty draft (just the account's default
// signature, if any) and switches into the compose pane. Focus starts on
// To, since there's nothing pre-filled to edit.
func (m App) startNewMessage() (tea.Model, tea.Cmd) {
	draft := m.composeSvc.NewMessage(m.ctx, m.selectedAccount)
	return m.enterCompose(draft, composeFieldTo), nil
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

	m.composeSubject = textinput.New()
	m.composeSubject.Prompt = "Subject: "
	m.composeSubject.SetValue(draft.Subject)

	m.composeBody = textarea.New()
	m.composeBody.SetValue(draft.Body)

	m.composeInReplyTo = draft.InReplyTo
	m.composeReferences = draft.References

	m.composing = true
	m = m.clearStatus()
	m.composeField = field
	switch field {
	case composeFieldTo:
		m.composeTo.Focus()
	case composeFieldCc:
		m.composeCc.Focus()
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
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			m.composing = false
			m = m.clearStatus()
			return m, nil
		case "tab":
			return m.cycleComposeField(), nil
		case "ctrl+s":
			return m.sendCompose()
		}
	}

	var cmd tea.Cmd
	switch m.composeField {
	case composeFieldTo:
		m.composeTo, cmd = m.composeTo.Update(msg)
	case composeFieldCc:
		m.composeCc, cmd = m.composeCc.Update(msg)
	case composeFieldSubject:
		m.composeSubject, cmd = m.composeSubject.Update(msg)
	case composeFieldBody:
		m.composeBody, cmd = m.composeBody.Update(msg)
	}
	return m, cmd
}

func (m App) cycleComposeField() App {
	m.composeTo.Blur()
	m.composeCc.Blur()
	m.composeSubject.Blur()
	m.composeBody.Blur()

	m.composeField = (m.composeField + 1) % 4
	switch m.composeField {
	case composeFieldTo:
		m.composeTo.Focus()
	case composeFieldCc:
		m.composeCc.Focus()
	case composeFieldSubject:
		m.composeSubject.Focus()
	case composeFieldBody:
		m.composeBody.Focus()
	}
	return m
}

func (m App) sendCompose() (tea.Model, tea.Cmd) {
	draft := compose.Draft{
		To:         splitAddrs(m.composeTo.Value()),
		Cc:         splitAddrs(m.composeCc.Value()),
		Subject:    m.composeSubject.Value(),
		Body:       m.composeBody.Value(),
		InReplyTo:  m.composeInReplyTo,
		References: m.composeReferences,
	}
	cfg := m.selectedAccount
	ctx := m.ctx
	svc := m.composeSvc

	var cmd tea.Cmd
	m, cmd = m.setStatus("Sending...", sevInfo)
	return m, tea.Batch(cmd, func() tea.Msg {
		return sendResultMsg{err: svc.Send(ctx, cfg, draft)}
	})
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
	fields := lipgloss.JoinVertical(lipgloss.Left,
		m.composeTo.View(),
		m.composeCc.View(),
		m.composeSubject.View(),
		m.composeBody.View(),
	)
	return fields + "\n" + m.theme.StatusStyle(m.status.sev).Render(m.statusLine())
}
