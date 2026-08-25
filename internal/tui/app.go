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
	"strings"
	"time"

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
	"github.com/tomowang/pigeoncli/internal/core/settings"
)

var (
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

	accountSvc  *account.Service
	folderSvc   *folder.Service
	messageSvc  *message.Service
	composeSvc  *compose.Service
	settingsSvc *settings.Service

	width, height int
	focus         focusPane
	status        statusEntry
	statusHistory []statusEntry
	statusGen     int
	showHelp      bool
	showLog       bool

	syncCh       chan folder.Progress
	syncInterval time.Duration

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

func newApp(ctx context.Context, accountSvc *account.Service, folderSvc *folder.Service, messageSvc *message.Service, composeSvc *compose.Service, settingsSvc *settings.Service) App {
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
		ctx:          ctx,
		accountSvc:   accountSvc,
		folderSvc:    folderSvc,
		messageSvc:   messageSvc,
		composeSvc:   composeSvc,
		settingsSvc:  settingsSvc,
		accounts:     accounts,
		folders:      folders,
		messages:     messages,
		viewport:     viewport.New(0, 0),
		syncInterval: defaultSyncInterval,
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
	return tea.Batch(m.loadAccountsCmd(), m.loadSettingsCmd(), tickCmd(m.syncInterval))
}

func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case statusClearMsg:
		if msg.gen == m.statusGen {
			m = m.clearStatus()
		}
		return m, nil

	case accountsLoadedMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("load accounts: %v", msg.err), sevError)
			return m, cmd
		}
		items := make([]list.Item, len(msg.accounts))
		for i, a := range msg.accounts {
			items[i] = accountItem(a)
		}
		m.accounts.SetItems(items)
		if len(msg.accounts) == 0 {
			var cmd tea.Cmd
			m, cmd = m.setStatus("No accounts configured. Add one with `pigeon account add`, then `pigeon sync`.", sevInfo)
			return m, cmd
		}
		m.selectedAccount = msg.accounts[0]
		return m, m.loadFoldersCmd(msg.accounts[0].Slug)

	case foldersLoadedMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("load folders for %s: %v", msg.slug, msg.err), sevError)
			m.folders.SetItems(nil)
			return m, cmd
		}
		items := make([]list.Item, len(msg.folders))
		for i, f := range msg.folders {
			items[i] = folderItem(f)
		}
		m.folders.SetItems(items)
		if len(msg.folders) == 0 {
			m.messages.SetItems(nil)
			m.selectedFolder = ""
			return m, nil
		}
		// Keep the currently selected folder if it still exists (e.g. after
		// a sync refresh); otherwise default to the first folder.
		target := msg.folders[0].Path
		for _, f := range msg.folders {
			if f.Path == m.selectedFolder {
				target = f.Path
				break
			}
		}
		m.selectedFolder = target
		return m, m.loadMessagesCmd(msg.slug, target)

	case messagesLoadedMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("load messages for %s: %v", msg.folderPath, msg.err), sevError)
			m.messages.SetItems(nil)
			return m, cmd
		}
		items := make([]list.Item, len(msg.messages))
		for i, mm := range msg.messages {
			items[i] = messageItem(mm)
		}
		m.messages.SetItems(items)
		return m, nil

	case messageBodyLoadedMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("open message: %v", msg.err), sevError)
			return m, cmd
		}
		m.viewingMsg = &msg.msg
		m.viewBody = msg.body
		m.viewingRaw = false
		m.viewport.SetContent(msg.body.PlainText)
		m.viewport.GotoTop()
		m = m.clearStatus()
		return m, nil

	case sendResultMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("send failed: %v", msg.err), sevError)
			return m, cmd
		}
		m.composing = false
		var cmd tea.Cmd
		m, cmd = m.setStatus("Message sent.", sevSuccess)
		// Service.Send already refreshed the sqlite cache (e.g. a Sent
		// copy); reload folders so this pane's counts reflect that too.
		return m, tea.Batch(cmd, m.loadFoldersCmd(m.selectedAccount.Slug))

	case settingsLoadedMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("load settings: %v", msg.err), sevError)
			return m, cmd
		}
		if msg.settings.SyncInterval > 0 {
			m.syncInterval = msg.settings.SyncInterval
		}
		return m, nil

	case syncTickMsg:
		cmds := []tea.Cmd{tickCmd(m.syncInterval)}
		if m.syncCh == nil && m.selectedAccount.Slug != "" {
			ch := make(chan folder.Progress)
			m.syncCh = ch
			var statusCmd tea.Cmd
			m, statusCmd = m.setStatus("Background sync starting…", sevInfo)
			cmds = append(cmds, statusCmd, m.startSyncCmd(m.selectedAccount, ch), listenSyncProgressCmd(ch))
		}
		return m, tea.Batch(cmds...)

	case syncProgressMsg:
		p := msg.progress
		var cmd tea.Cmd
		if p.Err != nil {
			m, cmd = m.setStatus(fmt.Sprintf("sync %s: %v", p.Path, p.Err), sevError)
		} else {
			m, cmd = m.setStatus(fmt.Sprintf("sync %s: %d messages (%d unread)", p.Path, p.TotalCount, p.UnreadCount), sevInfo)
		}
		return m, tea.Batch(cmd, listenSyncProgressCmd(m.syncCh))

	case syncDoneMsg:
		m.syncCh = nil
		var cmd tea.Cmd
		if msg.err != nil {
			m, cmd = m.setStatus(fmt.Sprintf("sync failed: %v", msg.err), sevError)
		} else {
			m, cmd = m.setStatus("Sync complete.", sevSuccess)
		}
		return m, tea.Batch(cmd, m.loadFoldersCmd(m.selectedAccount.Slug))
	}

	if m.showHelp {
		if key, ok := msg.(tea.KeyMsg); ok {
			if key.String() == "q" || key.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.showHelp = false
		}
		return m, nil
	}

	if m.showLog {
		if key, ok := msg.(tea.KeyMsg); ok {
			if key.String() == "q" || key.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.showLog = false
		}
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
		case "?":
			m.showHelp = true
			return m, nil
		case "L":
			m.showLog = true
			return m, nil
		case "tab":
			m.focus = (m.focus + 1) % 3
			return m, nil
		case "enter":
			return m.openSelection()
		case "c":
			if m.selectedAccount.Slug == "" {
				var cmd tea.Cmd
				m, cmd = m.setStatus("No account selected.", sevInfo)
				return m, cmd
			}
			return m.startNewMessage()
		case "s":
			if m.selectedAccount.Slug == "" {
				var cmd tea.Cmd
				m, cmd = m.setStatus("No account selected.", sevInfo)
				return m, cmd
			}
			if m.syncCh != nil {
				var cmd tea.Cmd
				m, cmd = m.setStatus("Sync already in progress.", sevInfo)
				return m, cmd
			}
			ch := make(chan folder.Progress)
			m.syncCh = ch
			var cmd tea.Cmd
			m, cmd = m.setStatus("Syncing...", sevInfo)
			return m, tea.Batch(cmd, m.startSyncCmd(m.selectedAccount, ch), listenSyncProgressCmd(ch))
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
			var cmd tea.Cmd
			m, cmd = m.setStatus("Loading message...", sevInfo)
			return m, tea.Batch(cmd, m.openMessageCmd(message.Message(item)))
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
	if m.showHelp {
		return m.viewHelp()
	}

	if m.showLog {
		return m.viewLog()
	}

	if m.composing {
		return m.viewCompose()
	}

	if m.viewingMsg != nil {
		header := viewHeaderStyle.Render(fmt.Sprintf("%s — from %s", m.viewingMsg.Subject, m.viewingMsg.FromAddr))
		return header + "\n" + m.viewport.View() + "\n" + statusStyleFor(m.status.sev).Render(m.statusLine())
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
	return row + "\n" + statusStyleFor(m.status.sev).Render(m.statusLine())
}

func (m App) statusLine() string {
	if m.status.text != "" {
		return m.status.text
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
	return "pigeon — tab: switch pane · enter: open · c: compose · s: sync · ?: help · L: log · q: quit"
}

func (m App) viewHelp() string {
	lines := []string{
		"pigeon — keybindings",
		"",
		"Navigation",
		"  tab            switch pane (accounts / folders / messages)",
		"  ↑/↓, j/k       move selection, scroll",
		"  enter          open selection",
		"  c              compose a new message",
		"  s              sync the selected account",
		"  ?              toggle this help",
		"  q, ctrl+c      quit",
		"",
		"Message viewer",
		"  r              reply",
		"  R              reply-all",
		"  t              toggle raw / rendered",
		"  esc            back to message list",
		"",
		"Compose",
		"  tab            next field (To / Cc / Subject / Body)",
		"  ctrl+s         send",
		"  esc            cancel",
		"",
		"Press any key to close.",
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

// Run starts the Bubbletea program. It blocks until the user quits.
func Run(ctx context.Context, accountSvc *account.Service, folderSvc *folder.Service, messageSvc *message.Service, composeSvc *compose.Service, settingsSvc *settings.Service) error {
	p := tea.NewProgram(newApp(ctx, accountSvc, folderSvc, messageSvc, composeSvc, settingsSvc), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}
