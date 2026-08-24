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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tomowang/pigeoncli/internal/core/account"
	"github.com/tomowang/pigeoncli/internal/core/folder"
)

var (
	statusBarStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62"))

	activePaneStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62"))
	inactivePaneStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
)

type focusPane int

const (
	focusAccounts focusPane = iota
	focusFolders
)

// App is the root Bubbletea model: an account list and a folder tree pane
// side by side, plus a status bar. Later phases add message list/view and
// compose sub-models/panes here.
//
// Update has no context parameter (that's the Elm architecture's shape),
// but the account/folder service calls triggered from tea.Cmd closures
// still need one — so the ctx passed to Run is stored on the model and
// reused by every tea.Cmd it builds.
type App struct {
	ctx context.Context

	accountSvc *account.Service
	folderSvc  *folder.Service

	width, height int
	focus         focusPane
	status        string

	accounts list.Model
	folders  list.Model
}

func newApp(ctx context.Context, accountSvc *account.Service, folderSvc *folder.Service) App {
	accounts := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	accounts.Title = "Accounts"
	accounts.SetShowHelp(false)

	folders := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	folders.Title = "Folders"
	folders.SetShowHelp(false)

	return App{
		ctx:        ctx,
		accountSvc: accountSvc,
		folderSvc:  folderSvc,
		accounts:   accounts,
		folders:    folders,
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
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			if m.focus == focusAccounts {
				m.focus = focusFolders
			} else {
				m.focus = focusAccounts
			}
			return m, nil
		case "enter":
			if m.focus == focusAccounts {
				if item, ok := m.accounts.SelectedItem().(accountItem); ok {
					return m, m.loadFoldersCmd(item.Slug)
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.focus == focusAccounts {
		m.accounts, cmd = m.accounts.Update(msg)
	} else {
		m.folders, cmd = m.folders.Update(msg)
	}
	return m, cmd
}

// layout recomputes each pane's size from the current window size. Each
// pane is wrapped in a bordered box (see View), which costs 2 columns and
// 2 rows, so that's subtracted from what's handed to the list widgets.
func (m *App) layout() {
	const borderWidth, borderHeight = 2, 2

	leftWidth := m.width / 3
	if leftWidth < 20 {
		leftWidth = 20
	}
	rightWidth := m.width - leftWidth
	paneHeight := m.height - 1 // reserve the status bar row

	m.accounts.SetSize(max(leftWidth-borderWidth, 1), max(paneHeight-borderHeight, 1))
	m.folders.SetSize(max(rightWidth-borderWidth, 1), max(paneHeight-borderHeight, 1))
}

func (m App) View() string {
	accountsStyle, foldersStyle := inactivePaneStyle, inactivePaneStyle
	if m.focus == focusAccounts {
		accountsStyle = activePaneStyle
	} else {
		foldersStyle = activePaneStyle
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top,
		accountsStyle.Render(m.accounts.View()),
		foldersStyle.Render(m.folders.View()),
	)
	return row + "\n" + statusBarStyle.Render(m.statusLine())
}

func (m App) statusLine() string {
	if m.status != "" {
		return m.status
	}
	return "pigeon — tab: switch pane · enter: open account · q: quit"
}

// Run starts the Bubbletea program. It blocks until the user quits.
func Run(ctx context.Context, accountSvc *account.Service, folderSvc *folder.Service) error {
	p := tea.NewProgram(newApp(ctx, accountSvc, folderSvc), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}
