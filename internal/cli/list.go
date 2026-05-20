package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mikeqoo1/check-spec/internal/arceus"
)

func (a *App) newListCmd() *cobra.Command {
	var repoPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List change ids under .arceus/changes/ (debug helper)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ids, err := arceus.List(repoPath)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				_, _ = fmt.Fprintf(a.Stdout, "(no changes under %s/.arceus/changes/)\n", repoPath)
				return nil
			}
			for _, id := range ids {
				_, _ = fmt.Fprintln(a.Stdout, id)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repoPath, "repo", ".", "path to the repository root")
	return cmd
}
