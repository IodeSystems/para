package cli

import (
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/ops"
)

// localStamp shows a stored RFC 3339 time in this machine's zone. A value that
// does not parse is shown as it is, and an empty one as a dash.
func localStamp(s string) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04 MST")
}

func writeMeetings(p *printer, list []ops.MeetingView) {
	for _, v := range list {
		p.f("%s\t%s\t%s\t%s\t%s\n", v.ID, v.Status, localStamp(v.Start), joinOrDash(v.WithNames), v.Title)
	}
}

func changeMeetingText(w io.Writer, out ops.MeetingChangeOutput) error {
	p := &printer{w: w}
	p.f("%s\t%s\t%s\n", out.ID, out.Status, localStamp(out.Start))
	for _, id := range out.RFX {
		p.f("request %s\n", id)
	}
	for _, wn := range out.Warnings {
		p.f("warning: %s\n", wn)
	}
	return p.err
}

func newMeetingCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "meeting", Short: "Meetings: ask for one, book it, hold it", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.MeetingAdd, binding[ops.MeetingAddInput, ops.MeetingAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.MeetingAddInput) {
				fs.StringArrayVar(&in.With, "with", nil, "person or group id (repeatable); default: who the requests go to")
				fs.StringArrayVar(&in.RFX, "rfx", nil, "request id the meeting should settle (repeatable)")
				fs.StringArrayVar(&in.Scope, "scope", nil, "project, area, or task id whose open questions go on the agenda (repeatable)")
				fs.StringVar(&in.Start, "start", "", "RFC 3339 start with a zone, such as 2026-11-04T10:00:00-05:00; with --end the meeting is scheduled")
				fs.StringVar(&in.End, "end", "", "RFC 3339 end with a zone")
				fs.StringVar(&in.Body, "body", "", "text before the Questions section")
			},
			Bind: func(args []string, in *ops.MeetingAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: func(w io.Writer, out ops.MeetingAddOutput) error {
				p := &printer{w: w}
				p.f("%s\n", out.ID)
				for _, id := range out.RFX {
					p.f("request %s\n", id)
				}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				return p.err
			},
		}),
		opCommand(l, ops.MeetingList, binding[ops.MeetingListInput, ops.MeetingListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.MeetingListInput) {
				fs.StringVar((*string)(&in.Status), "status", "", "only requested, scheduled, held, or cancelled")
				fs.BoolVar(&in.All, "all", false, "include held and cancelled meetings")
			},
			Text: func(w io.Writer, out ops.MeetingListOutput) error {
				p := &printer{w: w}
				writeMeetings(p, out.Meetings)
				return p.err
			},
		}),
		opCommand(l, ops.MeetingShow, binding[ops.MeetingShowInput, ops.MeetingShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.MeetingShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.MeetingShowOutput) error {
				p := &printer{w: w}
				v := out.Meeting
				p.f("%s\t%s\t%s\t%s\t%s\n", v.ID, v.Status, localStamp(v.Start), localStamp(v.End), v.Title)
				p.f("with\t%s\n", joinOrDash(v.WithNames))
				p.f("requests\t%s\n", joinOrDash(v.RFX))
				p.f("%s\n", out.Body)
				return p.err
			},
		}),
		opCommand(l, ops.MeetingSchedule, binding[ops.MeetingScheduleInput, ops.MeetingChangeOutput]{
			Use:  "schedule <id> --start <time> --end <time>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.MeetingScheduleInput) {
				fs.StringVar(&in.Start, "start", "", "RFC 3339 start with a zone (required)")
				fs.StringVar(&in.End, "end", "", "RFC 3339 end with a zone (required)")
			},
			Bind: func(args []string, in *ops.MeetingScheduleInput) error { in.ID = args[0]; return nil },
			Text: changeMeetingText,
		}),
		opCommand(l, ops.MeetingCancel, binding[ops.MeetingCancelInput, ops.MeetingChangeOutput]{
			Use:  "cancel <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.MeetingCancelInput) {
				fs.StringVar(&in.Reason, "reason", "", "one line for the notes")
			},
			Bind: func(args []string, in *ops.MeetingCancelInput) error { in.ID = args[0]; return nil },
			Text: changeMeetingText,
		}),
	)
	return cmd
}
