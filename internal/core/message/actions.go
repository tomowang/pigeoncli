package message

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/imap"
)

// RFC 6154 special-use attributes for the folders Archive and Delete move
// messages into. Gmail has no \Archive folder — archiving there means
// moving to "All Mail" (\All), which drops the message's other labels.
const (
	archiveSpecialUse = `\Archive`
	allSpecialUse     = `\All`
	trashSpecialUse   = `\Trash`
)

var (
	// ErrNoArchiveFolder is returned by Archive when the account has no
	// synced folder tagged \Archive (or, for Gmail, \All).
	ErrNoArchiveFolder = errors.New("account has no Archive folder")

	// ErrNoTrashFolder is returned by Delete when the account has no synced
	// folder tagged \Trash. Callers can offer Purge instead.
	ErrNoTrashFolder = errors.New("account has no Trash folder")

	// ErrAlreadyInTrash is returned by Delete when every message is already
	// in the Trash. Delete never deletes permanently; callers escalate to
	// Purge (after confirming with the user).
	ErrAlreadyInTrash = errors.New("message is already in the Trash")

	// ErrPurgeUnsupported is returned by Purge when the server lacks UID
	// EXPUNGE (UIDPLUS), so the messages can't be deleted without also
	// expunging unrelated \Deleted messages.
	ErrPurgeUnsupported = errors.New("server can't permanently delete messages safely (no UIDPLUS)")

	// ErrNotUndoable is returned by Undo for a move whose new UID the
	// server didn't report (no UIDPLUS), so the message can't be found again.
	ErrNotUndoable = errors.New("move can't be undone: the server didn't report the message's new UID")
)

// Flag is a system IMAP flag that can be set on or cleared from a message.
type Flag string

// Flags that SetFlags accepts.
const (
	FlagSeen     Flag = `\Seen`
	FlagFlagged  Flag = `\Flagged`
	FlagAnswered Flag = `\Answered`
)

var supportedFlags = []Flag{FlagSeen, FlagFlagged, FlagAnswered}

// Has reports whether m carries flag f. IMAP flags compare
// case-insensitively.
func (m Message) Has(f Flag) bool {
	for _, x := range m.Flags {
		if strings.EqualFold(x, string(f)) {
			return true
		}
	}
	return false
}

// IsRead reports whether m has been read (\Seen).
func (m Message) IsRead() bool { return m.Has(FlagSeen) }

// IsStarred reports whether m is starred (\Flagged).
func (m Message) IsStarred() bool { return m.Has(FlagFlagged) }

// Ref identifies one cached message. Batch calls take []Ref and group them
// by folder internally, so a selection spanning folders (e.g. search
// results) works too.
type Ref struct {
	FolderPath string
	UID        uint32
}

// MoveResult records where one message went, so the move can be undone.
type MoveResult struct {
	Src  Ref
	Dest Ref // Dest.UID is 0 when the server didn't report the new UID
	// DestUIDValidity is the destination folder's UIDVALIDITY at move time,
	// so Undo can tell whether Dest.UID still means the same message.
	DestUIDValidity uint32
}

// perFolderTimeout is added to fetchTimeout for each folder a batch action
// touches, since each needs its own SELECT and command round trip.
const perFolderTimeout = 10 * time.Second

func batchTimeout(folders int) time.Duration {
	return fetchTimeout + time.Duration(folders)*perFolderTimeout
}

// withClient dials cfg's IMAP server (with the given overall timeout),
// runs fn on the connection, and closes it. It is the one place the
// dial → authenticate → watch-context dance lives.
func (s *Service) withClient(ctx context.Context, cfg config.Account, timeout time.Duration, fn func(ctx context.Context, cl *imap.Client) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dial := s.dial
	if dial == nil {
		dial = dialAccount
	}
	cl, err := dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = cl.Close() }()
	defer cl.WatchContext(ctx)()

	return fn(ctx, cl)
}

