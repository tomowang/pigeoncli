package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/core/message"
)

// relatedLoadedMsg reports the messages that In-Reply-To/References on the
// message currently being viewed resolve to.
type relatedLoadedMsg struct {
	results []message.SearchResult
	err     error
}

func (m App) relatedCmd(msg message.Message) tea.Cmd {
	ctx, svc, accountSlug := m.ctx, m.messageSvc, m.selectedAccount.Slug
	return func() tea.Msg {
		results, err := svc.Related(ctx, accountSlug, msg)
		return relatedLoadedMsg{results: results, err: err}
	}
}
