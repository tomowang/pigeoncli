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
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
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
	"github.com/tomowang/pigeoncli/internal/logo"
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
// swaps the accounts/folders panes for a card-style preview panel on the
// right of the message list (still raw/rendered toggleable); replying from
// there replaces the whole screen with a compose pane (To/Cc/Subject inputs
// over a body textarea).
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

	theme Theme

	width, height int
	focus         focusPane
	status        statusEntry
	statusHistory []statusEntry
	statusGen     int
	showHelp      bool
	showLog       bool
	quitting      bool
	lastCtrlC     time.Time

	syncCh            chan folder.Progress
	syncInterval      time.Duration
	initialSyncWindow int
	syncSpinner       spinner.Model
	lastSyncAt        time.Time

	// idle* track the IMAP IDLE watcher for the currently selected
	// account/folder, which pushes idleUpdateMsg to trigger an early sync
	// instead of waiting for the next syncTickMsg. idleCancel stops the
	// watcher (e.g. when the selection changes); idleAccount/idleFolder
	// record what it's currently watching, so a stray idleStoppedMsg from
	// a superseded watcher can be told apart from a real failure.
	idleCh      chan struct{}
	idleCancel  context.CancelFunc
	idleAccount string
	idleFolder  string

	// busy marks a one-off network op (opening a message, jumping to related
	// messages) as in flight, so the status bar keeps showing a spinner for
	// it instead of the status text auto-clearing after a few seconds while
	// the fetch is still running — see startBusy/stopBusy.
	busy bool

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

	searching         bool
	searchInput       textinput.Model
	showSearchResults bool
	searchResultsList list.Model

	pickingAttachment   bool
	attachmentPicker    list.Model
	savingAttachment    bool
	saveAttachmentInput textinput.Model
	pendingAttachment   message.Attachment

	addingAccount      bool
	accountForm        [accountFieldCount]textinput.Model
	accountFormField   accountFormField
	accountIMAPTLSIdx  int
	accountSMTPTLSIdx  int
	accountAuthTypeIdx int
}

