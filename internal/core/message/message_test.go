package message

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/storage/blob"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

func newTestService(t *testing.T) (*Service, *sqlite.DB, int64, int64) {
	t.Helper()
	ctx := context.Background()

	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	accountID, err := db.UpsertAccount(ctx, "work", "me@example.com", "Work")
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	folderID, err := db.UpsertFolder(ctx, accountID, "INBOX", "INBOX", "/", `\Inbox`)
	if err != nil {
		t.Fatalf("UpsertFolder: %v", err)
	}

	blobs := blob.NewStore(t.TempDir())
	return NewService(db, blobs), db, accountID, folderID
}

func TestListReturnsCachedHeaders(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, folderID := newTestService(t)

	headers := []sqlite.MessageHeader{
		{UID: 1, Subject: "Hello", FromAddr: "a@example.com", Date: time.Now(), Flags: []string{`\Seen`}},
		{UID: 2, Subject: "World", FromAddr: "b@example.com", Date: time.Now().Add(time.Minute), Flags: nil},
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, headers); err != nil {
		t.Fatalf("UpsertMessageHeaders: %v", err)
	}

	msgs, err := svc.List(ctx, "work", "INBOX")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	// Most recent first.
	if msgs[0].Subject != "World" || msgs[1].Subject != "Hello" {
		t.Fatalf("unexpected order: %+v", msgs)
	}
}

func TestListUnknownAccountErrors(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	if _, err := svc.List(context.Background(), "nope", "INBOX"); err == nil {
		t.Fatalf("expected error for unsynced account")
	}
}

func TestBodyReturnsCachedRawWithoutNetwork(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, folderID := newTestService(t)

	raw := "From: a@example.com\r\nSubject: Hi\r\nContent-Type: text/plain\r\n\r\nHello there.\r\n"
	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, []sqlite.MessageHeader{{UID: 1, Subject: "Hi"}}); err != nil {
		t.Fatalf("UpsertMessageHeaders: %v", err)
	}

	ref := svc.blobs.Ref(accountID, folderID, 1)
	if err := svc.blobs.Write(ref, []byte(raw)); err != nil {
		t.Fatalf("blobs.Write: %v", err)
	}
	if err := db.SetMessageBodyCached(ctx, folderID, 1, ref); err != nil {
		t.Fatalf("SetMessageBodyCached: %v", err)
	}

	cfg := config.Account{Slug: "work", Email: "me@example.com"}
	body, err := svc.Body(ctx, cfg, "INBOX", 1)
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	if string(body.Raw) != raw {
		t.Fatalf("unexpected raw body: %q", body.Raw)
	}
	if !strings.Contains(body.PlainText, "Hello there.") {
		t.Fatalf("unexpected plain text: %q", body.PlainText)
	}
}
