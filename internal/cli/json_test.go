package cli

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

func TestToJSONMessageDerivesUnreadAndStarred(t *testing.T) {
	cases := []struct {
		name        string
		flags       []string
		wantUnread  bool
		wantStarred bool
	}{
		{"no flags", nil, true, false},
		{"seen only", []string{`\Seen`}, false, false},
		{"seen and flagged", []string{`\Seen`, `\Flagged`}, false, true},
		{"flagged only", []string{`\Flagged`}, true, true},
	}
	for _, c := range cases {
		got := toJSONMessage(message.Message{UID: 1, Flags: c.flags})
		if got.Unread != c.wantUnread || got.Starred != c.wantStarred {
			t.Errorf("%s: unread=%v starred=%v, want %v/%v", c.name, got.Unread, got.Starred, c.wantUnread, c.wantStarred)
		}
	}
}

func TestToJSONMessagePreservesFields(t *testing.T) {
	when := time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC)
	m := message.Message{
		UID: 42, MessageID: "<a@b>", InReplyTo: "<c@d>", References: []string{"<c@d>"},
		Subject: "Hi", FromName: "Alice", FromAddr: "alice@example.com",
		ToAddrs: []string{"bob@example.com"}, CcAddrs: []string{"carol@example.com"},
		Date: when, Flags: []string{`\Seen`}, Size: 1234,
	}
	got := toJSONMessage(m)
	if got.UID != 42 || got.MessageID != "<a@b>" || got.Subject != "Hi" || got.FromAddr != "alice@example.com" ||
		got.Size != 1234 || !got.Date.Equal(when) || len(got.ToAddrs) != 1 || len(got.CcAddrs) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestToJSONMessagesPreservesOrder(t *testing.T) {
	msgs := []message.Message{{UID: 1}, {UID: 2}, {UID: 3}}
	got := toJSONMessages(msgs)
	if len(got) != 3 || got[0].UID != 1 || got[1].UID != 2 || got[2].UID != 3 {
		t.Fatalf("got %+v", got)
	}
	if toJSONMessages(nil) == nil {
		t.Fatal("toJSONMessages(nil) should still marshal as [], not null")
	}
}

func TestToJSONSearchResultsIncludesFolder(t *testing.T) {
	results := []message.SearchResult{
		{Message: message.Message{UID: 1, Subject: "a"}, FolderPath: "INBOX"},
		{Message: message.Message{UID: 2, Subject: "b"}, FolderPath: "Archive"},
	}
	got := toJSONSearchResults(results)
	if len(got) != 2 || got[0].Folder != "INBOX" || got[1].Folder != "Archive" {
		t.Fatalf("got %+v", got)
	}
	if got[0].UID != 1 || got[0].Subject != "a" {
		t.Fatalf("embedded message fields missing: %+v", got[0])
	}
}

func TestToJSONAttachments(t *testing.T) {
	if got := toJSONAttachments(nil); got != nil {
		t.Fatalf("toJSONAttachments(nil) = %+v, want nil", got)
	}
	atts := []message.Attachment{{Index: 0, Filename: "a.pdf", ContentType: "application/pdf", Size: 99}}
	got := toJSONAttachments(atts)
	if len(got) != 1 || got[0].Filename != "a.pdf" || got[0].ContentType != "application/pdf" || got[0].Size != 99 {
		t.Fatalf("got %+v", got)
	}
}

func TestToJSONFolders(t *testing.T) {
	folders := []folder.Folder{
		{Path: "INBOX", Name: "INBOX", SpecialUse: `\Inbox`, TotalCount: 10, UnreadCount: 3},
	}
	got := toJSONFolders(folders)
	if len(got) != 1 || got[0].Path != "INBOX" || got[0].SpecialUse != `\Inbox` || got[0].TotalCount != 10 || got[0].UnreadCount != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestWriteJSONProducesParseableOutput(t *testing.T) {
	var buf bytes.Buffer
	msgs := toJSONMessages([]message.Message{{UID: 1, Subject: "Hi"}})
	if err := writeJSON(&buf, msgs); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	var decoded []jsonMessage
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output isn't valid JSON: %v\n%s", err, buf.String())
	}
	if len(decoded) != 1 || decoded[0].UID != 1 || decoded[0].Subject != "Hi" {
		t.Fatalf("round-tripped = %+v", decoded)
	}

	// Field names are the documented snake_case JSON keys, not Go's.
	if !bytes.Contains(buf.Bytes(), []byte(`"uid"`)) || bytes.Contains(buf.Bytes(), []byte(`"UID"`)) {
		t.Fatalf("expected lowercase \"uid\" key, got:\n%s", buf.String())
	}
}

func TestWriteJSONEmptySliceIsEmptyArrayNotNull(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, toJSONMessages(nil)); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if got := buf.String(); got != "[]\n" {
		t.Fatalf("got %q, want \"[]\\n\" (empty results must still be valid JSON, not null)", got)
	}
}