func newApp(ctx context.Context, accountSvc *account.Service, folderSvc *folder.Service, messageSvc *message.Service, composeSvc *compose.Service, settingsSvc *settings.Service) App {
	// DisableQuitKeybindings on every list: by default bubbles/list binds
	// "q"/"esc" (Quit) and "ctrl+c" (ForceQuit) to return tea.Quit straight
	// from the widget's own Update, which would exit the program immediately
	// and skip App's quit-confirm prompt entirely. All quit handling is done
	// at the App level instead (see handleCtrlC and the "quitting" state).
	accounts := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	accounts.Title = "Accounts"
	accounts.SetShowHelp(false)
	accounts.DisableQuitKeybindings()

	folders := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	folders.Title = "Folders"
	folders.SetShowHelp(false)
	folders.DisableQuitKeybindings()

	messages := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	messages.Title = "Messages"
	messages.SetShowHelp(false)
	messages.DisableQuitKeybindings()

	searchResultsList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	searchResultsList.Title = "Search results"
	searchResultsList.SetShowHelp(false)
	searchResultsList.DisableQuitKeybindings()

	attachmentPicker := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	attachmentPicker.Title = "Attachments"
	attachmentPicker.SetShowHelp(false)
	attachmentPicker.DisableQuitKeybindings()

	defaultTheme, _ := themeByName("")

	syncSpinner := spinner.New()
	syncSpinner.Spinner = spinner.Dot

	return App{
		ctx:               ctx,
		accountSvc:        accountSvc,
		folderSvc:         folderSvc,
		messageSvc:        messageSvc,
		composeSvc:        composeSvc,
		settingsSvc:       settingsSvc,
		theme:             defaultTheme,
		accounts:          accounts,
		folders:           folders,
		messages:          messages,
		searchResultsList: searchResultsList,
		attachmentPicker:  attachmentPicker,
		viewport:          viewport.New(0, 0),
		syncInterval:      defaultSyncInterval,
		initialSyncWindow: defaultInitialSyncWindow,
		syncSpinner:       syncSpinner,
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

// moveMessageResultMsg reports the outcome of moveToJunkCmd/moveToInboxCmd.
// toJunk distinguishes which direction happened, for status text/logging.
type moveMessageResultMsg struct {
	folderPath string
	toJunk     bool
	err        error
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

func (m App) moveToJunkCmd(uid uint32) tea.Cmd {
	cfg := m.selectedAccount
	folderPath := m.selectedFolder
	return func() tea.Msg {
		err := m.messageSvc.MoveToJunk(m.ctx, cfg, folderPath, uid)
		return moveMessageResultMsg{folderPath: folderPath, toJunk: true, err: err}
	}
}

func (m App) moveToInboxCmd(uid uint32) tea.Cmd {
	cfg := m.selectedAccount
	folderPath := m.selectedFolder
	return func() tea.Msg {
		err := m.messageSvc.MoveToInbox(m.ctx, cfg, folderPath, uid)
		return moveMessageResultMsg{folderPath: folderPath, toJunk: false, err: err}
	}
}

// currentFolderSpecialUse returns the RFC 6154 special-use attribute of the
// currently selected folder (e.g. `\Junk`), or "" if unknown/none — used to
// make the "!" key context-sensitive (report spam vs. undo a spam report).
func (m App) currentFolderSpecialUse() string {
	for _, it := range m.folders.Items() {
		if f, ok := it.(folderItem); ok && f.Path == m.selectedFolder {
			return f.SpecialUse
		}
	}
	return ""
}

// toggleSpamCmd picks the spam direction based on the currently selected
// folder: moving to Junk from anywhere else, or back to the Inbox when
// already viewing Junk. It returns the busy-status text to show alongside
// the resulting tea.Cmd.
func (m App) toggleSpamCmd(uid uint32) (busyText string, cmd tea.Cmd) {
	if m.currentFolderSpecialUse() == `\Junk` {
		return "Marking as not spam...", m.moveToInboxCmd(uid)
	}
	return "Marking as spam...", m.moveToJunkCmd(uid)
}

func (m App) Init() tea.Cmd {
	return tea.Batch(m.loadAccountsCmd(), m.loadSettingsCmd(), tickCmd(m.syncInterval))
}

// ctrlCQuitWindow is how soon a second ctrl+c must follow the first to count
// as a double-tap. A single ctrl+c behaves like "q" (opens the confirm
// prompt); a second one within this window quits immediately, bypassing the
// prompt — the conventional shell escape hatch for "no really, quit now".
const ctrlCQuitWindow = time.Second

// handleCtrlC implements that double-tap: it quits immediately if the
// previous ctrl+c was recent enough, otherwise it records this press and
// opens the confirm prompt like "q" would.
func (m App) handleCtrlC() (App, tea.Cmd) {
	now := time.Now()
	if !m.lastCtrlC.IsZero() && now.Sub(m.lastCtrlC) < ctrlCQuitWindow {
		return m, tea.Quit
	}
	m.lastCtrlC = now
	m.quitting = true
	return m, nil
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
			slog.Error("load accounts failed", "err", msg.err)
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
		// Keep the currently selected account if it still exists (e.g.
		// after adding another account); otherwise default to the first.
		target := msg.accounts[0]
		for _, a := range msg.accounts {
			if a.Slug == m.selectedAccount.Slug {
				target = a
				break
			}
		}
		m.selectedAccount = target
		return m, m.loadFoldersCmd(target.Slug)

	case foldersLoadedMsg:
		if msg.err != nil {
			slog.Error("load folders failed", "account", msg.slug, "err", msg.err)
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
			slog.Error("load messages failed", "folder", msg.folderPath, "err", msg.err)
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
		var idleCmd tea.Cmd
		m, idleCmd = m.restartIdleCmd(m.selectedAccount, msg.folderPath)
		return m, idleCmd

	case idleUpdateMsg:
		cmds := []tea.Cmd{listenIdleCmd(m.idleCh)}
		if m.syncCh == nil && m.selectedAccount.Slug != "" {
			ch := make(chan folder.Progress)
			m.syncCh = ch
			var statusCmd tea.Cmd
			m, statusCmd = m.setStatus("Change detected, syncing…", sevInfo)
			cmds = append(cmds, statusCmd, m.startSyncCmd(m.selectedAccount, ch), listenSyncProgressCmd(ch), m.syncSpinner.Tick)
		}
		return m, tea.Batch(cmds...)

	case idleStoppedMsg:
		// A watcher stopping because it was superseded (account/folder
		// switch already replaced it) is expected and silent; only
		// surface it when it's still the one currently in effect.
		if msg.accountSlug != m.idleAccount || msg.folderPath != m.idleFolder {
			return m, nil
		}
		m.idleCancel = nil
		m.idleCh = nil
		if msg.err != nil {
			slog.Warn("live updates unavailable", "folder", msg.folderPath, "err", msg.err)
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("live updates unavailable for %s: %v", msg.folderPath, msg.err), sevInfo)
			return m, cmd
		}
		return m, nil

	case messageBodyLoadedMsg:
		m = m.stopBusy()
		if msg.err != nil {
			slog.Error("open message failed", "err", msg.err)
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("open message: %v", msg.err), sevError)
			return m, cmd
		}
		m.viewingMsg = &msg.msg
		m.viewBody = msg.body
		m.viewingRaw = false
		m.viewport.SetContent(hyperlinkify(msg.body.PlainText, m.theme.Link))
		m.viewport.GotoTop()
		m.layout() // viewport height depends on whether this message has an attachments line
		m = m.clearStatus()
		// Opening a message marks it \Seen (see Service.Body); reload the
		// message list and folder unread counts to reflect that.
		return m, tea.Batch(
			m.loadMessagesCmd(m.selectedAccount.Slug, m.selectedFolder),
			m.loadFoldersCmd(m.selectedAccount.Slug),
		)

	case sendResultMsg:
		if msg.err != nil {
			slog.Error("send message failed", "err", msg.err)
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("send failed: %v", msg.err), sevError)
			return m, cmd
		}
		slog.Info("message sent")
		m.composing = false
		var cmd tea.Cmd
		m, cmd = m.setStatus("Message sent.", sevSuccess)
		// Service.Send already refreshed the sqlite cache (e.g. a Sent
		// copy); reload folders so this pane's counts reflect that too.
		return m, tea.Batch(cmd, m.loadFoldersCmd(m.selectedAccount.Slug))

	case moveMessageResultMsg:
		m = m.stopBusy()
		verb := "mark as spam"
		successText := "Marked as spam."
		if !msg.toJunk {
			verb = "mark as not spam"
			successText = "Marked as not spam."
		}
		if msg.err != nil {
			slog.Error(verb+" failed", "folder", msg.folderPath, "err", msg.err)
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("%s: %v", verb, msg.err), sevError)
			return m, cmd
		}
		slog.Info("message moved", "folder", msg.folderPath, "toJunk", msg.toJunk)
		m.viewingMsg = nil
		m.layout()
		var cmd tea.Cmd
		m, cmd = m.setStatus(successText, sevSuccess)
		return m, tea.Batch(
			cmd,
			m.loadMessagesCmd(m.selectedAccount.Slug, m.selectedFolder),
			m.loadFoldersCmd(m.selectedAccount.Slug),
		)

	case accountAddedMsg:
		if msg.err != nil {
			slog.Error("add account failed", "err", msg.err)
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("add account: %v", msg.err), sevError)
			return m, cmd
		}
		slog.Info("account added", "slug", msg.slug)
		m.addingAccount = false
		m.selectedAccount = msg.cfg
		var cmd tea.Cmd
		m, cmd = m.setStatus(fmt.Sprintf("Account %q added, syncing…", msg.slug), sevSuccess)
		cmds := []tea.Cmd{cmd, m.loadAccountsCmd()}
		if m.syncCh == nil {
			ch := make(chan folder.Progress)
			m.syncCh = ch
			cmds = append(cmds, m.startSyncCmd(msg.cfg, ch), listenSyncProgressCmd(ch), m.syncSpinner.Tick)
		}
		return m, tea.Batch(cmds...)

	case googleAuthURLMsg:
		var cmd tea.Cmd
		m, cmd = m.setStatus(fmt.Sprintf("Sign in with Google in your browser. If it didn't open, visit: %s", msg.url), sevInfo)
		return m, cmd

	case settingsLoadedMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("load settings: %v", msg.err), sevError)
			return m, cmd
		}
		if msg.settings.SyncInterval > 0 {
			m.syncInterval = msg.settings.SyncInterval
		}
		m.initialSyncWindow = msg.settings.InitialSyncWindow
		theme, ok := themeByName(msg.settings.Theme)
		m.theme = theme
		if !ok {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("unknown theme %q, using default", msg.settings.Theme), sevInfo)
			return m, cmd
		}
		return m, nil

	case syncTickMsg:
		cmds := []tea.Cmd{tickCmd(m.syncInterval)}
		if m.syncCh == nil && m.selectedAccount.Slug != "" {
			ch := make(chan folder.Progress)
			m.syncCh = ch
			var statusCmd tea.Cmd
			m, statusCmd = m.setStatus("Background sync starting…", sevInfo)
			cmds = append(cmds, statusCmd, m.startSyncCmd(m.selectedAccount, ch), listenSyncProgressCmd(ch), m.syncSpinner.Tick)
		}
		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		if m.syncCh == nil && !m.busy {
			// Sync/busy op already finished; drop the tick instead of
			// rescheduling so the spinner doesn't keep ticking in the
			// background forever.
			return m, nil
		}
		var cmd tea.Cmd
		m.syncSpinner, cmd = m.syncSpinner.Update(msg)
		return m, cmd

	case syncProgressMsg:
		p := msg.progress
		var cmd tea.Cmd
		if p.Err != nil {
			m, cmd = m.setStatus(fmt.Sprintf("sync %s: %v", p.Path, p.Err), sevError)
		} else {
			m, cmd = m.setStatus(fmt.Sprintf("sync %s: %d messages (%d unread)", p.Path, p.TotalCount, p.UnreadCount), sevInfo)
		}
		return m, tea.Batch(cmd, listenSyncProgressCmd(m.syncCh))

	case searchResultsMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("search failed: %v", msg.err), sevError)
			return m, cmd
		}
		items := make([]list.Item, len(msg.results))
		for i, r := range msg.results {
			items[i] = searchResultItem(r)
		}
		m.searchResultsList.Title = "Search results"
		m.searchResultsList.SetItems(items)
		m.showSearchResults = true
		if len(msg.results) == 0 {
			var cmd tea.Cmd
			m, cmd = m.setStatus("No results.", sevInfo)
			return m, cmd
		}
		m = m.clearStatus()
		return m, nil

	case attachmentSavedMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("save attachment: %v", msg.err), sevError)
			return m, cmd
		}
		var cmd tea.Cmd
		m, cmd = m.setStatus(fmt.Sprintf("Saved to %s", msg.path), sevSuccess)
		return m, cmd

	case relatedLoadedMsg:
		m = m.stopBusy()
		if msg.err != nil {
			var cmd tea.Cmd
			m, cmd = m.setStatus(fmt.Sprintf("related messages: %v", msg.err), sevError)
			return m, cmd
		}
		if len(msg.results) == 0 {
			var cmd tea.Cmd
			m, cmd = m.setStatus("No related messages found.", sevInfo)
			return m, cmd
		}
		items := make([]list.Item, len(msg.results))
		for i, r := range msg.results {
			items[i] = searchResultItem(r)
		}
		m.searchResultsList.Title = "Related messages"
		m.searchResultsList.SetItems(items)
		m.showSearchResults = true
		m = m.clearStatus()
		return m, nil

	case syncDoneMsg:
		m.syncCh = nil
		var cmd tea.Cmd
		if msg.err != nil {
			slog.Error("tui sync failed", "account", m.selectedAccount.Slug, "err", msg.err)
			m, cmd = m.setStatus(fmt.Sprintf("sync failed: %v", msg.err), sevError)
		} else {
			slog.Info("tui sync complete", "account", m.selectedAccount.Slug)
			m.lastSyncAt = time.Now()
			m, cmd = m.setStatus("Sync complete.", sevSuccess)
		}
		return m, tea.Batch(cmd, m.loadFoldersCmd(m.selectedAccount.Slug))
	}

	if m.quitting {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "y", "Y", "enter", "ctrl+c":
				return m, tea.Quit
			case "n", "N", "esc":
				m.quitting = false
				m.lastCtrlC = time.Time{}
			}
		}
		return m, nil
	}

	if m.showHelp {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "ctrl+c":
				return m.handleCtrlC()
			case "q":
				m.quitting = true
				return m, nil
			}
			m.showHelp = false
		}
		return m, nil
	}

	if m.showLog {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "ctrl+c":
				return m.handleCtrlC()
			case "q":
				m.quitting = true
				return m, nil
			}
			m.showLog = false
		}
		return m, nil
	}

	if m.searching {
		return m.updateSearching(msg)
	}

	if m.showSearchResults {
		return m.updateSearchResults(msg)
	}

	if m.pickingAttachment {
		return m.updateAttachmentPicker(msg)
	}

	if m.savingAttachment {
		return m.updateSavingAttachment(msg)
	}

	if m.composing {
		return m.updateComposing(msg)
	}

	if m.addingAccount {
		return m.updateAddingAccount(msg)
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
		case "ctrl+c":
			return m.handleCtrlC()
		case "q":
			m.quitting = true
			return m, nil
		case "?":
			m.showHelp = true
			return m, nil
		case "L":
			m.showLog = true
			return m, nil
		case "/":
			if m.selectedAccount.Slug == "" {
				var cmd tea.Cmd
				m, cmd = m.setStatus("No account selected.", sevInfo)
				return m, cmd
			}
			return m.enterSearch(), nil
		case "tab":
			m.focus = (m.focus + 1) % 3
			return m, nil
		case "enter":
			return m.openSelection()
		case "a":
			return m.beginAddAccount(), nil
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
			return m, tea.Batch(cmd, m.startSyncCmd(m.selectedAccount, ch), listenSyncProgressCmd(ch), m.syncSpinner.Tick)
		case "!":
			if m.focus != focusMessages {
				return m, nil
			}
			item, ok := m.messages.SelectedItem().(messageItem)
			if !ok {
				return m, nil
			}
			busyText, mvCmd := m.toggleSpamCmd(item.UID)
			var cmd tea.Cmd
			m, cmd = m.startBusy(busyText)
			return m, tea.Batch(cmd, mvCmd)
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
			m, cmd = m.startBusy("Loading message...")
			return m, tea.Batch(cmd, m.openMessageCmd(message.Message(item)))
		}
	}
	return m, nil
}

