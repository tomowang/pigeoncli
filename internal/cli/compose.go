package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/core/compose"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

func newComposeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Compose and send mail",
	}
	cmd.AddCommand(newComposeSendCmd())
	cmd.AddCommand(newComposeReplyCmd())
	return cmd
}

func newComposeSendCmd() *cobra.Command {
	var to, cc []string
	var subject, body string
	cmd := &cobra.Command{
		Use:   "send <slug>",
		Short: "Compose and send a new message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			if len(to) == 0 {
				return fmt.Errorf("at least one --to recipient is required")
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

			bodyText := body
			if bodyText == "" {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read body from stdin: %w", err)
				}
				bodyText = string(b)
			}

			composeSvc := compose.NewService(st.Folder, st.Signature)
			draft := composeSvc.NewMessage(cmd.Context(), cfg)
			draft.To = to
			draft.Cc = cc
			draft.Subject = subject
			draft.Body = joinBody(bodyText, draft.Body)

			if err := composeSvc.Send(cmd.Context(), cfg, draft); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Message sent to %s.\n", joinAddrs(to))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&to, "to", nil, "recipient address (repeatable)")
	cmd.Flags().StringArrayVar(&cc, "cc", nil, "cc address (repeatable)")
	cmd.Flags().StringVar(&subject, "subject", "", "message subject")
	cmd.Flags().StringVar(&body, "body", "", "message body (default: read from stdin)")
	return cmd
}

func newComposeReplyCmd() *cobra.Command {
	var folderPath, subject, body string
	var replyAll bool
	cmd := &cobra.Command{
		Use:   "reply <slug> <uid>",
		Short: "Reply to a cached message",
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
			var orig message.Message
			found := false
			for _, m := range msgs {
				if m.UID == uid {
					orig = m
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("message %d not found in %q", uid, folderPath)
			}

			origBody, err := st.Message.Body(cmd.Context(), cfg, folderPath, uid)
			if err != nil {
				return err
			}

			bodyText := body
			if bodyText == "" {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read body from stdin: %w", err)
				}
				bodyText = string(b)
			}

			composeSvc := compose.NewService(st.Folder, st.Signature)
			draft := composeSvc.NewReply(cmd.Context(), cfg, orig, origBody, replyAll)
			if subject != "" {
				draft.Subject = subject
			}
			draft.Body = joinBody(bodyText, draft.Body)

			if err := composeSvc.Send(cmd.Context(), cfg, draft); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Reply sent to %s.\n", joinAddrs(draft.To))
			return nil
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the message lives in")
	cmd.Flags().BoolVar(&replyAll, "all", false, "reply to all original recipients, not just the sender")
	cmd.Flags().StringVar(&subject, "subject", "", "override the default \"Re: ...\" subject")
	cmd.Flags().StringVar(&body, "body", "", "text to add above the quoted original (default: read from stdin)")
	return cmd
}

// joinBody combines a user-supplied body with the account's default
// signature (already formatted as "-- \n<sig>\n" by core/compose, or empty
// if none is configured).
func joinBody(text, signature string) string {
	if signature == "" {
		return text
	}
	if text == "" {
		return signature
	}
	return text + "\n" + signature
}
