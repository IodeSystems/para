package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
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

// itemRefs reads --item values, <proposal>#<item>.
func itemRefs(list []string) ([]ops.ItemRef, error) {
	var out []ops.ItemRef
	for _, s := range list {
		p, it, ok := strings.Cut(s, "#")
		if !ok || p == "" || it == "" {
			return nil, fmt.Errorf("--item %q: want <proposal>#<item>", s)
		}
		out = append(out, ops.ItemRef{Proposal: p, Item: it})
	}
	return out, nil
}

// newDeferCmd is `para defer`: put questions, tasks, goals, io items, or
// dossier items off.
func newDeferCmd(l *loader) *cobra.Command {
	var items []string
	return opCommand(l, ops.Defer, binding[ops.DeferInput, ops.DeferOutput]{
		Use:  "defer [id]...",
		Args: cobra.ArbitraryArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.DeferInput) {
			fs.StringVar(&in.Until, "until", "", "YYYY-MM-DD it comes back; leave out for someday")
			fs.StringVar(&in.Reason, "reason", "", "why it can wait")
			fs.StringVar(&in.Scope, "scope", "", "a goal or an area: put off every open question in it that blocks nothing")
			fs.StringVar(&in.After, "after", "", "id of a task, question, ask, goal, or agent work: wait until it is done (adds a dependency)")
			fs.StringVar(&in.When, "when", "", "a condition in words, such as \"AI policy notes updated\": waits on the open task of that condition, or makes one")
			fs.StringVar(&in.Place, "place", "", "goal or area for the task --when makes; default the item's own")
			fs.BoolVar(&in.New, "new", false, "with --when: always make a new task, even when an open task has the same or a similar condition")
			fs.StringArrayVar(&items, "item", nil, "a dossier item as <proposal>#<item>; repeatable")
			fs.StringVar(&in.Tag, "tag", "", "a topic: every tagged open item and dossier item")
		},
		Bind: func(args []string, in *ops.DeferInput) error {
			in.IDs = args
			var err error
			in.Items, err = itemRefs(items)
			return err
		},
		Text: deferText,
	})
}

// newUndeferCmd is `para undefer`.
func newUndeferCmd(l *loader) *cobra.Command {
	var items []string
	return opCommand(l, ops.Undefer, binding[ops.UndeferInput, ops.DeferOutput]{
		Use:  "undefer [id]... [--item <proposal>#<item>]... [--tag <topic>]",
		Args: cobra.ArbitraryArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.UndeferInput) {
			fs.StringArrayVar(&items, "item", nil, "a dossier item as <proposal>#<item>; repeatable")
			fs.StringVar(&in.Tag, "tag", "", "a topic: every tagged item put off")
		},
		Bind: func(args []string, in *ops.UndeferInput) error {
			in.IDs = args
			var err error
			in.Items, err = itemRefs(items)
			return err
		},
		Text: deferText,
	})
}

// newDependsCmd is `para depends set` and `para depends cut`: what an item
// waits on.
func newDependsCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "depends", Short: "Edit what an item waits on", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.DependsSet, binding[ops.DependsSetInput, ops.DependsSetOutput]{
		Use:  "set <id> [<on> | --on <id>] [--optional] [--remove] [--reason <text>]",
		Args: cobra.RangeArgs(1, 2),
		Flags: func(fs *pflag.FlagSet, in *ops.DependsSetInput) {
			fs.StringVar(&in.On, "on", "", "id the item waits on (same as <on>)")
			fs.BoolVar(&in.Optional, "optional", false, "better before: shown, but it never holds the item")
			fs.BoolVar(&in.Remove, "remove", false, "take the dependency or reason away")
			fs.StringVar(&in.Reason, "reason", "", "a text reason the task waits (blocked_by), instead of <on>")
		},
		Bind: func(args []string, in *ops.DependsSetInput) error {
			in.ID = args[0]
			if len(args) > 1 {
				if in.On != "" && in.On != args[1] {
					return fmt.Errorf("depends set: give <on> or --on, not both")
				}
				in.On = args[1]
			}
			return nil
		},
		Text: func(w io.Writer, out ops.DependsSetOutput) error {
			_, err := fmt.Fprintf(w, "%s\tdepends_on %s\toptional_after %s\tblocked_by %s\n", out.ID, joinOrDash(out.DependsOn), joinOrDash(out.OptionalAfter), joinOrDash(out.BlockedBy))
			return err
		},
	}))
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
			fs.StringVar(&in.Kind, "kind", "", "only incident, goal, task, question, ask, meeting, work, or io")
		},
		Text: func(w io.Writer, out ops.OrderOutput) error {
			p := &printer{w: w}
			printRows(p, "", out.Summary, len(out.Rows), func(i int) (group, kind, id, why, title string) {
				r := out.Rows[i]
				return r.Group, r.Kind, r.ID, r.Why, r.Title
			})
			return p.err
		},
	})
}

// printRows prints value order rows, one per line. The reply group starts
// with one batch line, and the focus group and the rest after it with a
// heading, so the owner reads a batch, then focus, then the rest.
func printRows(p *printer, indent string, sum ops.OrderSummary, n int, row func(i int) (group, kind, id, why, title string)) {
	last := ""
	for i := range n {
		group, kind, id, why, title := row(i)
		if group != last {
			switch {
			case group == ops.GroupOwed:
				p.f("%sowed: promises to people and their prep\n", indent)
			case group == ops.GroupReply:
				p.f("%s%s\n", indent, replyBatch(sum))
			case group == ops.GroupFocus:
				p.f("%sfocus\n", indent)
			case group == ops.GroupRecheck:
				p.f("%srecheck\n", indent)
			case group == ops.GroupImpact && (last == ops.GroupOwed || last == ops.GroupReply || last == ops.GroupFocus || last == ops.GroupRecheck):
				p.f("%sthen\n", indent)
			}
			last = group
		}
		p.f("%s%s\t%s\t%s\t%s\n", indent, kind, id, why, title)
	}
}

// replyBatch is the batch line of the reply group.
func replyBatch(s ops.OrderSummary) string {
	return fmt.Sprintf("reply\t%d waiting on you, about %d min in all, %d an agent can prepare", s.Reply, s.ReplyMinutes, s.ReplyPrep)
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
