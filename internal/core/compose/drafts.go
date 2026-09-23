package compose

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/message"
	"github.com/tomowang/pigeoncli/internal/imap"
	"github.com/tomowang/pigeoncli/internal/mime"
)

// DraftRef identifies a saved draft's location — always the account's
// Drafts folder, plus the UID SaveDraft's last APPEND returned. The zero
// value means "not saved yet".
type DraftRef = message.Ref

// ErrNoDraftsFolder is returned by DraftsFolder (and so by SaveDraft) when
// the account has no synced folder tagged \Drafts.
var ErrNoDraftsFolder = errors.New("account has no Drafts folder")

// draftFlag is the IMAP system flag SaveDraft tags every saved copy with.
const draftFlag = `\Draft`

// DraftsFolder returns the path of accountSlug's synced Drafts folder
// (RFC 6154 special-use \Drafts). It requires folderSvc (see NewService).
func (s *Service) DraftsFolder(ctx context.Context, accountSlug string) (string, error) {
	if s.folderSvc == nil {
		return "", fmt.Errorf("locating the Drafts folder requires a local folder cache")
	}
	folders, err := s.folderSvc.List(ctx, accountSlug)
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		if f.SpecialUse == `\Drafts` {
			return f.Path, nil
		}
	}
	return "", ErrNoDraftsFolder
}

// SaveDraft uploads draft as a new \Draft-flagged message in cfg's Drafts
// folder and, if prev is non-zero, deletes prev's copy afterward — so
// repeated autosaves of the same in-progress draft replace it instead of
// piling up duplicates. A delete failure for prev is logged, not returned:
// the new copy (the important part) is already saved by that point.
//
// Unlike Send, the built copy's headers include draft.Bcc — nobody but the
// account owner ever sees a Drafts-folder message, since it's never
// delivered — which is what lets LoadDraft restore it later.
func (s *Service) SaveDraft(ctx context.Context, cfg config.Account, draft Draft, prev DraftRef) (DraftRef, error) {
	draftsPath, err := s.DraftsFolder(ctx, cfg.Slug)
	if err != nil {
		return DraftRef{}, err
	}

	from := mime.Recipient{Name: cfg.DisplayName, Addr: cfg.Email}
	raw, err := mime.BuildMessage(from, recipients(draft.To), recipients(draft.Cc), recipients(draft.Bcc),
		draft.Subject, draft.Body, draft.InReplyTo, draft.References, outgoingAttachments(draft.Attachments))
	if err != nil {
		return DraftRef{}, fmt.Errorf("build message: %w", err)
	}

	cl, err := s.dialCompose(ctx, cfg)
	if err != nil {
		return DraftRef{}, err
	}
	defer func() { _ = cl.Close() }()
	defer cl.WatchContext(ctx)()

	newUID, err := cl.Append(ctx, draftsPath, []string{draftFlag}, raw)
	if err != nil {
		return DraftRef{}, fmt.Errorf("save draft: %w", err)
	}
	ref := DraftRef{FolderPath: draftsPath, UID: newUID}

	if prev.UID != 0 && prev.FolderPath != "" {
		if _, _, _, _, selErr := cl.SelectFolder(ctx, prev.FolderPath); selErr != nil {
			slog.Warn("select drafts folder to remove stale copy failed", "folder", prev.FolderPath, "err", selErr)
		} else if delErr := cl.Purge(ctx, []uint32{prev.UID}); delErr != nil {
			slog.Warn("delete stale draft copy failed", "folder", prev.FolderPath, "uid", prev.UID, "err", delErr)
		}
	}

	if s.folderSvc != nil {
		// windowCount only matters for a folder's first-ever sync; by now
		// Drafts has already been synced at least once, so 0 (no windowing)
		// is fine here regardless of the user's configured window.
		_ = s.folderSvc.Sync(ctx, cfg, 0, nil)
	}
	return ref, nil
}

// DiscardDraft permanently deletes ref's copy from the Drafts folder —
// call it once a draft has been sent (Send has no DraftRef of its own to
// act on, so this is the caller's job) or when the user explicitly
// discards an in-progress draft. The zero DraftRef is a no-op, so it's
// safe to call unconditionally on a draft that may never have been saved.
func (s *Service) DiscardDraft(ctx context.Context, cfg config.Account, ref DraftRef) error {
	if ref.UID == 0 || ref.FolderPath == "" {
		return nil
	}
	cl, err := s.dialCompose(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = cl.Close() }()
	defer cl.WatchContext(ctx)()

	if _, _, _, _, err := cl.SelectFolder(ctx, ref.FolderPath); err != nil {
		return fmt.Errorf("select %q: %w", ref.FolderPath, err)
	}
	if err := cl.Purge(ctx, []uint32{ref.UID}); err != nil {
		return fmt.Errorf("delete draft: %w", err)
	}
	if s.folderSvc != nil {
		_ = s.folderSvc.Sync(ctx, cfg, 0, nil)
	}
	return nil
}

// LoadDraft reconstructs a Draft from a message previously saved by
// SaveDraft: To/Cc/Subject/InReplyTo come from msg, already parsed by the
// normal sync path; Bcc is parsed straight out of body.Raw via
// mime.DraftBcc, since Bcc never round-trips through that path; the body
// text is body.PlainText; and attachments are extracted the same
// best-effort way NewForward carries them over (one that fails to extract
// is logged and left off, not fatal).
func (s *Service) LoadDraft(msg message.Message, body message.Body) Draft {
	bcc, err := mime.DraftBcc(body.Raw)
	if err != nil {
		slog.Warn("parse draft bcc failed", "uid", msg.UID, "err", err)
	}
	return Draft{
		To: msg.ToAddrs, Cc: msg.CcAddrs, Bcc: bcc,
		Subject: msg.Subject, Body: body.PlainText,
		InReplyTo: msg.InReplyTo, References: msg.InReplyTo,
		Attachments: forwardAttachments(body),
	}
}

// dialCompose connects to cfg's IMAP server, via s.dial if a test has set
// one, otherwise with credentials from cfg's configured auth provider —
// SaveDraft and DiscardDraft's shared dial step (Send, in compose.go, goes
// over SMTP instead and so doesn't need this).
func (s *Service) dialCompose(ctx context.Context, cfg config.Account) (*imap.Client, error) {
	if s.dial != nil {
		return s.dial(ctx, cfg)
	}
	provider, err := auth.NewProvider(cfg.AuthType, cfg.Username, cfg.Slug)
	if err != nil {
		return nil, err
	}
	cl, err := imap.DialClient(ctx, imap.DialOptions{Host: cfg.IMAP.Host, Port: cfg.IMAP.Port, TLS: cfg.IMAP.TLS}, provider)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return cl, nil
}