func (m App) updateViewing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.handleCtrlC()
	case "q":
		m.quitting = true
		return m, nil
	case "esc", "backspace":
		m.viewingMsg = nil
		m.layout() // messages pane reclaims the width the preview panel was using
		return m, nil
	case "t":
		m.viewingRaw = !m.viewingRaw
		if m.viewingRaw {
			m.viewport.SetContent(string(m.viewBody.Raw))
		} else {
			m.viewport.SetContent(hyperlinkify(m.viewBody.PlainText, m.theme.Link))
		}
		m.viewport.GotoTop()
		return m, nil
	case "r":
		return m.startReply(false)
	case "R":
		return m.startReply(true)
	case "g":
		if m.viewingMsg == nil {
			return m, nil
		}
		var cmd tea.Cmd
		m, cmd = m.startBusy("Loading related messages...")
		return m, tea.Batch(cmd, m.relatedCmd(*m.viewingMsg))
	case "a":
		switch len(m.viewBody.Attachments) {
		case 0:
			var cmd tea.Cmd
			m, cmd = m.setStatus("No attachments.", sevInfo)
			return m, cmd
		case 1:
			return m.beginSaveAttachment(m.viewBody.Attachments[0]), nil
		default:
			return m.beginAttachmentPicker(), nil
		}
	case "!":
		if m.viewingMsg == nil {
			return m, nil
		}
		busyText, mvCmd := m.toggleSpamCmd(m.viewingMsg.UID)
		var cmd tea.Cmd
		m, cmd = m.startBusy(busyText)
		return m, tea.Batch(cmd, mvCmd)
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// cardHeaderLines is the number of lines viewCard renders above the
// viewport's own content (subject, from, to, date, separator) — layout uses
// this to size the viewport so the two stay in sync.
const cardHeaderLines = 5

