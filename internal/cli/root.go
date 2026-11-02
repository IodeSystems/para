// Package cli builds the para command tree. Commands read config through
// the loader, never through package-level globals (CLI-6).
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/guide"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/registry"
	"github.com/iodesystems/parasolve/internal/store/local"
	"github.com/iodesystems/parasolve/internal/vault"
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

	// exe returns the path of this binary; nil means os.Executable. Tests set it.
	exe func() (string, error)

	// raglitExe overrides the raglit binary; tests set it.
	raglitExe string
}

// executable is the absolute path of this binary, symlinks resolved.
func (l *loader) executable() (string, error) {
	get := l.exe
	if get == nil {
		get = os.Executable
	}
	p, err := get()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return filepath.Abs(p)
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

// selectVault applies --vault, PARA_VAULT, the default vault, then the single vault.
func (l *loader) selectVault() (registry.Selection, error) {
	return l.install.Select(l.vaultFlag, l.env("PARA_VAULT"))
}

// deps opens the selected vault for an op.
func (l *loader) deps() (ops.Deps, error) {
	sel, err := l.selectVault()
	if err != nil {
		return ops.Deps{}, err
	}
	return l.depsFor(sel.ID, sel.Path)
}

// depsFor opens the vault id at path as the CLI.
func (l *loader) depsFor(id, path string) (ops.Deps, error) {
	install, err := l.install.ID()
	if err != nil {
		return ops.Deps{}, err
	}
	s, err := local.Open(path, vault.Options{ID: id, Actor: cliActor, Surface: cliSurface, Install: install, StateDir: l.stateDir(id)})
	if err != nil {
		return ops.Deps{}, fmt.Errorf("vault %s: %w", id, err)
	}
	return ops.Deps{Store: s}, nil
}

// stateDir is this machine's state for a vault: lock, manifest, index.
func (l *loader) stateDir(id string) string {
	return filepath.Join(l.install.Home, "vaults", id)
}

// baseline records this machine's manifest for a new vault, so its first
// reconcile has a baseline. It writes no event.
func (l *loader) baseline(id, path string) error {
	install, err := l.install.ID()
	if err != nil {
		return err
	}
	v, err := vault.Open(path, vault.Options{ID: id, Actor: cliActor, Surface: cliSurface, Install: install, StateDir: l.stateDir(id)})
	if err != nil {
		return err
	}
	if _, err := v.Reconcile(context.Background()); err != nil {
		return err
	}
	return nil
}

// Execute runs the root command and returns its single error.
func Execute(version, srcDir string) error {
	return newRoot(&loader{version: version, srcDir: srcDir}).Execute()
}

func newRoot(l *loader) *cobra.Command {
	root := &cobra.Command{
		Use:           "para",
		Short:         "Progress without pain",
		Long:          guide.Intro(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if extended, _ := cmd.Flags().GetBool("help-extended"); extended {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), guide.Extended())
				return err
			}
			return runTUI(l)
		},
	}
	root.Flags().Bool("help-extended", false, "print use cases with real commands, and where each kind of thing goes")
	root.PersistentFlags().StringVar(&l.homeFlag, "home", "", "para home directory (or PARA_HOME; default ~/.para)")
	root.PersistentFlags().StringVar(&l.vaultFlag, "vault", "", "vault id (or PARA_VAULT)")
	root.AddCommand(
		newVersionCmd(l),
		newGuideCmd(),
		newSkillCmd(),
		newInitCmd(l),
		newVaultCmd(l),
		newLintCmd(l),
		newReconcileCmd(l),
		newEventsCmd(l),
		newInboxCmd(l),
		newMCPCmd(l),
		newBootstrapCmd(),
		newProjectCmd(l),
		newAreaCmd(l),
		newWhereCmd(l),
		newWaitingCmd(l),
		newIncidentCmd(l),
		newMeetingCmd(l),
		newAgendaCmd(l),
		newCalendarCmd(l),
		newQuestionCmd(l),
		newQuestionsCmd(l),
		newAnswerCmd(l),
		newDeferCmd(l),
		newUndeferCmd(l),
		newDependsCmd(l),
		newOrderCmd(l),
		newInvestigateCmd(l),
		newHealthCmd(l),
		newNoteCmd(l),
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
		newRaglitCmd(l),
		newJournalCmd(l),
		newTodayCmd(l),
		newRequestCmd(l),
		newProposalCmd(l),
		newReceiptCmd(l),
		newGateCmd(l),
		newHookCmd(l),
		newSimilarCmd(l),
		newReviewCmd(l),
		newTUICmd(l),
		newServeCmd(l),
		newKeyCmd(l),
		newSchemaCmd(l),
		newServiceCmd(l),
		newDoctorCmd(l),
		newTickCmd(l),
		newAgentCmd(l),
	)
	addGovernanceCmds(root, l)
	return root
}
