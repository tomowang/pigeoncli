package mime

import (
	"strings"
	"testing"

	"github.com/emersion/go-message"
)

func TestBuildMessageRoundTrips(t *testing.T) {
	raw, err := BuildMessage(
		Recipient{Name: "Alice", Addr: "alice@example.com"},
		[]Recipient{{Name: "Bob", Addr: "bob@example.com"}},
		[]Recipient{{Addr: "carol@example.com"}},
		"Re: Hi",
		"Thanks!\n\n> original text\n",
		"orig-id@example.com",
		"orig-id@example.com",
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
