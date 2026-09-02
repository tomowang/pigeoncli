package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newFolderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "folder",
		Short: "List synced folders",
	}
	cmd.AddCommand(newFolderListCmd())
	return cmd
}

func newFolderListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <slug>",
		Short: "List an account's synced folders",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			if _, err := acctSvc.Get(cmd.Context(), slug); err != nil {
				return err
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			folders, err := st.Folder.List(cmd.Context(), slug)
			if err != nil {
				return err
			}
			if len(folders) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No folders synced. Sync with `pigeon sync %s`.\n", slug)
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "PATH\tSPECIAL-USE\tTOTAL\tUNREAD")
			for _, f := range folders {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%d\n", f.Path, f.SpecialUse, f.TotalCount, f.UnreadCount)
			}
			return w.Flush()
		},
	}
}
