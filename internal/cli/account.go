package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/account"
)

func resolveConfigPath() (string, error) {
	if cfgPath != "" {
		return cfgPath, nil
	}
	return config.DefaultPath()
}

func newAccountService() (*account.Service, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}
	return account.NewService(path), nil
}

func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(pw), nil
}

func validateAccount(a account.Account) error {
	if a.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if a.Email == "" {
		return fmt.Errorf("--email is required")
	}
	if a.IMAP.Host == "" {
		return fmt.Errorf("--imap-host is required")
	}
	if a.SMTP.Host == "" {
		return fmt.Errorf("--smtp-host is required")
	}
	if !a.IMAP.TLS.Valid() {
		return fmt.Errorf("invalid --imap-tls %q (want tls, starttls, or none)", a.IMAP.TLS)
	}
	if !a.SMTP.TLS.Valid() {
		return fmt.Errorf("invalid --smtp-tls %q (want tls, starttls, or none)", a.SMTP.TLS)
	}
	return nil
}

func newAccountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Manage mail accounts",
	}
	cmd.AddCommand(newAccountAddCmd())
	cmd.AddCommand(newAccountListCmd())
	cmd.AddCommand(newAccountEditCmd())
	cmd.AddCommand(newAccountRemoveCmd())
	cmd.AddCommand(newAccountTestCmd())
	return cmd
}

// serverFlags holds the IMAP/SMTP flag variables shared by `account add`
// and `account edit`.
type serverFlags struct {
	email, displayName, username string
	imapHost                     string
	imapPort                     int
	imapTLS                      string
	smtpHost                     string
	smtpPort                     int
	smtpTLS                      string
}

func (f *serverFlags) register(cmd *cobra.Command, defaultPorts bool) {
	cmd.Flags().StringVar(&f.email, "email", "", "account email address")
	cmd.Flags().StringVar(&f.displayName, "display-name", "", "display name for outgoing mail")
	cmd.Flags().StringVar(&f.username, "username", "", "login username (default: email)")
	cmd.Flags().StringVar(&f.imapHost, "imap-host", "", "IMAP server host")
	cmd.Flags().StringVar(&f.smtpHost, "smtp-host", "", "SMTP server host")
	cmd.Flags().StringVar(&f.imapTLS, "imap-tls", "", "IMAP TLS mode: tls, starttls, or none")
	cmd.Flags().StringVar(&f.smtpTLS, "smtp-tls", "", "SMTP TLS mode: tls, starttls, or none")
	if defaultPorts {
		cmd.Flags().IntVar(&f.imapPort, "imap-port", 993, "IMAP server port")
		cmd.Flags().IntVar(&f.smtpPort, "smtp-port", 587, "SMTP server port")
		f.imapTLS = string(account.TLSModeTLS)
		f.smtpTLS = string(account.TLSModeSTARTTLS)
	} else {
		cmd.Flags().IntVar(&f.imapPort, "imap-port", 0, "IMAP server port")
		cmd.Flags().IntVar(&f.smtpPort, "smtp-port", 0, "SMTP server port")
	}
}

func newAccountAddCmd() *cobra.Command {
	var f serverFlags
	cmd := &cobra.Command{
		Use:   "add <slug>",
		Short: "Add a new mail account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			username := f.username
			if username == "" {
				username = f.email
			}

			a := account.Account{
				Slug:        slug,
				Email:       f.email,
				DisplayName: f.displayName,
				Username:    username,
				AuthType:    "password",
				IMAP:        account.ServerConfig{Host: f.imapHost, Port: f.imapPort, TLS: account.TLSMode(f.imapTLS)},
				SMTP:        account.ServerConfig{Host: f.smtpHost, Port: f.smtpPort, TLS: account.TLSMode(f.smtpTLS)},
			}
			if err := validateAccount(a); err != nil {
				return err
			}

			password, err := promptPassword(fmt.Sprintf("Password for %s: ", slug))
			if err != nil {
				return err
			}

			svc, err := newAccountService()
			if err != nil {
				return err
			}
			if err := svc.Add(cmd.Context(), a, password); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Account %q added.\n", slug)
			return nil
		},
	}
	f.register(cmd, true)
	cmd.MarkFlagRequired("email")
	cmd.MarkFlagRequired("imap-host")
	cmd.MarkFlagRequired("smtp-host")
	return cmd
}

func newAccountEditCmd() *cobra.Command {
	var f serverFlags
	var resetPassword bool
	cmd := &cobra.Command{
		Use:   "edit <slug>",
		Short: "Edit an existing mail account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			svc, err := newAccountService()
			if err != nil {
				return err
			}
			a, err := svc.Get(cmd.Context(), slug)
			if err != nil {
				return err
			}

			if cmd.Flags().Changed("email") {
				a.Email = f.email
			}
			if cmd.Flags().Changed("display-name") {
				a.DisplayName = f.displayName
			}
			if cmd.Flags().Changed("username") {
				a.Username = f.username
			}
			if cmd.Flags().Changed("imap-host") {
				a.IMAP.Host = f.imapHost
			}
			if cmd.Flags().Changed("imap-port") {
				a.IMAP.Port = f.imapPort
			}
			if cmd.Flags().Changed("imap-tls") {
				a.IMAP.TLS = account.TLSMode(f.imapTLS)
			}
			if cmd.Flags().Changed("smtp-host") {
				a.SMTP.Host = f.smtpHost
			}
			if cmd.Flags().Changed("smtp-port") {
				a.SMTP.Port = f.smtpPort
			}
			if cmd.Flags().Changed("smtp-tls") {
				a.SMTP.TLS = account.TLSMode(f.smtpTLS)
			}

			if err := validateAccount(a); err != nil {
				return err
			}

			var password *string
			if resetPassword {
				pw, err := promptPassword(fmt.Sprintf("New password for %s: ", slug))
				if err != nil {
					return err
				}
				password = &pw
			}

			if err := svc.Update(cmd.Context(), a, password); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Account %q updated.\n", slug)
			return nil
		},
	}
	f.register(cmd, false)
	cmd.Flags().BoolVar(&resetPassword, "password", false, "prompt for a new password")
	return cmd
}

func newAccountListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured accounts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newAccountService()
			if err != nil {
				return err
			}
			accounts, err := svc.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(accounts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No accounts configured. Add one with `pigeon account add`.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tEMAIL\tIMAP\tSMTP")
			for _, a := range accounts {
				fmt.Fprintf(w, "%s\t%s\t%s:%d (%s)\t%s:%d (%s)\n",
					a.Slug, a.Email, a.IMAP.Host, a.IMAP.Port, a.IMAP.TLS, a.SMTP.Host, a.SMTP.Port, a.SMTP.TLS)
			}
			return w.Flush()
		},
	}
}

func newAccountRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <slug>",
		Aliases: []string{"rm"},
		Short:   "Remove a mail account",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newAccountService()
			if err != nil {
				return err
			}
			if err := svc.Remove(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Account %q removed.\n", args[0])
			return nil
		},
	}
}

func newAccountTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <slug>",
		Short: "Test IMAP and SMTP connectivity for an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newAccountService()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()
			if err := svc.TestConnection(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Account %q: IMAP and SMTP login succeeded.\n", args[0])
			return nil
		},
	}
}
