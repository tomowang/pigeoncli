package imap

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/tomowang/pigeoncli/internal/auth"
)

// Client is a persistent, authenticated IMAP connection used for folder
// listing and header sync. Unlike TestLogin, it stays open across multiple
// commands; callers must Close it when done.
type Client struct {
	c *imapclient.Client
}

// DialClient connects to the IMAP server and authenticates using provider.
func DialClient(ctx context.Context, opts DialOptions, provider auth.Provider) (*Client, error) {
	return DialClientWithUpdates(ctx, opts, provider, nil)
}

// DialClientWithUpdates is like DialClient, but additionally invokes
// onMailboxUpdate (from the client's background read-loop goroutine —
// callers must not touch non-thread-safe state directly from it) whenever
// the server reports an unsolicited update to the selected mailbox, such
// as new mail or a flag/expunge change while an Idle() command is
// running. Regular sync/fetch connections have no need for this and pass
// a nil onMailboxUpdate (equivalent to DialClient).
func DialClientWithUpdates(ctx context.Context, opts DialOptions, provider auth.Provider, onMailboxUpdate func()) (*Client, error) {
	type result struct {
		c   *imapclient.Client
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		c, err := dialAuthenticated(ctx, opts, provider, onMailboxUpdate)
		resCh <- result{c, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-resCh:
		if r.err != nil {
			return nil, r.err
		}
		return &Client{c: r.c}, nil
	}
}

func dialAuthenticated(ctx context.Context, opts DialOptions, provider auth.Provider, onMailboxUpdate func()) (*imapclient.Client, error) {
	var clientOpts *imapclient.Options
	if onMailboxUpdate != nil {
		clientOpts = &imapclient.Options{
			UnilateralDataHandler: &imapclient.UnilateralDataHandler{
				Mailbox: func(*imapclient.UnilateralDataMailbox) { onMailboxUpdate() },
			},
		}
	}
	c, err := dial(opts, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", opts.addr(), err)
	}
	if err := c.WaitGreeting(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("greeting: %w", err)
	}
	saslClient, err := provider.IMAPSASLClient(ctx)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("credentials: %w", err)
	}
	if err := c.Authenticate(saslClient); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	return c, nil
}

// WatchContext closes the underlying connection if ctx is canceled before
// the returned stop func is called. go-imap's commands aren't
// context-aware, so this is what lets a long sync be aborted by its
// caller's context.
func (cl *Client) WatchContext(ctx context.Context) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = cl.c.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// Close logs out and closes the connection.
func (cl *Client) Close() error {
	_ = cl.c.Logout().Wait()
	return cl.c.Close()
}

// Folder is a mailbox as reported by LIST.
type Folder struct {
	Path       string
	Delim      string
	SpecialUse string // e.g. \Inbox, \Sent, \Drafts, \Trash, \Junk, \Archive — empty if none
}

// ListFolders lists all mailboxes, including special-use hints when the
// server supports the SPECIAL-USE extension. INBOX is synthetically
// tagged \Inbox, since RFC 6154 special-use attributes don't cover it
// (INBOX is just a reserved mailbox name).
func (cl *Client) ListFolders(ctx context.Context) ([]Folder, error) {
	data, err := cl.c.List("", "*", &imap.ListOptions{ReturnSpecialUse: true}).Collect()
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}

	folders := make([]Folder, 0, len(data))
	for _, d := range data {
		f := Folder{Path: d.Mailbox, Delim: string(d.Delim)}
		if f.Path == "INBOX" {
			f.SpecialUse = `\Inbox`
		}
		for _, attr := range d.Attrs {
			switch attr {
			case imap.MailboxAttrAll, imap.MailboxAttrArchive, imap.MailboxAttrDrafts, imap.MailboxAttrFlagged,
				imap.MailboxAttrJunk, imap.MailboxAttrSent, imap.MailboxAttrTrash, imap.MailboxAttrImportant:
				f.SpecialUse = string(attr)
			}
		}
		folders = append(folders, f)
	}
	return folders, nil
}

// SelectFolder selects a mailbox and returns its current UIDVALIDITY,
// UIDNEXT, message count, and (if the server supports RFC 7162 CONDSTORE)
// its HIGHESTMODSEQ. highestModSeq is 0 if the server — or this specific
// mailbox, per RFC 7162 — doesn't support persistent mod-sequences; callers
// must treat 0 as "no CONDSTORE baseline available" rather than a real
// mod-sequence.
func (cl *Client) SelectFolder(ctx context.Context, path string) (uidValidity, uidNext, numMessages uint32, highestModSeq uint64, err error) {
	opts := &imap.SelectOptions{CondStore: cl.c.Caps().Has(imap.CapCondStore)}
	data, err := cl.c.Select(path, opts).Wait()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("select %q: %w", path, err)
	}
	return data.UIDValidity, uint32(data.UIDNext), data.NumMessages, data.HighestModSeq, nil
}

