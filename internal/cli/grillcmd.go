package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// newGoalRiskCmd is `para goal risk`: the risks of a goal's brief.
func newGoalRiskCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "risk", Short: "Risks of a goal's brief", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.GoalRiskSet, binding[ops.GoalRiskSetInput, ops.GoalRiskSetOutput]{
		Use:  "set <goal> <text>",
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.GoalRiskSetInput) {
			fs.StringVar((*string)(&in.Likelihood), "likelihood", "", "low, medium, or high")
			fs.StringVar((*string)(&in.Impact), "impact", "", "low, medium, or high")
			fs.StringVar(&in.Owner, "owner", "", "who handles it: a link or a name")
			fs.StringVar(&in.Mitigation, "mitigation", "", "what reduces it")
			fs.StringVar(&in.Source, "source", "", "where it was said; required for a new risk")
			fs.BoolVar(&in.Remove, "remove", false, "remove the risk with this text")
		},
		Bind: func(args []string, in *ops.GoalRiskSetInput) error { in.ID, in.Text = args[0], args[1]; return nil },
		Text: func(w io.Writer, out ops.GoalRiskSetOutput) error {
			p := &printer{w: w}
			for _, r := range out.Risks {
				p.f("%s\t%s\tlikelihood %s\timpact %s\towner %s\tmitigation %s\n", out.ID, r.Text, dashIfEmpty(string(r.Likelihood)), dashIfEmpty(string(r.Impact)), dashIfEmpty(r.Owner), dashIfEmpty(r.Mitigation))
			}
			return p.err
		},
	}))
	return cmd
}

// newAssumptionCmd is `para assumption`: assumptions of a goal or a task.
func newAssumptionCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "assumption", Short: "Assumptions of a goal or a task, and what the last check found", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.AssumptionSet, binding[ops.AssumptionSetInput, ops.AssumptionSetOutput]{
		Use:  "set <goal|task> <text>",
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.AssumptionSetInput) {
			fs.StringVar(&in.Source, "source", "", "where it comes from; required for a new assumption")
			fs.StringVar((*string)(&in.Status), "status", "", "what a check found: holds, changed, or unknown")
			fs.StringVar(&in.Checked, "checked", "", "YYYY-MM-DD of the check (default today when --status is set)")
			fs.StringVar(&in.By, "by", "", "who checked (default you when --status is set)")
			fs.StringVar(&in.Fact, "fact", "", "what is true now, with --status changed")
			fs.BoolVar(&in.Remove, "remove", false, "remove the assumption with this text")
		},
		Bind: func(args []string, in *ops.AssumptionSetInput) error { in.ID, in.Text = args[0], args[1]; return nil },
		Text: func(w io.Writer, out ops.AssumptionSetOutput) error {
			p := &printer{w: w}
			for _, a := range out.Assumptions {
				writeAssumption(p, out.ID, a)
			}
			return p.err
		},
	}))
	return cmd
}

func writeAssumption(p *printer, id string, a model.Assumption) {
	p.f("%s\t%s\t%s\tchecked %s\t%s\n", id, a.Text, dashIfEmpty(string(a.Status)), dashIfEmpty(string(a.Checked)), dashIfEmpty(a.Fact))
}

// newGrillCmd is `para grill`: what to settle about a goal.
func newGrillCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "grill", Short: "What an interviewer must settle about a goal: risks, unknowns, owners, rules, and assumptions", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.GrillContext, binding[ops.GrillContextInput, ops.GrillContextOutput]{
		Use:  "context <goal>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.GrillContextInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.GrillContextOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\towner %s\n", out.ID, out.Title, dashIfEmpty(out.Owner))
			for _, pr := range out.Probes {
				p.f("%d\t%s\t%s\t%s\n", pr.Rank, pr.Kind, pr.About, pr.Known)
				p.f("  ask\t%s\n", pr.Ask)
				if len(pr.Sources) > 0 {
					p.f("  sources\t%s\n", strings.Join(pr.Sources, "; "))
				}
			}
			return p.err
		},
	}))
	return cmd
}

// newRecheckCmd is `para recheck`: queue a check of what might no longer be
// true, and record each result.
func newRecheckCmd(l *loader) *cobra.Command {
	cmd := opCommand(l, ops.Recheck, binding[ops.RecheckInput, ops.RecheckOutput]{
		Use:  "recheck [goal|area|task...]",
		Args: cobra.ArbitraryArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.RecheckInput) {
			fs.IntVar(&in.OlderThan, "older-than", 0, "days; leave out assumptions checked more recently (default recheck.days)")
			fs.StringVar(&in.Tag, "tag", "", "only items on documents with this tag")
			fs.BoolVar(&in.DryRun, "dry-run", false, "list the items, queue nothing")
		},
		Bind: func(args []string, in *ops.RecheckInput) error { in.Scope = strings.Join(args, ","); return nil },
		Text: func(w io.Writer, out ops.RecheckOutput) error {
			p := &printer{w: w}
			for _, it := range out.Items {
				p.f("%s\t%s\t%s\t%s\n", it.Kind, it.ID, dashIfEmpty(it.Text), it.Verify)
			}
			if out.Request != "" {
				p.f("queued\t%s\t%s\n", out.Request, out.Status)
			} else if len(out.Items) == 0 {
				p.f("nothing to recheck\n")
			}
			return p.err
		},
	})
	cmd.AddCommand(opCommand(l, ops.RecheckRecord, binding[ops.RecheckRecordInput, ops.RecheckRecordOutput]{
		Use:  "record <id> <holds|changed|unknown>",
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.RecheckRecordInput) {
			fs.StringVar(&in.Assumption, "assumption", "", "the assumption's text, for a goal or a task")
			fs.StringVar(&in.Source, "source", "", "where the check found it; required for holds and changed")
			fs.StringVar(&in.Fact, "fact", "", "what is true now; required for changed")
			fs.StringVar(&in.Question, "question", "", "the question to ask")
			fs.StringVar(&in.Scope, "scope", "", "goal, task, or area of the question, for a constraint or an exception (default root)")
			fs.StringVar(&in.NextReview, "next-review", "", "YYYY-MM-DD: new review date of a constraint or an exception that holds")
		},
		Bind: func(args []string, in *ops.RecheckRecordInput) error {
			in.ID, in.Result = args[0], model.AssumptionStatus(args[1])
			return nil
		},
		Text: func(w io.Writer, out ops.RecheckRecordOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\tchecked %s", out.ID, out.Result, out.Checked)
			if out.ReviewBy != "" {
				p.f("\treview by %s", out.ReviewBy)
			}
			if out.Question != "" {
				p.f("\tquestion %s", out.Question)
			}
			p.f("\n")
			return p.err
		},
	}))
	return cmd
}
