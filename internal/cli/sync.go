package cli

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/core/account"
	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/settings"
	"github.com/tomowang/pigeoncli/internal/storage/blob"
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

func newBlobStore() (*blob.Store, error) {
	dir := blobDir
	if dir == "" {
		var err error
		dir, err = blob.DefaultDir()
		if err != nil {
			return nil, err
		}
	}
	return blob.NewStore(dir), nil
}

func newSyncCmd() *cobra.Command {
	var full bool

	cmd := &cobra.Command{
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
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No accounts configured. Add one with `pigeon account add`.")
					return nil
				}
			}

			resolvedCfgPath, err := resolveConfigPath()
			if err != nil {
				return err
			}
			windowCount := 0
			if !full {
				s, err := settings.NewService(resolvedCfgPath).Get(cmd.Context())
				if err != nil {
					return err
				}
				windowCount = s.InitialSyncWindow
			}

			db, err := openDB(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			folderSvc := folder.NewService(db)

			for _, a := range targets {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Syncing %s...\n", a.Slug)
				slog.Info("cli sync start", "account", a.Slug)
				ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
				err := folderSvc.Sync(ctx, a, windowCount, func(p folder.Progress) {
					if p.Err != nil {
						slog.Error("cli sync folder failed", "account", a.Slug, "folder", p.Path, "err", p.Err)
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s: error: %v\n", p.Path, p.Err)
						return
					}
					slog.Info("cli sync folder complete", "account", a.Slug, "folder", p.Path, "total", p.TotalCount, "unread", p.UnreadCount)
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s: %d messages (%d unread)\n", p.Path, p.TotalCount, p.UnreadCount)
				})
				cancel()
				if err != nil {
					slog.Error("cli sync account failed", "account", a.Slug, "err", err)
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  account error: %v\n", err)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&full, "full", false, "sync full folder history on first sync, ignoring the configured initial sync window")
	return cmd
}
