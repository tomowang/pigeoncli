package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestFormatDateRoundTrip(t *testing.T) {
	want := time.Date(2026, 3, 5, 14, 30, 45, 123456789, time.FixedZone("PST", -8*60*60))

	got := parseDate(formatDate(want))

	if !got.Equal(want) {
		t.Fatalf("round trip mismatch: got %v, want %v", got, want)
	}
	if got.Location() != time.UTC {
		t.Fatalf("expected formatDate to normalize to UTC, got location %v", got.Location())
	}
}

func TestParseDateLegacyFormat(t *testing.T) {
	// Rows written before the RFC3339Nano fix hold Go's default
	// time.Time.String() format, e.g. "2026-03-05 14:30:45 +0000 UTC".
	got := parseDate("2026-03-05 14:30:45 +0000 UTC")
	want := time.Date(2026, 3, 5, 14, 30, 45, 0, time.UTC)

	if !got.Equal(want) {
		t.Fatalf("legacy format parse: got %v, want %v", got, want)
	}
}

func TestParseDateEmptyAndMalformed(t *testing.T) {
	cases := []string{
		"",
		"not a date",
		"2026-03-05",
	}
	for _, s := range cases {
		if got := parseDate(s); !got.IsZero() {
			t.Errorf("parseDate(%q) = %v, want zero time", s, got)
		}
	}
}

func TestMergeFlags(t *testing.T) {
	cases := []struct {
		name             string
		cur, add, remove []string
		want             []string
	}{
		{"add to empty", nil, []string{`\Seen`}, nil, []string{`\Seen`}},
		{"add keeps order and skips duplicates", []string{`\Flagged`, `\Seen`}, []string{`\seen`, `\Answered`}, nil, []string{`\Flagged`, `\Seen`, `\Answered`}},
		{"remove is case-insensitive", []string{`\Seen`, `\Flagged`}, nil, []string{`\SEEN`}, []string{`\Flagged`}},
		{"remove the last flag yields empty, not nil", []string{`\Seen`}, nil, []string{`\Seen`}, []string{}},
	}
	for _, c := range cases {
		got := mergeFlags(c.cur, c.add, c.remove)
		if got == nil || len(got) != len(c.want) {
			t.Errorf("%s: got %#v, want %#v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %#v, want %#v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestApplyFlagChangesDeleteMessagesAndRefreshCounts(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	accountID, err := db.UpsertAccount(ctx, "work", "me@example.com", "Work")
	if err != nil {
		t.Fatal(err)
	}
	folderID, err := db.UpsertFolder(ctx, accountID, "INBOX", "INBOX", "/", `\Inbox`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, []MessageHeader{
		{UID: 1, Subject: "a", Flags: []string{`\Flagged`}},
		{UID: 2, Subject: "b", Flags: []string{`\Seen`}},
		{UID: 3, Subject: "c"},
	}); err != nil {
		t.Fatal(err)
	}
	counts := func() (total, unread int) {
		t.Helper()
		rows, err := db.ListFolders(ctx, accountID)
		if err != nil || len(rows) != 1 {
			t.Fatalf("ListFolders: %v %v", rows, err)
		}
		return rows[0].TotalCount, rows[0].UnreadCount
	}

	// Counts are only maintained by sync, so they're stale until refreshed.
	if total, unread := counts(); total != 0 || unread != 0 {
		t.Fatalf("precondition: counts = %d/%d, want 0/0", total, unread)
	}
	if err := db.RefreshFolderCounts(ctx, folderID); err != nil {
		t.Fatal(err)
	}
	if total, unread := counts(); total != 3 || unread != 2 {
		t.Fatalf("counts = %d/%d, want 3/2", total, unread)
	}

	// UID 99 has no row: skipped, not an error.
	if err := db.ApplyFlagChanges(ctx, folderID, []uint32{1, 3, 99}, []string{`\Seen`}, nil); err != nil {
		t.Fatalf("ApplyFlagChanges add: %v", err)
	}
	flags, err := db.ListMessageFlags(ctx, folderID)
	if err != nil {
		t.Fatal(err)
	}
	if got := flags[1]; len(got) != 2 || got[0] != `\Flagged` || got[1] != `\Seen` {
		t.Fatalf("uid 1 flags = %v, want [\\Flagged \\Seen]", got)
	}
	if got := flags[3]; len(got) != 1 || got[0] != `\Seen` {
		t.Fatalf("uid 3 flags = %v, want [\\Seen]", got)
	}
	if err := db.RefreshFolderCounts(ctx, folderID); err != nil {
		t.Fatal(err)
	}
	if _, unread := counts(); unread != 0 {
		t.Fatalf("unread after marking all read = %d, want 0", unread)
	}

	if err := db.ApplyFlagChanges(ctx, folderID, []uint32{2}, nil, []string{`\Seen`}); err != nil {
		t.Fatalf("ApplyFlagChanges remove: %v", err)
	}
	if err := db.DeleteMessages(ctx, folderID, []uint32{1, 99}); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}
	if err := db.RefreshFolderCounts(ctx, folderID); err != nil {
		t.Fatal(err)
	}
	if total, unread := counts(); total != 2 || unread != 1 {
		t.Fatalf("counts after edits = %d/%d, want 2/1", total, unread)
	}
	flags, _ = db.ListMessageFlags(ctx, folderID)
	if _, ok := flags[1]; ok || len(flags) != 2 {
		t.Fatalf("remaining rows = %v, want uids 2 and 3", flags)
	}
}
