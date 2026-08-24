// Package cli defines the pigeon command tree (cobra). Commands are kept
// thin: they parse flags/args and call into internal/core services, never
// internal/imap, internal/smtp, or internal/storage directly.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/tui"
)

var (
	cfgPath  string
	logLevel string
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pigeon",
		Short: "pigeon is a local email client (TUI/CLI)",
		Long:  "pigeon is a local email client with IMAP/SMTP support, a terminal UI, and a CLI.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run(cmd.Context())
		},
	}

	cmd.PersistentFlags().StringVar(&cfgPath, "config", "", "path to config file (default: XDG config dir)")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	cmd.AddCommand(newVersionCmd())

	return cmd
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}
