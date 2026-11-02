package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/model"
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
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s%s\n", t.ID, t.Status, scope(t), dash(string(t.Due)), t.Title, assigneeSuffix(t)+laterSuffix(t)); err != nil {
			return err
		}
	}
	return nil
}

// laterSuffix marks a task a person put off.
func laterSuffix(t ops.TaskView) string {
	if t.Deferred {
		if len(t.WaitsOn) > 0 {
			return "\t(later, waits on: " + strings.Join(namesOrIDs(t.WaitsOnNames, t.WaitsOn), ", ") + ")"
		}
		return "\t(later: " + t.DeferUntil + ")"
	}
	return ""
}

// assigneeSuffix marks a task that is someone else's.
func assigneeSuffix(t ops.TaskView) string {
	if ops.IsOther(t.Assignee) {
		return "\t@" + t.Assignee
	}
	return ""
}

// scope is where a task lives: its project, else its area.
func scope(t ops.TaskView) string {
	if t.Project != "" {
		return t.Project
	}
	return t.Area
}

func newProjectCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Goals (projects): bounded outcomes with a done condition", Args: cobra.NoArgs}
	cmd.AddCommand(
		newLinkCmd(l, ops.ProjectLinkAdd, ops.ProjectLinkRemove),
		opCommand(l, ops.ProjectAdd, binding[ops.ProjectAddInput, ops.ProjectAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ProjectAddInput) {
				fs.StringVar(&in.Owner, "owner", "", "project owner (default: the vault's owner property, else self)")
				fs.StringVar(&in.Area, "area", "", "area id")
				fs.StringVar(&in.Body, "body", "", "text after the title")
				fs.StringVar(&in.DoneWhen, "done-when", "", "how we know the goal is reached (required); a goal that never finishes is an area")
				fs.StringVar(&in.Due, "due", "", "YYYY-MM-DD the goal is due")
			},
			Bind: func(args []string, in *ops.ProjectAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: printID(func(o ops.ProjectAddOutput) string { return o.ID }),
		}),
		opCommand(l, ops.ProjectEdit, binding[ops.ProjectEditInput, ops.ProjectEditOutput]{
			Use:  "edit <id> [--done-when <text>] [--due <date>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ProjectEditInput) {
				fs.StringVar(&in.DoneWhen, "done-when", "", "how we know the goal is reached (required unless you only change the date)")
				fs.StringVar(&in.Due, "due", "", "YYYY-MM-DD the goal is due")
				fs.BoolVar(&in.ClearDue, "clear-due", false, "remove the due date")
			},
			Bind: func(args []string, in *ops.ProjectEditInput) error { in.ID = args[0]; return nil },
			Text: printID(func(o ops.ProjectEditOutput) string { return o.ID }),
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
				pr := &printer{w: w}
				if p.DoneWhen != "" {
					pr.f("done when\t%s\n", p.DoneWhen)
				} else {
					pr.f("done when\t(none: set one with para project edit %s --done-when <text>)\n", p.ID)
				}
				writeLinks(pr, p.Links)
				if len(out.Questions) > 0 {
					pr.f("questions\n")
					writeQuestions(pr, "  ", out.Questions)
				}
				if pr.err != nil {
					return pr.err
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
				fs.StringVarP(&in.Project, "project", "p", "", "project id (a task needs a project or an area)")
				fs.StringVar(&in.Area, "area", "", "area id")
				fs.StringVar(&in.Due, "due", "", "due date YYYY-MM-DD")
				fs.StringVar(&in.Recur, "recur", "", "recurrence: daily, weekly, monthly, or \"every <n> days\"; done makes the next task")
				fs.StringVar(&in.Assignee, "assignee", "", "self (default), a person id, or agent:<name>")
				fs.StringVar(&in.Incident, "incident", "", "incident id this task follows up (the task goes in the incident's area unless you name a project or area)")
				fs.StringSliceVar(&in.DependsOn, "depends-on", nil, "task or rfx id this task waits on, in any project or area (repeatable)")
				fs.StringVar(&in.Body, "body", "", "text after the title")
				fs.StringVar(&in.DoneWhen, "done-when", "", "how we know it is done (optional for you; an agent must give it)")
			},
			Bind: func(args []string, in *ops.TaskAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: func(w io.Writer, out ops.TaskAddOutput) error {
				p := &printer{w: w}
				p.f("%s\n", out.ID)
				if out.Request != "" {
					p.f("request\t%s\n", out.Request)
				}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				return p.err
			},
		}),
		opCommand(l, ops.TaskList, binding[ops.TaskListInput, ops.TaskListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.TaskListInput) {
				fs.StringVarP(&in.Project, "project", "p", "", "only this project")
				fs.StringVar(&in.Area, "area", "", "only tasks with this area of their own")
				fs.StringVar((*string)(&in.Status), "status", "", "only this status")
				fs.StringVar(&in.Assignee, "assignee", "", "only tasks assigned to this person id or agent:<name>; self for your own")
				fs.StringVar(&in.Incident, "incident", "", "only follow-ups of this incident")
				fs.BoolVar(&in.Deferred, "deferred", false, "include the tasks put off with defer (Later); they are marked")
			},
			Text: func(w io.Writer, out ops.TaskListOutput) error { return writeTasks(w, out.Tasks) },
		}),
		opCommand(l, ops.TaskShow, binding[ops.TaskShowInput, ops.TaskShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.TaskShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.TaskShowOutput) error {
				t := out.Task
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\nproject\t%s\narea\t%s\ndue\t%s\nrecur\t%s\nprevious\t%s\nincident\t%s\nassignee\t%s\ndepends_on\t%s\nblocked_by\t%s\ndone_when\t%s\n%s\n",
					t.ID, t.Status, t.Title, dash(t.Project), dash(t.Area), dash(string(t.Due)), dash(t.Recur), dash(t.Previous), dash(t.Incident), orSelf(t.Assignee), joinOrDash(t.DependsOn), joinOrDash(t.BlockedBy), dash(t.DoneWhen), out.Body)
				if err == nil && len(out.Questions) > 0 {
					p := &printer{w: w}
					p.f("questions\n")
					writeQuestions(p, "  ", out.Questions)
					err = p.err
				}
				return err
			},
		}),
		opCommand(l, ops.TaskEdit, binding[ops.TaskEditInput, ops.TaskEditOutput]{
			Use:  "edit <id> --done-when <text>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskEditInput) {
				fs.StringVar(&in.DoneWhen, "done-when", "", "how we know the task is done (required)")
			},
			Bind: func(args []string, in *ops.TaskEditInput) error { in.ID = args[0]; return nil },
			Text: printID(func(o ops.TaskEditOutput) string { return o.ID }),
		}),
		opCommand(l, ops.TaskDue, binding[ops.TaskDueInput, ops.TaskDueOutput]{
			Use:  "due <id> [date]",
			Args: cobra.RangeArgs(1, 2),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskDueInput) {
				fs.BoolVar(&in.Clear, "clear", false, "remove the due date (and the recurrence)")
				fs.StringVar(&in.Recur, "recur", "", "recurrence: daily, weekly, monthly, or \"every <n> days\"; done makes the next task")
			},
			Bind: func(args []string, in *ops.TaskDueInput) error {
				in.ID = args[0]
				if len(args) == 2 {
					in.Date = args[1]
				}
				return nil
			},
			Text: func(w io.Writer, out ops.TaskDueOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\n", out.ID, dash(string(out.Due)), dash(out.Recur))
				return err
			},
		}),
		opCommand(l, ops.TaskAssign, binding[ops.TaskAssignInput, ops.TaskAssignOutput]{
			Use:  "assign <id> <assignee>",
			Args: cobra.ExactArgs(2),
			Bind: func(args []string, in *ops.TaskAssignInput) error { in.ID, in.Assignee = args[0], args[1]; return nil },
			Text: func(w io.Writer, out ops.TaskAssignOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, orSelf(out.Assignee))
				return err
			},
		}),
		opCommand(l, ops.TaskToQuestion, binding[ops.TaskToQuestionInput, ops.TaskToQuestionOutput]{
			Use:  "to-question <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskToQuestionInput) {
				fs.StringVar(&in.Title, "title", "", "retitle the question; default the task's title")
				fs.StringVar((*string)(&in.Kind), "kind", "", "question (default), decision, or input")
				fs.StringArrayVar(&in.Options, "option", nil, "a choice; repeatable")
				fs.StringVar(&in.Recommended, "recommended", "", "the option you suggest")
			},
			Bind: func(args []string, in *ops.TaskToQuestionInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.TaskToQuestionOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\n", out.Question, out.Task)
				for _, id := range out.Relinked {
					p.f("relinked\t%s\n", id)
				}
				return p.err
			},
		}),
		newTaskDependsCmd(l),
		opCommand(l, ops.TaskStart, binding[ops.TaskStartInput, ops.TaskChangeOutput]{
			Use:  "start <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskStartInput) {
				fs.BoolVar(&in.Force, "force", false, "start with unfinished dependencies")
			},
			Bind: func(args []string, in *ops.TaskStartInput) error { in.ID = args[0]; return nil },
			Text: changeText,
		}),
		confirmDone(l, opCommand(l, ops.TaskDone, binding[ops.TaskDoneInput, ops.TaskDoneOutput]{
			Use:  "done <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskDoneInput) {
				fs.BoolVar(&in.Skip, "skip", false, "a recurring task: advance to the next one without doing this one")
				fs.StringVar(&in.Note, "note", "", "how it ended; stored under ## Outcome")
			},
			Bind: func(args []string, in *ops.TaskDoneInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.TaskDoneOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\n", out.ID, out.Status)
				if out.DoneWhen != "" {
					p.f("done when\t%s\n", out.DoneWhen)
				}
				if out.Next != "" {
					p.f("next\t%s\n", out.Next)
				}
				if out.Request != "" {
					p.f("request\t%s\n", out.Request)
				}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				return p.err
			},
		})),
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

