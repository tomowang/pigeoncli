package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/core/account"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

func resolveDBPath() (string, error) {
	if dbPath != "" {
		return dbPath, nil
	}
	return sqlite.DefaultPath()
}

func openDB(ctx context.Context) (*sqlite.DB, error) {
	path, err := resolveDBPath()
	if err != nil {
		return nil, err
	}
	return sqlite.Open(ctx, path)
}

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync [slug]",
		Short: "Sync mail accounts' folders and message headers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}

			var targets []account.Account
			if len(args) == 1 {
				a, err := acctSvc.Get(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				targets = []account.Account{a}
			} else {
				targets, err = acctSvc.List(cmd.Context())
				if err != nil {
					return err
				}
				if len(targets) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No accounts configured. Add one with `pigeon account add`.")
					return nil
				}
			}

			db, err := openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()

			folderSvc := folder.NewService(db)

			for _, a := range targets {
				fmt.Fprintf(cmd.OutOrStdout(), "Syncing %s...\n", a.Slug)
				ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
				err := folderSvc.Sync(ctx, a, func(p folder.Progress) {
					if p.Err != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s: error: %v\n", p.Path, p.Err)
						return
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %d messages (%d unread)\n", p.Path, p.TotalCount, p.UnreadCount)
				})
				cancel()
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "  account error: %v\n", err)
				}
			}
			return nil
		},
	}
}
