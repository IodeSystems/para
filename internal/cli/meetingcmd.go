package cli

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
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

// holdFlags holds the flags of meeting hold that build structured input.
type holdFlags struct {
	actions, answers []string
	stdin            bool
}

// parseAction reads "title", "title | assignee", or "title | assignee | YYYY-MM-DD".
func parseAction(s string) (ops.HoldAction, error) {
	parts := strings.Split(s, "|")
	if len(parts) > 3 {
		return ops.HoldAction{}, fmt.Errorf("--action %q: want title | assignee | YYYY-MM-DD", s)
	}
	a := ops.HoldAction{Title: strings.TrimSpace(parts[0])}
	if len(parts) > 1 {
		a.Assignee = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		a.Due = strings.TrimSpace(parts[2])
	}
	return a, nil
}

func newMeetingHoldCmd(l *loader) *cobra.Command {
	var hf holdFlags
	cmd := opCommand(l, ops.MeetingHold, binding[ops.MeetingHoldInput, ops.MeetingHoldOutput]{
		Use:  "hold <id>",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.MeetingHoldInput) {
			fs.StringVar(&in.Notes, "notes", "", "what was said, in your words (never a transcript)")
			fs.StringVar(&in.Start, "start", "", "RFC 3339 start with a zone; only for a meeting that was never scheduled")
			fs.StringVar(&in.End, "end", "", "RFC 3339 end with a zone; as --start")
			fs.StringVar(&in.Goal, "project", "", "goal of the action items")
			fs.StringVar(&in.Area, "area", "", "area of the action items")
			fs.StringArrayVar(&hf.actions, "action", nil, `action item "title | assignee | YYYY-MM-DD" (repeatable); assignee is self, a person id, or agent:<name>`)
			fs.StringArrayVar(&hf.answers, "answer", nil, `answer "<question-id>=<text>" (repeatable)`)
			fs.BoolVar(&hf.stdin, "stdin-json", false, "read the whole input (replies, answers, actions, notes) as JSON on stdin; the flags above are then not allowed")
		},
		Bind: func(args []string, in *ops.MeetingHoldInput) error {
			in.ID = args[0]
			in.Actions, in.Answers = nil, nil
			for _, a := range hf.actions {
				act, err := parseAction(a)
				if err != nil {
					return err
				}
				in.Actions = append(in.Actions, act)
			}
			for _, a := range hf.answers {
				id, text, ok := strings.Cut(a, "=")
				if !ok || strings.TrimSpace(id) == "" || strings.TrimSpace(text) == "" {
					return fmt.Errorf("--answer %q: want <question-id>=<text>", a)
				}
				in.Answers = append(in.Answers, ops.QuestionAnswerInput{ID: strings.TrimSpace(id), Text: strings.TrimSpace(text)})
			}
			return nil
		},
		Stdin: func(r io.Reader, in *ops.MeetingHoldInput) error {
			if !hf.stdin {
				return nil
			}
			if in.Notes != "" || len(in.Actions) > 0 || len(in.Answers) > 0 || in.Start != "" || in.Goal != "" || in.Area != "" {
				return errors.New("--stdin-json takes the whole input: leave out the other flags")
			}
			id := in.ID
			raw, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(in); err != nil {
				return fmt.Errorf("--stdin-json: %w", err)
			}
			if in.ID != "" && in.ID != id {
				return fmt.Errorf("--stdin-json: id %q differs from the argument %q", in.ID, id)
			}
			in.ID = id
			return nil
		},
		Text: func(w io.Writer, out ops.MeetingHoldOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\n", out.ID, out.Status)
			for _, id := range out.Replied {
				p.f("replied\t%s\n", id)
			}
			for _, id := range out.Constraints {
				p.f("constraint\t%s\n", id)
			}
			for _, id := range out.Answered {
				p.f("answered\t%s\n", id)
			}
			for _, id := range out.Tasks {
				p.f("task\t%s\n", id)
			}
			for _, id := range out.Waiting {
				p.f("still waiting\t%s\n", id)
			}
			for _, wn := range out.Warnings {
				p.f("warning: %s\n", wn)
			}
			return p.err
		},
	})
	return cmd
}

