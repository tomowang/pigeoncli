// Package cli defines the pigeon command tree (cobra). Commands are kept
// thin: they parse flags/args and call into internal/core services, never
// internal/imap, internal/smtp, or internal/storage directly.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
	"github.com/tomowang/pigeoncli/internal/tui"
)

var (
	cfgPath  string
	dbPath   string
	blobDir  string
	logLevel string
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pigeon",
		Short: "pigeon is a local email client (TUI/CLI)",
		Long:  "pigeon is a local email client with IMAP/SMTP support, a terminal UI, and a CLI.",
		// main.go prints the returned error itself; don't let cobra print it
		// a second time or dump usage for runtime (non-flag-parsing) errors.
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			db, err := openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()

			blobs, err := newBlobStore()
			if err != nil {
				return err
			}

			return tui.Run(cmd.Context(), acctSvc, folder.NewService(db), message.NewService(db, blobs))
		},
	}

	cmd.PersistentFlags().StringVar(&cfgPath, "config", "", "path to config file (default: XDG config dir)")
	cmd.PersistentFlags().StringVar(&dbPath, "db", "", "path to local sqlite cache (default: XDG cache dir)")
	cmd.PersistentFlags().StringVar(&blobDir, "blobs", "", "path to local message body cache (default: XDG cache dir)")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newAccountCmd())
	cmd.AddCommand(newSyncCmd())

	return cmd
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}