// layout recomputes each pane's size from the current window size. Each
// list pane is wrapped in a bordered box (see View), which costs 2 columns
// and 2 rows, so that's subtracted from what's handed to the list widgets.
func (m *App) layout() {
	const borderWidth, borderHeight = 2, 2
	const cardPaddingWidth = 2 // theme.Card's horizontal Padding(0, 1)

	paneHeight := m.height - 2 // reserve the status bar and shortcuts rows

	if m.viewingMsg != nil {
		// Reading a message: accounts/folders make way for a card-style
		// preview panel to the right of the (now narrower) message list.
		messagesWidth := max(m.width/3, 20)
		previewWidth := max(m.width-messagesWidth, 24)

		m.messages.SetSize(max(messagesWidth-borderWidth, 1), max(paneHeight-borderHeight, 1))

		viewportHeight := max(paneHeight-borderHeight-cardHeaderLines, 1)
		if len(m.viewBody.Attachments) > 0 {
			viewportHeight = max(viewportHeight-1, 1) // reserve the attachments line
		}
		m.viewport.Width = max(previewWidth-borderWidth-cardPaddingWidth, 1)
		m.viewport.Height = viewportHeight
	} else {
		accountsWidth := max(m.width/5, 16)
		foldersWidth := max(m.width/5, 16)
		messagesWidth := max(m.width-accountsWidth-foldersWidth, 16)

		m.accounts.SetSize(max(accountsWidth-borderWidth, 1), max(paneHeight-borderHeight, 1))
		m.folders.SetSize(max(foldersWidth-borderWidth, 1), max(paneHeight-borderHeight, 1))
		m.messages.SetSize(max(messagesWidth-borderWidth, 1), max(paneHeight-borderHeight, 1))
	}

	m.searchInput.Width = max(m.width-4, 10)
	m.searchResultsList.SetSize(m.width, max(m.height-3, 1)) // reserve the header, status bar, and shortcuts rows

	m.saveAttachmentInput.Width = max(m.width-4, 10)
	m.attachmentPicker.SetSize(m.width, max(m.height-3, 1)) // reserve the header, status bar, and shortcuts rows

	if m.composing {
		m.composeTo.Width = max(m.width-4, 10)
		m.composeCc.Width = max(m.width-4, 10)
		m.composeSubject.Width = max(m.width-4, 10)
		m.composeBody.SetWidth(max(m.width-2, 10))
		m.composeBody.SetHeight(max(m.height-6, 3)) // reserve To/Cc/Subject + status bar + shortcuts row
	}

	if m.addingAccount {
		inputWidth, _ := m.accountFieldLayout()
		for i := range m.accountForm {
			if isSelectField(accountFormField(i)) {
				continue
			}
			m.accountForm[i].Width = inputWidth
		}
	}
}

