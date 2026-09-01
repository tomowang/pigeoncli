// Package cli defines the pigeon command tree (cobra). Commands are kept
// thin: they parse flags/args and call into internal/core services, never
// internal/imap, internal/smtp, or internal/storage directly.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/core/compose"
	"github.com/tomowang/pigeoncli/internal/core/settings"
	"github.com/tomowang/pigeoncli/internal/logging"
	"github.com/tomowang/pigeoncli/internal/tui"
)

var (
	cfgPath     string
	dbPath      string
	blobDir     string
	logLevel    string
	logFilePath string

	closeLogging func() error
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
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			level, err := logging.ParseLevel(logLevel)
			if err != nil {
				return fmt.Errorf("invalid --log-level: %w", err)
			}
			path := logFilePath
			if path == "" {
				path, err = logging.DefaultPath()
				if err != nil {
					return err
				}
			}
			closeLogging, err = logging.Init(path, level)
			return err
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if closeLogging != nil {
				_ = closeLogging()
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			resolvedCfgPath, err := resolveConfigPath()
			if err != nil {
				return err
			}
			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			composeSvc := compose.NewService(st.Folder, st.Signature)
			settingsSvc := settings.NewService(resolvedCfgPath)

			return tui.Run(cmd.Context(), acctSvc, st.Folder, st.Message, composeSvc, settingsSvc)
		},
	}

	cmd.PersistentFlags().StringVar(&cfgPath, "config", "", "path to config file (default: XDG config dir)")
	cmd.PersistentFlags().StringVar(&dbPath, "db", "", "path to local sqlite cache (default: XDG cache dir)")
	cmd.PersistentFlags().StringVar(&blobDir, "blobs", "", "path to local message body cache (default: XDG cache dir)")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")
	cmd.PersistentFlags().StringVar(&logFilePath, "log-file", "", "path to log file (default: XDG cache dir)")

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newAccountCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newSignatureCmd())

	return cmd
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}
