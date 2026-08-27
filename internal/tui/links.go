package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// urlPattern matches bare http(s) URLs in message preview text so they can
// be turned into clickable terminal hyperlinks.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

// trailingPunct is punctuation that commonly trails a URL in prose (a
// sentence-ending period, a closing paren picked up from surrounding text)
// and should stay outside the clickable span.
const trailingPunct = ".,;:!?)]}\"'"

// hyperlinkify rewrites bare URLs in text into clickable OSC8 terminal
// hyperlinks. Terminals that support OSC8 (iTerm2, WezTerm, Kitty, Windows
// Terminal, and others) render the link as click/cmd-click-able; terminals
// without support just show the styled link text. style is applied to the
// visible URL before it's wrapped in the OSC8 escape sequence.
func hyperlinkify(text string, style lipgloss.Style) string {
	return urlPattern.ReplaceAllStringFunc(text, func(match string) string {
		url := strings.TrimRight(match, trailingPunct)
		if url == "" {
			return match // match was punctuation-only (shouldn't happen, but be safe)
		}
		suffix := match[len(url):]
		return termenv.Hyperlink(url, style.Render(url)) + suffix
	})
}