func (m App) View() string {
	if m.quitting {
		return m.viewQuitConfirm()
	}

	if m.showHelp {
		return m.viewHelp()
	}

	if m.showLog {
		return m.viewLog()
	}

	if m.searching {
		return m.viewSearch()
	}

	if m.showSearchResults {
		return m.viewSearchResults()
	}

	if m.pickingAttachment {
		return m.viewAttachmentPicker()
	}

	if m.savingAttachment {
		return m.viewSaveAttachment()
	}

	if m.composing {
		return m.viewCompose()
	}

	if m.addingAccount {
		return m.viewAddAccount()
	}

	if m.viewingMsg != nil {
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			m.theme.InactivePane.Render(m.messages.View()),
			m.theme.Card.Render(m.viewCard()),
		)
		return row + "\n" + m.footer()
	}

	accountsStyle, foldersStyle, messagesStyle := m.theme.InactivePane, m.theme.InactivePane, m.theme.InactivePane
	switch m.focus {
	case focusAccounts:
		accountsStyle = m.theme.ActivePane
	case focusFolders:
		foldersStyle = m.theme.ActivePane
	case focusMessages:
		messagesStyle = m.theme.ActivePane
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top,
		accountsStyle.Render(m.accounts.View()),
		foldersStyle.Render(m.folders.View()),
		messagesStyle.Render(m.messages.View()),
	)
	return row + "\n" + m.footer()
}

