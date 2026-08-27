package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/core/message"
)

// searchResultsMsg reports the outcome of a header search.
type searchResultsMsg struct {
	results []message.SearchResult
	err     error
}

func (m App) startSearchCmd(accountSlug, query string) tea.Cmd {
	ctx, svc := m.ctx, m.messageSvc
	return func() tea.Msg {
		results, err := svc.Search(ctx, accountSlug, query)
		return searchResultsMsg{results: results, err: err}
	}
}

// enterSearch switches into the search-input mode with a fresh, focused
// text field.
func (m App) enterSearch() App {
	m.searchInput = textinput.New()
	m.searchInput.Prompt = "Search: "
	m.searchInput.Focus()
	m.searching = true
	m = m.clearStatus()
	m.layout()
	return m
}

// updateSearching handles input while the search text field is active.
func (m App) updateSearching(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			m.searching = false
			m = m.clearStatus()
			return m, nil
		case "enter":
			query := m.searchInput.Value()
			m.searching = false
			var cmd tea.Cmd
			m, cmd = m.setStatus("Searching...", sevInfo)
			return m, tea.Batch(cmd, m.startSearchCmd(m.selectedAccount.Slug, query))
		}
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

// updateSearchResults handles input while the search-results overlay is
// shown (after a search has run).
func (m App) updateSearchResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m.handleCtrlC()
		case "q":
			m.quitting = true
			return m, nil
		case "esc":
			m.showSearchResults = false
			m = m.clearStatus()
			return m, nil
		case "enter":
			if item, ok := m.searchResultsList.SelectedItem().(searchResultItem); ok {
				m.showSearchResults = false
				m.selectedFolder = item.FolderPath
				var cmd tea.Cmd
				m, cmd = m.startBusy("Loading message...")
				return m, tea.Batch(cmd, m.openMessageCmd(item.Message), m.loadMessagesCmd(m.selectedAccount.Slug, item.FolderPath))
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.searchResultsList, cmd = m.searchResultsList.Update(msg)
	return m, cmd
}

func (m App) viewSearch() string {
	return m.searchInput.View() + "\n" + m.footer()
}

func (m App) viewSearchResults() string {
	header := m.theme.ViewHeader.Render(fmt.Sprintf("%s — enter: open · esc: back", m.searchResultsList.Title))
	return header + "\n" + m.searchResultsList.View() + "\n" + m.footer()
}
