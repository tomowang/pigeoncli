package mime

import (
	"strings"
	"testing"
)

func TestPlainTextFromTextPlainMessage(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: Hi\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hello there.\r\n"

	got, err := PlainText([]byte(raw))
	if err != nil {
		t.Fatalf("PlainText: %v", err)
	}
	if !strings.Contains(got, "Hello there.") {
		t.Fatalf("expected plain body, got %q", got)
	}
}

func TestPlainTextFallsBackToHTML(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: Hi\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<html><body><p>Hello <b>there</b>.</p><script>evil()</script></body></html>\r\n"

	got, err := PlainText([]byte(raw))
	if err != nil {
		t.Fatalf("PlainText: %v", err)
	}
	if !strings.Contains(got, "Hello there.") {
		t.Fatalf("expected stripped html body, got %q", got)
	}
	if strings.Contains(got, "evil()") {
		t.Fatalf("expected script contents to be dropped, got %q", got)
	}
}

func TestPlainTextPrefersPlainOverHTMLInMultipart(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: Hi\r\n" +
		"Content-Type: multipart/alternative; boundary=BOUNDARY\r\n" +
		"\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Plain version.\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>HTML version.</p>\r\n" +
		"--BOUNDARY--\r\n"

	got, err := PlainText([]byte(raw))
	if err != nil {
		t.Fatalf("PlainText: %v", err)
	}
	if !strings.Contains(got, "Plain version.") {
		t.Fatalf("expected plain part preferred, got %q", got)
	}
	if strings.Contains(got, "HTML version.") {
		t.Fatalf("expected html part to be ignored when plain exists, got %q", got)
	}
}
