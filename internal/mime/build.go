package mime

import (
	"bytes"
	"fmt"
	stdmime "mime"
	"path/filepath"
	"time"

	"github.com/emersion/go-message/mail"
)

// Recipient is a single mail address, with an optional display name.
type Recipient struct {
	Name string
	Addr string
}

func (r Recipient) mailAddress() *mail.Address {
	return &mail.Address{Name: r.Name, Address: r.Addr}
}

// OutgoingAttachment is a file to attach to a message built by
// BuildMessage: a filename and its raw bytes. Content-Type is guessed from
// the filename's extension (defaulting to application/octet-stream) rather
// than taken as input, the same way a mail client's "attach file" only
// asks for a path.
type OutgoingAttachment struct {
	Filename string
	Data     []byte
}

// BuildMessage builds a raw RFC 5322 message ready to hand to an SMTP
// client's DATA command: a text/plain body, and — when len(attachments) >
// 0 — a multipart/mixed envelope carrying it alongside each attachment.
// inReplyTo and references, when non-empty, are the Message-IDs (without
// angle brackets) this message is replying to.
func BuildMessage(from Recipient, to, cc []Recipient, subject, body, inReplyTo, references string, attachments []OutgoingAttachment) ([]byte, error) {
	var h mail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", []*mail.Address{from.mailAddress()})
	if len(to) > 0 {
		h.SetAddressList("To", addressList(to))
	}
	if len(cc) > 0 {
		h.SetAddressList("Cc", addressList(cc))
	}
	h.SetSubject(subject)
	if err := h.GenerateMessageID(); err != nil {
		return nil, fmt.Errorf("generate message id: %w", err)
	}
	if inReplyTo != "" {
		h.SetMsgIDList("In-Reply-To", []string{inReplyTo})
	}
	if references != "" {
		h.SetMsgIDList("References", []string{references})
	}

	var buf bytes.Buffer
	if len(attachments) == 0 {
		h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		w, err := mail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, fmt.Errorf("create message writer: %w", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			return nil, fmt.Errorf("write message body: %w", err)
		}
		if err := w.Close(); err != nil {
			return nil, fmt.Errorf("close message writer: %w", err)
		}
		return buf.Bytes(), nil
	}

	w, err := mail.CreateWriter(&buf, h)
	if err != nil {
		return nil, fmt.Errorf("create message writer: %w", err)
	}

	var th mail.InlineHeader
	th.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	tw, err := w.CreateSingleInline(th)
	if err != nil {
		return nil, fmt.Errorf("create body part: %w", err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		return nil, fmt.Errorf("write message body: %w", err)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close body part: %w", err)
	}

	for _, a := range attachments {
		var ah mail.AttachmentHeader
		ah.SetFilename(a.Filename)
		ah.SetContentType(attachmentContentType(a.Filename), nil)
		aw, err := w.CreateAttachment(ah)
		if err != nil {
			return nil, fmt.Errorf("create attachment %q: %w", a.Filename, err)
		}
		if _, err := aw.Write(a.Data); err != nil {
			return nil, fmt.Errorf("write attachment %q: %w", a.Filename, err)
		}
		if err := aw.Close(); err != nil {
			return nil, fmt.Errorf("close attachment %q: %w", a.Filename, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close message writer: %w", err)
	}
	return buf.Bytes(), nil
}

// attachmentContentType guesses filename's MIME type from its extension,
// falling back to application/octet-stream — the same default browsers and
// mail clients use for an unrecognized file type.
func attachmentContentType(filename string) string {
	if ct := stdmime.TypeByExtension(filepath.Ext(filename)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func addressList(rs []Recipient) []*mail.Address {
	out := make([]*mail.Address, len(rs))
	for i, r := range rs {
		out[i] = r.mailAddress()
	}
	return out
}
