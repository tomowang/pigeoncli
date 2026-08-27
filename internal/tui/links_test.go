package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestHyperlinkifyWrapsBareURL(t *testing.T) {
	got := hyperlinkify("See https://example.com for details.", lipgloss.NewStyle())

	if !strings.Contains(got, "\x1b]8;;https://example.com\x1b\\") {
		t.Fatalf("expected OSC8 open sequence for the URL, got %q", got)
	}
	if !strings.HasSuffix(got, "for details.") {
		t.Fatalf("expected trailing prose preserved outside the link, got %q", got)
	}
}

func TestHyperlinkifyExcludesTrailingPunctuation(t *testing.T) {
	got := hyperlinkify("Visit https://example.com/reset).", lipgloss.NewStyle())

	if !strings.Contains(got, "https://example.com/reset\x1b]8;;\x1b\\).") {
		t.Fatalf("expected trailing ')' and '.' left outside the clickable span, got %q", got)
	}
}

func TestHyperlinkifyLeavesPlainTextUntouched(t *testing.T) {
	const text = "No links here, just words."
	if got := hyperlinkify(text, lipgloss.NewStyle()); got != text {
		t.Fatalf("expected text without URLs to be unchanged, got %q", got)
	}
}
