package tui

import (
	"fmt"

	"github.com/tomowang/pigeoncli/internal/core/account"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

// accountItem adapts account.Account to bubbles/list's Item/DefaultItem.
type accountItem account.Account

func (a accountItem) FilterValue() string { return a.Slug }
func (a accountItem) Title() string       { return a.Slug }
func (a accountItem) Description() string { return a.Email }

// folderItem adapts folder.Folder to bubbles/list's Item/DefaultItem.
type folderItem folder.Folder

func (f folderItem) FilterValue() string { return f.Path }

func (f folderItem) Title() string {
	if f.UnreadCount > 0 {
		return fmt.Sprintf("%s (%d)", f.Name, f.UnreadCount)
	}
	return f.Name
}

func (f folderItem) Description() string {
	return fmt.Sprintf("%d messages", f.TotalCount)
}

// messageItem adapts message.Message to bubbles/list's Item/DefaultItem.
type messageItem message.Message

func (m messageItem) FilterValue() string { return m.Subject }

func (m messageItem) Title() string {
	if !hasFlag(m.Flags, `\Seen`) {
		return "* " + m.Subject
	}
	return m.Subject
}

func (m messageItem) Description() string {
	from := m.FromAddr
	if m.FromName != "" {
		from = m.FromName
	}
	return fmt.Sprintf("%s — %s", from, m.Date.Format("2006-01-02 15:04"))
}

// searchResultItem adapts message.SearchResult to bubbles/list's
// Item/DefaultItem.
type searchResultItem message.SearchResult

func (s searchResultItem) FilterValue() string { return s.Subject }

func (s searchResultItem) Title() string {
	return fmt.Sprintf("%s — %s", s.Subject, s.FolderPath)
}

func (s searchResultItem) Description() string {
	from := s.FromAddr
	if s.FromName != "" {
		from = s.FromName
	}
	return fmt.Sprintf("%s — %s", from, s.Date.Format("2006-01-02 15:04"))
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}
