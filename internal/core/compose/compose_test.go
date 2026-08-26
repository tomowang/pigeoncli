package compose

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/message"
	"github.com/tomowang/pigeoncli/internal/core/signature"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
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

	d := buildReplyDraft(cfg, orig, body, false)
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

	d := buildReplyDraft(cfg, orig, body, true)
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

	d := buildReplyDraft(cfg, orig, body, false)
	if d.Subject != "Re: Hi" {
		t.Fatalf("Subject = %q, want %q", d.Subject, "Re: Hi")
	}
}

func TestServiceNewReplyAppendsDefaultSignature(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	sigSvc := signature.NewService(db)
	if _, err := sigSvc.Add(ctx, "", "Global", "-- \nSent from pigeon", true); err != nil {
		t.Fatalf("Add signature: %v", err)
	}

	svc := NewService(nil, sigSvc)
	cfg := config.Account{Slug: "work", Email: "me@example.com"}
	orig, body := testOrig()

	d := svc.NewReply(ctx, cfg, orig, body, false)
	if !strings.Contains(d.Body, "Sent from pigeon") {
		t.Fatalf("expected signature appended, got body: %q", d.Body)
	}
	if !strings.Contains(d.Body, "> Original body.") {
		t.Fatalf("expected quoted body preserved, got: %q", d.Body)
	}
}

func TestServiceNewReplyWithoutSignatureServiceSkipsSignature(t *testing.T) {
	svc := NewService(nil, nil)
	cfg := config.Account{Slug: "work", Email: "me@example.com"}
	orig, body := testOrig()

	d := svc.NewReply(context.Background(), cfg, orig, body, false)
	if strings.Contains(d.Body, "-- ") {
		t.Fatalf("expected no signature appended, got body: %q", d.Body)
	}
}

func TestNewMessageIsEmptyWithSignature(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	sigSvc := signature.NewService(db)
	if _, err := sigSvc.Add(ctx, "", "Global", "-- \nSent from pigeon", true); err != nil {
		t.Fatalf("Add signature: %v", err)
	}

	svc := NewService(nil, sigSvc)
	cfg := config.Account{Slug: "work", Email: "me@example.com"}

	d := svc.NewMessage(ctx, cfg)
	if len(d.To) != 0 || len(d.Cc) != 0 || d.Subject != "" || d.InReplyTo != "" {
		t.Fatalf("expected an otherwise-empty draft, got %+v", d)
	}
	if !strings.Contains(d.Body, "Sent from pigeon") {
		t.Fatalf("expected signature in new-message body, got %q", d.Body)
	}
}

func TestNewMessageWithoutSignatureServiceIsEmpty(t *testing.T) {
	svc := NewService(nil, nil)
	cfg := config.Account{Slug: "work", Email: "me@example.com"}

	d := svc.NewMessage(context.Background(), cfg)
	if d.Body != "" {
		t.Fatalf("expected empty body, got %q", d.Body)
	}
}