// SupportsIdle reports whether the server advertises RFC 2177 IDLE.
func (cl *Client) SupportsIdle() bool {
	return cl.c.Caps().Has(imap.CapIdle)
}

// Idle sends an IDLE command and blocks until the connection ends — e.g.
// via WatchContext closing it when the caller's context is canceled, or a
// server/network error. While idling, the server's unsolicited updates
// (new mail, flag/expunge changes) are delivered via the handler
// registered when this Client was dialed — see DialClientWithUpdates.
// go-imap automatically restarts the underlying IDLE command every ~28
// minutes to avoid inactivity timeouts (RFC 2177).
func (cl *Client) Idle() error {
	cmd, err := cl.c.Idle()
	if err != nil {
		return fmt.Errorf("idle: %w", err)
	}
	return cmd.Wait()
}

// MessageHeader is a message's header fields as fetched from IMAP.
type MessageHeader struct {
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

// FetchUIDsAndFlags fetches just the UID and flags for every message
// currently in the selected mailbox — cheap compared to FetchHeaders since
// it skips envelopes entirely. Sync uses this to diff against the local
// cache and decide which messages actually need a full header fetch vs.
// just a flags refresh.
func (cl *Client) FetchUIDsAndFlags(ctx context.Context) (map[uint32][]string, error) {
	var uidSet imap.UIDSet
	uidSet.AddRange(1, 0) // "1:*"

	bufs, err := cl.c.Fetch(uidSet, &imap.FetchOptions{
		UID:   true,
		Flags: true,
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch uids/flags: %w", err)
	}

	out := make(map[uint32][]string, len(bufs))
	for _, b := range bufs {
		flags := make([]string, len(b.Flags))
		for i, f := range b.Flags {
			flags[i] = string(f)
		}
		out[uint32(b.UID)] = flags
	}
	return out, nil
}

// FetchChangedUIDsAndFlags fetches UID+flags only for messages whose
// MODSEQ has changed since sinceModSeq (RFC 7162 CONDSTORE) — this covers
// both new messages and flag changes, in a response that's typically far
// smaller than FetchUIDsAndFlags's whole-folder listing. Callers must only
// use sinceModSeq values obtained from a prior SelectFolder on this same
// mailbox/UIDVALIDITY; the server rejects an unknown/stale mod-sequence.
//
// CHANGEDSINCE alone doesn't report expunges (that needs QRESYNC's
// VANISHED response, which this library doesn't implement), so deletion
// reconciliation still needs ListUIDs.
func (cl *Client) FetchChangedUIDsAndFlags(ctx context.Context, sinceModSeq uint64) (map[uint32][]string, error) {
	var uidSet imap.UIDSet
	uidSet.AddRange(1, 0) // "1:*"

	bufs, err := cl.c.Fetch(uidSet, &imap.FetchOptions{
		UID:          true,
		Flags:        true,
		ChangedSince: sinceModSeq,
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch changed uids/flags: %w", err)
	}

	out := make(map[uint32][]string, len(bufs))
	for _, b := range bufs {
		flags := make([]string, len(b.Flags))
		for i, f := range b.Flags {
			flags[i] = string(f)
		}
		out[uint32(b.UID)] = flags
	}
	return out, nil
}

// ListUIDs returns every UID currently present in the selected mailbox, via
// UID SEARCH ALL — a single compact response (IMAP UID sets compress into
// ranges) rather than one line per message. Used for deletion
// reconciliation, including alongside the CONDSTORE fast path above, since
// CHANGEDSINCE never reports expunged messages.
func (cl *Client) ListUIDs(ctx context.Context) ([]uint32, error) {
	data, err := cl.c.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search all uids: %w", err)
	}
	uids := data.AllUIDs()
	out := make([]uint32, len(uids))
	for i, u := range uids {
		out[i] = uint32(u)
	}
	return out, nil
}

// FetchHeaders fetches envelope/flags/size for the given UIDs. Message
// headers are immutable once assigned to a UID, so this only needs to be
// called for UIDs not already in the local cache — see FetchUIDsAndFlags
// for the cheap path that refreshes flags on already-cached messages.
func (cl *Client) FetchHeaders(ctx context.Context, uids []uint32) ([]MessageHeader, error) {
	if len(uids) == 0 {
		return nil, nil
	}

	nums := make([]imap.UID, len(uids))
	for i, u := range uids {
		nums[i] = imap.UID(u)
	}
	uidSet := imap.UIDSetNum(nums...)

	// ENVELOPE (RFC 3501) only carries In-Reply-To, never References, so
	// References needs its own raw-header fetch. BODY.PEEK[HEADER.FIELDS
	// (References)] doesn't mark messages \Seen server-side.
	bufs, err := cl.c.Fetch(uidSet, &imap.FetchOptions{
		Envelope:   true,
		Flags:      true,
		RFC822Size: true,
		UID:        true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"References"}, Peek: true},
		},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch headers: %w", err)
	}

	headers := make([]MessageHeader, 0, len(bufs))
	for _, b := range bufs {
		headers = append(headers, headerFromBuffer(b))
	}
	return headers, nil
}

