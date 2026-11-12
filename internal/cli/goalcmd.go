package cli

import (
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// newGoalCmd is `para goal`: goals (add, list, show, edit, close, link), the
// goal brief's readings, the owner's rank, and goals side by side. The brief itself is edited with `para doc edit`.
func newGoalCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "goal", Short: "Goals: bounded outcomes with a done condition; validation: signal readings, risks, rank, and a side-by-side comparison", Args: cobra.NoArgs}
	cmd.AddCommand(goalCommands(l)...)
	signal := &cobra.Command{Use: "signal", Short: "Readings of the signals a goal's brief defines", Args: cobra.NoArgs}
	signal.AddCommand(
		opCommand(l, ops.GoalSignalAdd, binding[ops.GoalSignalAddInput, ops.GoalSignalAddOutput]{
			Use:  "add <goal> <what> --value <text> --source <where>",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.GoalSignalAddInput) {
				fs.StringVar(&in.Value, "value", "", "what was read")
				fs.StringVar(&in.Source, "source", "", "where it was read: a URL, a document id, a command")
				fs.StringVar(&in.Date, "date", "", "YYYY-MM-DD of the reading (default today)")
			},
			Bind: func(args []string, in *ops.GoalSignalAddInput) error { in.ID, in.What = args[0], args[1]; return nil },
			Text: func(w io.Writer, out ops.GoalSignalAddOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\treadings %d\n", out.ID, out.What, out.Date, out.Readings)
				return p.err
			},
		}),
	)
	cmd.AddCommand(
		signal,
		newGoalRiskCmd(l),
		opCommand(l, ops.GoalRank, binding[ops.GoalRankInput, ops.GoalRankOutput]{
			Use:  "rank <goal> <n>",
			Args: cobra.ExactArgs(2),
			Bind: func(args []string, in *ops.GoalRankInput) error {
				n, err := strconv.Atoi(args[1])
				if err != nil {
					return err
				}
				in.ID, in.Rank = args[0], n
				return nil
			},
			Text: func(w io.Writer, out ops.GoalRankOutput) error {
				p := &printer{w: w}
				p.f("%s\trank %d\n", out.ID, out.Rank)
				return p.err
			},
		}),
		opCommand(l, ops.GoalCompare, binding[ops.GoalCompareInput, ops.GoalCompareOutput]{
			Use:  "compare [goal...]",
			Args: cobra.ArbitraryArgs,
			Bind: func(args []string, in *ops.GoalCompareInput) error { in.IDs = strings.Join(args, ","); return nil },
			Text: func(w io.Writer, out ops.GoalCompareOutput) error {
				p := &printer{w: w}
				for _, g := range out.Goals {
					p.f("%s\t%s\trank %s\tdue %s\t%s\tconfidence %d%%\n", g.ID, g.Title, dashInt(g.Rank), dashIfEmpty(g.Due), dashIfEmpty(g.Validation), g.Confidence)
					p.f("  why\t%s\n  benefit\t%s\n  cost\t%s\n", dashIfEmpty(g.Why), dashIfEmpty(g.Benefit), dashIfEmpty(g.Cost))
					for _, s := range g.Signals {
						late := ""
						if s.Overdue {
							late = "\tdue now"
						}
						p.f("  signal\t%s\texpected %s\tlast %s %s%s\n", s.What, dashIfEmpty(s.Expected), dashIfEmpty(s.Last), s.LastValue, late)
					}
					if g.PivotDate != "" {
						p.f("  pivot\t%s\t%s\n", g.PivotDate, g.PivotAction)
					}
				}
				return p.err
			},
		}),
	)
	return cmd
}

func dashInt(n int) string {
	if n <= 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

// writeBrief prints the brief of a goal in a few lines; nothing when it has none.
func writeBrief(p *printer, b *model.GoalBrief) {
	if b == nil {
		return
	}
	if b.Validation != "" || b.Rank > 0 {
		p.f("validation\t%s\trank %s\n", dashIfEmpty(string(b.Validation)), dashInt(b.Rank))
	}
	if b.Why != nil {
		p.f("why\t%s\n", b.Why.Text)
	}
	if c := b.Confidence; c != nil {
		p.f("confidence\t%d%%\t%s\n", c.Percent, c.Basis)
	}
	if len(b.Alternatives) > 0 {
		p.f("alternatives\t%d\n", len(b.Alternatives))
	}
	for _, u := range b.Unknowns {
		p.f("unknown\t%s\t%s\n", u.Text, dashIfEmpty(string(u.Link)))
	}
	for _, s := range b.Signals {
		last := "no reading"
		if r := s.Last(); r != "" {
			last = "last " + string(r)
		}
		p.f("signal\t%s\texpected %s\t%s\n", s.What, dashIfEmpty(s.Expected), last)
	}
	if pv := b.Pivot; pv != nil {
		p.f("pivot\t%s\t%s\t%s\n", dashIfEmpty(string(pv.Date)), pv.Action, pv.Condition)
	}
}
