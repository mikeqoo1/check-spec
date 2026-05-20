package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mikeqoo1/check-spec/internal/version"
)

func (a *App) newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Run: func(_ *cobra.Command, _ []string) {
			_, _ = fmt.Fprintf(a.Stdout, "check-spec %s (commit %s, built %s)\n",
				version.Version, version.Commit, version.Date)
		},
	}
}
