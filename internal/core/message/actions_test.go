package message

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/imap"
	"github.com/tomowang/pigeoncli/internal/storage/blob"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

// --- offline behaviour (no IMAP connection is ever attempted) ---

func TestMessageFlagHelpers(t *testing.T) {
	m := Message{Flags: []string{`\seen`, `\Flagged`}}
	if !m.IsRead() || !m.IsStarred() || m.Has(FlagAnswered) {
		t.Fatalf("unexpected flag helpers for %v", m.Flags)
	}
	if (Message{}).IsRead() {
		t.Fatal("message with no flags reported as read")
	}
}

func TestDeleteWithNoTrashFolderErrors(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, folderID := newTestService(t)
	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, []sqlite.MessageHeader{{UID: 1}}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Account{Slug: "work"}
	if _, err := svc.Delete(ctx, cfg, []Ref{{"INBOX", 1}}); !errors.Is(err, ErrNoTrashFolder) {
		t.Fatalf("expected ErrNoTrashFolder, got %v", err)
	}
}

func TestDeleteAlreadyInTrashErrors(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, _ := newTestService(t)
	trashID, err := db.UpsertFolder(ctx, accountID, "Trash", "Trash", "/", `\Trash`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, trashID, []sqlite.MessageHeader{{UID: 1}}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Account{Slug: "work"}
	if _, err := svc.Delete(ctx, cfg, []Ref{{"Trash", 1}}); !errors.Is(err, ErrAlreadyInTrash) {
		t.Fatalf("expected ErrAlreadyInTrash, got %v", err)
	}
}

func TestArchiveWithNoArchiveFolderErrors(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, folderID := newTestService(t)
	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, []sqlite.MessageHeader{{UID: 1}}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Account{Slug: "work"}
	if _, err := svc.Archive(ctx, cfg, []Ref{{"INBOX", 1}}); !errors.Is(err, ErrNoArchiveFolder) {
		t.Fatalf("expected ErrNoArchiveFolder, got %v", err)
	}
}

func TestArchiveFallsBackToGmailAllMailAndSkipsItself(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, _ := newTestService(t)
	allID, err := db.UpsertFolder(ctx, accountID, "[Gmail]/All Mail", "All Mail", "/", `\All`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessageHeaders(ctx, accountID, allID, []sqlite.MessageHeader{{UID: 1}}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Account{Slug: "work"}
	// The destination resolved to \All, and the only message is already
	// there, so nothing is left to move (and no connection is attempted).
	if _, err := svc.Archive(ctx, cfg, []Ref{{"[Gmail]/All Mail", 1}}); err == nil ||
		errors.Is(err, ErrNoArchiveFolder) {
		t.Fatalf("expected an already-archived error, got %v", err)
	}
}

func TestSetFlagsValidation(t *testing.T) {
	ctx := context.Background()
	svc, db, accountID, folderID := newTestService(t)
	if err := db.UpsertMessageHeaders(ctx, accountID, folderID, []sqlite.MessageHeader{{UID: 1}}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Account{Slug: "work"}
	refs := []Ref{{"INBOX", 1}}

	if err := svc.SetFlags(ctx, cfg, refs, []Flag{`\Deleted`}, nil); err == nil {
		t.Fatal("expected an error for an unsupported flag")
	}
	if err := svc.SetFlags(ctx, cfg, refs, []Flag{FlagSeen}, []Flag{FlagSeen}); err == nil {
		t.Fatal("expected an error for a flag both added and removed")
	}
	if err := svc.SetFlags(ctx, cfg, []Ref{{"INBOX", 0}}, []Flag{FlagSeen}, nil); err == nil {
		t.Fatal("expected an error for UID 0")
	}
	if err := svc.SetFlags(ctx, cfg, []Ref{{"Nope", 1}}, []Flag{FlagSeen}, nil); err == nil {
		t.Fatal("expected an error for an unsynced folder")
	}
	// Empty input is a no-op, not an error.
	if err := svc.SetFlags(ctx, cfg, nil, []Flag{FlagSeen}, nil); err != nil {
		t.Fatalf("empty refs: %v", err)
	}
}

func TestUndoWithoutDestUIDIsNotUndoable(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	err := svc.Undo(context.Background(), config.Account{Slug: "work"}, []MoveResult{
		{Src: Ref{"INBOX", 1}, Dest: Ref{"Archive", 0}},
	})
	if !errors.Is(err, ErrNotUndoable) {
		t.Fatalf("expected ErrNotUndoable, got %v", err)
	}
}

// --- behaviour against a real in-process IMAP server ---

type plainProvider struct{}

func (plainProvider) IMAPSASLClient(context.Context) (sasl.Client, error) {
	return sasl.NewPlainClient("", "me", "pw"), nil
}
func (plainProvider) SMTPSASLClient(context.Context) (sasl.Client, error) {
	return sasl.NewPlainClient("", "me", "pw"), nil
}

type fixture struct {
	svc       *Service
	db        *sqlite.DB
	cfg       config.Account
	user      *imapmemserver.User
	accountID int64
	folderIDs map[string]int64
	dialOpts  imap.DialOptions
}

// newFixture starts an in-memory IMAP server (MOVE + UIDPLUS) with the given
// folders — path -> special-use — and points a Service at it, with the same
// folders registered in the local cache.
func newFixture(t *testing.T, folders map[string]string) *fixture {
	t.Helper()
	ctx := context.Background()

	mem := imapmemserver.New()
	user := imapmemserver.NewUser("me", "pw")
	mem.AddUser(user)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps: goimap.CapSet{
			goimap.CapIMAP4rev1: {},
			goimap.CapUIDPlus:   {},
			goimap.CapMove:      {},
		},
		InsecureAuth: true,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	accountID, err := db.UpsertAccount(ctx, "work", "me@example.com", "Work")
	if err != nil {
		t.Fatal(err)
	}

	fx := &fixture{
		db: db, user: user, accountID: accountID, folderIDs: map[string]int64{},
		cfg: config.Account{Slug: "work", Email: "me@example.com"},
		dialOpts: imap.DialOptions{
			Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, TLS: config.TLSModeNone,
		},
	}
	for path, use := range folders {
		if path != "INBOX" { // the memserver has no INBOX until created
			if err := user.Create(path, nil); err != nil {
				t.Fatalf("create %q: %v", path, err)
			}
		} else if err := user.Create("INBOX", nil); err != nil {
			t.Fatalf("create INBOX: %v", err)
		}
		id, err := db.UpsertFolder(ctx, accountID, path, path, "/", use)
		if err != nil {
			t.Fatal(err)
		}
		fx.folderIDs[path] = id
	}

	fx.svc = NewService(db, blob.NewStore(t.TempDir()))
	fx.svc.dial = func(ctx context.Context, _ config.Account) (*imap.Client, error) {
		return imap.DialClient(ctx, fx.dialOpts, plainProvider{})
	}
	return fx
}

// seed appends a message with the given subject to folder on the server and
// caches its header (with flags) locally, returning its UID.
func (fx *fixture) seed(t *testing.T, folder, subject string, flags ...string) uint32 {
	t.Helper()
	raw := fmt.Sprintf("From: a@example.com\r\nTo: me@example.com\r\nSubject: %s\r\nMessage-ID: <%s@example.com>\r\n\r\nbody\r\n", subject, subject)
	opts := &goimap.AppendOptions{Time: time.Now()}
	for _, f := range flags {
		opts.Flags = append(opts.Flags, goimap.Flag(f))
	}
	data, err := fx.user.Append(folder, strings.NewReader(raw), opts)
	if err != nil {
		t.Fatalf("append to %q: %v", folder, err)
	}
	uid := uint32(data.UID)
	if err := fx.db.UpsertMessageHeaders(context.Background(), fx.accountID, fx.folderIDs[folder], []sqlite.MessageHeader{
		{UID: uid, Subject: subject, MessageID: "<" + subject + "@example.com>", Flags: flags, Date: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	if err := fx.db.RefreshFolderCounts(context.Background(), fx.folderIDs[folder]); err != nil {
		t.Fatal(err)
	}
	return uid
}

// serverFolder returns subject and flags for every message the server holds
// in folder, keyed by UID, via an independent connection.
func (fx *fixture) serverFolder(t *testing.T, folder string) map[uint32]serverMsg {
	t.Helper()
	ctx := context.Background()
	cl, err := imap.DialClient(ctx, fx.dialOpts, plainProvider{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cl.Close() }()
	if _, _, _, _, err := cl.SelectFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
	uids, err := cl.ListUIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	flags, err := cl.FetchUIDsAndFlags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := map[uint32]serverMsg{}
	if len(uids) == 0 {
		return out
	}
	headers, err := cl.FetchHeaders(ctx, uids)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range headers {
		out[h.UID] = serverMsg{Subject: h.Subject, Flags: flags[h.UID]}
	}
	return out
}

type serverMsg struct {
	Subject string
	Flags   []string
}

func (fx *fixture) subjects(t *testing.T, folder string) []string {
	t.Helper()
	var out []string
	for _, m := range fx.serverFolder(t, folder) {
		out = append(out, m.Subject)
	}
	slices.Sort(out)
	return out
}

func (fx *fixture) counts(t *testing.T, folder string) (total, unread int) {
	t.Helper()
	rows, err := fx.db.ListFolders(context.Background(), fx.accountID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Path == folder {
			return r.TotalCount, r.UnreadCount
		}
	}
	t.Fatalf("folder %q not found", folder)
	return 0, 0
}

func (fx *fixture) cachedUIDs(t *testing.T, folder string) []uint32 {
	t.Helper()
	flags, err := fx.db.ListMessageFlags(context.Background(), fx.folderIDs[folder])
	if err != nil {
		t.Fatal(err)
	}
	var uids []uint32
	for uid := range flags {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	return uids
}

func hasFlag(flags []string, want string) bool {
	return slices.ContainsFunc(flags, func(f string) bool { return strings.EqualFold(f, want) })
}

func TestSetFlagsWritesBackToServerAndCache(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t, map[string]string{"INBOX": `\Inbox`})
	u1 := fx.seed(t, "INBOX", "one")
	u2 := fx.seed(t, "INBOX", "two")
	fx.seed(t, "INBOX", "three")
	if _, unread := fx.counts(t, "INBOX"); unread != 3 {
		t.Fatalf("precondition: unread = %d, want 3", unread)
	}

	refs := []Ref{{"INBOX", u1}, {"INBOX", u2}}
	if err := fx.svc.MarkRead(ctx, fx.cfg, refs); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	srv := fx.serverFolder(t, "INBOX")
	if !hasFlag(srv[u1].Flags, `\Seen`) || !hasFlag(srv[u2].Flags, `\Seen`) {
		t.Fatalf("server flags after MarkRead: %v", srv)
	}
	// The folder's unread badge is refreshed immediately, not on next sync.
	if total, unread := fx.counts(t, "INBOX"); total != 3 || unread != 1 {
		t.Fatalf("counts after MarkRead = %d/%d, want 3/1", total, unread)
	}

	if err := fx.svc.Star(ctx, fx.cfg, []Ref{{"INBOX", u1}}); err != nil {
		t.Fatalf("Star: %v", err)
	}
	msgs, err := fx.svc.List(ctx, "work", "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		switch m.UID {
		case u1:
			if !m.IsRead() || !m.IsStarred() {
				t.Fatalf("u1 cached flags %v, want read+starred", m.Flags)
			}
		case u2:
			if !m.IsRead() || m.IsStarred() {
				t.Fatalf("u2 cached flags %v, want read only", m.Flags)
			}
		}
	}
	if !hasFlag(fx.serverFolder(t, "INBOX")[u1].Flags, `\Flagged`) {
		t.Fatal("server lost \\Flagged")
	}

	if err := fx.svc.MarkUnread(ctx, fx.cfg, refs); err != nil {
		t.Fatalf("MarkUnread: %v", err)
	}
	if err := fx.svc.Unstar(ctx, fx.cfg, []Ref{{"INBOX", u1}}); err != nil {
		t.Fatalf("Unstar: %v", err)
	}
	srv = fx.serverFolder(t, "INBOX")
	if hasFlag(srv[u1].Flags, `\Seen`) || hasFlag(srv[u1].Flags, `\Flagged`) {
		t.Fatalf("server flags after undoing: %v", srv[u1].Flags)
	}
	if _, unread := fx.counts(t, "INBOX"); unread != 3 {
		t.Fatalf("unread after MarkUnread = %d, want 3", unread)
	}
}

func TestBodyMarkSeenRefreshesUnreadCount(t *testing.T) {
	// Regression: opening a message used to leave the folder's stored unread
	// badge stale until the next sync.
	ctx := context.Background()
	fx := newFixture(t, map[string]string{"INBOX": `\Inbox`})
	uid := fx.seed(t, "INBOX", "one")
	fx.seed(t, "INBOX", "two")

	if _, err := fx.svc.Body(ctx, fx.cfg, "INBOX", uid); err != nil {
		t.Fatalf("Body: %v", err)
	}
	if _, unread := fx.counts(t, "INBOX"); unread != 1 {
		t.Fatalf("unread after opening a message = %d, want 1", unread)
	}
	if !hasFlag(fx.serverFolder(t, "INBOX")[uid].Flags, `\Seen`) {
		t.Fatal("server didn't record \\Seen")
	}
}

func TestArchiveMovesBatchAndUndoRestores(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t, map[string]string{"INBOX": `\Inbox`, "Archive": `\Archive`})
	// A pre-existing archived message shifts the destination UIDs, so the
	// source->dest mapping is not the identity.
	fx.seed(t, "Archive", "already-archived")
	u1 := fx.seed(t, "INBOX", "m1")
	fx.seed(t, "INBOX", "m2")
	fx.seed(t, "INBOX", "m3")
	u4 := fx.seed(t, "INBOX", "m4")

	moved, err := fx.svc.Archive(ctx, fx.cfg, []Ref{{"INBOX", u4}, {"INBOX", u1}, {"INBOX", u1}})
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if len(moved) != 2 {
		t.Fatalf("moved %d messages, want 2 (duplicate ref collapsed): %+v", len(moved), moved)
	}

	if got, want := fx.subjects(t, "INBOX"), []string{"m2", "m3"}; !slices.Equal(got, want) {
		t.Fatalf("INBOX on server = %v, want %v", got, want)
	}
	if got, want := fx.subjects(t, "Archive"), []string{"already-archived", "m1", "m4"}; !slices.Equal(got, want) {
		t.Fatalf("Archive on server = %v, want %v", got, want)
	}
	// Each result's Dest UID must point at the same message on the server.
	archive := fx.serverFolder(t, "Archive")
	wantSubject := map[uint32]string{u1: "m1", u4: "m4"}
	for _, m := range moved {
		if m.Dest.FolderPath != "Archive" || m.DestUIDValidity == 0 {
			t.Fatalf("bad result %+v", m)
		}
		if got := archive[m.Dest.UID].Subject; got != wantSubject[m.Src.UID] {
			t.Fatalf("Dest UID %d holds %q, want %q", m.Dest.UID, got, wantSubject[m.Src.UID])
		}
	}

	// Local cache: source rows gone, counts refreshed.
	if got := len(fx.cachedUIDs(t, "INBOX")); got != 2 {
		t.Fatalf("cached INBOX rows = %d, want 2", got)
	}
	if total, unread := fx.counts(t, "INBOX"); total != 2 || unread != 2 {
		t.Fatalf("INBOX counts = %d/%d, want 2/2", total, unread)
	}

	if err := fx.svc.Undo(ctx, fx.cfg, moved); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got, want := fx.subjects(t, "INBOX"), []string{"m1", "m2", "m3", "m4"}; !slices.Equal(got, want) {
		t.Fatalf("INBOX after undo = %v, want %v", got, want)
	}
	if got, want := fx.subjects(t, "Archive"), []string{"already-archived"}; !slices.Equal(got, want) {
		t.Fatalf("Archive after undo = %v, want %v", got, want)
	}
}

func TestDeleteTrashesThenPurgeRemovesForGood(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t, map[string]string{"INBOX": `\Inbox`, "Trash": `\Trash`})
	u1 := fx.seed(t, "INBOX", "keep")
	u2 := fx.seed(t, "INBOX", "bin")

	moved, err := fx.svc.Delete(ctx, fx.cfg, []Ref{{"INBOX", u2}})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := fx.subjects(t, "Trash"); !slices.Equal(got, []string{"bin"}) {
		t.Fatalf("Trash = %v", got)
	}
	if got := fx.subjects(t, "INBOX"); !slices.Equal(got, []string{"keep"}) {
		t.Fatalf("INBOX = %v", got)
	}
	_ = u1

	// Once the Trash folder is synced, deleting from it is refused rather
	// than silently permanent.
	trashUID := moved[0].Dest.UID
	if err := fx.db.UpsertMessageHeaders(ctx, fx.accountID, fx.folderIDs["Trash"],
		[]sqlite.MessageHeader{{UID: trashUID, Subject: "bin", Date: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.svc.Delete(ctx, fx.cfg, []Ref{{"Trash", trashUID}}); !errors.Is(err, ErrAlreadyInTrash) {
		t.Fatalf("Delete in Trash: got %v, want ErrAlreadyInTrash", err)
	}

	// An unrelated message flagged \Deleted must survive Purge (UID EXPUNGE,
	// not a plain EXPUNGE).
	other := fx.seed(t, "Trash", "flagged-deleted-elsewhere", `\Deleted`)
	if err := fx.svc.Purge(ctx, fx.cfg, []Ref{{"Trash", trashUID}}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if got := fx.subjects(t, "Trash"); !slices.Equal(got, []string{"flagged-deleted-elsewhere"}) {
		t.Fatalf("Trash after Purge = %v", got)
	}
	if got := fx.cachedUIDs(t, "Trash"); !slices.Equal(got, []uint32{other}) {
		t.Fatalf("cached Trash UIDs after Purge = %v, want [%d]", got, other)
	}
}

func TestMoveAcrossFoldersInOneCall(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t, map[string]string{"INBOX": `\Inbox`, "Work": "", "Later": ""})
	a := fx.seed(t, "INBOX", "from-inbox")
	b := fx.seed(t, "Work", "from-work")

	moved, err := fx.svc.Move(ctx, fx.cfg, []Ref{{"INBOX", a}, {"Work", b}}, "Later")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if len(moved) != 2 {
		t.Fatalf("moved = %+v", moved)
	}
	if got := fx.subjects(t, "Later"); !slices.Equal(got, []string{"from-inbox", "from-work"}) {
		t.Fatalf("Later = %v", got)
	}
	if len(fx.serverFolder(t, "INBOX"))+len(fx.serverFolder(t, "Work")) != 0 {
		t.Fatal("source folders should be empty")
	}

	// Messages already in the destination are skipped, not an error.
	c := fx.seed(t, "Later", "already-there")
	moved, err = fx.svc.Move(ctx, fx.cfg, []Ref{{"Later", c}}, "Later")
	if err == nil {
		t.Fatalf("expected an already-there error, got moved=%+v", moved)
	}

	if _, err := fx.svc.Move(ctx, fx.cfg, []Ref{{"Later", c}}, "Nowhere"); err == nil {
		t.Fatal("expected an error for an unsynced destination")
	}
}

func TestMoveToJunkStillWorksThroughSharedPath(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t, map[string]string{"INBOX": `\Inbox`, "Junk": `\Junk`})
	uid := fx.seed(t, "INBOX", "spammy")

	if err := fx.svc.MoveToJunk(ctx, fx.cfg, "INBOX", uid); err != nil {
		t.Fatalf("MoveToJunk: %v", err)
	}
	if got := fx.subjects(t, "Junk"); !slices.Equal(got, []string{"spammy"}) {
		t.Fatalf("Junk = %v", got)
	}
	if len(fx.cachedUIDs(t, "INBOX")) != 0 {
		t.Fatal("source row should leave the cache")
	}
}
