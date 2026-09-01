package message

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/imap"
	"github.com/tomowang/pigeoncli/internal/mime"
	"github.com/tomowang/pigeoncli/internal/storage/blob"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

// seenFlag is the IMAP flag marking a message as read.
const seenFlag = `\Seen`

// junkSpecialUse and inboxSpecialUse are the RFC 6154 special-use
// attributes identifying an account's Junk/Spam and Inbox folders.
const (
	junkSpecialUse  = `\Junk`
	inboxSpecialUse = `\Inbox`
)

// ErrNoJunkFolder is returned by MoveToJunk when the account has no synced
// folder tagged \Junk — not every IMAP server advertises a Junk folder.
var ErrNoJunkFolder = errors.New("account has no Junk folder")

// ErrNoInboxFolder is returned by MoveToInbox when the account has no
// synced folder tagged \Inbox.
var ErrNoInboxFolder = errors.New("account has no Inbox folder")

// listLimit caps how many cached messages List returns per folder. Full
// pagination/infinite-scroll is deferred; at personal-mailbox scale, the
// most recent N messages cover the common case.
const listLimit = 200

// Message is the message-header DTO used by internal/cli, internal/tui,
// and (later) internal/httpapi.
type Message struct {
	UID        uint32
	MessageID  string
	InReplyTo  string
	References []string
	Subject    string
	FromName   string
	FromAddr   string
	ToAddrs    []string
	CcAddrs    []string
	Date       time.Time
	Flags      []string
	Size       int64
}

// Body is a message's content, both as originally received and rendered
// to best-effort plain text.
type Body struct {
	Raw         []byte
	PlainText   string
	Attachments []Attachment
}

// Attachment is one attachment's metadata, mirroring internal/mime.Attachment
// field-for-field — kept as a separate local DTO so internal/mime's types
// don't leak into the public core surface.
type Attachment struct {
	Index       int
	Filename    string
	ContentType string
	Size        int64
}

// SearchResult is a header-search hit, carrying the folder it lives in so
// callers can open it directly.
type SearchResult struct {
	Message
	FolderPath string
}

// searchLimit caps how many results Search returns, matching listLimit's
// personal-mailbox-scale reasoning.
const searchLimit = 200

// Service reads cached message headers and lazily fetches/caches message
// bodies on first open.
type Service struct {
	db    *sqlite.DB
	blobs *blob.Store
}

// NewService creates a Service backed by db and blobs.
func NewService(db *sqlite.DB, blobs *blob.Store) *Service {
	return &Service{db: db, blobs: blobs}
}

// List returns the cached messages in accountSlug/folderPath, most recent
// first. It reads only from the local cache — the folder must already have
// been synced (see core/folder.Service.Sync).
func (s *Service) List(ctx context.Context, accountSlug, folderPath string) ([]Message, error) {
	_, folderID, err := s.resolveIDs(ctx, accountSlug, folderPath)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.ListMessages(ctx, folderID, listLimit)
	if err != nil {
		return nil, err
	}
	out := make([]Message, len(rows))
	for i, r := range rows {
		out[i] = Message{
			UID: r.UID, MessageID: r.MessageID, InReplyTo: r.InReplyTo, References: r.References, Subject: r.Subject,
			FromName: r.FromName, FromAddr: r.FromAddr, ToAddrs: r.ToAddrs, CcAddrs: r.CcAddrs,
			Date: r.Date, Flags: r.Flags, Size: r.Size,
		}
	}
	return out, nil
}

// Related returns the cached messages that msg's In-Reply-To/References
// headers point to, across all of accountSlug's synced folders (e.g.
// matching a Sent copy to an Inbox reply). It reads only from the local
// cache.
func (s *Service) Related(ctx context.Context, accountSlug string, msg Message) ([]SearchResult, error) {
	accountID, ok, err := s.db.AccountID(ctx, accountSlug)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("account %q has not been synced yet", accountSlug)
	}

	seen := make(map[string]struct{}, len(msg.References)+1)
	var ids []string
	add := func(id string) {
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(msg.InReplyTo)
	for _, r := range msg.References {
		add(r)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.db.FindMessagesByMessageIDs(ctx, accountID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, len(rows))
	for i, r := range rows {
		out[i] = SearchResult{
			Message: Message{
				UID: r.UID, MessageID: r.MessageID, InReplyTo: r.InReplyTo, Subject: r.Subject,
				FromName: r.FromName, FromAddr: r.FromAddr, ToAddrs: r.ToAddrs, CcAddrs: r.CcAddrs,
				Date: r.Date, Flags: r.Flags, Size: r.Size,
			},
			FolderPath: r.FolderPath,
		}
	}
	return out, nil
}

// Search runs a full-text search over subject/from/to/cc headers for
// accountSlug's synced messages. It reads only from the local FTS5 index —
// bodies aren't searched, since they're only cached after a message has
// been opened.
func (s *Service) Search(ctx context.Context, accountSlug, query string) ([]SearchResult, error) {
	accountID, ok, err := s.db.AccountID(ctx, accountSlug)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("account %q has not been synced yet", accountSlug)
	}

	rows, err := s.db.SearchMessages(ctx, accountID, query, searchLimit)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, len(rows))
	for i, r := range rows {
		out[i] = SearchResult{
			Message: Message{
				UID: r.UID, MessageID: r.MessageID, InReplyTo: r.InReplyTo, Subject: r.Subject,
				FromName: r.FromName, FromAddr: r.FromAddr, ToAddrs: r.ToAddrs, CcAddrs: r.CcAddrs,
				Date: r.Date, Flags: r.Flags, Size: r.Size,
			},
			FolderPath: r.FolderPath,
		}
	}
	return out, nil
}

