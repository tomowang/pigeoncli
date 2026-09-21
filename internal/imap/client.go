package imap

import (
	"context"
	"errors"
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

// ListFolders lists all selectable mailboxes, including special-use hints
// when the server supports the SPECIAL-USE extension. INBOX is
// synthetically tagged \Inbox, since RFC 6154 special-use attributes don't
// cover it (INBOX is just a reserved mailbox name).
//
// A mailbox flagged \Noselect or \NonExistent is skipped: it's a naming
// placeholder for its children, not a mailbox that can hold messages of
// its own. Gmail's "[Gmail]" parent is the common example — it exists only
// so "[Gmail]/All Mail" etc. can nest under it, is reported with
// \NonExistent (not \Noselect) in Gmail's LIST response, and errors with
// "NONEXISTENT" if SELECTed directly.
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
		var unselectable bool
		for _, attr := range d.Attrs {
			switch attr {
			case imap.MailboxAttrAll, imap.MailboxAttrArchive, imap.MailboxAttrDrafts, imap.MailboxAttrFlagged,
				imap.MailboxAttrJunk, imap.MailboxAttrSent, imap.MailboxAttrTrash, imap.MailboxAttrImportant:
				f.SpecialUse = string(attr)
			case imap.MailboxAttrNoSelect, imap.MailboxAttrNonExistent:
				unselectable = true
			}
		}
		if unselectable {
			continue
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

// ErrUIDPlusRequired is returned by operations that would otherwise have to
// fall back to a plain EXPUNGE, which deletes every \Deleted message in the
// mailbox rather than just the ones being acted on. That's only safe with
// UID EXPUNGE (UIDPLUS, RFC 4315).
var ErrUIDPlusRequired = errors.New("server supports neither MOVE nor UIDPLUS, so this can't be done safely")

// MoveResult describes where a batch of moved messages landed.
type MoveResult struct {
	// UIDValidity is the destination mailbox's UIDVALIDITY, so a caller
	// that acts on Dest later can tell whether the UIDs are still valid.
	UIDValidity uint32
	// Dest maps each moved source UID to its new UID in the destination
	// mailbox (COPYUID, RFC 4315). It is empty when the server didn't
	// report UIDs (no UIDPLUS) or reported a mapping that couldn't be
	// trusted.
	Dest map[uint32]uint32
}

func uidSetOf(uids []uint32) imap.UIDSet {
	var set imap.UIDSet
	for _, u := range uids {
		set.AddNum(imap.UID(u))
	}
	return set
}

// MoveToFolder moves the given UIDs from the currently selected mailbox to
// destPath, using the server's MOVE extension (RFC 6851) when available and
// otherwise falling back to COPY + STORE \Deleted + UID EXPUNGE (see
// imapclient.Client.Move). Without MOVE or UIDPLUS that fallback would
// expunge every \Deleted message in the mailbox, so it returns
// ErrUIDPlusRequired instead.
func (cl *Client) MoveToFolder(ctx context.Context, uids []uint32, destPath string) (MoveResult, error) {
	if len(uids) == 0 {
		return MoveResult{}, nil
	}
	caps := cl.c.Caps()
	if !caps.Has(imap.CapMove) && !caps.Has(imap.CapUIDPlus) {
		return MoveResult{}, ErrUIDPlusRequired
	}

	src := uidSetOf(uids)
	data, err := cl.c.Move(src, destPath).Wait()
	if err != nil {
		return MoveResult{}, fmt.Errorf("move %d message(s) to %q: %w", len(uids), destPath, err)
	}
	return moveResultFromData(src, data), nil
}

// moveResultFromData pairs the COPYUID source and destination UIDs
// positionally (RFC 4315: the n-th source UID became the n-th destination
// UID). Both sets are normalized to ascending order by the client library,
// which matches the order the server copies an ascending request in; the
// pairing is only trusted when the reported source set is exactly the set
// requested and the counts agree, otherwise Dest is left empty.
func moveResultFromData(requested imap.UIDSet, data *imapclient.MoveData) MoveResult {
	res := MoveResult{}
	if data == nil {
		return res
	}
	res.UIDValidity = data.UIDValidity
	srcSet, ok := data.SourceUIDs.(imap.UIDSet)
	if !ok {
		return res
	}
	dstSet, ok := data.DestUIDs.(imap.UIDSet)
	if !ok {
		return res
	}
	srcUIDs, ok1 := srcSet.Nums()
	dstUIDs, ok2 := dstSet.Nums()
	reqUIDs, ok3 := requested.Nums()
	if !ok1 || !ok2 || !ok3 || len(srcUIDs) != len(dstUIDs) || len(srcUIDs) != len(reqUIDs) {
		return res
	}
	for i := range srcUIDs {
		if srcUIDs[i] != reqUIDs[i] {
			return res
		}
	}
	res.Dest = make(map[uint32]uint32, len(srcUIDs))
	for i := range srcUIDs {
		res.Dest[uint32(srcUIDs[i])] = uint32(dstUIDs[i])
	}
	return res
}

// StoreFlags adds and/or removes flags on the given UIDs in the currently
// selected mailbox. Flags are IMAP flag strings such as `\Seen`.
func (cl *Client) StoreFlags(ctx context.Context, uids []uint32, add, remove []string) error {
	if len(uids) == 0 {
		return nil
	}
	set := uidSetOf(uids)
	for _, op := range []struct {
		op    imap.StoreFlagsOp
		flags []string
	}{
		{imap.StoreFlagsAdd, add},
		{imap.StoreFlagsDel, remove},
	} {
		if len(op.flags) == 0 {
			continue
		}
		flags := make([]imap.Flag, len(op.flags))
		for i, f := range op.flags {
			flags[i] = imap.Flag(f)
		}
		storeFlags := &imap.StoreFlags{Op: op.op, Flags: flags, Silent: true}
		if err := cl.c.Store(set, storeFlags, nil).Close(); err != nil {
			return fmt.Errorf("store flags on %d message(s): %w", len(uids), err)
		}
	}
	return nil
}

// Purge permanently deletes the given UIDs from the currently selected
// mailbox: STORE +FLAGS \Deleted followed by UID EXPUNGE. Without UIDPLUS
// it returns ErrUIDPlusRequired rather than issuing a plain EXPUNGE, which
// would also delete unrelated messages already flagged \Deleted.
func (cl *Client) Purge(ctx context.Context, uids []uint32) error {
	if len(uids) == 0 {
		return nil
	}
	if !cl.c.Caps().Has(imap.CapUIDPlus) {
		return ErrUIDPlusRequired
	}
	set := uidSetOf(uids)
	storeFlags := &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}
	if err := cl.c.Store(set, storeFlags, nil).Close(); err != nil {
		return fmt.Errorf("flag %d message(s) deleted: %w", len(uids), err)
	}
	if err := cl.c.UIDExpunge(set).Close(); err != nil {
		return fmt.Errorf("expunge %d message(s): %w", len(uids), err)
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
