package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/core/signature"
)

func newSignatureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "signature",
		Short: "Manage saved signatures",
	}
	cmd.AddCommand(newSignatureAddCmd())
	cmd.AddCommand(newSignatureListCmd())
	cmd.AddCommand(newSignatureEditCmd())
	cmd.AddCommand(newSignatureRemoveCmd())
	cmd.AddCommand(newSignatureSetDefaultCmd())
	return cmd
}

func readBody(bodyFlag string) (string, error) {
	if bodyFlag != "" {
		return bodyFlag, nil
	}
	_, _ = fmt.Fprintln(os.Stderr, "Enter signature body, then press Ctrl+D:")
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read signature body: %w", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

func newSignatureAddCmd() *cobra.Command {
	var (
		account   string
		body      string
		isDefault bool
	)
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a new signature",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := readBody(body)
			if err != nil {
				return err
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			sig, err := st.Signature.Add(cmd.Context(), account, args[0], text, isDefault)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Signature %q added (id %d).\n", sig.Name, sig.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "scope to this account slug (default: global, used by any account without its own default)")
	cmd.Flags().StringVar(&body, "body", "", "signature body (default: read from stdin)")
	cmd.Flags().BoolVar(&isDefault, "default", false, "make this the default signature in its scope")
	return cmd
}

func newSignatureListCmd() *cobra.Command {
	var account string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved signatures",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			svc := st.Signature
			var sigs []signature.Signature
			if cmd.Flags().Changed("account") {
				sigs, err = svc.List(cmd.Context(), account)
			} else {
				sigs, err = svc.ListAll(cmd.Context())
			}
			if err != nil {
				return err
			}
			if len(sigs) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No signatures configured. Add one with `pigeon signature add`.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tACCOUNT\tNAME\tDEFAULT")
			for _, s := range sigs {
				scope := s.AccountSlug
				if scope == "" {
					scope = "(global)"
				}
				def := ""
				if s.IsDefault {
					def = "*"
				}
				_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", s.ID, scope, s.Name, def)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "list only this account's signatures plus global ones")
	return cmd
}

func newSignatureEditCmd() *cobra.Command {
	var name, body string
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit a signature's name and/or body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid signature id %q", args[0])
			}
			if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("body") {
				return fmt.Errorf("at least one of --name or --body is required")
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			svc := st.Signature
			sigs, err := svc.ListAll(cmd.Context())
			if err != nil {
				return err
			}
			var current *signature.Signature
			for i := range sigs {
				if sigs[i].ID == id {
					current = &sigs[i]
					break
				}
			}
			if current == nil {
				return fmt.Errorf("signature %d not found", id)
			}

			newName, newBody := current.Name, current.Body
			if cmd.Flags().Changed("name") {
				newName = name
			}
			if cmd.Flags().Changed("body") {
				newBody = body
			}
			if err := svc.Update(cmd.Context(), id, newName, newBody); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Signature %d updated.\n", id)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new name")
	cmd.Flags().StringVar(&body, "body", "", "new body")
	return cmd
}

func newSignatureRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <id>",
		Aliases: []string{"rm"},
		Short:   "Remove a signature",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid signature id %q", args[0])
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			if err := st.Signature.Remove(cmd.Context(), id); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Signature %d removed.\n", id)
			return nil
		},
	}
}

func newSignatureSetDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default <id>",
		Short: "Make a signature the default in its scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid signature id %q", args[0])
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			if err := st.Signature.SetDefault(cmd.Context(), id); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Signature %d is now the default.\n", id)
			return nil
		},
	}
}
