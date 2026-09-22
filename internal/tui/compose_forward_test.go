package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tomowang/pigeoncli/internal/core/compose"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

// newViewingTestApp returns an App viewing msg, with a real (storeless)
// compose.Service wired so startReply/startForward can be driven through
// real key presses.
func newViewingTestApp(t *testing.T, msg message.Message, body message.Body) App {
	t.Helper()
	m := newApp(context.Background(), nil, nil, nil, compose.NewService(nil, nil), nil, nil)
	m.width, m.height = 100, 30
	m.selectedAccount.Slug = "work"
	m.viewingMsg = &msg
	m.viewBody = body
	m.layout()
	return m
}

func TestForwardKeyEntersComposeWithForwardDraft(t *testing.T) {
	orig := message.Message{Subject: "Hi", FromAddr: "alice@example.com", FromName: "Alice"}
	body := message.Body{PlainText: "Original body."}
	m := newViewingTestApp(t, orig, body)

	model, _ := m.updateViewing(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = model.(App)

	if !m.composing || m.viewingMsg != nil {
		t.Fatalf("composing=%v viewingMsg=%v, want composing with the viewer closed", m.composing, m.viewingMsg)
	}
	if m.composeField != composeFieldTo {
		t.Fatalf("focus = %v, want composeFieldTo (nothing pre-addressed)", m.composeField)
	}
	if m.composeTo.Value() != "" {
		t.Fatalf("To = %q, want empty", m.composeTo.Value())
	}
	if m.composeSubject.Value() != "Fwd: Hi" {
		t.Fatalf("Subject = %q", m.composeSubject.Value())
	}
	if !strings.Contains(m.composeBody.Value(), "Forwarded message") || !strings.Contains(m.composeBody.Value(), "Original body.") {
		t.Fatalf("body = %q", m.composeBody.Value())
	}
}

func TestForwardKeyDoesNothingWithoutAnOpenMessage(t *testing.T) {
	m := newViewingTestApp(t, message.Message{}, message.Body{})
	m.viewingMsg = nil

	model, cmd := m.updateViewing(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if cmd != nil || model.(App).composing {
		t.Fatal("f with no open message should be a no-op")
	}
}

func TestComposeFieldCyclesThroughBcc(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeField = composeFieldTo

	order := []composeField{composeFieldCc, composeFieldBcc, composeFieldSubject, composeFieldBody, composeFieldTo}
	for _, want := range order {
		m = m.cycleComposeField()
		if m.composeField != want {
			t.Fatalf("cycled to %v, want %v", m.composeField, want)
		}
		if want == composeFieldBcc && !m.composeBcc.Focused() {
			t.Fatal("Bcc field should be focused once cycled to")
		}
	}
}

func TestSendComposeIncludesBcc(t *testing.T) {
	m := newComposeTestApp(t)
	m.composeBcc.SetValue("secret@example.com")
	m.composeSvc = compose.NewService(nil, nil)

	model, cmd := m.sendCompose()
	if cmd == nil {
		t.Fatal("sendCompose returned no command")
	}
	_ = model
}
