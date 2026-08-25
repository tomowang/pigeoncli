package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tomowang/pigeoncli/internal/core/account"
)

// accountFormField identifies one input in the add-account form. Order here
// is also tab order.
type accountFormField int

const (
	accountFieldSlug accountFormField = iota
	accountFieldEmail
	accountFieldDisplayName
	accountFieldUsername
	accountFieldIMAPHost
	accountFieldIMAPPort
	accountFieldIMAPTLS
	accountFieldSMTPHost
	accountFieldSMTPPort
	accountFieldSMTPTLS
	accountFieldPassword
	accountFieldCount
)

// accountAddedMsg reports the outcome of submitting the add-account form.
type accountAddedMsg struct {
	slug string
	err  error
}

// beginAddAccount switches into the add-account form, pre-filled with the
// same defaults `pigeon account add` uses (993/tls for IMAP, 587/starttls
// for SMTP).
func (m App) beginAddAccount() App {
	m.accountForm = [accountFieldCount]textinput.Model{}

	newField := func(prompt, value string) textinput.Model {
		f := textinput.New()
		f.Prompt = prompt
		f.SetValue(value)
		return f
	}

	m.accountForm[accountFieldSlug] = newField("Slug:         ", "")
	m.accountForm[accountFieldEmail] = newField("Email:        ", "")
	m.accountForm[accountFieldDisplayName] = newField("Display name: ", "")
	m.accountForm[accountFieldUsername] = newField("Username:     ", "")
	m.accountForm[accountFieldIMAPHost] = newField("IMAP host:    ", "")
	m.accountForm[accountFieldIMAPPort] = newField("IMAP port:    ", "993")
	m.accountForm[accountFieldIMAPTLS] = newField("IMAP TLS:     ", string(account.TLSModeTLS))
	m.accountForm[accountFieldSMTPHost] = newField("SMTP host:    ", "")
	m.accountForm[accountFieldSMTPPort] = newField("SMTP port:    ", "587")
	m.accountForm[accountFieldSMTPTLS] = newField("SMTP TLS:     ", string(account.TLSModeSTARTTLS))

	pw := newField("Password:     ", "")
	pw.EchoMode = textinput.EchoPassword
	pw.EchoCharacter = '•'
	m.accountForm[accountFieldPassword] = pw

	m.addingAccount = true
	m.accountFormField = accountFieldSlug
	m.accountForm[accountFieldSlug].Focus()
	m = m.clearStatus()
	m.layout()
	return m
}

// updateAddingAccount handles input while the add-account form is active.
// Global keys (submit/cancel/next-field) are intercepted first; everything
// else goes to whichever field currently has focus.
func (m App) updateAddingAccount(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			m.addingAccount = false
			m = m.clearStatus()
			return m, nil
		case "tab":
			return m.cycleAccountFormField(1), nil
		case "shift+tab":
			return m.cycleAccountFormField(-1), nil
		case "ctrl+s":
			return m.submitAddAccount()
		}
	}

	var cmd tea.Cmd
	m.accountForm[m.accountFormField], cmd = m.accountForm[m.accountFormField].Update(msg)
	return m, cmd
}

func (m App) cycleAccountFormField(dir int) App {
	m.accountForm[m.accountFormField].Blur()
	m.accountFormField = accountFormField((int(m.accountFormField) + dir + int(accountFieldCount)) % int(accountFieldCount))
	m.accountForm[m.accountFormField].Focus()
	return m
}

// submitAddAccount validates the form, then hands the resulting account and
// password off to account.Service.Add via a tea.Cmd (that call touches the
// OS keyring and the config file, so it can't run inline in Update).
func (m App) submitAddAccount() (tea.Model, tea.Cmd) {
	imapPort, err := strconv.Atoi(strings.TrimSpace(m.accountForm[accountFieldIMAPPort].Value()))
	if err != nil {
		var cmd tea.Cmd
		m, cmd = m.setStatus("IMAP port must be a number.", sevError)
		return m, cmd
	}
	smtpPort, err := strconv.Atoi(strings.TrimSpace(m.accountForm[accountFieldSMTPPort].Value()))
	if err != nil {
		var cmd tea.Cmd
		m, cmd = m.setStatus("SMTP port must be a number.", sevError)
		return m, cmd
	}

	username := strings.TrimSpace(m.accountForm[accountFieldUsername].Value())
	email := strings.TrimSpace(m.accountForm[accountFieldEmail].Value())
	if username == "" {
		username = email
	}

	a := account.Account{
		Slug:        strings.TrimSpace(m.accountForm[accountFieldSlug].Value()),
		Email:       email,
		DisplayName: strings.TrimSpace(m.accountForm[accountFieldDisplayName].Value()),
		Username:    username,
		AuthType:    "password",
		IMAP: account.ServerConfig{
			Host: strings.TrimSpace(m.accountForm[accountFieldIMAPHost].Value()),
			Port: imapPort,
			TLS:  account.TLSMode(strings.TrimSpace(m.accountForm[accountFieldIMAPTLS].Value())),
		},
		SMTP: account.ServerConfig{
			Host: strings.TrimSpace(m.accountForm[accountFieldSMTPHost].Value()),
			Port: smtpPort,
			TLS:  account.TLSMode(strings.TrimSpace(m.accountForm[accountFieldSMTPTLS].Value())),
		},
	}
	if err := account.Validate(a); err != nil {
		var cmd tea.Cmd
		m, cmd = m.setStatus(err.Error(), sevError)
		return m, cmd
	}

	password := m.accountForm[accountFieldPassword].Value()
	if password == "" {
		var cmd tea.Cmd
		m, cmd = m.setStatus("Password is required.", sevError)
		return m, cmd
	}

	ctx, svc := m.ctx, m.accountSvc
	var cmd tea.Cmd
	m, cmd = m.setStatus("Adding account...", sevInfo)
	return m, tea.Batch(cmd, func() tea.Msg {
		return accountAddedMsg{slug: a.Slug, err: svc.Add(ctx, a, password)}
	})
}

func (m App) viewAddAccount() string {
	header := m.theme.ViewHeader.Render("Add account — tab/shift+tab: next/prev field · ctrl+s: save · esc: cancel")
	fields := make([]string, accountFieldCount)
	for i, f := range m.accountForm {
		fields[i] = f.View()
	}
	body := header + "\n\n" + lipgloss.JoinVertical(lipgloss.Left, fields...)
	return body + "\n" + m.footer()
}
