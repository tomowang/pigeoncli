package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestParseUIDs(t *testing.T) {
	got, err := parseUIDs([]string{"1", "42", "4294967295"})
	if err != nil || len(got) != 3 || got[1] != 42 || got[2] != 4294967295 {
		t.Fatalf("parseUIDs = %v, %v", got, err)
	}
	for _, bad := range []string{"x", "-1", "4294967296", ""} {
		if _, err := parseUIDs([]string{"1", bad}); err == nil {
			t.Errorf("parseUIDs accepted %q", bad)
		}
	}
}

func TestPlural(t *testing.T) {
	if got := plural(1, "message"); got != "1 message" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(3, "message"); got != "3 messages" {
		t.Errorf("plural(3) = %q", got)
	}
}

func TestFormatFlags(t *testing.T) {
	cases := map[string][]string{
		"unread":         nil,
		"":               {`\Seen`},
		"starred":        {`\Seen`, `\Flagged`},
		"unread,starred": {`\Flagged`},
	}
	for want, flags := range cases {
		if got := formatFlags(flags); got != want {
			t.Errorf("formatFlags(%v) = %q, want %q", flags, got, want)
		}
	}
}

func TestConfirmOnlyAcceptsExplicitYes(t *testing.T) {
	ask := func(input string) bool {
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader(input))
		cmd.SetErr(&strings.Builder{})
		return confirm(cmd, "sure? ")
	}
	for _, yes := range []string{"y\n", "YES\n", " yes \n", "y"} {
		if !ask(yes) {
			t.Errorf("confirm(%q) = false, want true", yes)
		}
	}
	for _, no := range []string{"n\n", "\n", "", "yep\n", "no\n"} {
		if ask(no) {
			t.Errorf("confirm(%q) = true, want false", no)
		}
	}
}

func TestLoadAttachments(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notes.txt"
	if err := os.WriteFile(path, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	atts, err := loadAttachments([]string{path})
	if err != nil {
		t.Fatalf("loadAttachments: %v", err)
	}
	if len(atts) != 1 || atts[0].Filename != "notes.txt" || string(atts[0].Data) != "hi" {
		t.Fatalf("got %+v", atts)
	}

	if _, err := loadAttachments([]string{dir + "/nope"}); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if got, err := loadAttachments(nil); got != nil || err != nil {
		t.Fatalf("loadAttachments(nil) = %v, %v", got, err)
	}
}