// dialAccount connects to cfg's IMAP server with credentials from its
// configured auth provider.
func dialAccount(ctx context.Context, cfg config.Account) (*imap.Client, error) {
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

// folderGroup is the subset of a batch that lives in one folder.
type folderGroup struct {
	path string
	id   int64
	uids []uint32 // ascending, no duplicates
}

// groupRefs resolves refs against the local cache and groups them by
// folder, in first-seen folder order. Every folder must already be synced.
func (s *Service) groupRefs(ctx context.Context, accountSlug string, refs []Ref) (accountID int64, groups []folderGroup, err error) {
	index := make(map[string]int)
	seen := make(map[Ref]struct{}, len(refs))
	for _, r := range refs {
		if r.UID == 0 {
			return 0, nil, fmt.Errorf("invalid message UID 0 in %q", r.FolderPath)
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}

		i, ok := index[r.FolderPath]
		if !ok {
			acc, folderID, err := s.resolveIDs(ctx, accountSlug, r.FolderPath)
			if err != nil {
				return 0, nil, err
			}
			accountID = acc
			groups = append(groups, folderGroup{path: r.FolderPath, id: folderID})
			i = len(groups) - 1
			index[r.FolderPath] = i
		}
		groups[i].uids = append(groups[i].uids, r.UID)
	}
	for i := range groups {
		slices.Sort(groups[i].uids)
	}
	return accountID, groups, nil
}

// specialUsePath returns the path of the first of accountID's synced
// folders carrying one of the special-use attributes in uses, or
// errNoFolder if there is none.
func (s *Service) specialUsePath(ctx context.Context, accountID int64, uses []string, errNoFolder error) (string, error) {
	for _, use := range uses {
		path, ok, err := s.db.FolderBySpecialUse(ctx, accountID, use)
		if err != nil {
			return "", err
		}
		if ok {
			return path, nil
		}
	}
	return "", errNoFolder
}

// Move moves the messages in refs to the folder at destPath (which must be
// a synced folder). Messages already in destPath are skipped. The returned
// results cover every message that moved, even when err is non-nil because
// some other folder's messages failed.
//
// After a successful server-side move the source rows leave the local cache
// and folder counts are refreshed; the destination folder picks the
// messages up (under their new UIDs) on its next sync.
func (s *Service) Move(ctx context.Context, cfg config.Account, refs []Ref, destPath string) ([]MoveResult, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	accountID, groups, err := s.groupRefs(ctx, cfg.Slug, refs)
	if err != nil {
		return nil, err
	}
	_, ok, err := s.db.FolderID(ctx, accountID, destPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("folder %q has not been synced yet", destPath)
	}
	return s.moveGroups(ctx, cfg, groups, destPath, fmt.Errorf("message is already in %q", destPath))
}

// Archive moves the messages in refs out of their folder into the
// account's Archive folder (\Archive), or All Mail (\All) on Gmail.
func (s *Service) Archive(ctx context.Context, cfg config.Account, refs []Ref) ([]MoveResult, error) {
	return s.moveToSpecialUse(ctx, cfg, refs, []string{archiveSpecialUse, allSpecialUse},
		ErrNoArchiveFolder, errors.New("message is already archived"))
}

// Delete moves the messages in refs to the account's Trash folder. It never
// deletes permanently: messages already in the Trash are skipped, and if
// there's nothing else to do it returns ErrAlreadyInTrash so callers can
// confirm with the user and call Purge.
func (s *Service) Delete(ctx context.Context, cfg config.Account, refs []Ref) ([]MoveResult, error) {
	return s.moveToSpecialUse(ctx, cfg, refs, []string{trashSpecialUse}, ErrNoTrashFolder, ErrAlreadyInTrash)
}

// Undo reverses moves returned by Move, Archive, or Delete, putting each
// message back in the folder it came from. The moved messages get fresh
// UIDs there, and show up in the local cache on that folder's next sync.
// A move whose destination UID is unknown yields ErrNotUndoable; the rest
// are still undone.
func (s *Service) Undo(ctx context.Context, cfg config.Account, moved []MoveResult) error {
	type key struct {
		now, back string // folder the message is in now, and the one it came from
		validity  uint32
	}
	var (
		order       []key
		uids        = make(map[key][]uint32)
		errs        []error
		notUndoable bool
	)
	for _, m := range moved {
		if m.Dest.UID == 0 {
			notUndoable = true
			continue
		}
		k := key{now: m.Dest.FolderPath, back: m.Src.FolderPath, validity: m.DestUIDValidity}
		if _, ok := uids[k]; !ok {
			order = append(order, k)
		}
		uids[k] = append(uids[k], m.Dest.UID)
	}
	if notUndoable {
		errs = append(errs, ErrNotUndoable)
	}
	if len(order) == 0 {
		return errors.Join(errs...)
	}

	groups := make([]folderGroup, len(order))
	for i, k := range order {
		_, folderID, err := s.resolveIDs(ctx, cfg.Slug, k.now)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		groups[i] = folderGroup{path: k.now, id: folderID, uids: uids[k]}
	}

	err := s.withClient(ctx, cfg, batchTimeout(len(groups)), func(ctx context.Context, cl *imap.Client) error {
		for i, g := range groups {
			slices.Sort(g.uids)
			if _, err := s.moveGroup(ctx, cl, g, order[i].back, order[i].validity); err != nil {
				errs = append(errs, err)
			}
		}
		return nil
	})
	return errors.Join(append(errs, err)...)
}

// Purge permanently deletes the messages in refs from the server (STORE
// \Deleted + UID EXPUNGE) and the local cache. It cannot be undone; callers
// should confirm first. Servers without UIDPLUS yield ErrPurgeUnsupported.
func (s *Service) Purge(ctx context.Context, cfg config.Account, refs []Ref) error {
	if len(refs) == 0 {
		return nil
	}
	_, groups, err := s.groupRefs(ctx, cfg.Slug, refs)
	if err != nil {
		return err
	}
	return s.eachGroup(ctx, cfg, groups, func(ctx context.Context, cl *imap.Client, g folderGroup) error {
		if err := cl.Purge(ctx, g.uids); err != nil {
			if errors.Is(err, imap.ErrUIDPlusRequired) {
				return ErrPurgeUnsupported
			}
			return err
		}
		return s.dropFromCache(ctx, g)
	})
}

// SetFlags adds and removes flags on the messages in refs, on the server and
// in the local cache, and refreshes the affected folders' unread counts.
// Only the flags listed in the Flag constants are accepted.
func (s *Service) SetFlags(ctx context.Context, cfg config.Account, refs []Ref, add, remove []Flag) error {
	if len(refs) == 0 || len(add)+len(remove) == 0 {
		return nil
	}
	for _, f := range slices.Concat(add, remove) {
		if !slices.Contains(supportedFlags, f) {
			return fmt.Errorf("unsupported flag %q", f)
		}
	}
	for _, f := range add {
		if slices.Contains(remove, f) {
			return fmt.Errorf("flag %q is both added and removed", f)
		}
	}

	_, groups, err := s.groupRefs(ctx, cfg.Slug, refs)
	if err != nil {
		return err
	}
	addS, removeS := flagStrings(add), flagStrings(remove)
	return s.eachGroup(ctx, cfg, groups, func(ctx context.Context, cl *imap.Client, g folderGroup) error {
		if err := cl.StoreFlags(ctx, g.uids, addS, removeS); err != nil {
			return err
		}
		if err := s.db.ApplyFlagChanges(ctx, g.id, g.uids, addS, removeS); err != nil {
			return fmt.Errorf("update local cache: %w", err)
		}
		if err := s.db.RefreshFolderCounts(ctx, g.id); err != nil {
			return fmt.Errorf("update local cache: %w", err)
		}
		return nil
	})
}

// MarkRead marks the messages in refs as read.
func (s *Service) MarkRead(ctx context.Context, cfg config.Account, refs []Ref) error {
	return s.SetFlags(ctx, cfg, refs, []Flag{FlagSeen}, nil)
}

// MarkUnread marks the messages in refs as unread.
func (s *Service) MarkUnread(ctx context.Context, cfg config.Account, refs []Ref) error {
	return s.SetFlags(ctx, cfg, refs, nil, []Flag{FlagSeen})
}

// Star stars (\Flagged) the messages in refs.
func (s *Service) Star(ctx context.Context, cfg config.Account, refs []Ref) error {
	return s.SetFlags(ctx, cfg, refs, []Flag{FlagFlagged}, nil)
}

// Unstar clears the star from the messages in refs.
func (s *Service) Unstar(ctx context.Context, cfg config.Account, refs []Ref) error {
	return s.SetFlags(ctx, cfg, refs, nil, []Flag{FlagFlagged})
}

func flagStrings(flags []Flag) []string {
	out := make([]string, len(flags))
	for i, f := range flags {
		out[i] = string(f)
	}
	return out
}

// moveToSpecialUse resolves the destination as the account's folder
// carrying the first matching special-use attribute in uses, then moves
// refs there. It returns errNoFolder if the account has none, and
// errAlready if everything in refs is already in the destination.
func (s *Service) moveToSpecialUse(ctx context.Context, cfg config.Account, refs []Ref, uses []string, errNoFolder, errAlready error) ([]MoveResult, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	accountID, groups, err := s.groupRefs(ctx, cfg.Slug, refs)
	if err != nil {
		return nil, err
	}
	destPath, err := s.specialUsePath(ctx, accountID, uses, errNoFolder)
	if err != nil {
		return nil, err
	}
	return s.moveGroups(ctx, cfg, groups, destPath, errAlready)
}

// moveGroups moves each group to destPath over one connection, skipping
// groups already in destPath (and returning errAlready if that's all of
// them). A failing group doesn't stop the others; results include every
// message that did move, alongside the joined errors.
func (s *Service) moveGroups(ctx context.Context, cfg config.Account, groups []folderGroup, destPath string, errAlready error) ([]MoveResult, error) {
	todo := make([]folderGroup, 0, len(groups))
	for _, g := range groups {
		if g.path != destPath {
			todo = append(todo, g)
		}
	}
	if len(todo) == 0 {
		return nil, errAlready
	}

	var (
		results []MoveResult
		errs    []error
	)
	err := s.withClient(ctx, cfg, batchTimeout(len(todo)), func(ctx context.Context, cl *imap.Client) error {
		for _, g := range todo {
			moved, err := s.moveGroup(ctx, cl, g, destPath, 0)
			results = append(results, moved...)
			if err != nil {
				errs = append(errs, err)
			}
		}
		return nil
	})
	return results, errors.Join(append(errs, err)...)
}

// moveGroup moves g's messages to destPath on cl and drops them from the
// local cache. If wantValidity is non-zero, it first checks that g's folder
// still has that UIDVALIDITY — Undo uses this so a UID recorded at move
// time isn't applied to a folder that has since been renumbered. It returns
// the moved messages even when only the cache update failed.
func (s *Service) moveGroup(ctx context.Context, cl *imap.Client, g folderGroup, destPath string, wantValidity uint32) ([]MoveResult, error) {
	validity, _, _, _, err := cl.SelectFolder(ctx, g.path)
	if err != nil {
		return nil, err
	}
	if wantValidity != 0 && validity != wantValidity {
		return nil, fmt.Errorf("%q was renumbered by the server since the move, so it can't be undone", g.path)
	}
	res, err := cl.MoveToFolder(ctx, g.uids, destPath)
	if err != nil {
		return nil, err
	}

	moved := make([]MoveResult, len(g.uids))
	for i, uid := range g.uids {
		moved[i] = MoveResult{
			Src:             Ref{FolderPath: g.path, UID: uid},
			Dest:            Ref{FolderPath: destPath, UID: res.Dest[uid]},
			DestUIDValidity: res.UIDValidity,
		}
	}
	return moved, s.dropFromCache(ctx, g)
}

// dropFromCache removes g's messages from the local cache after they left
// the server folder (moved or purged) and refreshes the folder's counts.
func (s *Service) dropFromCache(ctx context.Context, g folderGroup) error {
	if err := s.db.DeleteMessages(ctx, g.id, g.uids); err != nil {
		return fmt.Errorf("update local cache: %w", err)
	}
	if err := s.db.RefreshFolderCounts(ctx, g.id); err != nil {
		return fmt.Errorf("update local cache: %w", err)
	}
	return nil
}

// eachGroup runs fn for every group over one connection, selecting each
// group's folder first. A failing group doesn't stop the others; their
// errors are joined.
func (s *Service) eachGroup(ctx context.Context, cfg config.Account, groups []folderGroup, fn func(ctx context.Context, cl *imap.Client, g folderGroup) error) error {
	var errs []error
	err := s.withClient(ctx, cfg, batchTimeout(len(groups)), func(ctx context.Context, cl *imap.Client) error {
		for _, g := range groups {
			if _, _, _, _, err := cl.SelectFolder(ctx, g.path); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := fn(ctx, cl, g); err != nil {
				errs = append(errs, err)
			}
		}
		return nil
	})
	return errors.Join(append(errs, err)...)
}
