package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
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
	if t.Returns != "" {
		return "\t(returns " + t.Returns + ")"
	}
	if t.Deferred {
		if len(t.WaitsOn) > 0 {
			return "\t(later, waits on: " + strings.Join(namesOrIDs(t.WaitsOnNames, t.WaitsOn), ", ") + ")"
		}
		if t.LaterText != "" {
			return "\t(later " + t.LaterText + ")"
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

// scope is where a task lives: its goal, else its area.
func scope(t ops.TaskView) string {
	if t.Goal != "" {
		return t.Goal
	}
	return t.Area
}

// goalCommands are the goal commands of `para goal` (newGoalCmd): goals are
// bounded outcomes with a done condition.
func goalCommands(l *loader) []*cobra.Command {
	return []*cobra.Command{
		newLinkCmd(l, ops.GoalLinkAdd, ops.GoalLinkRemove),
		opCommand(l, ops.GoalAdd, binding[ops.GoalAddInput, ops.GoalAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.GoalAddInput) {
				fs.StringVar(&in.Owner, "owner", "", "goal owner (default: the vault's owner property, else self)")
				fs.StringVar(&in.Area, "area", "", "id of the goal's home, an area of kind project")
				fs.StringVar(&in.Project, "project", "", "id of the ongoing project the goal belongs to (vault format 11)")
				fs.StringSliceVar(&in.Concepts, "concepts", nil, "ids of concept areas the goal draws on (repeat or comma-separate)")
				fs.StringVar(&in.Body, "body", "", "text after the title")
				fs.StringVar(&in.DoneWhen, "done-when", "", "how we know the goal is reached (required); a thing that never finishes is a project or an area")
				fs.StringVar(&in.Due, "due", "", "YYYY-MM-DD the goal is due")
			},
			Bind: func(args []string, in *ops.GoalAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: printID(func(o ops.GoalAddOutput) string { return o.ID }),
		}),
		opCommand(l, ops.GoalEdit, binding[ops.GoalEditInput, ops.GoalEditOutput]{
			Use:  "edit <id> [--done-when <text>] [--due <date>] [--area <id>] [--concepts <ids>] [--status active|paused|parked --resume-when <text>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.GoalEditInput) {
				fs.StringVar(&in.DoneWhen, "done-when", "", "how we know the goal is reached (required unless you change only the date, area, concepts, or status)")
				fs.StringVar(&in.Due, "due", "", "YYYY-MM-DD the goal is due")
				fs.BoolVar(&in.ClearDue, "clear-due", false, "remove the due date")
				fs.StringVar(&in.Area, "area", "", "id of the goal's home, an area of kind project")
				fs.StringSliceVar(&in.Concepts, "concepts", nil, "ids of concept areas the goal draws on; replaces the list")
				fs.StringVar((*string)(&in.Status), "status", "", "active, paused, or parked (the icebox; needs --resume-when); close ends a goal")
				fs.StringVar(&in.ResumeWhen, "resume-when", "", "parked: what the goal waits on before it resumes")
			},
			Bind: func(args []string, in *ops.GoalEditInput) error { in.ID = args[0]; return nil },
			Text: printID(func(o ops.GoalEditOutput) string { return o.ID }),
		}),
		opCommand(l, ops.GoalList, binding[ops.GoalListInput, ops.GoalListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.GoalListInput) {
				fs.BoolVar(&in.All, "all", false, "include archived goals")
			},
			Text: func(w io.Writer, out ops.GoalListOutput) error {
				for _, p := range out.Goals {
					if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, p.Status, p.Owner, p.Title); err != nil {
						return err
					}
				}
				return nil
			},
		}),
		opCommand(l, ops.GoalShow, binding[ops.GoalShowInput, ops.GoalShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.GoalShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.GoalShowOutput) error {
				p := out.Goal
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Status, p.Owner, p.Folder, p.Title); err != nil {
					return err
				}
				pr := &printer{w: w}
				if p.DoneWhen != "" {
					pr.f("done when\t%s\n", p.DoneWhen)
				} else {
					pr.f("done when\t(none: set one with para goal edit %s --done-when <text>)\n", p.ID)
				}
				writeLinks(pr, p.Links)
				writeBrief(pr, p.Brief)
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
		opCommand(l, ops.GoalClose, binding[ops.GoalCloseInput, ops.GoalCloseOutput]{
			Use:  "close <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.GoalCloseInput) {
				fs.BoolVar(&in.Dropped, "dropped", false, "close as dropped, not done")
				fs.BoolVar(&in.Force, "force", false, "close with open tasks")
			},
			Bind: func(args []string, in *ops.GoalCloseInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.GoalCloseOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\n", out.ID, out.Status, out.Folder)
				return err
			},
		}),
	}
}

