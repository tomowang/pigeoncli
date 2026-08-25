package message

import (
	"context"
	"fmt"
	"time"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/imap"
	"github.com/tomowang/pigeoncli/internal/mime"
	"github.com/tomowang/pigeoncli/internal/storage/blob"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

// listLimit caps how many cached messages List returns per folder. Full
// pagination/infinite-scroll is deferred; at personal-mailbox scale, the
// most recent N messages cover the common case.
const listLimit = 200

// Message is the message-header DTO used by internal/cli, internal/tui,
// and (later) internal/httpapi.
type Message struct {
	UID       uint32
	MessageID string
	InReplyTo string
	Subject   string
	FromName  string
	FromAddr  string
	ToAddrs   []string
	CcAddrs   []string
	Date      time.Time
	Flags     []string
	Size      int64
}

// Body is a message's content, both as originally received and rendered
// to best-effort plain text.
type Body struct {
	Raw       []byte
	PlainText string
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
			UID: r.UID, MessageID: r.MessageID, InReplyTo: r.InReplyTo, Subject: r.Subject,
			FromName: r.FromName, FromAddr: r.FromAddr, ToAddrs: r.ToAddrs, CcAddrs: r.CcAddrs,
			Date: r.Date, Flags: r.Flags, Size: r.Size,
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
// returning.
func (s *Service) Body(ctx context.Context, cfg config.Account, folderPath string, uid uint32) (Body, error) {
	_, folderID, err := s.resolveIDs(ctx, cfg.Slug, folderPath)
	if err != nil {
		return Body{}, err
	}

	raw, err := s.cachedOrFetchRaw(ctx, cfg, folderPath, folderID, uid)
	if err != nil {
		return Body{}, err
	}

	plainText, err := mime.PlainText(raw)
	if err != nil {
		return Body{}, fmt.Errorf("render message: %w", err)
	}
	return Body{Raw: raw, PlainText: plainText}, nil
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

func (s *Service) fetchRaw(ctx context.Context, cfg config.Account, folderPath string, uid uint32) ([]byte, error) {
	provider := auth.PasswordProvider{Username: cfg.Username, AccountSlug: cfg.Slug}
	cl, err := imap.DialClient(ctx, imap.DialOptions{Host: cfg.IMAP.Host, Port: cfg.IMAP.Port, TLS: cfg.IMAP.TLS}, provider)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer cl.Close()
	defer cl.WatchContext(ctx)()

	if _, _, _, err := cl.SelectFolder(ctx, folderPath); err != nil {
		return nil, fmt.Errorf("select %q: %w", folderPath, err)
	}
	raw, err := cl.FetchRawBody(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("fetch body: %w", err)
	}
	return raw, nil
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
