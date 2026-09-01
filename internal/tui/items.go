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
	title := f.Name
	// The server's folder name doesn't always make its purpose obvious
	// (e.g. Gmail's "[Gmail]/Bulk Mail"), so a recognized Junk folder is
	// tagged regardless of what it's actually called.
	if f.SpecialUse == `\Junk` {
		title = "⚠ " + title
	}
	if f.UnreadCount > 0 {
		title = fmt.Sprintf("%s (%d)", title, f.UnreadCount)
	}
	return title
}

func (f folderItem) Description() string {
	return fmt.Sprintf("%d messages", f.TotalCount)
}

// messageItem adapts message.Message to bubbles/list's Item/DefaultItem.
type messageItem message.Message

func (m messageItem) FilterValue() string { return m.Subject }

func (m messageItem) Title() string {
	title := m.Subject
	if m.InReplyTo != "" || len(m.References) > 0 {
		title = "↩ " + title
	}
	if !hasFlag(m.Flags, `\Seen`) {
		title = "* " + title
	}
	return title
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

// attachmentItem adapts message.Attachment to bubbles/list's
// Item/DefaultItem.
type attachmentItem message.Attachment

func (a attachmentItem) FilterValue() string { return a.Filename }

func (a attachmentItem) Title() string {
	return fmt.Sprintf("[%d] %s", a.Index+1, a.Filename)
}

func (a attachmentItem) Description() string {
	return fmt.Sprintf("%s — %s", a.ContentType, humanSize(a.Size))
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}
