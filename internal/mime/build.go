package mime

import (
	"bytes"
	"fmt"
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

// BuildMessage builds a raw, single-part text/plain RFC 5322 message ready
// to hand to an SMTP client's DATA command. inReplyTo and references, when
// non-empty, are the Message-IDs (without angle brackets) this message is
// replying to.
func BuildMessage(from Recipient, to, cc []Recipient, subject, body, inReplyTo, references string) ([]byte, error) {
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
	h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})

	var buf bytes.Buffer
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

func addressList(rs []Recipient) []*mail.Address {
	out := make([]*mail.Address, len(rs))
	for i, r := range rs {
		out[i] = r.mailAddress()
	}
	return out
}
