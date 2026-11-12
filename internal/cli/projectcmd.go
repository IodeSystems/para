package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/recur"
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
				if len(out.KPIAlerts) > 0 {
					pr.f("kpi alerts\t%s\n", strings.Join(out.KPIAlerts, "; "))
				}
				for _, k := range out.KPIs {
					pr.f("kpi\t%s\n", k)
				}
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

// newKPICmd is `para kpi`: the KPIs of a project and their readings.
func newKPICmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "kpi", Short: "A project's KPIs: define one, add a reading with its source, list them", Args: cobra.NoArgs}
	reading := &cobra.Command{Use: "reading", Short: "Readings of a project's KPIs", Args: cobra.NoArgs}
	number := func(name, s string) (*float64, error) {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("%s %q is not a number", name, s)
		}
		return &v, nil
	}
	reading.AddCommand(
		opCommand(l, ops.KPIReadingAdd, binding[ops.KPIReadingAddInput, ops.KPIReadingAddOutput]{
			Use:  "add <project> <name> <value> --source <where>",
			Args: cobra.ExactArgs(3),
			Flags: func(fs *pflag.FlagSet, in *ops.KPIReadingAddInput) {
				fs.StringVar(&in.Source, "source", "", "where it was read: a URL, a document id, a path")
				fs.StringVar(&in.Date, "date", "", "YYYY-MM-DD of the reading (default today)")
			},
			Bind: func(args []string, in *ops.KPIReadingAddInput) (err error) {
				in.ID, in.Name = args[0], args[1]
				in.Value, err = number("value", args[2])
				return err
			},
			Text: func(w io.Writer, out ops.KPIReadingAddOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\treadings %d\n", out.ID, out.Line, out.Readings)
				return p.err
			},
		}),
	)
	cmd.AddCommand(
		opCommand(l, ops.KPIAdd, binding[ops.KPIAddInput, ops.KPIAddOutput]{
			Use:  "add <project> <name> <target>",
			Args: cobra.ExactArgs(3),
			Flags: func(fs *pflag.FlagSet, in *ops.KPIAddInput) {
				fs.StringVar(&in.Unit, "unit", "", "unit of the value, such as users or ms")
				fs.StringVar((*string)(&in.Direction), "direction", "up", "up (at or above target is on), down (at or below), or hold (within --tolerance)")
				fs.Float64Var(&in.Tolerance, "tolerance", 0, "with hold: how far from target is still on")
				fs.StringVar(&in.Cadence, "cadence", "", "how often it is read: "+recur.Forms)
				fs.StringVar(&in.Owner, "owner", "", "who acts when it is off target or stale: self (a Next card), agent:<name> (agent work), or a person id (shown only)")
			},
			Bind: func(args []string, in *ops.KPIAddInput) (err error) {
				in.ID, in.Name = args[0], args[1]
				in.Target, err = number("target", args[2])
				return err
			},
			Text: func(w io.Writer, out ops.KPIAddOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\tkpis %d\n", out.ID, out.Name, out.KPIs)
				return p.err
			},
		}),
		reading,
		opCommand(l, ops.KPIList, binding[ops.KPIListInput, ops.KPIListOutput]{
			Use:  "list <project>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.KPIListInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.KPIListOutput) error {
				p := &printer{w: w}
				for _, k := range out.KPIs {
					p.f("%s\n", k.Line)
				}
				return p.err
			},
		}),
	)
	return cmd
}
