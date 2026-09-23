package compose

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
	"github.com/tomowang/pigeoncli/internal/imap"
	"github.com/tomowang/pigeoncli/internal/mime"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

type plainProvider struct{}

func (plainProvider) IMAPSASLClient(context.Context) (sasl.Client, error) {
	return sasl.NewPlainClient("", "me", "pw"), nil
}
func (plainProvider) SMTPSASLClient(context.Context) (sasl.Client, error) {
	return sasl.NewPlainClient("", "me", "pw"), nil
}

// draftsFixture starts an in-memory IMAP server (UIDPLUS; no MOVE, since
// drafts don't need it) with the given folders — path -> special-use —
// and points a Service at it, with the same folders registered in the
// local cache folderSvc.List reads from.
type draftsFixture struct {
	svc       *Service
	db        *sqlite.DB
	cfg       config.Account
	user      *imapmemserver.User
	accountID int64
	dialOpts  imap.DialOptions
}

func newDraftsFixture(t *testing.T, folders map[string]string) *draftsFixture {
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

	fx := &draftsFixture{
		db: db, user: user, accountID: accountID,
		cfg: config.Account{Slug: "work", Email: "me@example.com"},
		dialOpts: imap.DialOptions{
			Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, TLS: config.TLSModeNone,
		},
	}
	for path, use := range folders {
		if err := user.Create(path, nil); err != nil {
			t.Fatalf("create %q: %v", path, err)
		}
		if _, err := db.UpsertFolder(ctx, accountID, path, path, "/", use); err != nil {
			t.Fatal(err)
		}
	}

	fx.svc = NewService(folder.NewService(db), nil)
	fx.svc.dial = func(ctx context.Context, _ config.Account) (*imap.Client, error) {
		return imap.DialClient(ctx, fx.dialOpts, plainProvider{})
	}
	return fx
}

