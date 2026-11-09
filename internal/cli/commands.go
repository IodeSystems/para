package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/vault"
)

// newLintCmd wraps the lint op. With a path it lints files directly, needs no
// vault selection, and is not part of the op: a filesystem path has no
// meaning on a remote store or an agent surface.
func newLintCmd(l *loader) *cobra.Command {
	text := func(w io.Writer, out ops.LintOutput) error {
		return writeLint(w, out.Findings, out.Errors, out.Warnings, "", false)
	}
	cmd := opCommand(l, ops.Lint, binding[ops.LintInput, ops.LintOutput]{
		Use:  "lint [path] [--file]",
		Args: cobra.MaximumNArgs(1),
		Text: text,
	})
	cmd.Long = `Lint every document in the vault, or the files under a path. Each finding is
path:line, severity (error red, warn yellow on a terminal; NO_COLOR turns
colour off), the code, and the message. The last line is the verdict. It exits
non-zero when there are errors.

--file also files the findings as one agent work request (kind vault-check,
titled "Vault check: N errors, M warnings"); an open one is updated, not added
again. The agent fixes the data with ops, or finishes with a bug report for
para's developer when para is wrong. Run /para loop to work it.`
	var file bool
	cmd.Flags().BoolVar(&file, "file", false, "file the findings as one agent work request (vault check); updates the open one")
	check := newVaultCheckCmd(l)
	runVault := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if file {
			if len(args) > 0 {
				return fmt.Errorf("lint: --file lints the vault and takes no path")
			}
			return check.RunE(cmd, args)
		}
		if len(args) == 0 {
			return runVault(cmd, args)
		}
		findings, err := vault.LintPath(args[0])
		if err != nil {
			return err
		}
		return emit(cmd, ops.NewLintOutput(findings), text)
	}
	return cmd
}

// newVaultCheckCmd is `para vault check`, which `para lint --file` runs.
func newVaultCheckCmd(l *loader) *cobra.Command {
	cmd := opCommand(l, ops.VaultCheck, binding[ops.VaultCheckInput, ops.VaultCheckOutput]{
		Use:  "check",
		Args: cobra.NoArgs,
		Text: func(w io.Writer, out ops.VaultCheckOutput) error {
			return writeLint(w, out.Findings, out.Errors, out.Warnings, out.Request, out.Updated)
		},
	})
	cmd.Short = "Lint the vault and file the findings as one agent work request (para lint --file)"
	return cmd
}

// writeLint prints each finding, then the verdict: the counts, and the agent
// work that holds them when one was filed.
func writeLint(w io.Writer, findings []model.Finding, errors, warnings int, request string, updated bool) error {
	for _, f := range findings {
		if _, err := fmt.Fprintln(w, findingLine(w, f)); err != nil {
			return err
		}
	}
	return verdict(w, countTone(errors, warnings), "lint: %d errors, %d warnings%s", errors, warnings, filedNote(request, updated))
}

func newReconcileCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Reconcile, binding[ops.ReconcileInput, model.ReconcileResult]{
		Use:  "reconcile",
		Args: cobra.NoArgs,
		Text: func(w io.Writer, out model.ReconcileResult) error {
			if out.Baseline {
				if _, err := fmt.Fprintln(w, "no manifest; current files are the new baseline"); err != nil {
					return err
				}
			}
			for _, c := range out.Changes {
				if _, err := fmt.Fprintf(w, "%s\t%s\n", c.Kind, c.Path); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

func newEventsCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "events", Short: "Read the vault event log", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.EventsTail, binding[ops.EventsTailInput, ops.EventsTailOutput]{
		Use:  "tail",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.EventsTailInput) {
			fs.IntVarP(&in.Limit, "limit", "n", 20, "number of events")
		},
		Text: func(w io.Writer, out ops.EventsTailOutput) error {
			for _, e := range out.Events {
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					e.TS.Format("2006-01-02T15:04:05Z"), e.Actor, e.Surface, e.Op, e.Doc, e.From, e.To); err != nil {
					return err
				}
			}
			return nil
		},
	}))
	return cmd
}
