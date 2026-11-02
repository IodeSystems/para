package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/ops"
)

func newAreaCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "area", Short: "Areas: ongoing responsibilities", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.AreaAdd, binding[ops.AreaAddInput, ops.AreaAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.AreaAddInput) {
				fs.StringVar(&in.ID, "id", "", "document id (default area-<slug of title>)")
				fs.StringVar((*string)(&in.Status), "status", "", "active or paused (default active)")
				fs.StringVar(&in.Body, "body", "", "text after the title")
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
					p.f("%s\t%s\t%s\n", a.ID, a.Status, a.Title)
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
				p.f("projects\n")
				for _, pr := range out.Projects {
					p.f("  %s\t%s\t%s\t%s\n", pr.ID, pr.Status, pr.Owner, pr.Title)
				}
				p.f("tasks\n")
				if p.err == nil {
					p.err = writeTasks(w, out.Tasks)
				}
				return p.err
			},
		}),
	)
	return cmd
}
