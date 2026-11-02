package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/guide"
)

// newGuideCmd prints what --help-extended prints, for anyone who looks for a
// command.
func newGuideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Print use cases with real commands, and where things go",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), guide.Extended())
			return err
		},
	}
}