// viewCard renders the currently viewed message as card content: a subject
// header, From/To/Date meta, a separator, then the raw/rendered body in the
// viewport. Its line count above the viewport must track cardHeaderLines in
// layout so the viewport is always sized to fit without clipping or gaps.
func (m App) viewCard() string {
	to := strings.Join(m.viewingMsg.ToAddrs, ", ")
	if to == "" {
		to = "-"
	}
	from := m.viewingMsg.FromAddr
	if m.viewingMsg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", m.viewingMsg.FromName, m.viewingMsg.FromAddr)
	}

	lines := []string{
		m.theme.CardHeader.Render(m.viewingMsg.Subject),
		m.theme.CardMeta.Render("From: " + from),
		m.theme.CardMeta.Render("To:   " + to),
		m.theme.CardMeta.Render("Date: " + m.viewingMsg.Date.Format("2006-01-02 15:04")),
		m.theme.CardMeta.Render(strings.Repeat("─", m.viewport.Width)),
		m.viewport.View(),
	}
	if line := m.attachmentsLine(); line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// shortcutsLine returns the contextual keybinding hint for the current mode.
// Unlike the status bar above it, this row is always shown.
func (m App) shortcutsLine() string {
	if m.composing {
		return "pigeon — compose — tab: next field · ctrl+s: send · esc: cancel"
	}
	if m.addingAccount {
		return "pigeon — add account — tab/shift+tab: field · ←/→: change security · ctrl+s: save · esc: cancel"
	}
	if m.viewingMsg != nil {
		mode := "rendered"
		if m.viewingRaw {
			mode = "raw"
		}
		return fmt.Sprintf("pigeon — viewing (%s) — r: reply · R: reply-all · t: toggle raw · !: spam · esc: back · q: quit", mode)
	}
	return "pigeon — tab: switch pane · enter: open · c: compose · s: sync · /: search · !: spam · ?: help · L: log · q: quit"
}

