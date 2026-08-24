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
	t.Cleanup(func() { db.Close() })
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

	if err := db.UpdateFolderSyncState(ctx, folderID, 100, 3); err != nil {
		t.Fatalf("UpdateFolderSyncState: %v", err)
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
	if f.UIDValidity != 100 || f.UIDNext != 3 {
		t.Fatalf("unexpected sync state: %+v", f)
	}

	// Reconcile: UID 1 was deleted server-side, UID 2 remains.
	if err := db.DeleteMessagesNotIn(ctx, folderID, []uint32{2}); err != nil {
		t.Fatalf("DeleteMessagesNotIn: %v", err)
	}
	if err := db.UpdateFolderSyncState(ctx, folderID, 100, 3); err != nil {
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
	if err := db.UpdateFolderSyncState(ctx, folderID, 100, 3); err != nil {
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