// Body returns the raw and rendered content of one message. If the body
// hasn't been fetched yet, it connects to cfg's IMAP server (using its
// keyring credentials), fetches it, and caches it locally before
// returning. It also marks the message \Seen, both on the server and in
// the local cache, the same as any other mail client does on open.
func (s *Service) Body(ctx context.Context, cfg config.Account, folderPath string, uid uint32) (Body, error) {
	_, folderID, err := s.resolveIDs(ctx, cfg.Slug, folderPath)
	if err != nil {
		return Body{}, err
	}

	raw, err := s.cachedOrFetchRaw(ctx, cfg, folderPath, folderID, uid)
	if err != nil {
		return Body{}, err
	}

	// Best-effort: a failed mark-as-read (e.g. a network blip right after
	// the body fetch succeeded) shouldn't stop the message from being
	// shown, so this only logs rather than returning the error.
	if err := s.markSeen(ctx, cfg, folderPath, folderID, uid); err != nil {
		slog.Warn("mark message seen failed", "folder", folderPath, "uid", uid, "err", err)
	}

	plainText, err := mime.PlainText(raw)
	if err != nil {
		return Body{}, fmt.Errorf("render message: %w", err)
	}

	parts, err := mime.Attachments(raw)
	if err != nil {
		return Body{}, fmt.Errorf("list attachments: %w", err)
	}
	attachments := make([]Attachment, len(parts))
	for i, p := range parts {
		attachments[i] = Attachment{Index: p.Index, Filename: p.Filename, ContentType: p.ContentType, Size: p.Size}
	}

	return Body{Raw: raw, PlainText: plainText, Attachments: attachments}, nil
}

// SaveAttachment writes the decoded bytes of one attachment (identified by
// the Index from Body.Attachments) to destPath. The message's raw body is
// re-fetched via the same cache-or-fetch path as Body, so calling this
// after viewing a message's attachment list is a cache hit in the common
// case.
func (s *Service) SaveAttachment(ctx context.Context, cfg config.Account, folderPath string, uid uint32, index int, destPath string) error {
	_, folderID, err := s.resolveIDs(ctx, cfg.Slug, folderPath)
	if err != nil {
		return err
	}
	raw, err := s.cachedOrFetchRaw(ctx, cfg, folderPath, folderID, uid)
	if err != nil {
		return err
	}
	if err := mime.SaveAttachment(raw, index, destPath); err != nil {
		return fmt.Errorf("save attachment: %w", err)
	}
	return nil
}

func (s *Service) cachedOrFetchRaw(ctx context.Context, cfg config.Account, folderPath string, folderID int64, uid uint32) ([]byte, error) {
	ref, synced, err := s.db.MessageBodyState(ctx, folderID, uid)
	if err != nil {
		return nil, err
	}
	if synced {
		raw, err := s.blobs.Read(ref)
		if err == nil {
			return raw, nil
		}
		// Cached ref is stale (e.g. blob directory was cleared) — fall
		// through and re-fetch from the server.
	}

	raw, err := s.fetchRaw(ctx, cfg, folderPath, uid)
	if err != nil {
		return nil, err
	}

	accountID, ok, err := s.db.AccountID(ctx, cfg.Slug)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("account %q has not been synced yet", cfg.Slug)
	}
	ref = s.blobs.Ref(accountID, folderID, uid)
	if err := s.blobs.Write(ref, raw); err != nil {
		return nil, fmt.Errorf("cache message body: %w", err)
	}
	if err := s.db.SetMessageBodyCached(ctx, folderID, uid, ref); err != nil {
		return nil, fmt.Errorf("record cached message body: %w", err)
	}
	return raw, nil
}

// fetchTimeout bounds a single on-demand body fetch (dial + auth + select +
// fetch). Without it, a stalled connection (dead network, firewall dropping
// packets silently) leaves the caller — the TUI's "Loading message..."
// status — waiting indefinitely, since the underlying net.Dial has no
// deadline of its own.
const fetchTimeout = 20 * time.Second