// statusBarText returns what the status bar (the row above shortcuts) should
// show right now: the current status message (prefixed with a spinner while
// a sync is running), or — once idle with nothing to report — when the last
// sync finished. Blank only if neither has ever happened yet.
func (m App) statusBarText() (text string, sev severity) {
	if m.syncCh != nil || m.busy {
		msg := m.status.text
		if msg == "" {
			switch {
			case m.syncCh != nil:
				msg = "Syncing…"
			default:
				msg = "Working…"
			}
		}
		return m.syncSpinner.View() + " " + msg, sevInfo
	}
	if m.status.text != "" {
		return m.status.text, m.status.sev
	}
	if !m.lastSyncAt.IsZero() {
		return "Last synced " + m.lastSyncAt.Format("15:04:05"), sevInfo
	}
	return "", sevInfo
}

// footer renders the two-row status area shown at the bottom of every view:
// the status bar (sync progress, errors, confirmations, last-sync time — a
// blank styled row when there's nothing to report yet) above the shortcuts
// row, which always shows the current mode's keybindings.
func (m App) footer() string {
	shortcuts := m.theme.Shortcuts.Render(m.shortcutsLine())
	text, sev := m.statusBarText()
	if text == "" {
		return "\n" + shortcuts
	}
	return m.theme.StatusStyle(sev).Render(text) + "\n" + shortcuts
}

