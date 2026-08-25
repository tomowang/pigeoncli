package mime

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/emersion/go-message"
)

// Attachment is one attachment part in a message, identified by a stable
// walk-order index so SaveAttachment can re-locate it.
type Attachment struct {
	Index       int
	Filename    string
	ContentType string
	Size        int64
}

// Attachments lists the attachment parts in raw (a full RFC 822 message):
// any part with Content-Disposition: attachment or a filename parameter, in
// walk order. It re-parses raw on every call (matching PlainText's
// approach) rather than keeping an open parser open — raw is already
// blob-cached after a message's first open, so re-walking costs nothing
// extra over the network.
func Attachments(raw []byte) ([]Attachment, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, fmt.Errorf("parse message: %w", err)
	}

	var out []Attachment
	walkErr := entity.Walk(func(path []int, e *message.Entity, err error) error {
		if err != nil {
			// Charset/encoding problem on this part — skip it rather than
			// aborting the whole walk over an otherwise-readable message.
			return nil
		}
		filename := attachmentFilename(e)
		if filename == "" {
			return nil
		}
		ct, _, _ := e.Header.ContentType()
		var size int64
		if body, err := io.ReadAll(e.Body); err == nil {
			size = int64(len(body))
		}
		out = append(out, Attachment{Index: len(out), Filename: filename, ContentType: ct, Size: size})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk message: %w", walkErr)
	}
	return out, nil
}

// SaveAttachment writes the decoded bytes of the index-th attachment part
// (0-based, in the same order Attachments returns them) in raw to
// destPath.
func SaveAttachment(raw []byte, index int, destPath string) error {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return fmt.Errorf("parse message: %w", err)
	}

	var (
		found bool
		n     int
	)
	walkErr := entity.Walk(func(path []int, e *message.Entity, err error) error {
		if err != nil || found {
			return nil
		}
		if attachmentFilename(e) == "" {
			return nil
		}
		if n != index {
			n++
			return nil
		}
		found = true

		body, readErr := io.ReadAll(e.Body)
		if readErr != nil {
			return fmt.Errorf("read attachment: %w", readErr)
		}
		if writeErr := os.WriteFile(destPath, body, 0o600); writeErr != nil {
			return fmt.Errorf("write attachment: %w", writeErr)
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("walk message: %w", walkErr)
	}
	if !found {
		return fmt.Errorf("attachment index %d not found", index)
	}
	return nil
}

// attachmentFilename returns e's attachment filename — from
// Content-Disposition's filename parameter, or Content-Type's name
// parameter as a fallback — or "" if e isn't an attachment part.
func attachmentFilename(e *message.Entity) string {
	disp, dispParams, _ := e.Header.ContentDisposition()
	if filename := dispParams["filename"]; filename != "" {
		return filename
	}
	if disp == "attachment" {
		return "attachment"
	}
	_, ctParams, _ := e.Header.ContentType()
	return ctParams["name"]
}
