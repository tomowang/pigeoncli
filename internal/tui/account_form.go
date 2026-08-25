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

// maxAccountFormWidth caps how wide the add-account form's field boxes and
// select control ever get, even on an ultra-wide terminal. Past this a
// single-column stacked form starts to look sparse and the eye has to
// travel too far from label to input; 72 columns keeps every row roughly
// paragraph-width, matching the field's Prompt line above it.
const maxAccountFormWidth = 72

// accountFieldHints holds the small helper line shown under a field's box,
// keyed by field. Only fields that benefit from one have an entry.
var accountFieldHints = map[accountFormField]string{
	accountFieldUsername: "Leave blank to use the email address above.",
}

// isSelectField reports whether f is a select (not free-text) field, i.e.
// the IMAP/SMTP security choice. Select fields don't have a live
// textinput.Model behind them in accountForm — that slot is left unused.
func isSelectField(f accountFormField) bool {
	return f == accountFieldIMAPTLS || f == accountFieldSMTPTLS
}

// accountTLSModes are the selectable security options for the IMAP/SMTP
// select fields, in cycle order.
var accountTLSModes = []account.TLSMode{account.TLSModeTLS, account.TLSModeSTARTTLS, account.TLSModeNone}

func tlsModeLabel(mode account.TLSMode) string {
	switch mode {
	case account.TLSModeTLS:
		return "TLS"
	case account.TLSModeSTARTTLS:
		return "STARTTLS"
	case account.TLSModeNone:
		return "None"
	default:
		return string(mode)
	}
}

func tlsModeIndex(mode account.TLSMode) int {
	for i, opt := range accountTLSModes {
		if opt == mode {
			return i
		}
	}
	return 0
}

// accountAddedMsg reports the outcome of submitting the add-account form.
type accountAddedMsg struct {
	slug string
	err  error
}

// beginAddAccount switches into the add-account form, pre-filled with
// sensible defaults: 993/TLS for IMAP, 465/TLS (implicit TLS/SSL) for SMTP
// — the common case for mailbox providers today, with STARTTLS/none still
// selectable for servers that need them.
func (m App) beginAddAccount() App {
	m.accountForm = [accountFieldCount]textinput.Model{}

	newField := func(value string) textinput.Model {
		f := textinput.New()
		f.Prompt = ""
		f.SetValue(value)
		return f
	}

	m.accountForm[accountFieldSlug] = newField("")
	m.accountForm[accountFieldEmail] = newField("")
	m.accountForm[accountFieldDisplayName] = newField("")
	m.accountForm[accountFieldUsername] = newField("")
	m.accountForm[accountFieldIMAPHost] = newField("")
	m.accountForm[accountFieldIMAPPort] = newField("993")
	m.accountForm[accountFieldSMTPHost] = newField("")
	m.accountForm[accountFieldSMTPPort] = newField("465")

	pw := newField("")
	pw.EchoMode = textinput.EchoPassword
	pw.EchoCharacter = '•'
	m.accountForm[accountFieldPassword] = pw

	m.accountIMAPTLSIdx = tlsModeIndex(account.TLSModeTLS)
	m.accountSMTPTLSIdx = tlsModeIndex(account.TLSModeTLS)

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
		case "left", "right":
			if isSelectField(m.accountFormField) {
				return m.cycleTLSOption(key.String() == "right"), nil
			}
		case "enter":
			if isSelectField(m.accountFormField) {
				return m.cycleTLSOption(true), nil
			}
		}
	}

	if isSelectField(m.accountFormField) {
		return m, nil
	}

	var cmd tea.Cmd
	m.accountForm[m.accountFormField], cmd = m.accountForm[m.accountFormField].Update(msg)
	return m, cmd
}

func (m App) cycleAccountFormField(dir int) App {
	if !isSelectField(m.accountFormField) {
		m.accountForm[m.accountFormField].Blur()
	}
	m.accountFormField = accountFormField((int(m.accountFormField) + dir + int(accountFieldCount)) % int(accountFieldCount))
	if !isSelectField(m.accountFormField) {
		m.accountForm[m.accountFormField].Focus()
	}
	return m
}

