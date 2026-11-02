package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/ops"
	"github.com/iodesystems/paradize/internal/vault"
)

func newInboxCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "inbox", Short: "Unsorted captures", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.InboxAdd, binding[ops.InboxAddInput, ops.InboxAddOutput]{
		Use:  "add <text>",
		Args: cobra.MinimumNArgs(1),
		Bind: func(args []string, in *ops.InboxAddInput) error {
			in.Text = strings.Join(args, " ")
			return nil
		},
		Text: func(w io.Writer, out ops.InboxAddOutput) error {
			_, err := fmt.Fprintln(w, out.ID)
			return err
		},
	}))
	return cmd
}

// newLintCmd wraps the lint op. With a path it lints files directly, needs no
// vault selection, and is not part of the op: a filesystem path has no
// meaning on a remote store or an agent surface.
func newLintCmd(l *loader) *cobra.Command {
	text := func(w io.Writer, out ops.LintOutput) error {
		for _, f := range out.Findings {
			if _, err := fmt.Fprintln(w, f); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(w, "%d errors, %d warnings\n", out.Errors, out.Warnings)
		return err
	}
	cmd := opCommand(l, ops.Lint, binding[ops.LintInput, ops.LintOutput]{
		Use:  "lint [path]",
		Args: cobra.MaximumNArgs(1),
		Text: text,
	})
	runVault := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
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
