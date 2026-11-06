package ops

import (
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// A meeting is an obligation of kind meeting, or, in a vault not yet at the
// obligation format, a task of kind meeting. Older vaults hold `type:
// meeting` documents; they are read through the same shape and written back
// as they are (vault upgrade steps rewrite them).
//
// Status comes from the stored state: an open one with no scheduled time is
// requested, with one scheduled; done (held) is held; any other close is
// cancelled.

// mtg is a meeting in any shape. Edit the exported fields, then call sync
// before saving the document.
type mtg struct {
	Status model.MeetingStatus
	Start  string
	End    string
	With   []model.Link
	RFX    []model.Link
	// Legacy is true for a `type: meeting` document.
	Legacy bool
	// Day is the date sync writes as the day an obligation closed.
	Day string

	old *model.Meeting
	t   *model.Task
	ob  *model.Obligation
	// occ is the attendance and calendar of a task or an obligation; nil for
	// an older meeting document.
	occ   *model.Occasion
	recur string
}

// readMeeting reads a document as a meeting: an obligation or a task of kind
// meeting, or a `type: meeting` document. It holds the document's own meta, so
// sync writes to it.
func readMeeting(d model.Document) (*mtg, bool) {
	switch m := d.Meta.(type) {
	case *model.Meeting:
		return &mtg{Status: m.Status, Start: m.Start, End: m.End, With: m.With, RFX: m.RFX, Legacy: true, old: m}, true
	case *model.Obligation:
		if m.Kind != model.KindMeeting {
			return nil, false
		}
		st := model.MeetingRequested
		switch {
		case !m.Open() && m.Outcome == model.OutcomeDone:
			st = model.MeetingHeld
		case !m.Open():
			st = model.MeetingCancelled
		case m.Scheduled != "":
			st = model.MeetingScheduled
		}
		return &mtg{Status: st, Start: m.Scheduled, End: m.End, With: m.With, RFX: m.RFX, ob: m, occ: &m.Occasion, recur: m.Recur}, true
	case *model.Task:
		if m.EffectiveKind() != model.KindMeeting {
			return nil, false
		}
		st := model.MeetingRequested
		switch {
		case m.Status == model.TaskDone:
			st = model.MeetingHeld
		case m.Status == model.TaskDropped:
			st = model.MeetingCancelled
		case m.Scheduled != "":
			st = model.MeetingScheduled
		}
		return &mtg{Status: st, Start: m.Scheduled, End: m.End, With: m.With, RFX: m.RFX, t: m, occ: &m.Occasion, recur: m.Recur}, true
	}
	return nil, false
}

// sync writes the fields back to the document's meta.
func (m *mtg) sync() {
	switch {
	case m.old != nil:
		m.old.Status, m.old.Start, m.old.End, m.old.With, m.old.RFX = m.Status, m.Start, m.End, m.With, m.RFX
		return
	case m.ob != nil:
		o := m.ob
		o.Scheduled, o.End, o.With, o.RFX = m.Start, m.End, m.With, m.RFX
		switch m.Status {
		case model.MeetingHeld:
			o.State, o.Outcome = model.StateClosed, model.OutcomeDone
		case model.MeetingCancelled:
			o.State, o.Outcome = model.StateClosed, model.OutcomeDropped
		default:
			o.State, o.Outcome, o.Reason, o.Closed = model.StateOpen, "", "", ""
			if m.Start != "" && o.Schedule == model.ScheduleOnce {
				o.Schedule = model.ScheduleAt
			}
			return
		}
		if m.Day != "" {
			o.Closed = model.Date(m.Day)
		}
		return
	}
	t := m.t
	t.Scheduled, t.End, t.With, t.RFX = m.Start, m.End, m.With, m.RFX
	switch m.Status {
	case model.MeetingHeld:
		t.Status, t.Outcome, t.BlockedBy = model.TaskDone, model.OutcomeDone, nil
	case model.MeetingCancelled:
		t.Status, t.Outcome, t.BlockedBy = model.TaskDropped, model.OutcomeDropped, nil
	default:
		if taskTerminal(t.Status) {
			t.Status, t.Outcome = model.TaskTodo, ""
		}
	}
}

func typeName(d model.Document) string { return string(d.Meta.Header().Type) }

func notMeeting(d model.Document) error {
	return fmt.Errorf("%s is a %s, not a meeting", d.ID, typeName(d))
}

// meetingAsTask is the task shape of an older meeting or of an obligation, for
// task views. It is a copy: nothing writes it back.
func meetingAsTask(d model.Document) (model.Document, bool) {
	switch m := d.Meta.(type) {
	case *model.Meeting:
		d.Meta = m.AsTask()
		return d, true
	case *model.Obligation:
		d.Meta = m.AsTask()
		return d, true
	}
	return d, false
}

// obligationShape is an obligation read as the task of its kind, for a view
// that reads tasks, and a task of kind incident as the incident it was before
// vault format 10 (the views read incidents apart); any other document as it
// is. It is a copy: never save it.
func obligationShape(d model.Document) model.Document {
	if o, ok := d.Meta.(*model.Obligation); ok {
		d.Meta = o.AsTask()
	}
	return incidentShape(d)
}

// incidentShape is a task of kind incident read as an incident (type
// incident); any other document as it is. It is a copy: never save it.
func incidentShape(d model.Document) model.Document {
	if t, ok := d.Meta.(*model.Task); ok && t.Kind == model.KindIncident {
		i, _ := model.IncidentOf(d)
		i.Type = model.TypeIncident
		d.Meta = i
	}
	return d
}

// ObligationShape is obligationShape for views outside ops.
func ObligationShape(d model.Document) model.Document { return obligationShape(d) }

// shortWhen formats a start for a history line.
func shortWhen(s string) string {
	if s == "" {
		return "no time"
	}
	return strings.TrimSpace(s)
}
