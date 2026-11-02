package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/tui"
)

// newTUICmd is `para tui`, also what bare `para` runs. The TUI is for review
// and status: it reads and writes through ops with surface "tui" and never
// sends anything.
func newTUICmd(l *loader) *cobra.Command {
	return &cobra.Command{
		Use:   "tui [vault]",
		Short: "Review and status in the terminal (also: para with no arguments)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if l.vaultFlag != "" && l.vaultFlag != args[0] {
					return fmt.Errorf("tui: --vault %s and vault %s disagree", l.vaultFlag, args[0])
				}
				l.vaultFlag = args[0]
			}
			return runTUI(l)
		},
	}
}

func runTUI(l *loader) error {
	if err := l.load(); err != nil {
		return err
	}
	return tui.Run(tui.Options{Install: l.install, VaultFlag: l.vaultFlag, VaultEnv: l.env("PARA_VAULT")})
}
