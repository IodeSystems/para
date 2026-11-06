// Package cli builds the para command tree. Commands read config through
// the loader, never through package-level globals (CLI-6).
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/guide"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/upgrade"
	"github.com/iodesystems/para/internal/vault"
)

// Event identity of this surface. One user per install (plan.md, Data); an
// agent surface sets its own.
const (
	cliActor   = "user"
	cliSurface = "cli"
	// cliAgentSurface is a CLI call made as a named agent (--actor). It is not
	// a person surface, so ops treat it as they treat MCP: why is required, a
	// receipt is filed, person-only ops are refused.
	cliAgentSurface = "cli-agent"
)

// loader carries build stamps, flag values, and resolved config. Each RunE
// calls l.load() explicitly (CLI-4); there is no PersistentPreRun.
type loader struct {
	version string
	srcDir  string

	env       func(string) string // nil means os.Getenv
	homeFlag  string
	vaultFlag string
	actorFlag string
	whyFlag   string
	// minorFlag, obviousFlag, and dossierFlag were an agent's ratings of a
	// write for the gate. They are accepted and ignored for one release.
	minorFlag   bool
	obviousFlag bool
	dossierFlag string

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

// actor is who the CLI acts as: --actor, then PARA_ACTOR, then the owner.
func (l *loader) actor() string {
	a := strings.TrimSpace(l.actorFlag)
	if a == "" && l.env != nil {
		a = strings.TrimSpace(l.env("PARA_ACTOR"))
	}
	if a == "" {
		return cliActor
	}
	return a
}

// agent reports whether the CLI acts as an agent, not the owner.
func (l *loader) agent() bool { return l.actor() != cliActor }

// surface is the event surface of this CLI call.
func (l *loader) surface() string {
	if l.agent() {
		return cliAgentSurface
	}
	return cliSurface
}

// depsFor opens the vault id at path as the CLI.
func (l *loader) depsFor(id, path string) (ops.Deps, error) {
	install, err := l.install.ID()
	if err != nil {
		return ops.Deps{}, err
	}
	s, err := local.Open(path, vault.Options{ID: id, Actor: l.actor(), Surface: l.surface(), Install: install, StateDir: l.stateDir(id)})
	if err == nil {
		err = upgrade.Check(s)
	}
	if err == nil {
		err = upgrade.RecordSession(context.Background(), s, l.stateDir(id), l.version)
	}
	if err != nil {
		return ops.Deps{}, fmt.Errorf("vault %s: %w", id, err)
	}
	base, open, signedIn := l.viewer(id, !l.agent())
	return ops.Deps{Store: s, ViewerBase: base, OpenURL: open, SignedIn: signedIn}, nil
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
		Long:          guide.Intro() + "\n\nWith an agent: para help claude (or para help dun) prints the setup: MCP servers, the skill, tracked asks, the loop, and status.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if extended, _ := cmd.Flags().GetBool("help-extended"); extended {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), guide.Extended())
				return err
			}
			return runAgendaHome(l, cmd)
		},
	}
	root.Flags().Bool("help-extended", false, "print use cases with real commands, and where each kind of thing goes")
	root.PersistentFlags().StringVar(&l.homeFlag, "home", "", "para home directory (or PARA_HOME; default ~/.para)")
	root.PersistentFlags().StringVar(&l.vaultFlag, "vault", "", "vault id (or PARA_VAULT)")
	root.PersistentFlags().StringVar(&l.actorFlag, "actor", "", "act as this agent (or PARA_ACTOR; default the owner): writes then need --why and file a receipt, as over MCP")
	root.PersistentFlags().StringVar(&l.whyFlag, "why", "", "why you make this write; required on a write when --actor names an agent")
	root.PersistentFlags().BoolVar(&l.minorFlag, "minor", false, "ignored since 0.3.0 (goes in a later release): an undoable write applies at once")
	root.PersistentFlags().BoolVar(&l.obviousFlag, "obvious", false, "ignored since 0.3.0 (goes in a later release): an undoable write applies at once")
	root.PersistentFlags().StringVar(&l.dossierFlag, "dossier", "", "ignored since 0.3.0 (goes in a later release): no write is held back")
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
		newIOCmd(l),
		newMCPCmd(l),
		newBootstrapCmd(),
		newProjectCmd(l),
		newAreaCmd(l),
		newWhereCmd(l),
		newRulesInEffectCmd(l),
		newWaitingCmd(l),
		newIncidentCmd(l),
		newMeetingCmd(l),
		newObligationCmd(l),
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
		newDocCmd(l),
		newNotesCmd(l),
		newAttachmentCmd(l),
		newItemCmd(l),
		newSourceCmd(l),
		newSweepCmd(l),
		newAttemptCmd(l),
		newReportCmd(l),
		newFeedbackCmd(l),
		newTagCmd(l),
		newSentCmd(l),
		newTaskCmd(l),
		newGraphCmd(l),
		newNextCmd(l),
		newRFXCmd(l),
		newOrgCmd(l),
		newPriorityCmd(l),
		newTriageCmd(l),
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
		newDossierCmd(l),
		newStackCmd(l),
		newTopOpenCmd(l),
		newReceiptCmd(l),
		newAlignCmd(l),
		newImportCmd(l),
		newActionCmd(l),
		newGoalCmd(l),
		newGrillCmd(l),
		newAssumptionCmd(l),
		newRecheckCmd(l),
		newSimilarCmd(l),
		newServeCmd(l),
		newKeyCmd(l),
		newSchemaCmd(l),
		newServiceCmd(l),
		newDoctorCmd(l),
		newUpgradeCmd(l),
		newChangesCmd(l),
		newTickCmd(l),
		newAgentCmd(l),
	)
	root.AddCommand(newVerbCmds(l)...)
	addGovernanceCmds(root, l)
	root.SetHelpCommand(newHelpCmd(l, root))
	return root
}

// newHelpCmd is cobra's help command with two topics more: help claude and
// help dun print the harness setup, as para mcp --help-claude does.
func newHelpCmd(l *loader, root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command | claude | dun]",
		Short: "Help about any command; help claude and help dun print the setup for that agent harness",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "claude" || args[0] == "dun") {
				if err := l.load(); err != nil {
					return err
				}
				return writeHarnessHelp(cmd, l, args[0])
			}
			target, _, err := root.Find(args)
			if err != nil || target == nil {
				return fmt.Errorf("unknown help topic %q: try para help claude, para help dun, or para <command> --help", strings.Join(args, " "))
			}
			target.InitDefaultHelpFlag()
			return target.Help()
		},
	}
}
