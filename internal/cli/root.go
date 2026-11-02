// Package cli builds the para command tree. Commands read config through
// the loader, never through package-level globals (CLI-6).
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/iodesystems/paradize/internal/ops"
	"github.com/iodesystems/paradize/internal/registry"
	"github.com/iodesystems/paradize/internal/store/local"
	"github.com/iodesystems/paradize/internal/vault"
)

// Event identity of this surface. One user per install (plan.md, Data); an
// agent surface sets its own.
const (
	cliActor   = "user"
	cliSurface = "cli"
)

// loader carries build stamps, flag values, and resolved config. Each RunE
// calls l.load() explicitly (CLI-4); there is no PersistentPreRun.
type loader struct {
	version string
	srcDir  string

	env       func(string) string // nil means os.Getenv
	homeFlag  string
	vaultFlag string

	install registry.Install
}

// load resolves the install home: --home, then PARA_HOME, then ~/.para.
func (l *loader) load() error {
	if l.env == nil {
		l.env = os.Getenv
	}
	home := l.homeFlag
	if home == "" {
		var err error
		if home, err = registry.DefaultHome(l.env); err != nil {
			return err
		}
	}
	l.install = registry.Install{Home: home}
	return nil
}

// selectVault applies --vault, PARA_VAULT, then the single vault.
func (l *loader) selectVault() (registry.Selection, error) {
	return l.install.Select(l.vaultFlag, l.env("PARA_VAULT"))
}

// deps opens the selected vault for an op.
func (l *loader) deps() (ops.Deps, error) {
	sel, err := l.selectVault()
	if err != nil {
		return ops.Deps{}, err
	}
	install, err := l.install.ID()
	if err != nil {
		return ops.Deps{}, err
	}
	s, err := local.Open(sel.Path, vault.Options{ID: sel.ID, Actor: cliActor, Surface: cliSurface, Install: install, StateDir: filepath.Join(l.install.Home, "vaults", sel.ID)})
	if err != nil {
		return ops.Deps{}, fmt.Errorf("vault %s: %w", sel.ID, err)
	}
	return ops.Deps{Store: s}, nil
}

// Execute runs the root command and returns its single error.
func Execute(version, srcDir string) error {
	return newRoot(&loader{version: version, srcDir: srcDir}).Execute()
}

func newRoot(l *loader) *cobra.Command {
	root := &cobra.Command{
		Use:           "para",
		Short:         "Progress without pain",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE:          func(*cobra.Command, []string) error { return runTUI(l) },
	}
	root.PersistentFlags().StringVar(&l.homeFlag, "home", "", "para home directory (or PARA_HOME; default ~/.para)")
	root.PersistentFlags().StringVar(&l.vaultFlag, "vault", "", "vault id (or PARA_VAULT)")
	root.AddCommand(
		newVersionCmd(l),
		newInitCmd(l),
		newVaultCmd(l),
		newLintCmd(l),
		newReconcileCmd(l),
		newEventsCmd(l),
		newInboxCmd(l),
		newMCPCmd(l),
		newProjectCmd(l),
		newTaskCmd(l),
		newGraphCmd(l),
		newNextCmd(l),
		newRFXCmd(l),
		newOrgCmd(l),
		newRouteCmd(l),
		newWhoCmd(l),
		newInteractionCmd(l),
		newIndexCmd(l),
		newSearchCmd(l),
		newJournalCmd(l),
		newTodayCmd(l),
		newRequestCmd(l),
		newTUICmd(l),
	)
	addGovernanceCmds(root, l)
	return root
}