// FetchRawBody fetches the raw RFC 822 source of the message with the
// given UID in the currently selected mailbox. It uses BODY.PEEK[] so
// viewing a message doesn't mark it \Seen server-side out from under the
// local cache.
func (cl *Client) FetchRawBody(ctx context.Context, uid uint32) ([]byte, error) {
	uidSet := imap.UIDSetNum(imap.UID(uid))

	bufs, err := cl.c.Fetch(uidSet, &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{{Peek: true}},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch body uid=%d: %w", uid, err)
	}
	if len(bufs) == 0 || len(bufs[0].BodySection) == 0 {
		return nil, fmt.Errorf("message uid=%d: no body returned", uid)
	}
	return bufs[0].BodySection[0].Bytes, nil
}

// MoveToFolder moves the given UID from the currently selected mailbox to
// destPath, using the server's MOVE extension (RFC 6851) when available and
// transparently falling back to COPY + STORE \Deleted + EXPUNGE otherwise
// (see imapclient.Client.Move).
func (cl *Client) MoveToFolder(ctx context.Context, uid uint32, destPath string) error {
	uidSet := imap.UIDSetNum(imap.UID(uid))
	if _, err := cl.c.Move(uidSet, destPath).Wait(); err != nil {
		return fmt.Errorf("move uid=%d to %q: %w", uid, destPath, err)
	}
	return nil
}

// MarkSeen sets the \Seen flag on the given UID in the currently selected
// mailbox, telling the server the message has been read.
func (cl *Client) MarkSeen(ctx context.Context, uid uint32) error {
	uidSet := imap.UIDSetNum(imap.UID(uid))
	storeFlags := &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagSeen},
		Silent: true,
	}
	if err := cl.c.Store(uidSet, storeFlags, nil).Close(); err != nil {
		return fmt.Errorf("mark seen uid=%d: %w", uid, err)
	}
	return nil
}

func headerFromBuffer(b *imapclient.FetchMessageBuffer) MessageHeader {
	h := MessageHeader{
		UID:     uint32(b.UID),
		Subject: b.Envelope.Subject,
		Size:    b.RFC822Size,
	}

	if len(b.Envelope.From) > 0 {
		h.FromName = b.Envelope.From[0].Name
		h.FromAddr = b.Envelope.From[0].Addr()
	}
	h.ToAddrs = addrStrings(b.Envelope.To)
	h.CcAddrs = addrStrings(b.Envelope.Cc)
	h.Date = b.Envelope.Date
	h.MessageID = b.Envelope.MessageID
	if len(b.Envelope.InReplyTo) > 0 {
		h.InReplyTo = b.Envelope.InReplyTo[0]
	}
	if len(b.BodySection) > 0 {
		h.References = parseReferences(b.BodySection[0].Bytes)
	}
	h.Flags = make([]string, len(b.Flags))
	for i, f := range b.Flags {
		h.Flags[i] = string(f)
	}
	return h
}

// referenceIDRe matches one <message-id> token in a raw References header
// value.
var referenceIDRe = regexp.MustCompile(`<[^<>]+>`)

// parseReferences extracts message-IDs from a raw "References: <a> <b>\r\n"
// header field fetch, in order, stripped of their angle brackets.
func parseReferences(raw []byte) []string {
	matches := referenceIDRe.FindAll(raw, -1)
	if len(matches) == 0 {
		return nil
	}
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = string(m[1 : len(m)-1])
	}
	return ids
}

func addrStrings(addrs []imap.Address) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.Addr())
	}
	return out
}
