package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/buildinfo"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print pigeon's version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pigeon %s (commit %s, built %s)\n",
				buildinfo.Version, buildinfo.Commit, buildinfo.Date)
			return nil
		},
	}
}
