package message

import (
	"context"
	"os"
	"path/filepath"
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
	t.Cleanup(func() { _ = db.Close() })

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

func TestSearchReturnsMatchesWithFolderPath(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, folderID := newTestService(t)

	headers := []sqlite.MessageHeader{
		{UID: 1, Subject: "Quarterly Report", FromAddr: "alice@example.com", Date: time.Now()},
		{UID: 2, Subject: "Lunch plans", FromAddr: "bob@example.com", Date: time.Now()},
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, headers); err != nil {
		t.Fatalf("UpsertMessageHeaders: %v", err)
	}

	results, err := svc.Search(ctx, "work", "quarterly")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Subject != "Quarterly Report" || results[0].FolderPath != "INBOX" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSearchUnknownAccountErrors(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	if _, err := svc.Search(context.Background(), "nope", "quarterly"); err == nil {
		t.Fatalf("expected error for unsynced account")
	}
}

const rawWithAttachment = "From: a@example.com\r\n" +
	"To: b@example.com\r\n" +
	"Subject: Hi\r\n" +
	"Content-Type: multipart/mixed; boundary=BOUNDARY\r\n" +
	"\r\n" +
	"--BOUNDARY\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Hello there.\r\n" +
	"--BOUNDARY\r\n" +
	"Content-Type: application/pdf; name=\"invoice.pdf\"\r\n" +
	"Content-Disposition: attachment; filename=\"invoice.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"SGVsbG8sIHdvcmxkIQ==\r\n" +
	"--BOUNDARY--\r\n"

func TestBodyListsAttachments(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, folderID := newTestService(t)

	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, []sqlite.MessageHeader{{UID: 1, Subject: "Hi"}}); err != nil {
		t.Fatalf("UpsertMessageHeaders: %v", err)
	}
	ref := svc.blobs.Ref(accountID, folderID, 1)
	if err := svc.blobs.Write(ref, []byte(rawWithAttachment)); err != nil {
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
	if len(body.Attachments) != 1 || body.Attachments[0].Filename != "invoice.pdf" {
		t.Fatalf("unexpected attachments: %+v", body.Attachments)
	}
}

func TestSaveAttachmentWritesDecodedBytes(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, folderID := newTestService(t)

	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, []sqlite.MessageHeader{{UID: 1, Subject: "Hi"}}); err != nil {
		t.Fatalf("UpsertMessageHeaders: %v", err)
	}
	ref := svc.blobs.Ref(accountID, folderID, 1)
	if err := svc.blobs.Write(ref, []byte(rawWithAttachment)); err != nil {
		t.Fatalf("blobs.Write: %v", err)
	}
	if err := db.SetMessageBodyCached(ctx, folderID, 1, ref); err != nil {
		t.Fatalf("SetMessageBodyCached: %v", err)
	}

	cfg := config.Account{Slug: "work", Email: "me@example.com"}
	destPath := filepath.Join(t.TempDir(), "invoice.pdf")
	if err := svc.SaveAttachment(ctx, cfg, "INBOX", 1, 0, destPath); err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "Hello, world!" {
		t.Fatalf("unexpected saved content: %q", data)
	}
}

func TestRelatedResolvesInReplyToAndReferencesAcrossFolders(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, inboxID := newTestService(t)

	sentID, err := db.UpsertFolder(ctx, accountID, "Sent", "Sent", "/", `\Sent`)
	if err != nil {
		t.Fatalf("UpsertFolder (sent): %v", err)
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, sentID, []sqlite.MessageHeader{
		{UID: 1, MessageID: "<original@example.com>", Subject: "Original", Date: time.Now()},
	}); err != nil {
		t.Fatalf("UpsertMessageHeaders (sent): %v", err)
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, inboxID, []sqlite.MessageHeader{
		{UID: 1, MessageID: "<reply@example.com>", Subject: "Re: Original", Date: time.Now()},
	}); err != nil {
		t.Fatalf("UpsertMessageHeaders (inbox): %v", err)
	}

	reply := Message{InReplyTo: "<original@example.com>", References: []string{"<original@example.com>"}}
	related, err := svc.Related(ctx, "work", reply)
	if err != nil {
		t.Fatalf("Related: %v", err)
	}
	if len(related) != 1 || related[0].Subject != "Original" || related[0].FolderPath != "Sent" {
		t.Fatalf("unexpected related messages: %+v", related)
	}
}

func TestRelatedWithNoReferencesReturnsEmpty(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	related, err := svc.Related(context.Background(), "work", Message{})
	if err != nil {
		t.Fatalf("Related: %v", err)
	}
	if len(related) != 0 {
		t.Fatalf("expected no related messages, got %+v", related)
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
