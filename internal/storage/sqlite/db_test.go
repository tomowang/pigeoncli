package sqlite

import (
	"context"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := db.migrate(context.Background()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestAccountFolderMessageLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	accountID, err := db.UpsertAccount(ctx, "work", "me@example.com", "Work")
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}

	folderID, err := db.UpsertFolder(ctx, accountID, "INBOX", "INBOX", "/", `\Inbox`)
	if err != nil {
		t.Fatalf("UpsertFolder: %v", err)
	}

	headers := []MessageHeader{
		{UID: 1, Subject: "Hello", FromAddr: "a@example.com", Date: time.Now(), Flags: []string{`\Seen`}},
		{UID: 2, Subject: "World", FromAddr: "b@example.com", Date: time.Now(), Flags: nil},
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, headers); err != nil {
		t.Fatalf("UpsertMessageHeaders: %v", err)
	}

	total, unread, err := db.UpdateFolderSyncState(ctx, folderID, 100, 3, 5, 0)
	if err != nil {
		t.Fatalf("UpdateFolderSyncState: %v", err)
	}
	if total != 2 || unread != 1 {
		t.Fatalf("expected UpdateFolderSyncState to return total=2 unread=1, got total=%d unread=%d", total, unread)
	}

	folders, err := db.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(folders))
	}
	f := folders[0]
	if f.TotalCount != 2 || f.UnreadCount != 1 {
		t.Fatalf("expected total=2 unread=1, got total=%d unread=%d", f.TotalCount, f.UnreadCount)
	}
	if f.UIDValidity != 100 || f.UIDNext != 3 || f.ModSeq != 5 {
		t.Fatalf("unexpected sync state: %+v", f)
	}

	// Reconcile: UID 1 was deleted server-side, UID 2 remains.
	if err := db.DeleteMessagesNotIn(ctx, folderID, []uint32{2}); err != nil {
		t.Fatalf("DeleteMessagesNotIn: %v", err)
	}
	if _, _, err := db.UpdateFolderSyncState(ctx, folderID, 100, 3, 5, 0); err != nil {
		t.Fatalf("UpdateFolderSyncState after delete: %v", err)
	}
	folders, err = db.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatalf("ListFolders after delete: %v", err)
	}
	if folders[0].TotalCount != 1 {
		t.Fatalf("expected total=1 after reconcile, got %d", folders[0].TotalCount)
	}

	if err := db.ClearFolderMessages(ctx, folderID); err != nil {
		t.Fatalf("ClearFolderMessages: %v", err)
	}
	if _, _, err := db.UpdateFolderSyncState(ctx, folderID, 100, 3, 5, 0); err != nil {
		t.Fatalf("UpdateFolderSyncState after clear: %v", err)
	}
	folders, err = db.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatalf("ListFolders after clear: %v", err)
	}
	if folders[0].TotalCount != 0 {
		t.Fatalf("expected total=0 after clear, got %d", folders[0].TotalCount)
	}

	if err := db.SetAccountSyncedNow(ctx, accountID); err != nil {
		t.Fatalf("SetAccountSyncedNow: %v", err)
	}
}

func TestFindMessagesByMessageIDs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	accountID, err := db.UpsertAccount(ctx, "work", "me@example.com", "Work")
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	inboxID, err := db.UpsertFolder(ctx, accountID, "INBOX", "INBOX", "/", `\Inbox`)
	if err != nil {
		t.Fatalf("UpsertFolder: %v", err)
	}
	sentID, err := db.UpsertFolder(ctx, accountID, "Sent", "Sent", "/", `\Sent`)
	if err != nil {
		t.Fatalf("UpsertFolder (sent): %v", err)
	}

	if err := db.UpsertMessageHeaders(ctx, accountID, sentID, []MessageHeader{
		{UID: 1, MessageID: "<original@example.com>", Subject: "Original", Date: time.Now()},
	}); err != nil {
		t.Fatalf("UpsertMessageHeaders (sent): %v", err)
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, inboxID, []MessageHeader{
		{
			UID: 1, MessageID: "<reply@example.com>", InReplyTo: "<original@example.com>",
			References: []string{"<original@example.com>"}, Subject: "Re: Original", Date: time.Now(),
		},
	}); err != nil {
		t.Fatalf("UpsertMessageHeaders (inbox): %v", err)
	}

	rows, err := db.ListMessages(ctx, inboxID, 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(rows) != 1 || len(rows[0].References) != 1 || rows[0].References[0] != "<original@example.com>" {
		t.Fatalf("expected references to round-trip, got %+v", rows)
	}

	related, err := db.FindMessagesByMessageIDs(ctx, accountID, []string{rows[0].InReplyTo})
	if err != nil {
		t.Fatalf("FindMessagesByMessageIDs: %v", err)
	}
	if len(related) != 1 || related[0].Subject != "Original" || related[0].FolderPath != "Sent" {
		t.Fatalf("unexpected related messages: %+v", related)
	}

	if empty, err := db.FindMessagesByMessageIDs(ctx, accountID, nil); err != nil || len(empty) != 0 {
		t.Fatalf("expected no results for empty id list, got %+v err=%v", empty, err)
	}
}

func TestFolderIDAndMessageBodyState(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	accountID, err := db.UpsertAccount(ctx, "work", "me@example.com", "Work")
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	if _, ok, err := db.FolderID(ctx, accountID, "INBOX"); err != nil || ok {
		t.Fatalf("FolderID before sync: ok=%v err=%v, want ok=false", ok, err)
	}

	folderID, err := db.UpsertFolder(ctx, accountID, "INBOX", "INBOX", "/", `\Inbox`)
	if err != nil {
		t.Fatalf("UpsertFolder: %v", err)
	}
	gotID, ok, err := db.FolderID(ctx, accountID, "INBOX")
	if err != nil || !ok || gotID != folderID {
		t.Fatalf("FolderID: got id=%d ok=%v err=%v, want id=%d ok=true", gotID, ok, err, folderID)
	}

	headers := []MessageHeader{
		{UID: 1, Subject: "Hello", FromAddr: "a@example.com", ToAddrs: []string{"me@example.com"}, Date: time.Now(), Flags: []string{`\Seen`}},
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, headers); err != nil {
		t.Fatalf("UpsertMessageHeaders: %v", err)
	}

	msgs, err := db.ListMessages(ctx, folderID, 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Subject != "Hello" || len(msgs[0].ToAddrs) != 1 {
		t.Fatalf("unexpected messages: %+v", msgs)
	}

	ref, synced, err := db.MessageBodyState(ctx, folderID, 1)
	if err != nil {
		t.Fatalf("MessageBodyState: %v", err)
	}
	if synced || ref != "" {
		t.Fatalf("expected unsynced body before caching, got ref=%q synced=%v", ref, synced)
	}

	if err := db.SetMessageBodyCached(ctx, folderID, 1, "1/1/1.eml"); err != nil {
		t.Fatalf("SetMessageBodyCached: %v", err)
	}
	ref, synced, err = db.MessageBodyState(ctx, folderID, 1)
	if err != nil {
		t.Fatalf("MessageBodyState after cache: %v", err)
	}
	if !synced || ref != "1/1/1.eml" {
		t.Fatalf("expected cached body state, got ref=%q synced=%v", ref, synced)
	}
}
