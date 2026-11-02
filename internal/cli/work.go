package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/ops"
)

func printID[O any](id func(O) string) func(io.Writer, O) error {
	return func(w io.Writer, out O) error {
		_, err := fmt.Fprintln(w, id(out))
		return err
	}
}

func joinOrDash(s []string) string {
	if len(s) == 0 {
		return "-"
	}
	return strings.Join(s, ",")
}

func writeTasks(w io.Writer, tasks []ops.TaskView) error {
	for _, t := range tasks {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.ID, t.Status, t.Project, t.Title); err != nil {
			return err
		}
	}
	return nil
}

func newProjectCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Projects", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.ProjectAdd, binding[ops.ProjectAddInput, ops.ProjectAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ProjectAddInput) {
				fs.StringVar(&in.Owner, "owner", "", "project owner (required)")
				fs.StringVar(&in.Body, "body", "", "text after the title")
			},
			Bind: func(args []string, in *ops.ProjectAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: printID(func(o ops.ProjectAddOutput) string { return o.ID }),
		}),
		opCommand(l, ops.ProjectList, binding[ops.ProjectListInput, ops.ProjectListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ProjectListInput) {
				fs.BoolVar(&in.All, "all", false, "include archived projects")
			},
			Text: func(w io.Writer, out ops.ProjectListOutput) error {
				for _, p := range out.Projects {
					if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, p.Status, p.Owner, p.Title); err != nil {
						return err
					}
				}
				return nil
			},
		}),
		opCommand(l, ops.ProjectShow, binding[ops.ProjectShowInput, ops.ProjectShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ProjectShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ProjectShowOutput) error {
				p := out.Project
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Status, p.Owner, p.Folder, p.Title); err != nil {
					return err
				}
				return writeTasks(w, out.Tasks)
			},
		}),
		opCommand(l, ops.ProjectClose, binding[ops.ProjectCloseInput, ops.ProjectCloseOutput]{
			Use:  "close <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ProjectCloseInput) {
				fs.BoolVar(&in.Dropped, "dropped", false, "close as dropped, not done")
				fs.BoolVar(&in.Force, "force", false, "close with open tasks")
			},
			Bind: func(args []string, in *ops.ProjectCloseInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ProjectCloseOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\n", out.ID, out.Status, out.Folder)
				return err
			},
		}),
	)
	return cmd
}

func changeText(w io.Writer, out ops.TaskChangeOutput) error {
	_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, out.Status)
	return err
}

func newTaskCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Tasks", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.TaskAdd, binding[ops.TaskAddInput, ops.TaskAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskAddInput) {
				fs.StringVarP(&in.Project, "project", "p", "", "project id (required)")
				fs.StringSliceVar(&in.DependsOn, "depends-on", nil, "task or rfx id this task waits on (repeatable)")
				fs.StringVar(&in.Body, "body", "", "text after the title")
			},
			Bind: func(args []string, in *ops.TaskAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: printID(func(o ops.TaskAddOutput) string { return o.ID }),
		}),
		opCommand(l, ops.TaskList, binding[ops.TaskListInput, ops.TaskListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.TaskListInput) {
				fs.StringVarP(&in.Project, "project", "p", "", "only this project")
				fs.StringVar((*string)(&in.Status), "status", "", "only this status")
			},
			Text: func(w io.Writer, out ops.TaskListOutput) error { return writeTasks(w, out.Tasks) },
		}),
		opCommand(l, ops.TaskShow, binding[ops.TaskShowInput, ops.TaskShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.TaskShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.TaskShowOutput) error {
				t := out.Task
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\ndepends_on\t%s\nblocked_by\t%s\n%s\n",
					t.ID, t.Status, t.Title, joinOrDash(t.DependsOn), joinOrDash(t.BlockedBy), out.Body)
				return err
			},
		}),
		opCommand(l, ops.TaskStart, binding[ops.TaskStartInput, ops.TaskChangeOutput]{
			Use:  "start <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskStartInput) {
				fs.BoolVar(&in.Force, "force", false, "start with unfinished dependencies")
			},
			Bind: func(args []string, in *ops.TaskStartInput) error { in.ID = args[0]; return nil },
			Text: changeText,
		}),
		opCommand(l, ops.TaskDone, binding[ops.TaskDoneInput, ops.TaskChangeOutput]{
			Use:  "done <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.TaskDoneInput) error { in.ID = args[0]; return nil },
			Text: changeText,
		}),
		opCommand(l, ops.TaskBlock, binding[ops.TaskBlockInput, ops.TaskChangeOutput]{
			Use:  "block <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskBlockInput) {
				fs.StringVar(&in.Reason, "reason", "", "why it is blocked, who said it, where it applies (required)")
			},
			Bind: func(args []string, in *ops.TaskBlockInput) error { in.ID = args[0]; return nil },
			Text: changeText,
		}),
	)
	return cmd
}

func newGraphCmd(l *loader) *cobra.Command {
	var mermaid bool
	cmd := opCommand(l, ops.Graph, binding[ops.GraphInput, ops.GraphOutput]{
		Use:  "graph [project]",
		Args: cobra.MaximumNArgs(1),
		Flags: func(fs *pflag.FlagSet, _ *ops.GraphInput) {
			fs.BoolVar(&mermaid, "mermaid", false, "print mermaid source (ignored with --json)")
		},
		Bind: func(args []string, in *ops.GraphInput) error {
			if len(args) == 1 {
				in.Project = args[0]
			}
			return nil
		},
		Text: func(w io.Writer, out ops.GraphOutput) error {
			if mermaid {
				_, err := io.WriteString(w, ops.Mermaid(out))
				return err
			}
			for _, n := range out.Nodes {
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\twaiting_on=%s\tblocked_by=%s\n",
					n.ID, n.Type, n.State, joinOrDash(n.WaitingOn), joinOrDash(n.BlockedBy)); err != nil {
					return err
				}
			}
			for _, e := range out.Edges {
				if _, err := fmt.Fprintf(w, "%s -> %s\n", e.From, e.To); err != nil {
					return err
				}
			}
			return nil
		},
	})
	return cmd
}

func newNextCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Next, binding[ops.NextInput, ops.NextOutput]{
		Use:  "next [project]",
		Args: cobra.MaximumNArgs(1),
		Bind: func(args []string, in *ops.NextInput) error {
			if len(args) == 1 {
				in.Project = args[0]
			}
			return nil
		},
		Text: func(w io.Writer, out ops.NextOutput) error { return writeTasks(w, out.Tasks) },
	})
}
