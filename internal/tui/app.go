// Package tui implements the Bubbletea-based terminal UI for pigeon.
//
// The top-level App model composes sub-models (account list, folder tree,
// message list, message view, compose, status bar) and depends only on the
// internal/core service layer — never on internal/imap, internal/smtp, or
// internal/storage directly. This keeps the TUI swappable for a future CLI
// or HTTP frontend without touching business logic.
package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tomowang/pigeoncli/internal/core/account"
	"github.com/tomowang/pigeoncli/internal/core/compose"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

var (
	statusBarStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62"))

	activePaneStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62"))
	inactivePaneStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))

	viewHeaderStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1)
)

type focusPane int

const (
	focusAccounts focusPane = iota
	focusFolders
	focusMessages
)

// composeField identifies which widget in the compose pane has focus.
type composeField int

const (
	composeFieldTo composeField = iota
	composeFieldCc
	composeFieldSubject
	composeFieldBody
)

// App is the root Bubbletea model: an account list, a folder tree pane, and
// a message list pane side by side, plus a status bar. Opening a message
// replaces that row with a full-width raw/rendered viewer; replying from
// there replaces it with a compose pane (To/Cc/Subject inputs over a body
// textarea).
//
// Update has no context parameter (that's the Elm architecture's shape),
// but the account/folder/message/compose service calls triggered from
// tea.Cmd closures still need one — so the ctx passed to Run is stored on
// the model and reused by every tea.Cmd it builds.
type App struct {
	ctx context.Context

	accountSvc *account.Service
	folderSvc  *folder.Service
	messageSvc *message.Service
	composeSvc *compose.Service

	width, height int
	focus         focusPane
	status        string

	accounts list.Model
	folders  list.Model
	messages list.Model

	selectedAccount account.Account
	selectedFolder  string

	viewingMsg *message.Message
	viewingRaw bool
	viewBody   message.Body
	viewport   viewport.Model

	composing         bool
	composeField      composeField
	composeTo         textinput.Model
	composeCc         textinput.Model
	composeSubject    textinput.Model
	composeBody       textarea.Model
	composeInReplyTo  string
	composeReferences string
}

func newApp(ctx context.Context, accountSvc *account.Service, folderSvc *folder.Service, messageSvc *message.Service, composeSvc *compose.Service) App {
	accounts := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	accounts.Title = "Accounts"
	accounts.SetShowHelp(false)

	folders := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	folders.Title = "Folders"
	folders.SetShowHelp(false)

	messages := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	messages.Title = "Messages"
	messages.SetShowHelp(false)

	return App{
		ctx:        ctx,
		accountSvc: accountSvc,
		folderSvc:  folderSvc,
		messageSvc: messageSvc,
		composeSvc: composeSvc,
		accounts:   accounts,
		folders:    folders,
		messages:   messages,
		viewport:   viewport.New(0, 0),
	}
}

type accountsLoadedMsg struct {
	accounts []account.Account
	err      error
}

type foldersLoadedMsg struct {
	slug    string
	folders []folder.Folder
	err     error
}

type messagesLoadedMsg struct {
	accountSlug string
	folderPath  string
	messages    []message.Message
	err         error
}

type messageBodyLoadedMsg struct {
	msg  message.Message
	body message.Body
	err  error
}

type sendResultMsg struct {
	err error
}

func (m App) loadAccountsCmd() tea.Cmd {
	return func() tea.Msg {
		accounts, err := m.accountSvc.List(m.ctx)
		return accountsLoadedMsg{accounts: accounts, err: err}
	}
}

func (m App) loadFoldersCmd(slug string) tea.Cmd {
	return func() tea.Msg {
		folders, err := m.folderSvc.List(m.ctx, slug)
		return foldersLoadedMsg{slug: slug, folders: folders, err: err}
	}
}

func (m App) loadMessagesCmd(accountSlug, folderPath string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := m.messageSvc.List(m.ctx, accountSlug, folderPath)
		return messagesLoadedMsg{accountSlug: accountSlug, folderPath: folderPath, messages: msgs, err: err}
	}
}

func (m App) openMessageCmd(msg message.Message) tea.Cmd {
	cfg := m.selectedAccount
	folderPath := m.selectedFolder
	return func() tea.Msg {
		body, err := m.messageSvc.Body(m.ctx, cfg, folderPath, msg.UID)
		return messageBodyLoadedMsg{msg: msg, body: body, err: err}
	}
}

func (m App) Init() tea.Cmd {
	return m.loadAccountsCmd()
}

