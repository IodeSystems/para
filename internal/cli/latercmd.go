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
	if out.Created != "" {
		p.f("created\t%s\n", out.Created)
	}
	for _, x := range out.Similar {
		p.f("similar\t%s\t%s\t(use --after %s, or --new)\n", x.ID, x.Title, x.ID)
	}
	for _, id := range out.Changed {
		switch {
		case out.After != "":
			p.f("deferred\t%s\tafter %s\t%s\n", id, out.After, out.Until)
		case out.Until != "":
			p.f("deferred\t%s\t%s\n", id, out.Until)
		default:
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
			fs.StringVar(&in.After, "after", "", "id of a task, question, ask, goal, or agent work: wait until it is done (adds a dependency)")
			fs.StringVar(&in.When, "when", "", "a condition in words, such as \"AI policy notes updated\": makes a task with that done_when and waits on it")
			fs.StringVar(&in.Place, "place", "", "goal or area for the task --when makes; default the item's own")
			fs.BoolVar(&in.New, "new", false, "with --when: make the task even when a similar open task exists")
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

// newHealthCmd is `para health`: gaps in the vault, as warnings. It exits 0
// with findings. Dismiss and undismiss are the owner's.
func newHealthCmd(l *loader) *cobra.Command {
	cmd := opCommand(l, ops.HealthOp, binding[ops.HealthInput, ops.HealthOutput]{
		Use:  "health",
		Args: cobra.NoArgs,
		Text: func(w io.Writer, out ops.HealthOutput) error {
			p := &printer{w: w}
			for _, g := range out.Groups {
				p.f("%s\t%s\n", g.Goal, g.Title)
				for _, f := range g.Findings {
					p.f("  %s\t%s\t%s\t%s\n    fix: %s\n", f.Kind, f.ID, f.Title, f.Message, f.Fix)
				}
			}
			p.f("%d findings, %d dismissed\n", out.Count, out.Dismissed)
			return p.err
		},
	})
	cmd.Short = "Report gaps in the vault as warnings grouped by goal (never fails)"
	cmd.AddCommand(opCommand(l, ops.HealthDismiss, binding[ops.HealthDismissInput, ops.HealthDismissOutput]{
		Use:  "dismiss <id> <kind> --reason <text>",
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.HealthDismissInput) {
			fs.StringVar(&in.Reason, "reason", "", "why it can stay as it is (required)")
		},
		Bind: func(args []string, in *ops.HealthDismissInput) error { in.ID, in.Kind = args[0], args[1]; return nil },
		Text: healthText,
	}))
	cmd.AddCommand(opCommand(l, ops.HealthUndismiss, binding[ops.HealthUndismissInput, ops.HealthDismissOutput]{
		Use:  "undismiss <id> <kind>",
		Args: cobra.ExactArgs(2),
		Bind: func(args []string, in *ops.HealthUndismissInput) error { in.ID, in.Kind = args[0], args[1]; return nil },
		Text: healthText,
	}))
	return cmd
}

func healthText(w io.Writer, out ops.HealthDismissOutput) error {
	_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, joinOrDash(out.Dismissed))
	return err
}

// newInvestigateCmd is `para investigate`: send an item to an agent to find
// context. A second call for the same item adds the hint to the open work.
func newInvestigateCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Investigate, binding[ops.InvestigateInput, ops.InvestigateOutput]{
		Use:  "investigate <id> [--hint <text>]",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.InvestigateInput) {
			fs.StringVar(&in.Hint, "hint", "", "ask the agent about the item, or where to look (optional; empty: investigate)")
			fs.StringVar(&in.Runner, "runner", "", "install id that may claim the work; default as request add")
		},
		Bind: func(args []string, in *ops.InvestigateInput) error { in.ID = args[0]; return nil },
		Text: investigateText,
	})
}
