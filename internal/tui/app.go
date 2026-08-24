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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var statusBarStyle = lipgloss.NewStyle().
	Bold(true).
	Padding(0, 1).
	Foreground(lipgloss.Color("15")).
	Background(lipgloss.Color("62"))

// App is the root Bubbletea model. Phase 0 only renders a status bar;
// later phases add accountlist/foldertree/messagelist/messageview/compose
// sub-models here.
type App struct {
	width  int
	height int
}

func newApp() App {
	return App{}
}

func (m App) Init() tea.Cmd {
	return nil
}

func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m App) View() string {
	return statusBarStyle.Render("pigeon — press q to quit") + "\n"
}

// Run starts the Bubbletea program. It blocks until the user quits.
func Run(ctx context.Context) error {
	p := tea.NewProgram(newApp(), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}