func (s *Service) fetchRaw(ctx context.Context, cfg config.Account, folderPath string, uid uint32) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	provider := auth.PasswordProvider{Username: cfg.Username, AccountSlug: cfg.Slug}
	cl, err := imap.DialClient(ctx, imap.DialOptions{Host: cfg.IMAP.Host, Port: cfg.IMAP.Port, TLS: cfg.IMAP.TLS}, provider)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = cl.Close() }()
	defer cl.WatchContext(ctx)()

	if _, _, _, _, err := cl.SelectFolder(ctx, folderPath); err != nil {
		return nil, fmt.Errorf("select %q: %w", folderPath, err)
	}
	raw, err := cl.FetchRawBody(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("fetch body: %w", err)
	}
	return raw, nil
}

// markSeen sets \Seen on uid, on the server and in the local cache, unless
// it's already marked seen locally — the common case once a message has
// been opened once, which skips the connection entirely.
func (s *Service) markSeen(ctx context.Context, cfg config.Account, folderPath string, folderID int64, uid uint32) error {
	flags, err := s.db.MessageFlags(ctx, folderID, uid)
	if err != nil {
		return err
	}
	for _, f := range flags {
		if f == seenFlag {
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	provider := auth.PasswordProvider{Username: cfg.Username, AccountSlug: cfg.Slug}
	cl, err := imap.DialClient(ctx, imap.DialOptions{Host: cfg.IMAP.Host, Port: cfg.IMAP.Port, TLS: cfg.IMAP.TLS}, provider)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = cl.Close() }()
	defer cl.WatchContext(ctx)()

	if _, _, _, _, err := cl.SelectFolder(ctx, folderPath); err != nil {
		return fmt.Errorf("select %q: %w", folderPath, err)
	}
	if err := cl.MarkSeen(ctx, uid); err != nil {
		return err
	}

	if err := s.db.UpdateMessageFlags(ctx, folderID, map[uint32][]string{uid: append(flags, seenFlag)}); err != nil {
		return fmt.Errorf("cache seen flag: %w", err)
	}
	return nil
}

// MoveToJunk moves the message at uid in folderPath to the account's
// server-designated Junk folder (RFC 6154 special-use \Junk) — the same
// move-based mechanism general mail clients use for "report spam".
func (s *Service) MoveToJunk(ctx context.Context, cfg config.Account, folderPath string, uid uint32) error {
	return s.moveToSpecialUse(ctx, cfg, folderPath, uid, junkSpecialUse, "the Junk folder", ErrNoJunkFolder)
}

// MoveToInbox moves the message at uid in folderPath back to the account's
// Inbox (RFC 6154 special-use \Inbox) — the "not spam" counterpart to
// MoveToJunk, for undoing a mistaken spam report.
func (s *Service) MoveToInbox(ctx context.Context, cfg config.Account, folderPath string, uid uint32) error {
	return s.moveToSpecialUse(ctx, cfg, folderPath, uid, inboxSpecialUse, "the Inbox", ErrNoInboxFolder)
}

// moveToSpecialUse moves the message at uid in folderPath to the account's
// folder tagged with destSpecialUse (RFC 6154): a MOVE (or COPY + STORE
// \Deleted + EXPUNGE fallback) on the server, then dropping the local cache
// row for the source folder, since the message no longer lives there — the
// destination folder picks it up on its next sync. destLabel names the
// destination folder for the "already there" error message; errNoFolder is
// returned if the account has no synced folder carrying destSpecialUse.
func (s *Service) moveToSpecialUse(ctx context.Context, cfg config.Account, folderPath string, uid uint32, destSpecialUse, destLabel string, errNoFolder error) error {
	accountID, folderID, err := s.resolveIDs(ctx, cfg.Slug, folderPath)
	if err != nil {
		return err
	}
	destPath, ok, err := s.db.FolderBySpecialUse(ctx, accountID, destSpecialUse)
	if err != nil {
		return err
	}
	if !ok {
		return errNoFolder
	}
	if destPath == folderPath {
		return fmt.Errorf("message is already in %s", destLabel)
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	provider := auth.PasswordProvider{Username: cfg.Username, AccountSlug: cfg.Slug}
	cl, err := imap.DialClient(ctx, imap.DialOptions{Host: cfg.IMAP.Host, Port: cfg.IMAP.Port, TLS: cfg.IMAP.TLS}, provider)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = cl.Close() }()
	defer cl.WatchContext(ctx)()

	if _, _, _, _, err := cl.SelectFolder(ctx, folderPath); err != nil {
		return fmt.Errorf("select %q: %w", folderPath, err)
	}
	if err := cl.MoveToFolder(ctx, uid, destPath); err != nil {
		return err
	}

	if err := s.db.DeleteMessage(ctx, folderID, uid); err != nil {
		return fmt.Errorf("update local cache: %w", err)
	}
	return nil
}

func (s *Service) resolveIDs(ctx context.Context, accountSlug, folderPath string) (accountID, folderID int64, err error) {
	accountID, ok, err := s.db.AccountID(ctx, accountSlug)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, fmt.Errorf("account %q has not been synced yet", accountSlug)
	}
	folderID, ok, err = s.db.FolderID(ctx, accountID, folderPath)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, fmt.Errorf("folder %q has not been synced yet", folderPath)
	}
	return accountID, folderID, nil
}
