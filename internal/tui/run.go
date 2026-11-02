package tui

import (
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/registry"
)

// Options says which vault to open.
type Options struct {
	Install   registry.Install
	VaultFlag string
	VaultEnv  string
}

// Run opens the vault by the normal rule (flag, PARA_VAULT, the single vault)
// and runs the TUI until the user quits.
func Run(o Options) error {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return errors.New("tui: stdin and stdout must be a terminal")
	}
	sel, err := o.Install.Select(o.VaultFlag, o.VaultEnv)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	env, err := OpenEnv(o.Install, sel.Vault)
	if err != nil {
		return err
	}
	InitStyle() // before the program owns the terminal
	if _, err := tea.NewProgram(New(env)).Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
