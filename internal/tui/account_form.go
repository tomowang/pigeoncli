package tui

import (
	"fmt"
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
	accountFieldAuthType
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

// isSelectField reports whether f is a select (not free-text) field: the
// IMAP/SMTP security choice, or the sign-in method. Select fields don't
// have a live textinput.Model behind them in accountForm — that slot is
// left unused.
func isSelectField(f accountFormField) bool {
	return f == accountFieldIMAPTLS || f == accountFieldSMTPTLS || f == accountFieldAuthType
}

// accountFieldVisible reports whether f should be shown/reachable given the
// form's current auth-type selection: the Password field only applies to
// AuthTypePassword — a Google account authenticates interactively on
// submit instead.
func (m App) accountFieldVisible(f accountFormField) bool {
	if f == accountFieldPassword {
		return accountAuthTypes[m.accountAuthTypeIdx] == account.AuthTypePassword
	}
	return true
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

// accountAuthTypes are the selectable sign-in methods, in cycle order.
var accountAuthTypes = []string{account.AuthTypePassword, account.AuthTypeGoogle}

func authTypeLabel(authType string) string {
	switch authType {
	case account.AuthTypePassword:
		return "Password"
	case account.AuthTypeGoogle:
		return "Google"
	default:
		return authType
	}
}

func authTypeIndex(authType string) int {
	for i, t := range accountAuthTypes {
		if t == authType {
			return i
		}
	}
	return 0
}

// accountAddedMsg reports the outcome of submitting the add-account form.
// cfg is the account as submitted (zero value on failure), used to select it
// and kick off its initial sync once added.
type accountAddedMsg struct {
	slug string
	cfg  account.Account
	err  error
}

// googleAuthURLMsg reports the Google consent-screen URL once
// AuthorizeGoogle determines it, streamed through a channel — the same
// "channel plus a tea.Cmd" pattern sync.go uses for progress — since it
// becomes known partway through the overall sign-in, which itself
// completes (or fails) later as an accountAddedMsg.
type googleAuthURLMsg struct {
	url string
}

// startGoogleAddAccountCmd runs the interactive Google OAuth2 login for a
// new account (see auth.LoginGoogle via account.Service.AuthorizeGoogle),
// pushing the consent URL into urlCh as soon as it's known, then adds the
// account once sign-in completes.
func (m App) startGoogleAddAccountCmd(a account.Account, urlCh chan<- string) tea.Cmd {
	ctx, svc := m.ctx, m.accountSvc
	return func() tea.Msg {
		token, err := svc.AuthorizeGoogle(ctx, func(url string) { urlCh <- url })
		if err != nil {
			return accountAddedMsg{slug: a.Slug, err: fmt.Errorf("google sign-in: %w", err)}
		}
		err = svc.AddGoogle(ctx, a, token)
		a.AuthType = account.AuthTypeGoogle
		return accountAddedMsg{slug: a.Slug, cfg: a, err: err}
	}
}

// listenGoogleAuthURLCmd reads the one-shot consent URL off ch.
func listenGoogleAuthURLCmd(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		url, ok := <-ch
		if !ok {
			return nil
		}
		return googleAuthURLMsg{url: url}
	}
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
	m.accountAuthTypeIdx = authTypeIndex(account.AuthTypePassword)

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
				return m.cycleSelectOption(key.String() == "right"), nil
			}
		case "enter":
			if isSelectField(m.accountFormField) {
				return m.cycleSelectOption(true), nil
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
	next := m.accountFormField
	for i := 0; i < int(accountFieldCount); i++ {
		next = accountFormField((int(next) + dir + int(accountFieldCount)) % int(accountFieldCount))
		if m.accountFieldVisible(next) {
			break
		}
	}
	m.accountFormField = next
	if !isSelectField(m.accountFormField) {
		m.accountForm[m.accountFormField].Focus()
	}
	return m
}

// cycleSelectOption advances (or reverses) whichever select field currently
// has focus: IMAP/SMTP security, or the sign-in method. Switching the
// sign-in method to Google also fills in Gmail's well-known IMAP/SMTP
// settings, so the user doesn't need to look them up.
func (m App) cycleSelectOption(forward bool) App {
	delta := -1
	if forward {
		delta = 1
	}
	switch m.accountFormField {
	case accountFieldIMAPTLS:
		n := len(accountTLSModes)
		m.accountIMAPTLSIdx = (m.accountIMAPTLSIdx + delta + n) % n
	case accountFieldSMTPTLS:
		n := len(accountTLSModes)
		m.accountSMTPTLSIdx = (m.accountSMTPTLSIdx + delta + n) % n
	case accountFieldAuthType:
		n := len(accountAuthTypes)
		m.accountAuthTypeIdx = (m.accountAuthTypeIdx + delta + n) % n
		if accountAuthTypes[m.accountAuthTypeIdx] == account.AuthTypeGoogle {
			imapCfg, smtpCfg := account.GmailServerConfig()
			m.accountForm[accountFieldIMAPHost].SetValue(imapCfg.Host)
			m.accountForm[accountFieldIMAPPort].SetValue(strconv.Itoa(imapCfg.Port))
			m.accountIMAPTLSIdx = tlsModeIndex(imapCfg.TLS)
			m.accountForm[accountFieldSMTPHost].SetValue(smtpCfg.Host)
			m.accountForm[accountFieldSMTPPort].SetValue(strconv.Itoa(smtpCfg.Port))
			m.accountSMTPTLSIdx = tlsModeIndex(smtpCfg.TLS)
		}
	}
	return m
}

// submitAddAccount validates the form, then hands the resulting account off
// to account.Service via a tea.Cmd (those calls touch the OS keyring and
// the config file, and for Google also a network round trip, so none of
// them can run inline in Update).
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

	if accountAuthTypes[m.accountAuthTypeIdx] == account.AuthTypeGoogle {
		urlCh := make(chan string, 1)
		var cmd tea.Cmd
		m, cmd = m.setStatus("Opening your browser to sign in with Google...", sevInfo)
		return m, tea.Batch(cmd, m.startGoogleAddAccountCmd(a, urlCh), listenGoogleAuthURLCmd(urlCh))
	}

	a.AuthType = account.AuthTypePassword
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
		return accountAddedMsg{slug: a.Slug, cfg: a, err: svc.Add(ctx, a, password)}
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

// renderSelectField renders a bordered, label-headed segmented control over
// labels, with the option at idx bracketed. boxWidth is the field box's
// total on-screen width (border included), kept constant regardless of
// focus so rows don't jitter as the user tabs through them.
func (m App) renderSelectField(label string, f accountFormField, labels []string, idx, boxWidth int) string {
	box, labelStyle := m.fieldStyles(m.accountFormField == f)

	opts := make([]string, len(labels))
	for i, lbl := range labels {
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

func (m App) renderTLSSelectField(label string, f accountFormField, idx, boxWidth int) string {
	labels := make([]string, len(accountTLSModes))
	for i, mode := range accountTLSModes {
		labels[i] = tlsModeLabel(mode)
	}
	return m.renderSelectField(label, f, labels, idx, boxWidth)
}

func (m App) viewAddAccount() string {
	header := m.theme.ViewHeader.Render(
		"Add account — tab/shift+tab: field · ←/→: change option · ctrl+s: save · esc: cancel")

	_, boxWidth := m.accountFieldLayout()
	sectionHeader := m.theme.CardHeader.Render

	authLabels := make([]string, len(accountAuthTypes))
	for i, t := range accountAuthTypes {
		authLabels[i] = authTypeLabel(t)
	}

	credentialsRows := []string{
		sectionHeader("Credentials"),
		m.renderSelectField("Sign in with", accountFieldAuthType, authLabels, m.accountAuthTypeIdx, boxWidth),
	}
	if accountAuthTypes[m.accountAuthTypeIdx] == account.AuthTypeGoogle {
		credentialsRows = append(credentialsRows,
			m.theme.CardMeta.Italic(true).Render("Saving (ctrl+s) opens your browser to sign in with Google."))
	} else {
		credentialsRows = append(credentialsRows, m.renderTextField("Password", accountFieldPassword))
	}

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
		m.renderTLSSelectField("Security", accountFieldIMAPTLS, m.accountIMAPTLSIdx, boxWidth),
		"",
		sectionHeader("Outgoing (SMTP)"),
		m.renderTextField("Host", accountFieldSMTPHost),
		m.renderTextField("Port", accountFieldSMTPPort),
		m.renderTLSSelectField("Security", accountFieldSMTPTLS, m.accountSMTPTLSIdx, boxWidth),
		"",
		lipgloss.JoinVertical(lipgloss.Left, credentialsRows...),
	)

	return header + "\n\n" + body + "\n" + m.footer()
}
