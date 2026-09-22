package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/message"
	"github.com/tomowang/pigeoncli/internal/core/signature"
	"github.com/tomowang/pigeoncli/internal/mime"
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

func TestNewAttachmentFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	att, err := NewAttachmentFromFile(path)
	if err != nil {
		t.Fatalf("NewAttachmentFromFile: %v", err)
	}
	if att.Filename != "notes.txt" || string(att.Data) != "hello" {
		t.Fatalf("got %+v", att)
	}
}

func TestNewAttachmentFromFileErrors(t *testing.T) {
	dir := t.TempDir()

	if _, err := NewAttachmentFromFile(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if _, err := NewAttachmentFromFile(dir); err == nil {
		t.Fatal("expected an error for a directory")
	}

	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, make([]byte, MaxAttachmentSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAttachmentFromFile(big); !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("got %v, want ErrAttachmentTooLarge", err)
	}
}

func TestSendIncludesAttachments(t *testing.T) {
	// Send dials real IMAP/SMTP servers, which this test doesn't stand up;
	// it only checks that the draft's attachments survive into the built
	// MIME message, via the same helper Send uses.
	draft := Draft{
		To:          []string{"bob@example.com"},
		Subject:     "Hi",
		Body:        "See attached.",
		Attachments: []Attachment{{Filename: "notes.txt", Data: []byte("hello")}},
	}
	from := mime.Recipient{Addr: "me@example.com"}
	raw, err := mime.BuildMessage(from, recipients(draft.To), recipients(draft.Cc), draft.Subject, draft.Body,
		draft.InReplyTo, draft.References, outgoingAttachments(draft.Attachments))
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	atts, err := mime.Attachments(raw)
	if err != nil {
		t.Fatalf("Attachments: %v", err)
	}
	if len(atts) != 1 || atts[0].Filename != "notes.txt" {
		t.Fatalf("got %+v", atts)
	}
}
