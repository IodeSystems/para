package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func newAreaCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "area", Short: "Areas: ongoing responsibilities", Args: cobra.NoArgs}
	cmd.AddCommand(
		newLinkCmd(l, ops.AreaLinkAdd, ops.AreaLinkRemove),
		opCommand(l, ops.AreaAdd, binding[ops.AreaAddInput, ops.AreaAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.AreaAddInput) {
				fs.StringVar(&in.ID, "id", "", "document id (default area-<slug of title>)")
				fs.StringVar((*string)(&in.Status), "status", "", "active or paused (default active)")
				fs.StringVar(&in.Body, "body", "", "text after the title")
				fs.StringVar((*string)(&in.Kind), "kind", "", "project (default: ongoing work goals call home) or concept (a discipline that spans work)")
				fs.StringVar(&in.Cadence, "cadence", "", "review cadence: daily, weekly, monthly, quarterly, yearly, or \"every <n> days\"")
				fs.StringVar(&in.LastReviewed, "last-reviewed", "", "date of the last review, YYYY-MM-DD")
				fs.StringSliceVar(&in.Concepts, "concepts", nil, "ids of concept areas a project area draws on (repeat or comma-separate)")
			},
			Bind: func(args []string, in *ops.AreaAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: printID(func(o ops.AreaAddOutput) string { return o.ID }),
		}),
		opCommand(l, ops.AreaList, binding[ops.AreaListInput, ops.AreaListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Text: func(w io.Writer, out ops.AreaListOutput) error {
				p := &printer{w: w}
				for _, a := range out.Areas {
					p.f("%s\t%s\t%d links\t%s\n", a.ID, a.Status, len(a.Links), a.Title)
				}
				return p.err
			},
		}),
		opCommand(l, ops.AreaShow, binding[ops.AreaShowInput, ops.AreaShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.AreaShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.AreaShowOutput) error {
				p := &printer{w: w}
				a := out.Area
				p.f("%s\t%s\t%s\n", a.ID, a.Status, a.Title)
				p.f("kind\t%s\n", a.Kind)
				if a.Cadence != "" {
					p.f("review\t%s\tlast %s\tnext %s\n", a.Cadence, dash(string(a.LastReviewed)), dash(a.NextReview))
				}
				if len(a.Concepts) > 0 {
					p.f("concepts\t%s\n", strings.Join(a.Concepts, " "))
				}
				writeLinks(p, a.Links)
				if len(out.DrawnOnBy) > 0 {
					p.f("drawn on by\n")
					for _, u := range out.DrawnOnBy {
						p.f("  %s\t%s\t%s\n", u.ID, u.Type, u.Title)
					}
				}
				p.f("projects\n")
				for _, pr := range out.Projects {
					p.f("  %s\t%s\t%s\t%s\n", pr.ID, pr.Status, pr.Owner, pr.Title)
				}
				p.f("tasks\n")
				if p.err == nil {
					p.err = writeTasks(w, out.Tasks)
				}
				p.f("notes\n")
				if p.err == nil {
					p.err = writeNotes(w, out.Notes)
				}
				p.f("incidents\n")
				writeIncidents(p, "  ", out.Incidents)
				p.f("questions\n")
				writeQuestions(p, "  ", out.Questions)
				p.f("recurring\n")
				for _, t := range out.Recurring {
					p.f("  %s\t%s\t%s\t%s\t%s\n", t.ID, t.Recur, dash(string(t.Due)), orSelf(t.Assignee), t.Title)
				}
				return p.err
			},
		}),
		opCommand(l, ops.AreaReview, binding[ops.AreaReviewInput, ops.AreaReviewOutput]{
			Use:  "review <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.AreaReviewInput) {
				fs.StringVar(&in.Note, "note", "", "one line on what the review found")
			},
			Bind: func(args []string, in *ops.AreaReviewInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.AreaReviewOutput) error {
				p := &printer{w: w}
				p.f("%s\treviewed %s\tnext %s\n", out.ID, out.LastReviewed, dash(out.NextReview))
				return p.err
			},
		}),
	)
	return cmd
}

func newRulesInEffectCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.RulesInEffect, binding[ops.RulesInEffectInput, ops.RulesInEffectOutput]{
		Use:  "rules-in-effect <id>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.RulesInEffectInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.RulesInEffectOutput) error {
			p := &printer{w: w}
			p.f("scopes\t%s\n", strings.Join(out.Scopes, " "))
			for _, r := range out.Rules {
				p.f("%s\t%s\tvia %s\trefines %s\t%s\n", r.ID, r.Type, r.Via, dash(r.Refines), r.Title)
			}
			return p.err
		},
	})
}

func writeLinks(p *printer, links []model.Pointer) {
	p.f("links\n")
	for _, k := range links {
		p.f("  %s\t%s\t%s\n", k.Label, k.Kind, k.Target)
	}
}

// newLinkCmd builds `link add|remove` for an area or a project.
func newLinkCmd(l *loader, add *ops.Op[ops.LinkAddInput, ops.LinkOutput], remove *ops.Op[ops.LinkRemoveInput, ops.LinkOutput]) *cobra.Command {
	cmd := &cobra.Command{Use: "link", Short: "Point at repos, plans, and docs that live elsewhere (never read or copied)", Args: cobra.NoArgs}
	text := func(w io.Writer, out ops.LinkOutput) error {
		p := &printer{w: w}
		p.f("%s\n", out.ID)
		writeLinks(p, out.Links)
		return p.err
	}
	cmd.AddCommand(
		opCommand(l, add, binding[ops.LinkAddInput, ops.LinkOutput]{
			Use:  "add <id> <label>",
			Args: cobra.MinimumNArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.LinkAddInput) {
				fs.StringVar((*string)(&in.Kind), "kind", "", "repo, plan, docs, runbook, tracker, dashboard, or other (required)")
				fs.StringVar(&in.Target, "target", "", "URL or path, such as ~/doc/plan.md (required)")
			},
			Bind: func(args []string, in *ops.LinkAddInput) error {
				in.ID, in.Label = args[0], strings.Join(args[1:], " ")
				return nil
			},
			Text: text,
		}),
		opCommand(l, remove, binding[ops.LinkRemoveInput, ops.LinkOutput]{
			Use:  "remove <id> <label>",
			Args: cobra.MinimumNArgs(2),
			Bind: func(args []string, in *ops.LinkRemoveInput) error {
				in.ID, in.Label = args[0], strings.Join(args[1:], " ")
				return nil
			},
			Text: text,
		}),
	)
	return cmd
}

func newWhereCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Where, binding[ops.WhereInput, ops.WhereOutput]{
		Use:  "where <text>",
		Args: cobra.MinimumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.WhereInput) {
			fs.BoolVar(&in.All, "all", false, "include archived projects")
		},
		Bind: func(args []string, in *ops.WhereInput) error {
			in.Query = strings.Join(args, " ")
			return nil
		},
		Text: func(w io.Writer, out ops.WhereOutput) error {
			p := &printer{w: w}
			for _, m := range out.Matches {
				p.f("%s\t%s\t%s\n", m.ID, m.Type, m.Title)
				for _, k := range m.Links {
					p.f("  %s\t%s\t%s\n", k.Label, k.Kind, k.Target)
				}
				for _, v := range m.Incidents {
					p.f("  incident\t%s\t%s\t%s\t%s\n", v.ID, v.Severity, v.Status, v.Title)
				}
				if len(m.Questions) > 0 {
					writeQuestions(p, "  question\t", m.Questions)
				}
			}
			return p.err
		},
	})
}
