package imap

import (
	"context"
	"fmt"
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
	type result struct {
		c   *imapclient.Client
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		c, err := dialAuthenticated(ctx, opts, provider)
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

func dialAuthenticated(ctx context.Context, opts DialOptions, provider auth.Provider) (*imapclient.Client, error) {
	c, err := dial(opts)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", opts.addr(), err)
	}
	if err := c.WaitGreeting(); err != nil {
		c.Close()
		return nil, fmt.Errorf("greeting: %w", err)
	}
	saslClient, err := provider.IMAPSASLClient(ctx)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("credentials: %w", err)
	}
	if err := c.Authenticate(saslClient); err != nil {
		c.Close()
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
			cl.c.Close()
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
// UIDNEXT, and message count.
func (cl *Client) SelectFolder(ctx context.Context, path string) (uidValidity, uidNext, numMessages uint32, err error) {
	data, err := cl.c.Select(path, nil).Wait()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("select %q: %w", path, err)
	}
	return data.UIDValidity, uint32(data.UIDNext), data.NumMessages, nil
}

// MessageHeader is a message's header fields as fetched from IMAP.
type MessageHeader struct {
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

// FetchAllHeaders fetches envelope/flags/size for every message currently
// in the selected mailbox.
//
// This always re-fetches the whole mailbox rather than only new UIDs plus a
// separate flag-refresh pass for known ones (as sketched in the design
// doc) — simpler and correct at personal-mailbox scale. If sync time on
// large mailboxes becomes a problem, split this into a UID-range fetch for
// new messages and a FLAGS-only fetch for previously known ones.
func (cl *Client) FetchAllHeaders(ctx context.Context) ([]MessageHeader, error) {
	var uidSet imap.UIDSet
	uidSet.AddRange(1, 0) // "1:*"

	bufs, err := cl.c.Fetch(uidSet, &imap.FetchOptions{
		Envelope:   true,
		Flags:      true,
		RFC822Size: true,
		UID:        true,
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
	h.Flags = make([]string, len(b.Flags))
	for i, f := range b.Flags {
		h.Flags[i] = string(f)
	}
	return h
}

func addrStrings(addrs []imap.Address) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.Addr())
	}
	return out
}
