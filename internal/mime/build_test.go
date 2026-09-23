package mime

import (
	"os"
	"strings"
	"testing"

	"github.com/emersion/go-message"
)

func TestBuildMessageRoundTrips(t *testing.T) {
	raw, err := BuildMessage(
		Recipient{Name: "Alice", Addr: "alice@example.com"},
		[]Recipient{{Name: "Bob", Addr: "bob@example.com"}},
		[]Recipient{{Addr: "carol@example.com"}},
		nil,
		"Re: Hi",
		"Thanks!\n\n> original text\n",
		"orig-id@example.com",
		"orig-id@example.com",
		nil,
	)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	entity, err := message.Read(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("parse built message: %v", err)
	}

	if got, _ := entity.Header.Text("Subject"); got != "Re: Hi" {
		t.Fatalf("Subject = %q, want %q", got, "Re: Hi")
	}
	if got, _ := entity.Header.Text("To"); !strings.Contains(got, "bob@example.com") {
		t.Fatalf("To = %q, want to contain bob@example.com", got)
	}
	if got, _ := entity.Header.Text("Cc"); !strings.Contains(got, "carol@example.com") {
		t.Fatalf("Cc = %q, want to contain carol@example.com", got)
	}
	if got, _ := entity.Header.Text("In-Reply-To"); !strings.Contains(got, "orig-id@example.com") {
		t.Fatalf("In-Reply-To = %q, want to contain orig-id@example.com", got)
	}

	plain, err := PlainText(raw)
	if err != nil {
		t.Fatalf("PlainText: %v", err)
	}
	if !strings.Contains(plain, "Thanks!") || !strings.Contains(plain, "> original text") {
		t.Fatalf("unexpected rendered body: %q", plain)
	}
}

func TestBuildMessageWithAttachments(t *testing.T) {
	raw, err := BuildMessage(
		Recipient{Name: "Alice", Addr: "alice@example.com"},
		[]Recipient{{Addr: "bob@example.com"}},
		nil, nil,
		"Report",
		"See attached.",
		"", "",
		[]OutgoingAttachment{
			{Filename: "report.pdf", Data: []byte("%PDF-1.4 fake")},
			{Filename: "notes.txt", Data: []byte("plain text notes")},
			{Filename: "mystery.xyz", Data: []byte{0x00, 0x01, 0x02}},
		},
	)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	plain, err := PlainText(raw)
	if err != nil {
		t.Fatalf("PlainText: %v", err)
	}
	if !strings.Contains(plain, "See attached.") {
		t.Fatalf("unexpected rendered body: %q", plain)
	}

	atts, err := Attachments(raw)
	if err != nil {
		t.Fatalf("Attachments: %v", err)
	}
	if len(atts) != 3 {
		t.Fatalf("got %d attachments, want 3: %+v", len(atts), atts)
	}
	want := map[string]string{
		"report.pdf":  "application/pdf",
		"notes.txt":   "text/plain",
		"mystery.xyz": "application/octet-stream", // unrecognized extension
	}
	for _, a := range atts {
		if ct, ok := want[a.Filename]; !ok {
			t.Errorf("unexpected attachment %q", a.Filename)
		} else if a.ContentType != ct {
			t.Errorf("%s: ContentType = %q, want %q", a.Filename, a.ContentType, ct)
		}
	}

	// Round-trip the bytes back out, not just the metadata.
	dir := t.TempDir()
	dest := dir + "/out.pdf"
	if err := SaveAttachment(raw, 0, dest); err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "%PDF-1.4 fake" {
		t.Fatalf("saved attachment content = %q", got)
	}
}

func TestBuildMessageWithoutAttachmentsIsSinglePart(t *testing.T) {
	raw, err := BuildMessage(Recipient{Addr: "alice@example.com"}, nil, nil, nil, "Hi", "body", "", "", nil)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	atts, err := Attachments(raw)
	if err != nil {
		t.Fatalf("Attachments: %v", err)
	}
	if len(atts) != 0 {
		t.Fatalf("expected no attachments, got %+v", atts)
	}
	if strings.Contains(string(raw), "multipart/mixed") {
		t.Fatal("a message with no attachments shouldn't be multipart")
	}
}

func TestBuildMessageWithBccSetsHeaderAndRoundTripsViaDraftBcc(t *testing.T) {
	raw, err := BuildMessage(
		Recipient{Addr: "alice@example.com"},
		[]Recipient{{Addr: "bob@example.com"}},
		nil,
		[]Recipient{{Addr: "secret@example.com"}, {Addr: "other@example.com"}},
		"Hi", "body", "", "", nil,
	)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	if !strings.Contains(string(raw), "secret@example.com") {
		t.Fatal("Bcc header should be present on a message built with bcc addresses")
	}

	got, err := DraftBcc(raw)
	if err != nil {
		t.Fatalf("DraftBcc: %v", err)
	}
	if len(got) != 2 || got[0] != "secret@example.com" || got[1] != "other@example.com" {
		t.Fatalf("DraftBcc = %v, want [secret@example.com other@example.com]", got)
	}
}

func TestBuildMessageWithoutBccHasNoBccHeader(t *testing.T) {
	raw, err := BuildMessage(Recipient{Addr: "alice@example.com"}, nil, nil, nil, "Hi", "body", "", "", nil)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "bcc:") {
		t.Fatal("a message built with no bcc addresses shouldn't have a Bcc header at all")
	}
	got, err := DraftBcc(raw)
	if err != nil || got != nil {
		t.Fatalf("DraftBcc = %v, %v; want nil, nil", got, err)
	}
}
