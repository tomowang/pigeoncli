package tui

import (
	"fmt"

	"github.com/tomowang/pigeoncli/internal/core/account"
	"github.com/tomowang/pigeoncli/internal/core/folder"
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
