package cli

import (
	"github.com/spf13/cobra"

	"github.com/iodesystems/paradize/internal/tui"
)

// newTUICmd is `para tui`, also what bare `para` runs. The TUI is for review
// and status: it reads and writes through ops with surface "tui" and never
// sends anything.
func newTUICmd(l *loader) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Review and status in the terminal (also: para with no arguments)",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return runTUI(l) },
	}
}

func runTUI(l *loader) error {
	if err := l.load(); err != nil {
		return err
	}
	return tui.Run(tui.Options{Install: l.install, VaultFlag: l.vaultFlag, VaultEnv: l.env("PARA_VAULT")})
}
