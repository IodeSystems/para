package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/ops"
)

// deferText prints what a defer or undefer changed.
func deferText(w io.Writer, out ops.DeferOutput) error {
	p := &printer{w: w}
	for _, id := range out.Changed {
		if out.Until != "" {
			p.f("deferred\t%s\t%s\n", id, out.Until)
		} else {
			p.f("back\t%s\n", id)
		}
	}
	for _, s := range out.Skipped {
		p.f("skipped\t%s\n", s)
	}
	return p.err
}

// newDeferCmd is `para defer`: put questions, tasks, or goals off.
func newDeferCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Defer, binding[ops.DeferInput, ops.DeferOutput]{
		Use:  "defer [id]...",
		Args: cobra.ArbitraryArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.DeferInput) {
			fs.StringVar(&in.Until, "until", "", "YYYY-MM-DD it comes back; leave out for someday")
			fs.StringVar(&in.Reason, "reason", "", "why it can wait")
			fs.StringVar(&in.Scope, "scope", "", "a goal or an area: put off every open question in it that blocks nothing")
		},
		Bind: func(args []string, in *ops.DeferInput) error { in.IDs = args; return nil },
		Text: deferText,
	})
}

// newUndeferCmd is `para undefer`.
func newUndeferCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Undefer, binding[ops.UndeferInput, ops.DeferOutput]{
		Use:  "undefer <id>...",
		Args: cobra.MinimumNArgs(1),
		Bind: func(args []string, in *ops.UndeferInput) error { in.IDs = args; return nil },
		Text: deferText,
	})
}

// newDependsCmd is `para depends cut`: the edge-trimming command.
func newDependsCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "depends", Short: "Trim what a task or question waits on", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.DependsCut, binding[ops.DependsCutInput, ops.DependsCutOutput]{
		Use:  "cut <id> <depends-on> --reason <text>",
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.DependsCutInput) {
			fs.StringVar(&in.Reason, "reason", "", "why it can go ahead without it (required); recorded as an assumption")
		},
		Bind: func(args []string, in *ops.DependsCutInput) error { in.ID, in.Dep = args[0], args[1]; return nil },
		Text: func(w io.Writer, out ops.DependsCutOutput) error {
			_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, joinOrDash(out.DependsOn))
			return err
		},
	}))
	return cmd
}

// newOrderCmd is `para order`: the value order of the vault.
func newOrderCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.OrderOp, binding[ops.OrderInput, ops.OrderOutput]{
		Use:  "order",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.OrderInput) {
			fs.StringVar(&in.Kind, "kind", "", "only incident, goal, task, question, ask, meeting, or work")
		},
		Text: func(w io.Writer, out ops.OrderOutput) error {
			p := &printer{w: w}
			for _, r := range out.Rows {
				p.f("%s\t%s\t%s\t%s\n", r.Kind, r.ID, r.Why, r.Title)
			}
			return p.err
		},
	})
}
