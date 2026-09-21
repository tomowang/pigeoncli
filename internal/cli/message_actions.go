package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

// newMessageActionCmds returns the triage commands that act on one or more
// cached messages by UID: archive, delete, move, and the flag toggles.
func newMessageActionCmds() []*cobra.Command {
	return []*cobra.Command{
		newMessageArchiveCmd(),
		newMessageDeleteCmd(),
		newMessageMoveDestCmd(),
		newMessageFlagCmd("read", "Mark messages as read", (*message.Service).MarkRead, "marked as read"),
		newMessageFlagCmd("unread", "Mark messages as unread", (*message.Service).MarkUnread, "marked as unread"),
		newMessageFlagCmd("star", "Star messages", (*message.Service).Star, "starred"),
		newMessageFlagCmd("unstar", "Remove the star from messages", (*message.Service).Unstar, "unstarred"),
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func parseUIDs(args []string) ([]uint32, error) {
	uids := make([]uint32, 0, len(args))
	for _, a := range args {
		u, err := strconv.ParseUint(a, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid uid %q: %w", a, err)
		}
		uids = append(uids, uint32(u))
	}
	return uids, nil
}

// runOnMessages resolves slug's account, opens the store, and runs action
// on refs built from uidArgs in folderPath.
func runOnMessages(cmd *cobra.Command, slug string, uidArgs []string, folderPath string,
	action func(ctx context.Context, svc *message.Service, cfg config.Account, refs []message.Ref) error) error {
	uids, err := parseUIDs(uidArgs)
	if err != nil {
		return err
	}
	refs := make([]message.Ref, len(uids))
	for i, u := range uids {
		refs[i] = message.Ref{FolderPath: folderPath, UID: u}
	}

	acctSvc, err := newAccountService()
	if err != nil {
		return err
	}
	cfg, err := acctSvc.Get(cmd.Context(), slug)
	if err != nil {
		return err
	}

	st, err := openStore(cmd.Context())
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	return action(cmd.Context(), st.Message, cfg, refs)
}

func newMessageArchiveCmd() *cobra.Command {
	var folderPath string
	cmd := &cobra.Command{
		Use:   "archive <slug> <uid>...",
		Short: "Archive messages (move them to the account's Archive folder)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnMessages(cmd, args[0], args[1:], folderPath,
				func(ctx context.Context, svc *message.Service, cfg config.Account, refs []message.Ref) error {
					moved, err := svc.Archive(ctx, cfg, refs)
					reportMoved(cmd, moved, "archived")
					return err
				})
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the messages currently live in")
	return cmd
}

func newMessageDeleteCmd() *cobra.Command {
	var (
		folderPath string
		permanent  bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "delete <slug> <uid>...",
		Short: "Delete messages (move to Trash, or permanently with --permanent)",
		Long: "Delete messages by moving them to the account's Trash folder.\n\n" +
			"Messages already in the Trash can be deleted for good with --permanent, " +
			"which can't be undone and asks for confirmation unless --yes is given.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnMessages(cmd, args[0], args[1:], folderPath,
				func(ctx context.Context, svc *message.Service, cfg config.Account, refs []message.Ref) error {
					if !permanent {
						moved, err := svc.Delete(ctx, cfg, refs)
						reportMoved(cmd, moved, "moved to Trash")
						if errors.Is(err, message.ErrAlreadyInTrash) {
							return fmt.Errorf("%w; use --permanent to delete for good", err)
						}
						return err
					}
					if !yes {
						if !confirm(cmd, fmt.Sprintf("Permanently delete %s? This can't be undone. [y/N] ", plural(len(refs), "message"))) {
							_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
							return nil
						}
					}
					if err := svc.Purge(ctx, cfg, refs); err != nil {
						return err
					}
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Permanently deleted %s.\n", plural(len(refs), "message"))
					return nil
				})
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the messages currently live in")
	cmd.Flags().BoolVar(&permanent, "permanent", false, "delete permanently instead of moving to Trash")
	cmd.Flags().BoolVar(&yes, "yes", false, "don't ask for confirmation before a permanent delete")
	return cmd
}

func newMessageMoveDestCmd() *cobra.Command {
	var folderPath string
	cmd := &cobra.Command{
		Use:   "move <slug> <dest-folder> <uid>...",
		Short: "Move messages to another synced folder",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := args[1]
			return runOnMessages(cmd, args[0], args[2:], folderPath,
				func(ctx context.Context, svc *message.Service, cfg config.Account, refs []message.Ref) error {
					moved, err := svc.Move(ctx, cfg, refs, dest)
					reportMoved(cmd, moved, "moved to "+dest)
					return err
				})
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the messages currently live in")
	return cmd
}

func newMessageFlagCmd(use, short string,
	action func(*message.Service, context.Context, config.Account, []message.Ref) error, done string) *cobra.Command {
	var folderPath string
	cmd := &cobra.Command{
		Use:   use + " <slug> <uid>...",
		Short: short,
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnMessages(cmd, args[0], args[1:], folderPath,
				func(ctx context.Context, svc *message.Service, cfg config.Account, refs []message.Ref) error {
					if err := action(svc, ctx, cfg, refs); err != nil {
						return err
					}
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s.\n", plural(len(refs), "message"), done)
					return nil
				})
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the messages live in")
	return cmd
}

// reportMoved prints how many messages moved, e.g. "3 messages archived.".
// It's called even when the action also returned an error, since a batch
// can partly succeed.
func reportMoved(cmd *cobra.Command, moved []message.MoveResult, outcome string) {
	if len(moved) == 0 {
		return
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s.\n", plural(len(moved), "message"), outcome)
}

// confirm asks a yes/no question on the command's stdin. Anything but an
// explicit yes (including EOF) counts as no.
func confirm(cmd *cobra.Command, prompt string) bool {
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), prompt)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
