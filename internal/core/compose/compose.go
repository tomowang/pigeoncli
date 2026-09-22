package compose

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
	"github.com/tomowang/pigeoncli/internal/core/signature"
	"github.com/tomowang/pigeoncli/internal/mime"
	"github.com/tomowang/pigeoncli/internal/smtp"
)

// Draft is an editable outgoing message. The TUI's compose pane renders
// and edits a Draft directly — no reply/quoting logic lives there.
type Draft struct {
	To      []string
	Cc      []string
	Bcc     []string // never appears in the built message's headers; SMTP recipients only
	Subject string
	Body    string
	// InReplyTo and References are the orig Message-ID this draft is
	// replying to; both empty for a new message or a forward.
	InReplyTo   string
	References  string
	Attachments []Attachment
}

// Attachment is a file attached to an outgoing Draft: a filename and its
// raw bytes, normally produced by NewAttachmentFromFile. It's a local DTO
// mirroring internal/mime.OutgoingAttachment field-for-field, kept separate
// so internal/mime's types don't leak into the public core surface.
type Attachment struct {
	Filename string
	Data     []byte
}

// MaxAttachmentSize is the largest file NewAttachmentFromFile will read
// into a Draft. It's well under common SMTP submission limits (typically
// 25-35MB — attachments inflate by about a third once base64-encoded),
// while keeping a single attachment from using an outsized amount of
// memory before it's even sent.
const MaxAttachmentSize = 20 << 20 // 20 MiB

// ErrAttachmentTooLarge is returned by NewAttachmentFromFile for a file
// over MaxAttachmentSize.
var ErrAttachmentTooLarge = fmt.Errorf("attachment exceeds the %d MiB limit", MaxAttachmentSize>>20)

// NewAttachmentFromFile reads path into an Attachment named after its base
// name. It's the one place a file from disk becomes part of a Draft, used
// by both internal/cli and internal/tui so the size limit and directory
// check apply everywhere a file gets attached.
func NewAttachmentFromFile(path string) (Attachment, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Attachment{}, err
	}
	if info.IsDir() {
		return Attachment{}, fmt.Errorf("%s is a directory, not a file", path)
	}
	if info.Size() > MaxAttachmentSize {
		return Attachment{}, fmt.Errorf("%s: %w", path, ErrAttachmentTooLarge)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{Filename: filepath.Base(path), Data: data}, nil
}

// Service builds reply drafts and sends them.
type Service struct {
	// folderSvc is used, best-effort, to refresh the account's folders
	// after a successful send so a server-side copy in Sent shows up
	// locally. It may be nil, in which case Send skips that step.
	folderSvc *folder.Service
	// signatureSvc supplies the default signature appended to new reply
	// drafts. It may be nil, in which case NewReply appends none.
	signatureSvc *signature.Service
}

// NewService creates a Service. folderSvc may be nil to skip the
// post-send folder refresh, and signatureSvc may be nil to skip signature
// insertion (e.g. in contexts without a local cache).
func NewService(folderSvc *folder.Service, signatureSvc *signature.Service) *Service {
	return &Service{folderSvc: folderSvc, signatureSvc: signatureSvc}
}

// NewReply builds a reply (or, if replyAll, reply-all) draft to orig,
// quoting origBody, addressed based on cfg's own address (so cfg's address
// is excluded from the reply-all recipient list), with cfg's default
// signature appended if one is configured.
func (s *Service) NewReply(ctx context.Context, cfg config.Account, orig message.Message, origBody message.Body, replyAll bool) Draft {
	draft := buildReplyDraft(cfg, orig, origBody, replyAll)
	draft.Body = s.appendSignature(ctx, cfg, draft.Body)
	return draft
}

// NewForward builds a draft forwarding orig to a recipient still to be
// filled in: empty To, a "Fwd: " subject (not doubled if orig is already a
// forward), a forwarded-message header block above orig's plain text, and
// orig's attachments carried over — extracted from origBody.Raw, best
// effort: one that fails to extract is logged and left off rather than
// failing the whole forward. cfg's default signature is appended if one is
// configured. Unlike a reply, a forward carries no In-Reply-To/References,
// since it starts a new thread rather than continuing orig's.
func (s *Service) NewForward(ctx context.Context, cfg config.Account, orig message.Message, origBody message.Body) Draft {
	draft := buildForwardDraft(orig, origBody)
	draft.Body = s.appendSignature(ctx, cfg, draft.Body)
	return draft
}