// newMeetingAttendCmd is meeting attend (op meeting_attend), or meeting rsvp
// (op meeting_rsvp, the owner's twin of it): <id> [yes|no|maybe|unset].
func newMeetingAttendCmd(l *loader, op *ops.Op[ops.MeetingAttendInput, ops.MeetingAttendOutput], use string) *cobra.Command {
	var optional, required bool
	return opCommand(l, op, binding[ops.MeetingAttendInput, ops.MeetingAttendOutput]{
		Use:  use + " <id> [yes|no|maybe|unset]",
		Args: cobra.RangeArgs(1, 2),
		Flags: func(fs *pflag.FlagSet, in *ops.MeetingAttendInput) {
			fs.BoolVar(&optional, "optional", false, "the owner may skip it (passed on to each next occurrence)")
			fs.BoolVar(&required, "required", false, "the owner is expected to go")
			fs.BoolVar(&in.AllFuture, "all-future", false, "each next occurrence of a recurring meeting starts with this answer")
		},
		Bind: func(args []string, in *ops.MeetingAttendInput) error {
			in.ID = args[0]
			if len(args) > 1 {
				in.Attend = args[1]
			}
			switch {
			case optional && required:
				return errors.New("--optional and --required: give one")
			case optional || required:
				in.Optional = &optional
			}
			return nil
		},
		Text: func(w io.Writer, out ops.MeetingAttendOutput) error {
			p := &printer{w: w}
			kind := "required"
			if out.Now.Optional {
				kind = "optional"
			}
			p.f("%s\tattend %s\t%s\n", out.ID, cmp.Or(string(out.Now.Attend), "undecided"), kind)
			if out.Now.AttendDefault != "" {
				p.f("next occurrences\tattend %s\n", out.Now.AttendDefault)
			}
			for _, wn := range out.Warnings {
				p.f("warning: %s\n", wn)
			}
			return p.err
		},
	})
}

func newMeetingCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "meeting", Short: "Meetings: ask for one, book it, hold it", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.MeetingAdd, binding[ops.MeetingAddInput, ops.MeetingAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.MeetingAddInput) {
				fs.StringArrayVar(&in.With, "with", nil, "person or group id, name, or email (repeatable); default: who the requests go to")
				fs.StringArrayVar(&in.Attendees, "attendee", nil, "invitee name, email, or \"Name <email>\" kept as text; a match in the directory becomes a --with link (repeatable)")
				fs.StringArrayVar(&in.RFX, "rfx", nil, "request id the meeting should settle (repeatable)")
				fs.StringArrayVar(&in.Scope, "scope", nil, "project, area, or task id whose open questions go on the agenda (repeatable)")
				fs.StringVar(&in.Start, "start", "", "RFC 3339 start with a zone, such as 2026-11-04T10:00:00-05:00; with --end the meeting is scheduled")
				fs.StringVar(&in.End, "end", "", "RFC 3339 end with a zone")
				fs.StringVar(&in.CalendarUID, "calendar-uid", "", "UID of the meeting's event in your calendar, for calendar compare")
				fs.Var(&calendarEventsFlag{&in.CalendarUIDs}, "calendar-event", "another calendar event of it: <uid>=<role>, such as room@x=room (repeatable)")
				fs.StringVar(&in.Body, "body", "", "text before the Questions section")
				fs.BoolVar(&in.Optional, "optional", false, "you are an optional attendee: it asks nothing of you until you say you go")
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
				if len(v.Attendees) > 0 {
					p.f("attendees\t%s\n", strings.Join(v.Attendees, ", "))
				}
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
				fs.StringVar(&in.CalendarUID, "calendar-uid", "", "UID of the meeting's event in your calendar (role primary); the stored ones stay")
				fs.Var(&calendarEventsFlag{&in.CalendarUIDs}, "calendar-event", "another calendar event of it: <uid>=<role>, such as room@x=room (repeatable)")
			},
			Bind: func(args []string, in *ops.MeetingScheduleInput) error { in.ID = args[0]; return nil },
			Text: changeMeetingText,
		}),
		newMeetingHoldCmd(l),
		newMeetingAttendCmd(l, ops.MeetingAttend, "attend"),
		newMeetingAttendCmd(l, ops.MeetingRSVP, "rsvp"),
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

// calendarEventsFlag reads repeatable --calendar-event <uid>[=<role>] flags.
type calendarEventsFlag struct{ list *[]model.CalendarEvent }

func (f *calendarEventsFlag) String() string { return "" }
func (f *calendarEventsFlag) Type() string   { return "uid=role" }
func (f *calendarEventsFlag) Set(v string) error {
	uid, role, _ := strings.Cut(v, "=")
	if strings.TrimSpace(uid) == "" {
		return fmt.Errorf("--calendar-event %q: want <uid>=<role>", v)
	}
	*f.list = append(*f.list, model.CalendarEvent{UID: strings.TrimSpace(uid), Role: strings.TrimSpace(role)})
	return nil
}