func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case accountsLoadedMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("load accounts: %v", msg.err)
			return m, nil
		}
		items := make([]list.Item, len(msg.accounts))
		for i, a := range msg.accounts {
			items[i] = accountItem(a)
		}
		m.accounts.SetItems(items)
		if len(msg.accounts) == 0 {
			m.status = "No accounts configured. Add one with `pigeon account add`, then `pigeon sync`."
			return m, nil
		}
		m.selectedAccount = msg.accounts[0]
		return m, m.loadFoldersCmd(msg.accounts[0].Slug)

	case foldersLoadedMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("load folders for %s: %v", msg.slug, msg.err)
			m.folders.SetItems(nil)
			return m, nil
		}
		items := make([]list.Item, len(msg.folders))
		for i, f := range msg.folders {
			items[i] = folderItem(f)
		}
		m.folders.SetItems(items)
		m.status = ""
		if len(msg.folders) == 0 {
			m.messages.SetItems(nil)
			return m, nil
		}
		m.selectedFolder = msg.folders[0].Path
		return m, m.loadMessagesCmd(msg.slug, msg.folders[0].Path)

	case messagesLoadedMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("load messages for %s: %v", msg.folderPath, msg.err)
			m.messages.SetItems(nil)
			return m, nil
		}
		items := make([]list.Item, len(msg.messages))
		for i, mm := range msg.messages {
			items[i] = messageItem(mm)
		}
		m.messages.SetItems(items)
		m.status = ""
		return m, nil

	case messageBodyLoadedMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("open message: %v", msg.err)
			return m, nil
		}
		m.viewingMsg = &msg.msg
		m.viewBody = msg.body
		m.viewingRaw = false
		m.viewport.SetContent(msg.body.PlainText)
		m.viewport.GotoTop()
		return m, nil

	case sendResultMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("send failed: %v", msg.err)
			return m, nil
		}
		m.composing = false
		m.status = "Message sent."
		return m, nil
	}

	if m.composing {
		return m.updateComposing(msg)
	}

	if m.viewingMsg != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			return m.updateViewing(key)
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.focus = (m.focus + 1) % 3
			return m, nil
		case "enter":
			return m.openSelection()
		}
	}

	var cmd tea.Cmd
	switch m.focus {
	case focusAccounts:
		m.accounts, cmd = m.accounts.Update(msg)
	case focusFolders:
		m.folders, cmd = m.folders.Update(msg)
	case focusMessages:
		m.messages, cmd = m.messages.Update(msg)
	}
	return m, cmd
}

func (m App) openSelection() (tea.Model, tea.Cmd) {
	switch m.focus {
	case focusAccounts:
		if item, ok := m.accounts.SelectedItem().(accountItem); ok {
			m.selectedAccount = account.Account(item)
			return m, m.loadFoldersCmd(item.Slug)
		}
	case focusFolders:
		if item, ok := m.folders.SelectedItem().(folderItem); ok {
			m.selectedFolder = item.Path
			return m, m.loadMessagesCmd(m.selectedAccount.Slug, item.Path)
		}
	case focusMessages:
		if item, ok := m.messages.SelectedItem().(messageItem); ok {
			m.status = "Loading message..."
			return m, m.openMessageCmd(message.Message(item))
		}
	}
	return m, nil
}

func (m App) updateViewing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "backspace":
		m.viewingMsg = nil
		return m, nil
	case "t":
		m.viewingRaw = !m.viewingRaw
		if m.viewingRaw {
			m.viewport.SetContent(string(m.viewBody.Raw))
		} else {
			m.viewport.SetContent(m.viewBody.PlainText)
		}
		m.viewport.GotoTop()
		return m, nil
	case "r":
		return m.startReply(false)
	case "R":
		return m.startReply(true)
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// layout recomputes each pane's size from the current window size. Each
// list pane is wrapped in a bordered box (see View), which costs 2 columns
// and 2 rows, so that's subtracted from what's handed to the list widgets.
func (m *App) layout() {
	const borderWidth, borderHeight = 2, 2

	accountsWidth := max(m.width/5, 16)
	foldersWidth := max(m.width/5, 16)
	messagesWidth := max(m.width-accountsWidth-foldersWidth, 16)
	paneHeight := m.height - 1 // reserve the status bar row

	m.accounts.SetSize(max(accountsWidth-borderWidth, 1), max(paneHeight-borderHeight, 1))
	m.folders.SetSize(max(foldersWidth-borderWidth, 1), max(paneHeight-borderHeight, 1))
	m.messages.SetSize(max(messagesWidth-borderWidth, 1), max(paneHeight-borderHeight, 1))

	m.viewport.Width = m.width
	m.viewport.Height = max(m.height-2, 1) // reserve the header and status bar rows

	if m.composing {
		m.composeTo.Width = max(m.width-4, 10)
		m.composeCc.Width = max(m.width-4, 10)
		m.composeSubject.Width = max(m.width-4, 10)
		m.composeBody.SetWidth(max(m.width-2, 10))
		m.composeBody.SetHeight(max(m.height-5, 3)) // reserve To/Cc/Subject + status bar
	}
}

func (m App) View() string {
	if m.composing {
		return m.viewCompose()
	}

	if m.viewingMsg != nil {
		header := viewHeaderStyle.Render(fmt.Sprintf("%s — from %s", m.viewingMsg.Subject, m.viewingMsg.FromAddr))
		return header + "\n" + m.viewport.View() + "\n" + statusBarStyle.Render(m.statusLine())
	}

	accountsStyle, foldersStyle, messagesStyle := inactivePaneStyle, inactivePaneStyle, inactivePaneStyle
	switch m.focus {
	case focusAccounts:
		accountsStyle = activePaneStyle
	case focusFolders:
		foldersStyle = activePaneStyle
	case focusMessages:
		messagesStyle = activePaneStyle
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top,
		accountsStyle.Render(m.accounts.View()),
		foldersStyle.Render(m.folders.View()),
		messagesStyle.Render(m.messages.View()),
	)
	return row + "\n" + statusBarStyle.Render(m.statusLine())
}

func (m App) statusLine() string {
	if m.status != "" {
		return m.status
	}
	if m.composing {
		return "pigeon — compose — tab: next field · ctrl+s: send · esc: cancel"
	}
	if m.viewingMsg != nil {
		mode := "rendered"
		if m.viewingRaw {
			mode = "raw"
		}
		return fmt.Sprintf("pigeon — viewing (%s) — r: reply · R: reply-all · t: toggle raw · esc: back · q: quit", mode)
	}
	return "pigeon — tab: switch pane · enter: open · q: quit"
}

// Run starts the Bubbletea program. It blocks until the user quits.
func Run(ctx context.Context, accountSvc *account.Service, folderSvc *folder.Service, messageSvc *message.Service, composeSvc *compose.Service) error {
	p := tea.NewProgram(newApp(ctx, accountSvc, folderSvc, messageSvc, composeSvc), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}
