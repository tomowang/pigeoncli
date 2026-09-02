package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/core/compose"
)

func newComposeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Compose and send mail",
	}
	cmd.AddCommand(newComposeSendCmd())
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
