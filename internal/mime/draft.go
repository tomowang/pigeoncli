package mime

import (
	"bytes"
	"fmt"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
)

// DraftBcc parses raw's Bcc header — present only on a message BuildMessage
// built with a non-empty bcc, i.e. a Drafts-folder copy — back into
// addresses, so LoadDraft can restore a draft's Bcc list. It returns nil,
// nil for a message with no Bcc header (including every ordinary synced
// message, which never has one) rather than treating that as an error.
func DraftBcc(raw []byte) ([]string, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	h := mail.Header{Header: entity.Header}
	addrs, err := h.AddressList("Bcc")
	if err != nil || len(addrs) == 0 {
		return nil, nil
	}
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = a.Address
	}
	return out, nil
}