func changeText(w io.Writer, out ops.TaskChangeOutput) error {
	_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, out.Status)
	return err
}

func newTaskCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Tasks", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.TaskMove, binding[ops.TaskMoveInput, ops.TaskMoveOutput]{
			Use: "move <id>... (--goal <id> | --area <id>)", Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskMoveInput) {
				fs.StringVarP(&in.Goal, "goal", "p", "", "move to this goal id")
				fs.StringVar(&in.Area, "area", "", "move to this area id")
			},
			Bind: func(args []string, in *ops.TaskMoveInput) error { in.IDs = args; return nil },
			Text: func(w io.Writer, out ops.TaskMoveOutput) error {
				_, err := fmt.Fprintln(w, strings.Join(out.Moved, "\n"))
				return err
			},
		}),
		opCommand(l, ops.TaskAdd, binding[ops.TaskAddInput, ops.TaskAddOutput]{
			Use:  "add <title> | --title <title>",
			Args: cobra.ArbitraryArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.TaskAddInput) {
				fs.StringVar(&in.Title, "title", "", "the title (same as <title>)")
				fs.StringVarP(&in.Goal, "goal", "p", "", "goal id (a task needs a goal or an area)")
				fs.StringVar(&in.Area, "area", "", "area id")
				fs.StringVar(&in.Due, "due", "", "due date YYYY-MM-DD")
				fs.StringVar(&in.Recur, "recur", "", "recurrence: daily, weekly, monthly, quarterly, yearly, or \"every <n> days\"; done makes the next task")
				fs.StringVar(&in.Assignee, "assignee", "", "self (default), a person id, or agent:<name>")
				fs.StringVar(&in.RequestedBy, "requested-by", "", "who it is for: a person or group id, or free text")
				fs.StringVar(&in.Incident, "incident", "", "incident id this task follows up (the task goes in the incident's area unless you name a project or area)")
				fs.StringSliceVar(&in.DependsOn, "depends-on", nil, "id this task waits on: a task, obligation, rfx, question, goal, agent work, or io item (an outgoing draft), in any project or area (repeatable)")
				fs.StringVar(&in.Parent, "parent", "", "task or obligation id this is an action item of: prep for a meeting or a promise, or what came out of it (the parent does not wait on it)")
				fs.BoolVar(&in.Commitment, "commitment", false, "same as --kind commitment: owed to someone; needs --due")
				fs.StringVar((*string)(&in.Kind), "kind", "", "task (default), meeting, call, work_with, or commitment; all but task are with someone (--with)")
				fs.StringVar(&in.Scheduled, "scheduled", "", "when it happens: a date YYYY-MM-DD, or a start with a zone such as 2026-11-04T10:00:00-05:00 (not --due)")
				fs.StringVar(&in.End, "end", "", "end time with a zone; needs a timed --scheduled")
				fs.StringVar(&in.CalendarUID, "calendar-uid", "", "kind meeting or call: UID of its event in your calendar")
				fs.Var(&calendarEventsFlag{&in.CalendarUIDs}, "calendar-event", "kind meeting or call: another calendar event of it, <uid>=<role> (repeatable)")
				fs.StringSliceVar(&in.With, "with", nil, "person or group id the work is with (repeatable)")
				fs.StringVar((*string)(&in.Effort), "effort", "", effortHelp)
				fs.StringVar(&in.Body, "body", "", "text after the title")
				fs.StringVar(&in.DoneWhen, "done-when", "", "how we know it is done (optional for you; an agent must give it)")
				fs.StringVar(&in.DoURL, "do-url", "", "the page where it is done")
				fs.StringVar(&in.DoCommand, "do-command", "", "the exact command to run (no secret in it)")
				fs.StringVar(&in.Steps, "steps", "", "1 to 5 steps, a markdown list")
			},
			Bind: func(args []string, in *ops.TaskAddInput) error {
				switch {
				case len(args) > 0 && in.Title != "":
					return fmt.Errorf("task add: give <title> or --title, not both")
				case len(args) > 0:
					in.Title = strings.Join(args, " ")
				case strings.TrimSpace(in.Title) == "":
					return fmt.Errorf("task add: a title is required: <title> or --title")
				}
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
				fs.StringVarP(&in.Goal, "goal", "p", "", "only this goal")
				fs.StringVar(&in.Area, "area", "", "only tasks with this area of their own")
				fs.StringVar((*string)(&in.Status), "status", "", "only this status")
				fs.BoolVar(&in.Open, "open", false, "only open tasks: todo, doing, blocked")
				fs.StringVar(&in.Assignee, "assignee", "", "only tasks assigned to this person id or agent:<name>; self for your own")
				fs.StringVar(&in.Incident, "incident", "", "only follow-ups of this incident")
				fs.BoolVar(&in.Deferred, "deferred", false, "include the tasks put off with defer (Later); they are marked")
				fs.StringVar((*string)(&in.Outcome), "outcome", "", "only closed tasks with this outcome, such as failed")
			},
			Text: func(w io.Writer, out ops.TaskListOutput) error {
				if err := writeTasks(w, out.Tasks); err != nil {
					return err
				}
				if out.DeferredHidden > 0 {
					_, err := fmt.Fprintf(w, "%d shown; %d put off with defer (Later) not shown, use --deferred\n", len(out.Tasks), out.DeferredHidden)
					return err
				}
				return nil
			},
		}),
		opCommand(l, ops.TaskShow, binding[ops.TaskShowInput, ops.TaskShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.TaskShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.TaskShowOutput) error {
				t := out.Task
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\nproject\t%s\narea\t%s\ndue\t%s\nrecur\t%s\nprevious\t%s\nincident\t%s\nassignee\t%s\ndepends_on\t%s\nblocked_by\t%s\ndone_when\t%s\n%s\n",
					t.ID, t.Status, t.Title, dash(t.Goal), dash(t.Area), dash(string(t.Due)), dash(t.Recur), dash(t.Previous), dash(t.Incident), orSelf(t.Assignee), joinOrDash(t.DependsOn), joinOrDash(t.BlockedBy), dash(t.DoneWhen), out.Body)
				if err == nil && (t.DoURL != "" || t.DoCommand != "" || t.Steps != "") {
					_, err = fmt.Fprintf(w, "do_url\t%s\ndo_command\t%s\nsteps\t%s\n", dash(t.DoURL), dash(t.DoCommand), dash(t.Steps))
				}
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
			Use:  "edit <id> [--done-when <text>] [--effort xs|s|m|l] [--do-url <url>] [--do-command <cmd>] [--steps <list>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskEditInput) {
				fs.StringVar(&in.DoneWhen, "done-when", "", "how we know the task is done (one of --done-when and --effort is required)")
				fs.StringVar((*string)(&in.Effort), "effort", "", effortHelp)
				fs.StringVar(&in.DoURL, "do-url", "", "the page where it is done")
				fs.StringVar(&in.DoCommand, "do-command", "", "the exact command to run (no secret in it)")
				fs.StringVar(&in.Steps, "steps", "", "1 to 5 steps, a markdown list")
				fs.BoolVar(&in.ClearHow, "clear-how", false, "remove do-url, do-command, and steps")
				fs.StringVar(&in.RequestedBy, "requested-by", "", "who it is for: a person or group id, or free text")
				fs.BoolVar(&in.ClearRequestedBy, "clear-requested-by", false, "remove requested_by")
				fs.StringVar(&in.Parent, "parent", "", "task or obligation id this is an action item of (prep for a meeting or a promise)")
				fs.BoolVar(&in.ClearParent, "clear-parent", false, "remove parent")
			},
			Bind: func(args []string, in *ops.TaskEditInput) error { in.ID = args[0]; return nil },
			Text: printID(func(o ops.TaskEditOutput) string { return o.ID }),
		}),
		opCommand(l, ops.TaskDue, binding[ops.TaskDueInput, ops.TaskDueOutput]{
			Use:  "due <id> [date]",
			Args: cobra.RangeArgs(1, 2),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskDueInput) {
				fs.BoolVar(&in.Clear, "clear", false, "remove the due date (and the recurrence)")
				fs.StringVar(&in.Recur, "recur", "", "recurrence: daily, weekly, monthly, quarterly, yearly, or \"every <n> days\"; done makes the next task")
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
		opCommand(l, ops.TaskAttempt, binding[ops.TaskAttemptInput, ops.TaskAttemptOutput]{
			Use:  "attempt <id> --result no_answer|left_message|reached [--retry <when>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskAttemptInput) {
				fs.StringVar(&in.Result, "result", "", "no_answer, left_message, or reached")
				fs.StringVar(&in.Retry, "retry", "", "when to try again: a date, or a time with a zone; needed unless reached")
				fs.StringVar(&in.Note, "note", "", "one line to keep with the attempt")
			},
			Bind: func(args []string, in *ops.TaskAttemptInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.TaskAttemptOutput) error {
				p := &printer{w: w}
				p.f("%s\t%d\t%s\t%s\n", out.ID, out.Attempts, out.Result, out.Scheduled)
				if out.Stuck {
					p.f("stuck: %d attempts without reaching them\n", out.Attempts)
				}
				return p.err
			},
		}),
		opCommand(l, ops.TaskToAsks, binding[ops.TaskToAsksInput, ops.TaskToAsksOutput]{
			Use:  "to-asks <id> --person <id>...",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskToAsksInput) {
				fs.StringArrayVar(&in.People, "person", nil, "person or group id the task waits on, one Ask each (repeatable)")
				fs.StringVar(&in.Route, "route", "", "the domain; default each person's team, role, or org")
				fs.StringVar((*string)(&in.Medium), "medium", "", "how each Ask goes; default each person's preferred contact")
				fs.StringVar(&in.NextNudge, "next-nudge", "", "follow-up date YYYY-MM-DD; default the task's due date")
			},
			Bind: func(args []string, in *ops.TaskToAsksInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.TaskToAsksOutput) error {
				p := &printer{w: w}
				p.f("%s\tasks: %s\tmoved: %s\n", out.ID, strings.Join(out.Asks, ","), strings.Join(out.Moved, ","))
				for _, x := range out.Warnings {
					p.f("warning: %s\n", x)
				}
				return p.err
			},
		}),
		opCommand(l, ops.TaskResolve, binding[ops.TaskResolveInput, ops.TaskResolveOutput]{
			Use:  "resolve <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskResolveInput) {
				fs.StringVar((*string)(&in.Outcome), "outcome", "done", "done, partial, dropped, duplicate, superseded, declined, failed, or rerouted")
				fs.StringVar(&in.Note, "note", "", "how it ended; partial needs this or a follow-up, dropped needs it")
				fs.StringVar(&in.Link, "link", "", "duplicate or superseded: the id of the item that replaces it")
				fs.StringArrayVar(&in.FollowUps, "follow-up", nil, "title of a new task in the same goal or area; dependents move to the first (repeatable)")
				fs.StringVar(&in.DoneWhen, "done-when", "", "change the done condition first; the old text is kept under ## Outcome")
				fs.StringVar(&in.Due, "due", "", "due date YYYY-MM-DD of the follow-ups")
				fs.StringVar(&in.Reason, "reason", "", "declined or rerouted: why")
				fs.StringVar((*string)(&in.DeclinedBy), "declined-by", "", "declined: owner or other")
				fs.StringVar(&in.DeclinedPerson, "declined-person", "", "declined: id of the person who said no")
				fs.StringVar(&in.Source, "source", "", "declined: where the no was said, a document id or URL")
				fs.StringVar(&in.Date, "date", "", "declined: the day the no was said, YYYY-MM-DD")
				fs.StringVar(&in.Tried, "tried", "", "failed: what was tried")
				fs.StringVar(&in.StoppedBecause, "stopped-because", "", "failed: why it stopped")
				fs.StringVar(&in.RetryAfter, "retry-after", "", "failed: the day it may be tried again, YYYY-MM-DD")
				fs.StringVar(&in.To, "to", "", "rerouted: a vault id, or the URL of an outside system")
			},
			Bind: func(args []string, in *ops.TaskResolveInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.TaskResolveOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\n", out.ID, out.Status, out.Outcome)
				for _, id := range out.FollowUps {
					p.f("follow-up\t%s\n", id)
				}
				for _, id := range out.Moved {
					p.f("moved\t%s\n", id)
				}
				for _, id := range out.Released {
					p.f("released\t%s\n", id)
				}
				for _, a := range out.Asks {
					p.f("ask\t%s\t%s\t%s\n", a.ID, a.Action, a.Relation)
				}
				if out.Next != "" {
					p.f("next\t%s\n", out.Next)
				}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				return p.err
			},
		}),
		taskSplitCmd(l),
		opCommand(l, ops.TaskDelegate, binding[ops.TaskDelegateInput, ops.TaskDelegateOutput]{
			Use:  "delegate <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskDelegateInput) {
				fs.StringVar(&in.Agent, "agent", "", "agent name; default request.runner")
				fs.StringVar(&in.Hint, "hint", "", "where to start, in words")
			},
			Bind: func(args []string, in *ops.TaskDelegateInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.TaskDelegateOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\t%s\n", out.ID, out.Request, out.Status, dash(out.Assignee))
				if out.Appended {
					p.f("hint added to open work\n")
				}
				if out.Note != "" {
					p.f("note: %s\n", out.Note)
				}
				return p.err
			},
		}),
		opCommand(l, ops.TaskUpdate, binding[ops.TaskUpdateInput, ops.TaskUpdateOutput]{
			Use:  "update <id> [text]",
			Args: cobra.MinimumNArgs(1),
			Bind: func(args []string, in *ops.TaskUpdateInput) error {
				in.ID, in.Text = args[0], strings.Join(args[1:], " ")
				return nil
			},
			Stdin: func(r io.Reader, in *ops.TaskUpdateInput) error {
				if in.Text != "" {
					return nil
				}
				text, err := readStdin(r)
				in.Text = text
				return err
			},
			Text: func(w io.Writer, out ops.TaskUpdateOutput) error {
				p := &printer{w: w}
				p.f("%s\tsaved\t%s\n", out.ID, dash(out.Request))
				if out.Appended {
					p.f("added to open sort work\n")
				}
				if out.Note != "" {
					p.f("note: %s\n", out.Note)
				}
				return p.err
			},
		}),
		opCommand(l, ops.TaskBlock, binding[ops.TaskBlockInput, ops.TaskChangeOutput]{
			Use:  "block <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.TaskBlockInput) {
				fs.StringVar(&in.Reason, "reason", "", "why it is blocked, who said it, where it applies (required)")
				fs.StringVar(&in.Until, "until", "", "follow-up date YYYY-MM-DD: the task leaves next and the agenda until then (sets defer_until)")
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
				in.Goal = args[0]
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
				in.Goal = args[0]
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

// taskSplitCmd is `para task split`: --done and --evidence pair by order,
// --due is the due date of every remaining part.
func taskSplitCmd(l *loader) *cobra.Command {
	var done, evidence, remaining []string
	var due string
	return opCommand(l, ops.TaskSplit, binding[ops.TaskSplitInput, ops.TaskSplitOutput]{
		Use:  "split <id> --remaining <title>... [--done <title> --evidence <locator>]...",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.TaskSplitInput) {
			fs.StringArrayVar(&done, "done", nil, "title of a part that is done: it closes done at once (repeatable)")
			fs.StringArrayVar(&evidence, "evidence", nil, "what shows the done part of the same position done: a URL, vault id, or file (repeatable)")
			fs.StringArrayVar(&remaining, "remaining", nil, "title of a part that remains: an open task the task waits on (repeatable)")
			fs.StringVar(&due, "due", "", "due date YYYY-MM-DD of the remaining parts")
			fs.StringVar(&in.Title, "title", "", "a new title for the task")
			fs.StringVar(&in.Reason, "reason", "", "what is done and what remains")
		},
		Bind: func(args []string, in *ops.TaskSplitInput) error {
			in.ID = args[0]
			if len(evidence) > len(done) {
				return fmt.Errorf("%d --evidence for %d --done", len(evidence), len(done))
			}
			in.Done, in.Remaining = nil, nil
			for i, t := range done {
				p := ops.SplitDone{Title: t}
				if i < len(evidence) {
					p.Evidence = &model.Evidence{Locator: evidence[i]}
				}
				in.Done = append(in.Done, p)
			}
			for _, t := range remaining {
				in.Remaining = append(in.Remaining, ops.SplitRemaining{Title: t, Due: due})
			}
			return nil
		},
		Text: func(w io.Writer, out ops.TaskSplitOutput) error {
			p := &printer{w: w}
			p.f("%s\tsplit\n", out.ID)
			for _, id := range out.Done {
				p.f("done\t%s\n", id)
			}
			for _, id := range out.Remaining {
				p.f("remaining\t%s\n", id)
			}
			return p.err
		},
	})
}