// NewMessage builds an empty draft for composing a new message from
// scratch (no recipients, subject, or quoted body), with cfg's default
// signature pre-filled if one is configured.
func (s *Service) NewMessage(ctx context.Context, cfg config.Account) Draft {
	return Draft{Body: s.appendSignature(ctx, cfg, "")}
}

func (s *Service) appendSignature(ctx context.Context, cfg config.Account, body string) string {
	if s.signatureSvc == nil {
		return body
	}
	sig, ok, err := s.signatureSvc.Default(ctx, cfg.Slug)
	if err != nil || !ok {
		return body
	}
	if body != "" {
		body += "\n"
	}
	return body + "-- \n" + sig.Body + "\n"
}

// buildReplyDraft is NewReply's pure part: addressing and quoting, with no
// signature or other side effects, kept separate so it's cheap to test.
func buildReplyDraft(cfg config.Account, orig message.Message, origBody message.Body, replyAll bool) Draft {
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

// buildForwardDraft is NewForward's pure part: subject, header block, and
// attachment extraction, with no signature or other side effects, kept
// separate so it's cheap to test.
func buildForwardDraft(orig message.Message, origBody message.Body) Draft {
	subject := orig.Subject
	lower := strings.ToLower(subject)
	if !strings.HasPrefix(lower, "fwd:") && !strings.HasPrefix(lower, "fw:") {
		subject = "Fwd: " + subject
	}

	from := orig.FromAddr
	if orig.FromName != "" {
		from = fmt.Sprintf("%s <%s>", orig.FromName, orig.FromAddr)
	}

	var sb strings.Builder
	sb.WriteString("---------- Forwarded message ----------\n")
	fmt.Fprintf(&sb, "From: %s\n", from)
	fmt.Fprintf(&sb, "Date: %s\n", orig.Date.Format("Jan 2, 2006 at 3:04 PM"))
	fmt.Fprintf(&sb, "Subject: %s\n", orig.Subject)
	if len(orig.ToAddrs) > 0 {
		fmt.Fprintf(&sb, "To: %s\n", strings.Join(orig.ToAddrs, ", "))
	}
	sb.WriteString("\n")
	sb.WriteString(origBody.PlainText)

	return Draft{
		Subject:     subject,
		Body:        sb.String(),
		Attachments: forwardAttachments(origBody),
	}
}

// forwardAttachments extracts origBody's attachments (by index, from its
// raw source) into Draft-ready Attachments. One that fails to extract is
// logged and skipped, not fatal to the forward.
func forwardAttachments(origBody message.Body) []Attachment {
	if len(origBody.Attachments) == 0 {
		return nil
	}
	out := make([]Attachment, 0, len(origBody.Attachments))
	for _, a := range origBody.Attachments {
		data, err := mime.AttachmentData(origBody.Raw, a.Index)
		if err != nil {
			slog.Warn("extract attachment for forward failed", "filename", a.Filename, "err", err)
			continue
		}
		out = append(out, Attachment{Filename: a.Filename, Data: data})
	}
	return out
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
		draft.InReplyTo, draft.References, outgoingAttachments(draft.Attachments))
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}

	provider, err := auth.NewProvider(cfg.AuthType, cfg.Username, cfg.Slug)
	if err != nil {
		return err
	}
	opts := smtp.DialOptions{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, TLS: cfg.SMTP.TLS}
	// draft.Bcc is deliberately never passed to mime.BuildMessage: it's an
	// SMTP envelope recipient only, so it's invisible in the headers of the
	// single copy sent to every recipient (To, Cc, and Bcc alike) — nobody
	// sees who else was bcc'd.
	allRecipients := append(append(append([]string{}, draft.To...), draft.Cc...), draft.Bcc...)
	if err := smtp.Send(ctx, opts, provider, cfg.Email, allRecipients, raw); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	if s.folderSvc != nil {
		// windowCount only matters for a folder's first-ever sync; by now
		// Sent has already been synced at least once, so 0 (no windowing)
		// is fine here regardless of the user's configured window.
		_ = s.folderSvc.Sync(ctx, cfg, 0, nil)
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

func outgoingAttachments(atts []Attachment) []mime.OutgoingAttachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]mime.OutgoingAttachment, len(atts))
	for i, a := range atts {
		out[i] = mime.OutgoingAttachment{Filename: a.Filename, Data: a.Data}
	}
	return out
}
