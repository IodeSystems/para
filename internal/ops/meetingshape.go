package ops

import (
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// A meeting is a task of kind meeting. Older vaults hold `type: meeting`
// documents; they are read through the same shape and written back as they
// are, never rewritten into tasks (a vault upgrade step does that later).
//
// Status of a task meeting comes from the task: an open one with no scheduled
// time is requested, with one is scheduled; done is held; dropped is cancelled.

// mtg is a meeting in either shape. Edit the exported fields, then call sync
// before saving the document.
type mtg struct {
	Status model.MeetingStatus
	Start  string
	End    string
	With   []model.Link
	RFX    []model.Link
	// Legacy is true for a `type: meeting` document.
	Legacy bool

	old *model.Meeting
	t   *model.Task
}

// readMeeting reads a document as a meeting: a `type: meeting` document, or a
// task of kind meeting. It holds the document's own meta, so sync writes to it.
func readMeeting(d model.Document) (*mtg, bool) {
	switch m := d.Meta.(type) {
	case *model.Meeting:
		return &mtg{Status: m.Status, Start: m.Start, End: m.End, With: m.With, RFX: m.RFX, Legacy: true, old: m}, true
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
		return &mtg{Status: st, Start: m.Scheduled, End: m.End, With: m.With, RFX: m.RFX, t: m}, true
	}
	return nil, false
}

// sync writes the fields back to the document's meta.
func (m *mtg) sync() {
	if m.old != nil {
		m.old.Status, m.old.Start, m.old.End, m.old.With, m.old.RFX = m.Status, m.Start, m.End, m.With, m.RFX
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

// meetingAsTask is the task shape of an older meeting, for task views. It is a
// copy: nothing writes it back.
func meetingAsTask(d model.Document) (model.Document, bool) {
	m, ok := d.Meta.(*model.Meeting)
	if !ok {
		return d, false
	}
	d.Meta = m.AsTask()
	return d, true
}

// shortWhen formats a start for a history line.
func shortWhen(s string) string {
	if s == "" {
		return "no time"
	}
	return strings.TrimSpace(s)
}