// serverFolder returns UID -> flags for every message folderPath holds on
// the server, via an independent connection.
func (fx *draftsFixture) serverFolder(t *testing.T, folderPath string) map[uint32][]string {
	t.Helper()
	ctx := context.Background()
	cl, err := imap.DialClient(ctx, fx.dialOpts, plainProvider{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cl.Close() }()
	if _, _, _, _, err := cl.SelectFolder(ctx, folderPath); err != nil {
		t.Fatal(err)
	}
	flags, err := cl.FetchUIDsAndFlags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return flags
}

// serverRaw fetches uid's raw body from folderPath, via an independent
// connection.
func (fx *draftsFixture) serverRaw(t *testing.T, folderPath string, uid uint32) []byte {
	t.Helper()
	ctx := context.Background()
	cl, err := imap.DialClient(ctx, fx.dialOpts, plainProvider{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cl.Close() }()
	if _, _, _, _, err := cl.SelectFolder(ctx, folderPath); err != nil {
		t.Fatal(err)
	}
	raw, err := cl.FetchRawBody(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func testDraft() Draft {
	return Draft{
		To:      []string{"bob@example.com"},
		Cc:      []string{"carol@example.com"},
		Bcc:     []string{"secret@example.com"},
		Subject: "Draft subject",
		Body:    "Draft body.",
	}
}

func TestDraftsFolderResolvesSpecialUse(t *testing.T) {
	fx := newDraftsFixture(t, map[string]string{"INBOX": `\Inbox`, "Drafts": `\Drafts`})
	path, err := fx.svc.DraftsFolder(context.Background(), "work")
	if err != nil || path != "Drafts" {
		t.Fatalf("DraftsFolder = %q, %v", path, err)
	}
}

func TestDraftsFolderErrorsWithoutOne(t *testing.T) {
	fx := newDraftsFixture(t, map[string]string{"INBOX": `\Inbox`})
	if _, err := fx.svc.DraftsFolder(context.Background(), "work"); !errors.Is(err, ErrNoDraftsFolder) {
		t.Fatalf("expected ErrNoDraftsFolder, got %v", err)
	}
}

func TestDraftsFolderErrorsWithoutFolderService(t *testing.T) {
	svc := NewService(nil, nil)
	if _, err := svc.DraftsFolder(context.Background(), "work"); err == nil {
		t.Fatal("expected an error with no folder service configured")
	}
}

func TestSaveDraftAppendsWithDraftFlagAndBcc(t *testing.T) {
	ctx := context.Background()
	fx := newDraftsFixture(t, map[string]string{"INBOX": `\Inbox`, "Drafts": `\Drafts`})

	ref, err := fx.svc.SaveDraft(ctx, fx.cfg, testDraft(), DraftRef{})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if ref.FolderPath != "Drafts" || ref.UID == 0 {
		t.Fatalf("ref = %+v", ref)
	}

	flags := fx.serverFolder(t, "Drafts")
	got, ok := flags[ref.UID]
	if !ok {
		t.Fatalf("no message at uid %d in Drafts", ref.UID)
	}
	found := false
	for _, f := range got {
		if strings.EqualFold(f, draftFlag) {
			found = true
		}
	}
	if !found {
		t.Fatalf("flags = %v, want %q among them", got, draftFlag)
	}

	raw := fx.serverRaw(t, "Drafts", ref.UID)
	if !strings.Contains(string(raw), "secret@example.com") {
		t.Fatal("saved draft should include the Bcc header")
	}
	if !strings.Contains(string(raw), "Draft body.") {
		t.Fatal("saved draft should include the body")
	}
}

func TestSaveDraftReplacesPreviousCopy(t *testing.T) {
	ctx := context.Background()
	fx := newDraftsFixture(t, map[string]string{"INBOX": `\Inbox`, "Drafts": `\Drafts`})

	draft := testDraft()
	ref1, err := fx.svc.SaveDraft(ctx, fx.cfg, draft, DraftRef{})
	if err != nil {
		t.Fatalf("first SaveDraft: %v", err)
	}

	draft.Body = "Edited body."
	ref2, err := fx.svc.SaveDraft(ctx, fx.cfg, draft, ref1)
	if err != nil {
		t.Fatalf("second SaveDraft: %v", err)
	}
	if ref2.UID == ref1.UID {
		t.Fatalf("expected a new UID, got the same one: %d", ref2.UID)
	}

	flags := fx.serverFolder(t, "Drafts")
	if len(flags) != 1 {
		t.Fatalf("Drafts folder has %d messages, want 1 (the stale copy should be gone): %v", len(flags), flags)
	}
	if _, ok := flags[ref2.UID]; !ok {
		t.Fatalf("the surviving message should be the new copy, uid %d; got %v", ref2.UID, flags)
	}

	raw := fx.serverRaw(t, "Drafts", ref2.UID)
	if !strings.Contains(string(raw), "Edited body.") {
		t.Fatal("surviving copy should have the edited body")
	}
}

func TestDiscardDraftRemovesMessage(t *testing.T) {
	ctx := context.Background()
	fx := newDraftsFixture(t, map[string]string{"INBOX": `\Inbox`, "Drafts": `\Drafts`})

	ref, err := fx.svc.SaveDraft(ctx, fx.cfg, testDraft(), DraftRef{})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := fx.svc.DiscardDraft(ctx, fx.cfg, ref); err != nil {
		t.Fatalf("DiscardDraft: %v", err)
	}
	if flags := fx.serverFolder(t, "Drafts"); len(flags) != 0 {
		t.Fatalf("Drafts folder = %v, want empty", flags)
	}
}

func TestDiscardDraftZeroRefIsNoop(t *testing.T) {
	svc := NewService(nil, nil) // no folder service, no dial — would fail if either were touched
	if err := svc.DiscardDraft(context.Background(), config.Account{Slug: "work"}, DraftRef{}); err != nil {
		t.Fatalf("DiscardDraft(zero ref) = %v, want nil", err)
	}
}

func TestLoadDraftReconstructsToCCBccBodyAndAttachments(t *testing.T) {
	raw, err := mime.BuildMessage(
		mime.Recipient{Addr: "me@example.com"},
		[]mime.Recipient{{Addr: "bob@example.com"}},
		[]mime.Recipient{{Addr: "carol@example.com"}},
		[]mime.Recipient{{Addr: "secret@example.com"}},
		"Draft subject", "Draft body.", "", "",
		[]mime.OutgoingAttachment{{Filename: "notes.txt", Data: []byte("hello")}},
	)
	if err != nil {
		t.Fatalf("mime.BuildMessage: %v", err)
	}
	atts, err := mime.Attachments(raw)
	if err != nil {
		t.Fatalf("mime.Attachments: %v", err)
	}
	msgAtts := make([]message.Attachment, len(atts))
	for i, a := range atts {
		msgAtts[i] = message.Attachment{Index: a.Index, Filename: a.Filename, ContentType: a.ContentType, Size: a.Size}
	}

	msg := message.Message{UID: 7, ToAddrs: []string{"bob@example.com"}, CcAddrs: []string{"carol@example.com"}, Subject: "Draft subject"}
	body := message.Body{Raw: raw, PlainText: "Draft body.", Attachments: msgAtts}

	svc := NewService(nil, nil)
	got := svc.LoadDraft(msg, body)

	if len(got.To) != 1 || got.To[0] != "bob@example.com" {
		t.Errorf("To = %v", got.To)
	}
	if len(got.Cc) != 1 || got.Cc[0] != "carol@example.com" {
		t.Errorf("Cc = %v", got.Cc)
	}
	if len(got.Bcc) != 1 || got.Bcc[0] != "secret@example.com" {
		t.Errorf("Bcc = %v", got.Bcc)
	}
	if got.Subject != "Draft subject" || got.Body != "Draft body." {
		t.Errorf("Subject/Body = %q/%q", got.Subject, got.Body)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Filename != "notes.txt" || string(got.Attachments[0].Data) != "hello" {
		t.Errorf("Attachments = %+v", got.Attachments)
	}
}

func TestLoadDraftWithoutBccHeaderLeavesBccEmpty(t *testing.T) {
	raw, err := mime.BuildMessage(mime.Recipient{Addr: "me@example.com"}, nil, nil, nil, "Hi", "body", "", "", nil)
	if err != nil {
		t.Fatalf("mime.BuildMessage: %v", err)
	}
	svc := NewService(nil, nil)
	got := svc.LoadDraft(message.Message{Subject: "Hi"}, message.Body{Raw: raw, PlainText: "body"})
	if len(got.Bcc) != 0 {
		t.Fatalf("Bcc = %v, want none", got.Bcc)
	}
}

func TestSaveDraftAndSendDraftRoundTrip(t *testing.T) {
	// Save a draft, load it back the way a "resume this draft" flow would,
	// and check the reconstructed Draft matches what was saved — end to
	// end through SaveDraft's real IMAP APPEND rather than just BuildMessage.
	ctx := context.Background()
	fx := newDraftsFixture(t, map[string]string{"INBOX": `\Inbox`, "Drafts": `\Drafts`})

	draft := testDraft()
	draft.Attachments = []Attachment{{Filename: "a.txt", Data: []byte("x")}}
	ref, err := fx.svc.SaveDraft(ctx, fx.cfg, draft, DraftRef{})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	raw := fx.serverRaw(t, ref.FolderPath, ref.UID)
	atts, err := mime.Attachments(raw)
	if err != nil {
		t.Fatalf("mime.Attachments: %v", err)
	}
	msgAtts := make([]message.Attachment, len(atts))
	for i, a := range atts {
		msgAtts[i] = message.Attachment{Index: a.Index, Filename: a.Filename, ContentType: a.ContentType, Size: a.Size}
	}
	body := message.Body{Raw: raw, PlainText: "Draft body.", Attachments: msgAtts}
	msg := message.Message{UID: ref.UID, ToAddrs: draft.To, CcAddrs: draft.Cc, Subject: draft.Subject}

	got := fx.svc.LoadDraft(msg, body)
	if len(got.Bcc) != 1 || got.Bcc[0] != "secret@example.com" {
		t.Fatalf("Bcc = %v", got.Bcc)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Filename != "a.txt" {
		t.Fatalf("Attachments = %+v", got.Attachments)
	}
}
