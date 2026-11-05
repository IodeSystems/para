package ops

import (
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/recur"
)

func recurSlotTask(t *testing.T, d Deps, project string, tk *model.Task) (model.Document, *model.Task) {
	t.Helper()
	tk.Common = model.Common{Type: model.TypeTask}
	tk.Status = model.TaskTodo
	tk.Project = model.LinkTo(project)
	tk.Recur = "weekly"
	doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderProjects, Title: "Standup", Meta: tk})
	if err != nil {
		t.Fatal(err)
	}
	return doc, tk
}

func nextOf(t *testing.T, d Deps, id string) *model.Task {
	t.Helper()
	nid, err := successor(ctx, d, id)
	if err != nil || nid == "" {
		t.Fatalf("no successor of %s: %v", id, err)
	}
	doc, err := d.Store.Get(ctx, nid)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Meta.(*model.Task)
}

// A weekly timed meeting: the next one keeps kind, people, requests, calendar
// uid, and the local time across the November clock change.
func TestRecurringMeetingKeepsSlot(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("no tz data")
	}
	old := time.Local
	time.Local = la
	defer func() { time.Local = old }()

	d := newDepsAt(t, time.Date(2026, 10, 30, 12, 0, 0, 0, la), "m1")
	seedOrg(t, d)
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	person := "person-ada-example"
	doc, tk := recurSlotTask(t, d, proj, &model.Task{
		Kind: model.KindMeeting, Scheduled: "2026-10-29T09:00:00-07:00", End: "2026-10-29T09:30:00-07:00",
		With: []model.Link{model.LinkTo(person)}, Attendees: []string{"Bob Nobody <bob@vendor.example>"}, RFX: []model.Link{model.LinkTo("rfx-x")}, CalendarUID: "uid-1@example.com"})
	if _, _, _, err := makeNext(ctx, d, doc, tk, mustRule(t, "weekly")); err != nil {
		t.Fatal(err)
	}
	n := nextOf(t, d, doc.ID)
	if len(n.Attendees) != 1 || n.Attendees[0] != "Bob Nobody <bob@vendor.example>" {
		t.Errorf("next attendees = %v", n.Attendees)
	}
	if n.Kind != model.KindMeeting || len(n.UIDs()) != 1 || n.UIDs()[0].UID != "uid-1@example.com" || len(n.With) != 1 || len(n.RFX) != 1 {
		t.Errorf("next lost fields: %+v", n)
	}
	if n.Scheduled != "2026-11-05T09:00:00-08:00" || n.End != "2026-11-05T09:30:00-08:00" {
		t.Errorf("next slot = %s to %s", n.Scheduled, n.End)
	}
	if n.Due != "" {
		t.Errorf("next due = %q, want none", n.Due)
	}
}

// A call keeps its date-only schedule; with a due date, scheduled moves by the
// same step; rfx is for meetings only.
func TestRecurringCallDateAndDue(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 10, 30, 12, 0, 0, 0, time.Local), "m1")
	seedOrg(t, d)
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	doc, tk := recurSlotTask(t, d, proj, &model.Task{
		Kind: model.KindCall, Scheduled: "2026-11-02", Due: "2026-11-03", RFX: []model.Link{model.LinkTo("rfx-x")}})
	if _, _, _, err := makeNext(ctx, d, doc, tk, mustRule(t, "weekly")); err != nil {
		t.Fatal(err)
	}
	n := nextOf(t, d, doc.ID)
	if n.Kind != model.KindCall || n.Scheduled != "2026-11-09" || n.Due != "2026-11-10" || len(n.RFX) != 0 {
		t.Errorf("next = %+v", n)
	}
}

func mustRule(t *testing.T, s string) recur.Rule {
	t.Helper()
	r, err := recur.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// meeting_hold makes the next occurrence of a weekly timed meeting, once.
func TestMeetingHoldRecurs(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("no tz data")
	}
	old := time.Local
	time.Local = la
	defer func() { time.Local = old }()

	d := newDepsAt(t, time.Date(2026, 10, 30, 12, 0, 0, 0, la), "m1")
	seedOrg(t, d)
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	person := "person-ada-example"
	doc, _ := recurSlotTask(t, d, proj, &model.Task{
		Kind: model.KindMeeting, Scheduled: "2026-10-29T09:00:00-07:00", End: "2026-10-29T09:30:00-07:00",
		With: []model.Link{model.LinkTo(person)}, CalendarUID: "uid-1@example.com"})
	h := must(t, MeetingHold, d, MeetingHoldInput{ID: doc.ID, Notes: "Went well."})
	if h.Next == "" {
		t.Fatalf("meeting_hold made no next occurrence: %+v", h)
	}
	n := nextOf(t, d, doc.ID)
	if n.Kind != model.KindMeeting || n.Scheduled != "2026-11-05T09:00:00-08:00" || n.End != "2026-11-05T09:30:00-08:00" || len(n.UIDs()) != 1 || n.UIDs()[0].UID != "uid-1@example.com" || n.Status != model.TaskTodo {
		t.Errorf("next = %+v", n)
	}
	// A repeat of the closing path makes no second one.
	od, _ := d.Store.Get(ctx, doc.ID)
	ot := od.Meta.(*model.Task)
	if id, _, _, err := makeNext(ctx, d, od, ot, mustRule(t, "weekly")); err != nil || id != h.Next {
		t.Errorf("second makeNext = %q, %v; want %s", id, err, h.Next)
	}
	// A cancelled occurrence also recurs.
	c, _ := recurSlotTask(t, d, proj, &model.Task{Kind: model.KindMeeting, Scheduled: "2026-11-02T10:00:00-08:00", End: "2026-11-02T10:30:00-08:00",
		With: []model.Link{model.LinkTo(person)}})
	if o := must(t, MeetingCancel, d, MeetingCancelInput{ID: c.ID, Reason: "Out."}); o.Next == "" {
		t.Errorf("meeting_cancel made no next: %+v", o)
	}
}
