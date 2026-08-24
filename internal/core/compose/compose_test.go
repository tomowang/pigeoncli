package compose

import (
	"strings"
	"testing"
	"time"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

func testOrig() (message.Message, message.Body) {
	orig := message.Message{
		UID:       1,
		MessageID: "orig-id@example.com",
		Subject:   "Hi",
		FromName:  "Alice",
		FromAddr:  "alice@example.com",
		ToAddrs:   []string{"me@example.com", "carol@example.com"},
		CcAddrs:   []string{"dave@example.com"},
		Date:      time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC),
	}
	body := message.Body{PlainText: "Original body.\nSecond line."}
	return orig, body
}

func TestNewReplyAddressesOnlySender(t *testing.T) {
	cfg := config.Account{Email: "me@example.com"}
	orig, body := testOrig()

	d := NewReply(cfg, orig, body, false)
	if len(d.To) != 1 || d.To[0] != "alice@example.com" {
		t.Fatalf("unexpected To: %+v", d.To)
	}
	if len(d.Cc) != 0 {
		t.Fatalf("expected no Cc for non-reply-all, got %+v", d.Cc)
	}
	if d.Subject != "Re: Hi" {
		t.Fatalf("Subject = %q, want %q", d.Subject, "Re: Hi")
	}
	if d.InReplyTo != "orig-id@example.com" {
		t.Fatalf("InReplyTo = %q", d.InReplyTo)
	}
	if !strings.Contains(d.Body, "> Original body.") || !strings.Contains(d.Body, "> Second line.") {
		t.Fatalf("unexpected quoted body: %q", d.Body)
	}
}

func TestNewReplyAllExcludesSelfAndSender(t *testing.T) {
	cfg := config.Account{Email: "me@example.com"}
	orig, body := testOrig()

	d := NewReply(cfg, orig, body, true)
	if len(d.To) != 2 || d.To[0] != "alice@example.com" || d.To[1] != "carol@example.com" {
		t.Fatalf("unexpected To: %+v", d.To)
	}
	if len(d.Cc) != 1 || d.Cc[0] != "dave@example.com" {
		t.Fatalf("unexpected Cc: %+v", d.Cc)
	}
}

func TestNewReplyDoesNotDoublePrefixSubject(t *testing.T) {
	cfg := config.Account{Email: "me@example.com"}
	orig, body := testOrig()
	orig.Subject = "Re: Hi"

	d := NewReply(cfg, orig, body, false)
	if d.Subject != "Re: Hi" {
		t.Fatalf("Subject = %q, want %q", d.Subject, "Re: Hi")
	}
}