// cycleTLSOption advances (or reverses) the security select field that
// currently has focus.
func (m App) cycleTLSOption(forward bool) App {
	delta := -1
	if forward {
		delta = 1
	}
	n := len(accountTLSModes)
	switch m.accountFormField {
	case accountFieldIMAPTLS:
		m.accountIMAPTLSIdx = (m.accountIMAPTLSIdx + delta + n) % n
	case accountFieldSMTPTLS:
		m.accountSMTPTLSIdx = (m.accountSMTPTLSIdx + delta + n) % n
	}
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
			TLS:  accountTLSModes[m.accountIMAPTLSIdx],
		},
		SMTP: account.ServerConfig{
			Host: strings.TrimSpace(m.accountForm[accountFieldSMTPHost].Value()),
			Port: smtpPort,
			TLS:  accountTLSModes[m.accountSMTPTLSIdx],
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

// accountFieldLayout computes the textinput content width and the field
// box's total on-screen width (border included) for the current terminal
// width. Every field — text or select — is one line wide and shares this
// same box width, capped at maxAccountFormWidth.
func (m App) accountFieldLayout() (inputWidth, boxWidth int) {
	// A field box is a rounded border (2 cols) + 1-col padding each side (2
	// cols) around a textinput.View(), which itself always renders its
	// Width plus one extra column reserved for the cursor. So a box's total
	// on-screen width is (input.Width + 1) + 4, i.e. input.Width = boxWidth
	// - 5.
	const boxOverhead = 5

	avail := max(m.width-4, 20)
	boxWidth = min(avail, maxAccountFormWidth)
	inputWidth = max(boxWidth-boxOverhead, 8)
	return
}

// fieldStyles returns the label/box style pair for a field, based on
// whether it currently has focus.
func (m App) fieldStyles(focused bool) (box, label lipgloss.Style) {
	if focused {
		return m.theme.ActivePane, m.theme.CardHeader
	}
	return m.theme.InactivePane, m.theme.CardMeta
}

func (m App) renderTextField(label string, f accountFormField) string {
	box, labelStyle := m.fieldStyles(m.accountFormField == f)
	rows := []string{
		labelStyle.Render(label),
		box.Padding(0, 1).Render(m.accountForm[f].View()),
	}
	if hint, ok := accountFieldHints[f]; ok {
		rows = append(rows, m.theme.CardMeta.Italic(true).Render(hint))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderSelectField renders a bordered, label-headed segmented control for
// a TLS security choice, with the active option bracketed. boxWidth is the
// field box's total on-screen width (border included), kept constant
// regardless of focus so rows don't jitter as the user tabs through them.
func (m App) renderSelectField(label string, f accountFormField, idx, boxWidth int) string {
	box, labelStyle := m.fieldStyles(m.accountFormField == f)

	opts := make([]string, len(accountTLSModes))
	for i, mode := range accountTLSModes {
		lbl := tlsModeLabel(mode)
		if i == idx {
			opts[i] = m.theme.CardHeader.Render("[" + lbl + "]")
		} else {
			opts[i] = m.theme.CardMeta.Render(" " + lbl + " ")
		}
	}
	content := strings.Join(opts, "   ")

	return lipgloss.JoinVertical(lipgloss.Left,
		labelStyle.Render(label),
		box.Padding(0, 1).Width(boxWidth-2).Render(content),
	)
}

func (m App) viewAddAccount() string {
	header := m.theme.ViewHeader.Render(
		"Add account — tab/shift+tab: field · ←/→: change security · ctrl+s: save · esc: cancel")

	_, boxWidth := m.accountFieldLayout()
	sectionHeader := m.theme.CardHeader.Render

	body := lipgloss.JoinVertical(lipgloss.Left,
		sectionHeader("Account"),
		m.renderTextField("Slug", accountFieldSlug),
		m.renderTextField("Email", accountFieldEmail),
		m.renderTextField("Display name", accountFieldDisplayName),
		m.renderTextField("Username", accountFieldUsername),
		"",
		sectionHeader("Incoming (IMAP)"),
		m.renderTextField("Host", accountFieldIMAPHost),
		m.renderTextField("Port", accountFieldIMAPPort),
		m.renderSelectField("Security", accountFieldIMAPTLS, m.accountIMAPTLSIdx, boxWidth),
		"",
		sectionHeader("Outgoing (SMTP)"),
		m.renderTextField("Host", accountFieldSMTPHost),
		m.renderTextField("Port", accountFieldSMTPPort),
		m.renderSelectField("Security", accountFieldSMTPTLS, m.accountSMTPTLSIdx, boxWidth),
		"",
		sectionHeader("Credentials"),
		m.renderTextField("Password", accountFieldPassword),
	)

	return header + "\n\n" + body + "\n" + m.footer()
}
