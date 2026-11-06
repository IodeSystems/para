package cli

import (
	"io"
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
				fs.StringArrayVar(&in.OwedBy, "owed-by", nil, "person or group id that owes it; default you (repeatable)")
				fs.StringVarP(&in.Project, "project", "p", "", "project id")
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
			},
			Bind: func(args []string, in *ops.ObligationAddInput) error {
				in.Title = strings.Join(args, " ")
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
	)
	return cmd
}
