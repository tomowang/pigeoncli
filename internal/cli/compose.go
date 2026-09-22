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
	cmd.AddCommand(newComposeForwardCmd())
	return cmd
}

func newComposeSendCmd() *cobra.Command {
	var to, cc, bcc, attach []string
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
			attachments, err := loadAttachments(attach)
			if err != nil {
				return err
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
			draft.Bcc = bcc
			draft.Subject = subject
			draft.Body = joinBody(bodyText, draft.Body)
			draft.Attachments = attachments

			if err := composeSvc.Send(cmd.Context(), cfg, draft); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Message sent to %s.\n", joinAddrs(to))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&to, "to", nil, "recipient address (repeatable)")
	cmd.Flags().StringArrayVar(&cc, "cc", nil, "cc address (repeatable)")
	cmd.Flags().StringArrayVar(&bcc, "bcc", nil, "bcc address, hidden from every other recipient (repeatable)")
	cmd.Flags().StringVar(&subject, "subject", "", "message subject")
	cmd.Flags().StringVar(&body, "body", "", "message body (default: read from stdin)")
	cmd.Flags().StringArrayVar(&attach, "attach", nil, "path to a file to attach (repeatable)")
	return cmd
}

func newComposeReplyCmd() *cobra.Command {
	var folderPath, subject, body string
	var replyAll bool
	var attach, bcc []string
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
			attachments, err := loadAttachments(attach)
			if err != nil {
				return err
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
			draft.Attachments = attachments
			draft.Bcc = bcc

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
	cmd.Flags().StringArrayVar(&attach, "attach", nil, "path to a file to attach (repeatable)")
	cmd.Flags().StringArrayVar(&bcc, "bcc", nil, "bcc address, hidden from every other recipient (repeatable)")
	return cmd
}

func newComposeForwardCmd() *cobra.Command {
	var folderPath, subject, body string
	var to, cc, bcc, attach []string
	cmd := &cobra.Command{
		Use:   "forward <slug> <uid>",
		Short: "Forward a cached message",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			uid64, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid uid %q: %w", args[1], err)
			}
			uid := uint32(uid64)
			if len(to) == 0 {
				return fmt.Errorf("at least one --to recipient is required")
			}
			attachments, err := loadAttachments(attach)
			if err != nil {
				return err
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
			draft := composeSvc.NewForward(cmd.Context(), cfg, orig, origBody)
			draft.To = to
			draft.Cc = cc
			draft.Bcc = bcc
			if subject != "" {
				draft.Subject = subject
			}
			draft.Body = joinBody(bodyText, draft.Body)
			// The forwarded attachments NewForward already carried over come
			// first; --attach adds more on top rather than replacing them.
			draft.Attachments = append(draft.Attachments, attachments...)

			if err := composeSvc.Send(cmd.Context(), cfg, draft); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Forwarded to %s.\n", joinAddrs(to))
			return nil
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the message lives in")
	cmd.Flags().StringArrayVar(&to, "to", nil, "recipient address (repeatable)")
	cmd.Flags().StringArrayVar(&cc, "cc", nil, "cc address (repeatable)")
	cmd.Flags().StringArrayVar(&bcc, "bcc", nil, "bcc address, hidden from every other recipient (repeatable)")
	cmd.Flags().StringVar(&subject, "subject", "", "override the default \"Fwd: ...\" subject")
	cmd.Flags().StringVar(&body, "body", "", "text to add above the forwarded message (default: read from stdin)")
	cmd.Flags().StringArrayVar(&attach, "attach", nil, "path to an extra file to attach, in addition to the original's own (repeatable)")
	return cmd
}

// loadAttachments reads each path in paths into a compose.Attachment.
func loadAttachments(paths []string) ([]compose.Attachment, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]compose.Attachment, len(paths))
	for i, p := range paths {
		att, err := compose.NewAttachmentFromFile(p)
		if err != nil {
			return nil, fmt.Errorf("attach %s: %w", p, err)
		}
		out[i] = att
	}
	return out, nil
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
