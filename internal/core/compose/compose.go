package compose

import (
	"context"
	"fmt"
	"strings"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
	"github.com/tomowang/pigeoncli/internal/mime"
	"github.com/tomowang/pigeoncli/internal/smtp"
)

// Draft is an editable outgoing message. The TUI's compose pane renders
// and edits a Draft directly — no reply/quoting logic lives there.
type Draft struct {
	To         []string
	Cc         []string
	Subject    string
	Body       string
	InReplyTo  string // orig Message-ID being replied to, empty for a new message
	References string
}

// Service builds reply drafts and sends them.
type Service struct {
	// folderSvc is used, best-effort, to refresh the account's folders
	// after a successful send so a server-side copy in Sent shows up
	// locally. It may be nil, in which case Send skips that step.
	folderSvc *folder.Service
}

// NewService creates a Service. folderSvc may be nil to skip the
// post-send folder refresh (e.g. in contexts without a local cache).
func NewService(folderSvc *folder.Service) *Service {
	return &Service{folderSvc: folderSvc}
}

// NewReply builds a reply (or, if replyAll, reply-all) draft to orig,
// quoting origBody and addressed based on cfg's own address (so cfg's
// address is excluded from the reply-all recipient list).
func NewReply(cfg config.Account, orig message.Message, origBody message.Body, replyAll bool) Draft {
	self := strings.ToLower(cfg.Email)
	seen := map[string]bool{self: true, strings.ToLower(orig.FromAddr): true}

	to := []string{orig.FromAddr}
	var cc []string
	if replyAll {
		for _, a := range orig.ToAddrs {
			if lower := strings.ToLower(a); !seen[lower] {
				seen[lower] = true
				to = append(to, a)
			}
		}
		for _, a := range orig.CcAddrs {
			if lower := strings.ToLower(a); !seen[lower] {
				seen[lower] = true
				cc = append(cc, a)
			}
		}
	}

	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}

	return Draft{
		To:         to,
		Cc:         cc,
		Subject:    subject,
		Body:       quote(orig, origBody.PlainText),
		InReplyTo:  orig.MessageID,
		References: orig.MessageID,
	}
}

func quote(orig message.Message, plainText string) string {
	who := orig.FromName
	if who == "" {
		who = orig.FromAddr
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "On %s, %s wrote:\n", orig.Date.Format("Jan 2, 2006 at 3:04 PM"), who)
	for _, line := range strings.Split(plainText, "\n") {
		sb.WriteString("> ")
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Send builds the MIME message for draft, sends it via cfg's SMTP server
// using its keyring credentials, and (best-effort) refreshes the local
// folder cache afterward. A failure to refresh afterward is not returned
// as an error — the message has already been sent by that point.
func (s *Service) Send(ctx context.Context, cfg config.Account, draft Draft) error {
	from := mime.Recipient{Name: cfg.DisplayName, Addr: cfg.Email}
	raw, err := mime.BuildMessage(from, recipients(draft.To), recipients(draft.Cc), draft.Subject, draft.Body,
		draft.InReplyTo, draft.References)
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}

	provider := auth.PasswordProvider{Username: cfg.Username, AccountSlug: cfg.Slug}
	opts := smtp.DialOptions{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, TLS: cfg.SMTP.TLS}
	allRecipients := append(append([]string{}, draft.To...), draft.Cc...)
	if err := smtp.Send(ctx, opts, provider, cfg.Email, allRecipients, raw); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	if s.folderSvc != nil {
		_ = s.folderSvc.Sync(ctx, cfg, nil)
	}
	return nil
}

func recipients(addrs []string) []mime.Recipient {
	out := make([]mime.Recipient, len(addrs))
	for i, a := range addrs {
		out[i] = mime.Recipient{Addr: a}
	}
	return out
}
