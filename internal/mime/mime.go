package mime

import (
	"bytes"
	"fmt"
	"io"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // registers non-UTF-8 charset decoding
)

// PlainText renders raw (a full RFC 822 message) as best-effort plain
// text: the first text/plain part found, or the first text/html part with
// tags stripped if the message has no text/plain part.
func PlainText(raw []byte) (string, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return "", fmt.Errorf("parse message: %w", err)
	}

	var plain, htmlBody string
	walkErr := entity.Walk(func(path []int, e *message.Entity, err error) error {
		if err != nil {
			// Charset/encoding problem on this part — skip it rather than
			// aborting the whole walk over an otherwise-readable message.
			return nil
		}
		ct, _, _ := e.Header.ContentType()
		switch ct {
		case "text/plain":
			if plain == "" {
				if body, err := io.ReadAll(e.Body); err == nil {
					plain = string(body)
				}
			}
		case "text/html":
			if htmlBody == "" {
				if body, err := io.ReadAll(e.Body); err == nil {
					htmlBody = string(body)
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("walk message: %w", walkErr)
	}

	if plain != "" {
		return plain, nil
	}
	if htmlBody != "" {
		return stripHTML(htmlBody), nil
	}
	return "", nil
}