// helpSection is one titled group of keybinding rows in the help overlay.
// Keeping this as data (rather than a hand-formatted string block) means
// later features add their new key as one Rows entry instead of re-aligning
// a flat string slice.
type helpSection struct {
	Title string
	Rows  [][2]string // {key, description}
}

// helpSections is the full set of keybindings shown by viewHelp, grouped by
// the mode they apply in.
var helpSections = []helpSection{
	{
		Title: "Navigation",
		Rows: [][2]string{
			{"tab", "switch pane (accounts / folders / messages)"},
			{"↑/↓, j/k", "move selection, scroll"},
			{"enter", "open selection"},
			{"a", "add a new account"},
			{"c", "compose a new message"},
			{"s", "sync the selected account (also runs automatically in the background)"},
			{"/", "search subject/from/to/cc for the selected account"},
			{"!", "mark as spam (move to Junk); in the Junk folder, moves back to the Inbox instead"},
			{"?", "toggle this help"},
			{"L", "toggle the status log"},
			{"q / ctrl+c", "quit (asks to confirm)"},
			{"ctrl+c ctrl+c", "quit immediately, no confirm (press twice)"},
		},
	},
	{
		Title: "Message viewer",
		Rows: [][2]string{
			{"r", "reply"},
			{"R", "reply-all"},
			{"t", "toggle raw / rendered"},
			{"g", "go to related messages (In-Reply-To / References)"},
			{"a", "save an attachment"},
			{"esc", "back to message list"},
		},
	},
	{
		Title: "Compose",
		Rows: [][2]string{
			{"tab", "next field (To / Cc / Subject / Body)"},
			{"ctrl+s", "send"},
			{"esc", "cancel"},
		},
	},
	{
		Title: "Add account",
		Rows: [][2]string{
			{"tab / shift+tab", "next / previous field"},
			{"←/→ or enter", "change IMAP/SMTP security (on that field)"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		},
	},
}

func (m App) viewHelp() string {
	lines := append(strings.Split(logo.Banner(), "\n"), "", "pigeon — keybindings", "")
	for _, section := range helpSections {
		lines = append(lines, section.Title)
		for _, row := range section.Rows {
			lines = append(lines, fmt.Sprintf("  %-14s %s", row[0], row[1]))
		}
		lines = append(lines, "")
	}
	lines = append(lines, "Press any key to close.")
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

// viewQuitConfirm renders the "quit pigeon?" prompt centered over an
// otherwise blank frame. Bubbletea redraws the whole screen each View, so
// there's no need to layer this over whatever mode triggered it — that
// mode's state is left untouched on m and simply redrawn once quitting is
// cancelled.
func (m App) viewQuitConfirm() string {
	box := m.theme.ActivePane.Render("Quit pigeon? (y/n)")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// Run starts the Bubbletea program. It blocks until the user quits.
func Run(ctx context.Context, accountSvc *account.Service, folderSvc *folder.Service, messageSvc *message.Service, composeSvc *compose.Service, settingsSvc *settings.Service) error {
	p := tea.NewProgram(newApp(ctx, accountSvc, folderSvc, messageSvc, composeSvc, settingsSvc), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}