func orSelf(a string) string {
	if a == "" {
		return ops.SelfAssignee
	}
	return a
}

func newTaskDependsCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "depends", Short: "Edit what a task waits on", Args: cobra.NoArgs}
	text := func(w io.Writer, out ops.TaskDependsOutput) error {
		_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, joinOrDash(out.DependsOn))
		return err
	}
	bind := func(args []string, in *ops.TaskDependsInput) error {
		in.ID, in.DependsOn = args[0], args[1:]
		return nil
	}
	cmd.AddCommand(
		opCommand(l, ops.TaskDependsAdd, binding[ops.TaskDependsInput, ops.TaskDependsOutput]{
			Use: "add <id> <depends-on>...", Args: cobra.MinimumNArgs(2), Bind: bind, Text: text,
		}),
		opCommand(l, ops.TaskDependsRemove, binding[ops.TaskDependsInput, ops.TaskDependsOutput]{
			Use: "remove <id> <depends-on>...", Args: cobra.MinimumNArgs(2), Bind: bind, Text: text,
		}),
	)
	return cmd
}

func newGraphCmd(l *loader) *cobra.Command {
	var mermaid bool
	cmd := opCommand(l, ops.Graph, binding[ops.GraphInput, ops.GraphOutput]{
		Use:  "graph [project]", // with no project or area: every task in the vault
		Args: cobra.MaximumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.GraphInput) {
			fs.StringVar(&in.Area, "area", "", "graph one area: its tasks and those of its projects")
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
					n.ID, graphType(n), n.State, joinOrDash(n.WaitingOn), joinOrDash(n.BlockedBy)); err != nil {
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

func newWaitingCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Waiting, binding[ops.WaitingInput, ops.WaitingOutput]{
		Use:  "waiting",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.WaitingInput) {
			fs.StringVar(&in.Today, "today", "", "date YYYY-MM-DD; default today")
		},
		Text: func(w io.Writer, out ops.WaitingOutput) error {
			for _, it := range out.RFX {
				flag := ""
				switch {
				case it.Stalled:
					flag = "\tstalled"
				case it.Due:
					flag = "\tdue"
				}
				if _, err := fmt.Fprintf(w, "rfx\t%s\t%s\t%s%s\n", it.ID, dash(it.NextNudge), it.Title, flag); err != nil {
					return err
				}
			}
			for _, t := range out.Tasks {
				if _, err := fmt.Fprintf(w, "task\t%s\t%s\t%s\t@%s\n", t.ID, dash(string(t.Due)), t.Title, t.Assignee); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// graphType is the node type, with a question's kind and scope: question:decision:project-x.
func graphType(n ops.GraphNode) string {
	if n.Type != model.TypeQuestion {
		return string(n.Type)
	}
	return string(n.Type) + ":" + string(n.Kind) + ":" + n.Scope
}

// confirmDone makes `task done` on a terminal show the task's done condition
// and ask. Without a condition, or off a terminal, it does not ask.
func confirmDone(l *loader, cmd *cobra.Command) *cobra.Command {
	run := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if skip, _ := c.Flags().GetBool("skip"); skip || len(args) != 1 || !isTerminal(c.InOrStdin()) {
			return run(c, args)
		}
		if err := l.load(); err != nil {
			return err
		}
		deps, err := l.deps()
		if err != nil {
			return err
		}
		sh, err := ops.TaskShow.Run(c.Context(), deps, ops.TaskShowInput{ID: args[0]})
		if err != nil || sh.Task.DoneWhen == "" {
			return run(c, args)
		}
		p := &printer{w: c.ErrOrStderr()}
		p.f("%s\nDone when: %s\nIs it done? [y/N] ", sh.Task.Title, sh.Task.DoneWhen)
		if p.err != nil {
			return p.err
		}
		line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			return fmt.Errorf("not marked done: the condition is not met yet")
		}
		return run(c, args)
	}
	return cmd
}

// namesOrIDs prefers the titles of ids, one per id.
func namesOrIDs(names, ids []string) []string {
	if len(names) != len(ids) {
		return ids
	}
	out := make([]string, len(ids))
	for i := range ids {
		if out[i] = names[i]; out[i] == "" {
			out[i] = ids[i]
		}
	}
	return out
}
