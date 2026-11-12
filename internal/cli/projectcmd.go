package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newProjectCmd is `para project`: ongoing projects (vault format 11). A
// finite outcome is a goal: `para goal`.
func newProjectCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Ongoing projects: a state line and goals in flight, no done condition (goals: para goal)", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.ProjectAdd, binding[ops.ProjectAddInput, ops.ProjectAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ProjectAddInput) {
				fs.StringVar(&in.Owner, "owner", "", "project owner (default: the vault's owner property, else self)")
				fs.StringVar(&in.Area, "area", "", "area id, a facet")
				fs.StringVar(&in.StateLine, "state", "", "one line on where the project stands")
				fs.StringVar(&in.Body, "body", "", "text after the title")
			},
			Bind: func(args []string, in *ops.ProjectAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: printID(func(o ops.ProjectAddOutput) string { return o.ID }),
		}),
		opCommand(l, ops.ProjectShow, binding[ops.ProjectShowInput, ops.ProjectShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ProjectShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ProjectShowOutput) error {
				p := out.Project
				pr := &printer{w: w}
				pr.f("%s\t%s\t%s\t%s\n", p.ID, p.Status, p.Owner, p.Title)
				if p.StateLine != "" {
					pr.f("state\t%s\t%s\n", p.StateLine, p.StateDate)
				}
				writeLinks(pr, p.Links)
				if out.LastActivity != "" {
					pr.f("last activity\t%s\n", out.LastActivity)
				}
				for _, g := range out.Goals {
					pr.f("goal\t%s\t%s\t%s\n", g.ID, g.Status, g.Title)
					for _, t := range g.Tasks {
						pr.f("  task\t%s\t%s\t%s\n", t.ID, t.Status, t.Title)
					}
				}
				for _, sec := range []struct {
					name  string
					items []ops.BundleItem
				}{{"waits on", out.WaitsOn}, {"parked", out.Icebox}, {"linked", out.Direct}} {
					for _, it := range sec.items {
						pr.f("%s\t%s\t%s\t%s\t%s\n", sec.name, it.ID, it.Type, it.Title, it.Note)
					}
				}
				for _, h := range out.History {
					pr.f("history\t%s\t%s\t%s\n", h.At, h.Kind, h.Summary)
				}
				for _, m := range out.More {
					pr.f("%s\n", m)
				}
				return pr.err
			},
		}),
		opCommand(l, ops.ProjectRetire, binding[ops.ProjectRetireInput, ops.ProjectRetireOutput]{
			Use:  "retire <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ProjectRetireInput) error { in.ID = args[0]; return nil },
			Text: printID(func(o ops.ProjectRetireOutput) string { return o.ID }),
		}),
	)
	return cmd
}
