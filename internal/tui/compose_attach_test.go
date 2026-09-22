package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomowang/pigeoncli/internal/core/compose"
)

// newComposeTestApp returns an App in compose mode, body field focused, no
// services wired (attach/remove don't call any).
func newComposeTestApp(t *testing.T) App {
	t.Helper()
	m := newApp(context.Background(), nil, nil, nil, nil, nil, nil)
	m.width, m.height = 100, 30
	m = m.enterCompose(compose.Draft{To: []string{"bob@example.com"}}, composeFieldBody)
	return m
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAttachFileAddsToComposeState(t *testing.T) {
	m := newComposeTestApp(t)
	path := writeTempFile(t, "notes.txt", "hello world")

	m, _ = press(m, "ctrl+g")
	if !m.attachingFile {
		t.Fatal("ctrl+g did not open the attach-file prompt")
	}
	if !strings.Contains(m.View(), "Attach file") {
		t.Fatalf("view doesn't show the attach prompt:\n%s", m.View())
	}

	for _, r := range path {
		m, _ = press(m, string(r))
	}
	m, cmd := press(m, "enter")
	if m.attachingFile {
		t.Fatal("enter should close the attach-file prompt")
	}
	if cmd == nil {
		t.Fatal("expected the status auto-clear command")
	}
	if len(m.composeAttachments) != 1 || m.composeAttachments[0].Filename != "notes.txt" {
		t.Fatalf("composeAttachments = %+v", m.composeAttachments)
	}
	if string(m.composeAttachments[0].Data) != "hello world" {
		t.Fatalf("attached data = %q", m.composeAttachments[0].Data)
	}
	if !strings.Contains(m.status.text, "Attached notes.txt") {
		t.Fatalf("status = %q", m.status.text)
	}
	if !strings.Contains(m.View(), "Attachments: notes.txt") {
		t.Fatalf("compose view doesn't show the attachment:\n%s", m.View())
	}
}

func TestAttachFileEscCancelsWithoutAttaching(t *testing.T) {
	m := newComposeTestApp(t)
	m, _ = press(m, "ctrl+g")
	m, _ = press(m, "esc")
	if m.attachingFile {
		t.Fatal("esc did not close the attach-file prompt")
	}
	if len(m.composeAttachments) != 0 {
		t.Fatalf("composeAttachments = %+v, want none", m.composeAttachments)
	}
	if !m.composing {
		t.Fatal("esc from the attach prompt should not also cancel the whole compose")
	}
}

func TestAttachFileMissingPathShowsError(t *testing.T) {
	m := newComposeTestApp(t)
	m, _ = press(m, "ctrl+g")
	for _, r := range "/no/such/file" {
		m, _ = press(m, string(r))
	}
	m, _ = press(m, "enter")
	if m.status.sev != sevError || !strings.Contains(m.status.text, "attach") {
		t.Fatalf("status = %+v, want an attach error", m.status)
	}
	if len(m.composeAttachments) != 0 {
		t.Fatal("a failed attach should not add anything")
	}
}

func TestRemoveLastAttachment(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeAttachments = []compose.Attachment{
		{Filename: "a.txt", Data: []byte("a")},
		{Filename: "b.txt", Data: []byte("b")},
	}
	m.layout()

	m, cmd := press(m, "ctrl+r")
	if cmd == nil {
		t.Fatal("expected the status auto-clear command")
	}
	if len(m.composeAttachments) != 1 || m.composeAttachments[0].Filename != "a.txt" {
		t.Fatalf("composeAttachments = %+v, want just a.txt", m.composeAttachments)
	}
	if !strings.Contains(m.status.text, "Removed b.txt") {
		t.Fatalf("status = %q", m.status.text)
	}

	m, _ = press(m, "ctrl+r")
	if len(m.composeAttachments) != 0 {
		t.Fatalf("composeAttachments = %+v, want none", m.composeAttachments)
	}

	m, _ = press(m, "ctrl+r")
	if m.status.sev != sevInfo || !strings.Contains(m.status.text, "No attachments") {
		t.Fatalf("status = %+v", m.status)
	}
}

func TestSendComposeIncludesAttachments(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeAttachments = []compose.Attachment{{Filename: "a.txt", Data: []byte("a")}}
	m.composeSvc = compose.NewService(nil, nil)

	// sendCompose builds the draft synchronously; only the returned tea.Cmd
	// (which actually sends) touches the network, and it isn't executed
	// here.
	model, cmd := m.sendCompose()
	if cmd == nil {
		t.Fatal("sendCompose returned no command")
	}
	if model.(App).status.text != "Sending..." {
		t.Fatalf("status = %q", model.(App).status.text)
	}
}

func TestEnterComposeResetsAttachmentState(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeAttachments = []compose.Attachment{{Filename: "a.txt", Data: []byte("a")}}
	m.attachingFile = true

	m = m.enterCompose(compose.Draft{}, composeFieldTo)
	if len(m.composeAttachments) != 0 || m.attachingFile {
		t.Fatalf("enterCompose left stale attach state: attachments=%+v attachingFile=%v", m.composeAttachments, m.attachingFile)
	}
}
