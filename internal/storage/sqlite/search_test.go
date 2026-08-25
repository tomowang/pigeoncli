package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestSearchMessages(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	accountID, err := db.UpsertAccount(ctx, "work", "me@example.com", "Work")
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	otherAccountID, err := db.UpsertAccount(ctx, "personal", "me@personal.example", "Personal")
	if err != nil {
		t.Fatalf("UpsertAccount (other): %v", err)
	}

	inboxID, err := db.UpsertFolder(ctx, accountID, "INBOX", "INBOX", "/", `\Inbox`)
	if err != nil {
		t.Fatalf("UpsertFolder: %v", err)
	}
	otherInboxID, err := db.UpsertFolder(ctx, otherAccountID, "INBOX", "INBOX", "/", `\Inbox`)
	if err != nil {
		t.Fatalf("UpsertFolder (other account): %v", err)
	}

	headers := []MessageHeader{
		{UID: 1, Subject: "Quarterly Report", FromName: "Alice", FromAddr: "alice@example.com", Date: time.Now()},
		{UID: 2, Subject: "Lunch plans", FromName: "Bob", FromAddr: "bob@example.com", Date: time.Now()},
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, inboxID, headers); err != nil {
		t.Fatalf("UpsertMessageHeaders: %v", err)
	}
	if err := db.UpsertMessageHeaders(ctx, otherAccountID, otherInboxID, []MessageHeader{
		{UID: 1, Subject: "Quarterly Report", FromName: "Alice", FromAddr: "alice@example.com", Date: time.Now()},
	}); err != nil {
		t.Fatalf("UpsertMessageHeaders (other account): %v", err)
	}

	t.Run("matches subject", func(t *testing.T) {
		results, err := db.SearchMessages(ctx, accountID, "quarterly", 10)
		if err != nil {
			t.Fatalf("SearchMessages: %v", err)
		}
		if len(results) != 1 || results[0].Subject != "Quarterly Report" || results[0].FolderPath != "INBOX" {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	t.Run("matches from name prefix", func(t *testing.T) {
		results, err := db.SearchMessages(ctx, accountID, "ali", 10)
		if err != nil {
			t.Fatalf("SearchMessages: %v", err)
		}
		if len(results) != 1 || results[0].FromName != "Alice" {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	t.Run("scoped to account", func(t *testing.T) {
		results, err := db.SearchMessages(ctx, otherAccountID, "quarterly", 10)
		if err != nil {
			t.Fatalf("SearchMessages: %v", err)
		}
		if len(results) != 1 || results[0].FolderPath != "INBOX" {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		results, err := db.SearchMessages(ctx, accountID, "nonexistentterm", 10)
		if err != nil {
			t.Fatalf("SearchMessages: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("expected no results, got %+v", results)
		}
	})

	t.Run("empty query", func(t *testing.T) {
		results, err := db.SearchMessages(ctx, accountID, "   ", 10)
		if err != nil {
			t.Fatalf("SearchMessages: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("expected no results for empty query, got %+v", results)
		}
	})

	t.Run("special characters do not error", func(t *testing.T) {
		for _, q := range []string{`"quoted"`, "quarterly*", "quarterly:report", "quarterly OR report", "quarterly)("} {
			if _, err := db.SearchMessages(ctx, accountID, q, 10); err != nil {
				t.Fatalf("SearchMessages(%q): %v", q, err)
			}
		}
	})

	t.Run("index stays in sync after delete", func(t *testing.T) {
		if err := db.DeleteMessagesNotIn(ctx, inboxID, []uint32{2}); err != nil {
			t.Fatalf("DeleteMessagesNotIn: %v", err)
		}
		results, err := db.SearchMessages(ctx, accountID, "quarterly", 10)
		if err != nil {
			t.Fatalf("SearchMessages: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("expected deleted message to drop out of the index, got %+v", results)
		}
	})

	t.Run("index stays in sync after update", func(t *testing.T) {
		if err := db.UpsertMessageHeaders(ctx, accountID, inboxID, []MessageHeader{
			{UID: 2, Subject: "Renamed subject", FromName: "Bob", FromAddr: "bob@example.com", Date: time.Now()},
		}); err != nil {
			t.Fatalf("UpsertMessageHeaders (update): %v", err)
		}
		oldResults, err := db.SearchMessages(ctx, accountID, "lunch", 10)
		if err != nil {
			t.Fatalf("SearchMessages(old subject): %v", err)
		}
		if len(oldResults) != 0 {
			t.Fatalf("expected old subject to no longer match, got %+v", oldResults)
		}
		newResults, err := db.SearchMessages(ctx, accountID, "renamed", 10)
		if err != nil {
			t.Fatalf("SearchMessages(new subject): %v", err)
		}
		if len(newResults) != 1 {
			t.Fatalf("expected new subject to match, got %+v", newResults)
		}
	})
}
