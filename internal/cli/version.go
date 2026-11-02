package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd(l *loader) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the para version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return fmt.Errorf("version: load: %w", err)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), l.version)
			return err
		},
	}
}
