package tui

import "github.com/charmbracelet/lipgloss"

// Theme bundles the color styles used across the TUI. Selected via the
// `[ui] theme` config field (internal/core/settings.Settings.Theme).
type Theme struct {
	Name string

	StatusInfo    lipgloss.Style
	StatusSuccess lipgloss.Style
	StatusError   lipgloss.Style
	Shortcuts     lipgloss.Style // the always-on keybinding row below the status bar

	ActivePane   lipgloss.Style
	InactivePane lipgloss.Style
	ViewHeader   lipgloss.Style

	Card       lipgloss.Style
	CardHeader lipgloss.Style
	CardMeta   lipgloss.Style
	Link       lipgloss.Style // clickable URLs in the message preview
}

// StatusStyle returns the status-bar style for sev.
func (t Theme) StatusStyle(sev severity) lipgloss.Style {
	switch sev {
	case sevSuccess:
		return t.StatusSuccess
	case sevError:
		return t.StatusError
	default:
		return t.StatusInfo
	}
}

var themes = map[string]Theme{
	"default": {
		Name: "default",
		StatusInfo: lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62")),
		StatusSuccess: lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("15")).Background(lipgloss.Color("28")),
		StatusError: lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("15")).Background(lipgloss.Color("124")),
		Shortcuts: lipgloss.NewStyle().Padding(0, 1).
			Foreground(lipgloss.Color("250")).Background(lipgloss.Color("236")),
		ActivePane:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")),
		InactivePane: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")),
		ViewHeader:   lipgloss.NewStyle().Bold(true).Padding(0, 1),

		Card:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(0, 1),
		CardHeader: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")),
		CardMeta:   lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		Link:       lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Underline(true),
	},
	"dark": {
		Name: "dark",
		StatusInfo: lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("54")),
		StatusSuccess: lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("22")),
		StatusError: lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("88")),
		Shortcuts: lipgloss.NewStyle().Padding(0, 1).
			Foreground(lipgloss.Color("250")).Background(lipgloss.Color("235")),
		ActivePane:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("135")),
		InactivePane: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")),
		ViewHeader:   lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("231")),

		Card:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("135")).Padding(0, 1),
		CardHeader: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")),
		CardMeta:   lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		Link:       lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Underline(true),
	},
	"light": {
		Name: "light",
		StatusInfo: lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("0")).Background(lipgloss.Color("117")),
		StatusSuccess: lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("0")).Background(lipgloss.Color("120")),
		StatusError: lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("0")).Background(lipgloss.Color("210")),
		Shortcuts: lipgloss.NewStyle().Padding(0, 1).
			Foreground(lipgloss.Color("0")).Background(lipgloss.Color("253")),
		ActivePane:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("25")),
		InactivePane: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("252")),
		ViewHeader:   lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("0")),

		Card:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("25")).Padding(0, 1),
		CardHeader: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")),
		CardMeta:   lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		Link:       lipgloss.NewStyle().Foreground(lipgloss.Color("27")).Underline(true),
	},
}

const defaultThemeName = "default"

// themeByName returns the named theme. Unknown or empty names fall back to
// the default theme; ok reports whether name was recognized (false for
// empty, since that's not a user mistake to warn about).
func themeByName(name string) (theme Theme, ok bool) {
	if name == "" {
		return themes[defaultThemeName], true
	}
	t, found := themes[name]
	if !found {
		return themes[defaultThemeName], false
	}
	return t, true
}
