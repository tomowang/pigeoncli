package mime

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestAttachmentsListsAttachmentParts(t *testing.T) {
	atts, err := Attachments([]byte(rawWithAttachment))
	if err != nil {
		t.Fatalf("Attachments: %v", err)
	}
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %+v", atts)
	}
	got := atts[0]
	if got.Index != 0 || got.Filename != "invoice.pdf" || got.ContentType != "application/pdf" {
		t.Fatalf("unexpected attachment metadata: %+v", got)
	}
	if got.Size != int64(len("Hello, world!")) {
		t.Fatalf("expected decoded size %d, got %d", len("Hello, world!"), got.Size)
	}
}

func TestAttachmentsFromPlainMessageIsEmpty(t *testing.T) {
	raw := "From: a@example.com\r\nSubject: Hi\r\nContent-Type: text/plain\r\n\r\nHello.\r\n"
	atts, err := Attachments([]byte(raw))
	if err != nil {
		t.Fatalf("Attachments: %v", err)
	}
	if len(atts) != 0 {
		t.Fatalf("expected no attachments, got %+v", atts)
	}
}

func TestSaveAttachmentWritesDecodedBytes(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), "invoice.pdf")
	if err := SaveAttachment([]byte(rawWithAttachment), 0, destPath); err != nil {
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

func TestSaveAttachmentUnknownIndexErrors(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), "out")
	if err := SaveAttachment([]byte(rawWithAttachment), 1, destPath); err == nil {
		t.Fatalf("expected error for out-of-range attachment index")
	}
}
