package cli

import (
	"context"
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

// defaultListFolder is the folder `message list`/`message show` operate on
// when no folder is given — the common case of checking the inbox.
const defaultListFolder = "INBOX"

func newMessageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "message",
		Short: "List and read synced messages",
	}
	cmd.AddCommand(newMessageListCmd())
	cmd.AddCommand(newMessageShowCmd())
	cmd.AddCommand(newMessageSpamCmd())
	cmd.AddCommand(newMessageUnspamCmd())
	return cmd
}

func newMessageListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <slug> [folder]",
		Short: "List cached messages in a synced folder",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			folderPath := defaultListFolder
			if len(args) == 2 {
				folderPath = args[1]
			}

			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			if _, err := acctSvc.Get(cmd.Context(), slug); err != nil {
				return err
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			msgs, err := st.Message.List(cmd.Context(), slug, folderPath)
			if err != nil {
				return err
			}
			if len(msgs) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No messages cached in %q. Sync with `pigeon sync %s`.\n", folderPath, slug)
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "UID\tFLAGS\tFROM\tSUBJECT\tDATE")
			for _, m := range msgs {
				from := m.FromAddr
				if m.FromName != "" {
					from = m.FromName
				}
				_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
					m.UID, formatFlags(m.Flags), from, m.Subject, m.Date.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

func newMessageShowCmd() *cobra.Command {
	var folderPath string
	cmd := &cobra.Command{
		Use:   "show <slug> <uid>",
		Short: "Show a cached message's headers and body",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			uid64, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid uid %q: %w", args[1], err)
			}
			uid := uint32(uid64)

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

			msgs, err := st.Message.List(cmd.Context(), slug, folderPath)
			if err != nil {
				return err
			}
			var hdr message.Message
			found := false
			for _, m := range msgs {
				if m.UID == uid {
					hdr = m
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("message %d not found in %q", uid, folderPath)
			}

			body, err := st.Message.Body(cmd.Context(), cfg, folderPath, uid)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "From:    %s <%s>\n", hdr.FromName, hdr.FromAddr)
			_, _ = fmt.Fprintf(out, "To:      %s\n", joinAddrs(hdr.ToAddrs))
			if len(hdr.CcAddrs) > 0 {
				_, _ = fmt.Fprintf(out, "Cc:      %s\n", joinAddrs(hdr.CcAddrs))
			}
			_, _ = fmt.Fprintf(out, "Subject: %s\n", hdr.Subject)
			_, _ = fmt.Fprintf(out, "Date:    %s\n", hdr.Date.Format("2006-01-02 15:04:05"))
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, body.PlainText)
			if len(body.Attachments) > 0 {
				_, _ = fmt.Fprintln(out)
				_, _ = fmt.Fprintln(out, "Attachments:")
				for _, a := range body.Attachments {
					_, _ = fmt.Fprintf(out, "  [%d] %s (%s, %d bytes)\n", a.Index, a.Filename, a.ContentType, a.Size)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the message lives in")
	return cmd
}

func newMessageSpamCmd() *cobra.Command {
	return newMessageMoveCmd("spam", "Report a message as spam, moving it to the account's Junk folder",
		func(ctx context.Context, svc *message.Service, cfg config.Account, folderPath string, uid uint32) error {
			return svc.MoveToJunk(ctx, cfg, folderPath, uid)
		})
}

func newMessageUnspamCmd() *cobra.Command {
	return newMessageMoveCmd("unspam", "Undo a spam report, moving a message back to the account's Inbox",
		func(ctx context.Context, svc *message.Service, cfg config.Account, folderPath string, uid uint32) error {
			return svc.MoveToInbox(ctx, cfg, folderPath, uid)
		})
}

// newMessageMoveCmd builds a `message <use> <slug> <uid>` command that moves
// one message via action — the shared shape of `spam`/`unspam`, which only
// differ in which core/message.Service method they call.
func newMessageMoveCmd(use, short string, action func(ctx context.Context, svc *message.Service, cfg config.Account, folderPath string, uid uint32) error) *cobra.Command {
	var folderPath string
	cmd := &cobra.Command{
		Use:   use + " <slug> <uid>",
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			uid64, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid uid %q: %w", args[1], err)
			}
			uid := uint32(uid64)

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

			if err := action(cmd.Context(), st.Message, cfg, folderPath, uid); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Message %d moved.\n", uid)
			return nil
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the message currently lives in")
	return cmd
}

func formatFlags(flags []string) string {
	seen := false
	for _, f := range flags {
		if f == `\Seen` {
			seen = true
			break
		}
	}
	if seen {
		return ""
	}
	return "unread"
}

func joinAddrs(addrs []string) string {
	out := ""
	for i, a := range addrs {
		if i > 0 {
			out += ", "
		}
		out += a
	}
	return out
}
