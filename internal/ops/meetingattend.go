package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Attendance is the owner's answer, not a confirmation para asks for. Many
// meetings are optional and standing: an optional meeting asks nothing until
// the owner says they go. meeting_attend records the answer; with all_future
// it is also what each next occurrence of a recurring meeting starts with.

// attendUnset clears attend (or the series default with all_future).
const attendUnset = "unset"

// MeetingAttendInput is the input of meeting_attend.
type MeetingAttendInput struct {
	ID string `json:"id"`
	// Attend is yes, no, maybe, or unset (undecided). Empty keeps it.
	Attend string `json:"attend,omitempty"`
	// Optional sets whether the owner may skip the meeting; absent keeps it.
	// A recurring meeting passes it to each next occurrence.
	Optional *bool `json:"optional,omitempty"`
	// AllFuture also makes Attend the answer each next occurrence of a
	// recurring meeting starts with.
	AllFuture bool `json:"all_future,omitempty"`
}

// MeetingAttendState is a meeting's attendance fields.
type MeetingAttendState struct {
	Attend        model.Attend `json:"attend,omitempty"` // empty is undecided
	Optional      bool         `json:"optional"`
	AttendDefault model.Attend `json:"attend_default,omitempty"`
}

// MeetingAttendOutput is the output of meeting_attend. Previous is the state
// before: pass it back to put it back.
type MeetingAttendOutput struct {
	ID       string             `json:"id"`
	Title    string             `json:"title"`
	Now      MeetingAttendState `json:"now"`
	Previous MeetingAttendState `json:"previous"`
	Warnings []string           `json:"warnings"`
	Rev      string             `json:"rev"`
}

// MeetingAttendDescription is the description of meeting_attend.
const MeetingAttendDescription = "Record whether the owner goes to a meeting (attend yes, no, maybe, or unset) and whether it is optional. An optional meeting asks nothing of the owner until attend is yes; attend no takes it off the agenda and frees what waits on it. all_future makes attend the answer of each next occurrence of a recurring meeting; a recurrence copies optional, never attend. Set optional from the calendar; never ask the owner to confirm attendance"

// ObligationAttend records the owner's attendance of a meeting.
var ObligationAttend = defineSafe("obligation_attend", MeetingAttendDescription, meetingAttend)

// MeetingAttend is obligation_attend by its older name (an alias, opalias.go).
var MeetingAttend = defineSafe("meeting_attend", MeetingAttendDescription, meetingAttend)

// MeetingRSVP is meeting_attend for the owner in the web app: Attend and Skip
// on the agenda. It is an alias of obligation_attend that keeps its person
// check (opalias.go).
var MeetingRSVP = definePerson("meeting_rsvp", "Record from the web app whether the owner goes to a meeting (attend yes, no, maybe, or unset), and whether it is optional. People only", meetingAttend)

func meetingAttend(ctx context.Context, d Deps, in MeetingAttendInput) (MeetingAttendOutput, error) {
	attend := model.Attend(strings.TrimSpace(in.Attend))
	set := attend != ""
	switch {
	case !set && in.Optional == nil:
		return MeetingAttendOutput{}, errors.New("give attend (yes, no, maybe, unset), optional, or both")
	case !set && in.AllFuture:
		return MeetingAttendOutput{}, errors.New("all_future needs attend")
	case attend == attendUnset:
		attend = ""
	case set && !slices.Contains(model.Attends, attend):
		return MeetingAttendOutput{}, fmt.Errorf("attend %q: want yes, no, maybe, or unset", attend)
	}
	doc, m, err := getMeeting(ctx, d.Store, in.ID)
	if err != nil {
		return MeetingAttendOutput{}, err
	}
	if m.occ == nil {
		return MeetingAttendOutput{}, fmt.Errorf("%s is an older meeting document: the owner runs `para vault upgrade` to make it a task, then set attend", doc.ID)
	}
	if !meetingOpen(m.Status) {
		return MeetingAttendOutput{}, fmt.Errorf("%s is %s: attendance is for a meeting still ahead", doc.ID, m.Status)
	}
	t := m.occ
	out := MeetingAttendOutput{ID: doc.ID, Title: doc.Title, Warnings: []string{},
		Previous: MeetingAttendState{Attend: t.Attend, Optional: t.Optional, AttendDefault: t.AttendDefault}}
	if set {
		t.Attend = attend
		if in.AllFuture {
			t.AttendDefault = attend
		}
	}
	if in.Optional != nil {
		t.Optional = *in.Optional
	}
	if in.AllFuture {
		if _, ok := recurRule(m.recur); !ok {
			out.Warnings = append(out.Warnings, doc.ID+" does not recur: all_future has no next occurrence to apply to")
		}
	}
	out.Now = MeetingAttendState{Attend: t.Attend, Optional: t.Optional, AttendDefault: t.AttendDefault}
	if out.Now == out.Previous {
		out.Rev = doc.Rev
		return out, nil
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return MeetingAttendOutput{}, err
	}
	out.Rev = saved.Rev
	return out, nil
}
