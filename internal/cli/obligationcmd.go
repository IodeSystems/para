package cli

import (
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newObligationCmd is `para obligation`: what the owner owes or must attend.
// The meeting commands are aliases of these ops.
func newObligationCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "obligation", Short: "Obligations: meetings and commitments; close them with para close", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.ObligationAdd, binding[ops.ObligationAddInput, ops.ObligationAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ObligationAddInput) {
				fs.StringVar((*string)(&in.Kind), "kind", "", "meeting or commitment; default: meeting with --with, --attendee, --rfx, or --start, else commitment")
				fs.StringVar((*string)(&in.Schedule), "schedule", "", "once, at, series, or ongoing; default from --recur, --cadence, and --start")
				fs.StringArrayVar(&in.OwedTo, "owed-to", nil, "person or group id it is owed to (repeatable)")
				fs.StringArrayVar(&in.OwedBy, "owed-by", nil, "person or group id that owes it, or agent or agent:<name>; default you (repeatable)")
				fs.StringArrayVar(&in.Interested, "interested", nil, "person or group id to tell when it changes, beside owed-to and with (repeatable)")
				fs.StringVar(&in.Parent, "parent", "", "task or obligation id it came out of (a follow-up of a meeting)")
				fs.StringVarP(&in.Goal, "project", "p", "", "goal id")
				fs.StringVar(&in.Area, "area", "", "area id")
				fs.StringVar(&in.Due, "due", "", "YYYY-MM-DD")
				fs.StringVar(&in.Start, "start", "", "RFC 3339 start with a zone; a commitment may give a date")
				fs.StringVar(&in.End, "end", "", "RFC 3339 end with a zone")
				fs.StringVar(&in.Recur, "recur", "", "series: daily, weekly, monthly, quarterly, yearly, or every <n> days")
				fs.StringVar(&in.Cadence, "cadence", "", "ongoing: how often to review it, in the recur words")
				fs.StringVar(&in.DoneWhen, "done-when", "", "how we know it is done")
				fs.StringVar(&in.Body, "body", "", "text of the obligation")
				fs.StringArrayVar(&in.With, "with", nil, "meeting: person or group id, name, or email (repeatable)")
				fs.StringArrayVar(&in.Attendees, "attendee", nil, "meeting: invitee name or email kept as text (repeatable)")
				fs.StringArrayVar(&in.RFX, "rfx", nil, "meeting: request id it should settle (repeatable)")
				fs.StringArrayVar(&in.Scope, "scope", nil, "meeting: project, area, or task id whose open questions go on the agenda (repeatable)")
				fs.StringVar(&in.CalendarUID, "calendar-uid", "", "meeting: UID of its event in your calendar")
				fs.Var(&calendarEventsFlag{&in.CalendarUIDs}, "calendar-event", "meeting: another calendar event of it: <uid>=<role> (repeatable)")
				fs.BoolVar(&in.Optional, "optional", false, "meeting: you are an optional attendee")
				in.Produce = &ops.ProduceInput{}
				fs.StringVar((*string)(&in.Produce.Kind), "produce", "", "a recurring artifact an agent owes: journal, report, message, doc, or other (with --owed-by agent:<name>)")
				fs.StringVar(&in.Produce.Template, "template", "", "produce: the template, a document id or the text")
				fs.StringArrayVar(&in.Produce.To, "to", nil, "produce message: person or group id, or text such as a channel (repeatable)")
				fs.StringVar((*string)(&in.Produce.Medium), "medium", "", "produce message: email, slack, ...")
				fs.StringVar(&in.Produce.Guide, "guide", "", "produce: the prompt the agent follows (default produce)")
			},
			Bind: func(args []string, in *ops.ObligationAddInput) error {
				in.Title = strings.Join(args, " ")
				if p := in.Produce; p != nil && p.Kind == "" && p.Template == "" && len(p.To) == 0 && p.Medium == "" && p.Guide == "" {
					in.Produce = nil
				}
				return nil
			},
			Text: func(w io.Writer, out ops.ObligationAddOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\n", out.ID, out.Kind, out.Schedule)
				for _, id := range out.RFX {
					p.f("request %s\n", id)
				}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ObligationSchedule, binding[ops.ObligationScheduleInput, ops.ObligationScheduleOutput]{
			Use:  "schedule <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ObligationScheduleInput) {
				fs.StringVar(&in.Start, "start", "", "RFC 3339 start with a zone (a meeting); a commitment may give a date")
				fs.StringVar(&in.End, "end", "", "RFC 3339 end with a zone")
				fs.StringVar(&in.Due, "due", "", "YYYY-MM-DD: the date a commitment is owed")
				fs.StringVar(&in.CalendarUID, "calendar-uid", "", "meeting: UID of its calendar event")
				fs.Var(&calendarEventsFlag{&in.CalendarUIDs}, "calendar-event", "meeting: another calendar event of it: <uid>=<role> (repeatable)")
				fs.BoolVar(&in.Reviewed, "reviewed", false, "an ongoing obligation: reviewed today")
			},
			Bind: func(args []string, in *ops.ObligationScheduleInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ObligationScheduleOutput) error {
				if err := changeMeetingText(w, out.MeetingChangeOutput); err != nil {
					return err
				}
				p := &printer{w: w}
				if out.Due != "" {
					p.f("due\t%s\n", out.Due)
				}
				if out.LastReviewed != "" {
					p.f("reviewed\t%s\n", out.LastReviewed)
				}
				return p.err
			},
		}),
		newMeetingAttendCmd(l, ops.ObligationAttend, "attend"),
		opCommand(l, ops.ObligationChange, binding[ops.ObligationChangeInput, ops.ObligationChangeOutput]{
			Use:  "change <id> <push|redirect|cancel>",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.ObligationChangeInput) {
				fs.StringVar(&in.Due, "due", "", "push: the new date, YYYY-MM-DD")
				fs.StringVar(&in.Start, "start", "", "push: the new start, RFC 3339 with a zone (a meeting)")
				fs.StringVar(&in.End, "end", "", "push: the new end, RFC 3339 with a zone")
				fs.StringVar(&in.To, "to", "", "redirect: person or group id or agent:<name> who owes it now, or the item id or URL that holds it")
				fs.StringVar(&in.Outcome, "outcome", "", "cancel: declined (default) or dropped")
				fs.StringVar(&in.Reason, "reason", "", "why: the facts on the ground")
				fs.StringVar(&in.Because, "because", "", "id of the higher-priority item it gives way to")
				in.Notify = new(bool)
				*in.Notify = true
				fs.BoolVar(in.Notify, "notify", true, "draft a notice to the people it concerns; --notify=false writes none")
			},
			Bind: func(args []string, in *ops.ObligationChangeInput) error {
				in.ID, in.Action = args[0], args[1]
				return nil
			},
			Text: func(w io.Writer, out ops.ObligationChangeOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\n", out.ID, out.Action, out.State)
				if out.Next != "" {
					p.f("next\t%s\n", out.Next)
				}
				for _, id := range out.Notices {
					p.f("notice\t%s\n", id)
				}
				if len(out.Told) > 0 {
					p.f("told\t%s\n", strings.Join(out.Told, ", "))
				}
				if out.Calendar != "" {
					p.f("calendar\t%s\n", out.Calendar)
				}
				if out.Promise != "" {
					p.f("promise\t%s\n", out.Promise)
				}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ObligationParts, binding[ops.ObligationPartsInput, ops.ObligationPartsOutput]{
			Use:  "parts <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ObligationPartsInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ObligationPartsOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\t%s\n", out.ID, out.Kind, out.State, out.Date)
				if out.Promise != "" {
					p.f("promise\t%s\n", out.Promise)
				}
				part := func(label string, x ops.ObligationPart) {
					p.f("%s\t%s\t%s\t%s\t%s\tby %s\t%s\n", label, x.ID, x.Status, x.Effort, x.Relation, firstNonEmpty(x.Back, "-"), x.Title)
				}
				for _, x := range out.Before {
					part("before", x)
				}
				for _, x := range out.CameOut {
					part("came out", x)
				}
				for _, x := range out.Documents {
					p.f("document\t%s\t%s\t%s\n", x.ID, x.Kind, x.Title)
				}
				for _, x := range out.Parties {
					p.f("tell\t%s\t%s\n", x.Name, x.Medium)
				}
				for _, x := range out.Changes {
					p.f("change\t%s\n", x)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ObligationTimeline, binding[ops.ObligationTimelineInput, ops.ObligationTimelineOutput]{
			Use:  "timeline",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ObligationTimelineInput) {
				fs.IntVar(&in.Days, "days", 0, "days from today (default 14)")
			},
			Text: func(w io.Writer, out ops.ObligationTimelineOutput) error {
				p := &printer{w: w}
				point := func(x ops.TimelinePoint) {
					risk := ""
					if x.AtRisk {
						risk = "\tAT RISK: " + x.Risk
					}
					p.f("  %s\t%s\t%s\tprep %s, %s left%s\n", x.ID, x.Kind, x.Title, hoursText(x.PrepHours), hoursText(x.HoursLeft), risk)
					for _, pr := range x.Prep {
						p.f("    %s\t%s\t%s\tby %s\t%s\n", pr.ID, pr.Status, pr.Effort, firstNonEmpty(pr.Back, "-"), pr.Title)
					}
				}
				if len(out.Overdue) > 0 {
					p.f("overdue\n")
					for _, x := range out.Overdue {
						point(x)
					}
				}
				for _, dd := range out.Days {
					if len(dd.Points) == 0 {
						continue
					}
					p.f("%s %s\n", dd.Weekday, dd.Date)
					for _, x := range dd.Points {
						point(x)
					}
				}
				p.f("last %d days: kept %d, renegotiated %d, broken %d\n", out.Score.Days, out.Score.Kept, out.Score.Renegotiated, out.Score.Broken)
				return p.err
			},
		}),
	)
	return cmd
}

// hoursText says hours as "1.5h".
func hoursText(h float64) string { return strconv.FormatFloat(h, 'f', -1, 64) + "h" }
