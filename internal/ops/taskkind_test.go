package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

// oldMeeting writes a `type: meeting` document the way an older vault holds one.
func oldMeeting(t *testing.T, f meetingFixture, status model.MeetingStatus, start, end string) string {
	t.Helper()
	m := &model.Meeting{Common: model.Common{Type: model.TypeMeeting}, Status: status, Start: start, End: end,
		With: []model.Link{model.LinkTo(f.person)}}
	if f.rfx != "" {
		m.RFX = []model.Link{model.LinkTo(f.rfx)}
	}
	doc, err := f.d.Store.Create(ctx, model.Document{Folder: model.FolderMeetings, Title: "Older sync", Body: meetingBody("", nil), Meta: m})
	if err != nil {
		t.Fatal(err)
	}
	return doc.ID
}

func TestOlderMeetingReadsAsTaskAndStaysOlder(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	id := oldMeeting(t, f, model.MeetingScheduled, "2026-11-04T10:00:00-05:00", "2026-11-04T10:30:00-05:00")

	// Listed and shown as a task of kind meeting.
	var found *TaskView
	for _, v := range must(t, TaskList, d, TaskListInput{}).Tasks {
		if v.ID == id {
			found = &v
		}
	}
	if found == nil || found.Kind != model.KindMeeting || found.Scheduled != "2026-11-04T10:00:00-05:00" || !slices.Equal(found.With, []string{f.person}) {
		t.Fatalf("task_list shows %+v", found)
	}
	if got := must(t, TaskShow, d, TaskShowInput{ID: id}).Task; got.Kind != model.KindMeeting {
		t.Errorf("task_show kind = %s", got.Kind)
	}
	if got := must(t, MeetingList, d, MeetingListInput{}).Meetings; len(got) != 1 || got[0].ID != id {
		t.Fatalf("meeting_list = %+v", got)
	}
	// In the agenda and in value order, with its time (a clock fixed before it).
	ad, ada := agendaVault(t, "2026-11-01T12:00:00Z")
	af := meetingFixture{d: ad, person: ada}
	aid := oldMeeting(t, af, model.MeetingScheduled, "2026-11-04T10:00:00-05:00", "2026-11-04T10:30:00-05:00")
	ag := must(t, Agenda, ad, AgendaInput{Days: 14, Zone: "UTC"})
	if !slices.ContainsFunc(ag.Items, func(it AgendaItem) bool { return it.ID == aid && it.Kind == AgendaMeeting }) {
		t.Errorf("agenda lacks the older meeting: %+v", ag.Items)
	}
	ord, err := Order(ctx, ad.Store)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(ord.Rows, func(r ValueRow) bool { return r.ID == aid && r.Kind == KindMeeting }) {
		t.Error("value order lacks the older meeting")
	}

	// A move keeps the old time in the notes; hold writes it back as it was.
	must(t, MeetingSchedule, d, MeetingScheduleInput{ID: id, Start: "2026-11-05T10:00:00-05:00", End: "2026-11-05T10:30:00-05:00"})
	doc, _ := d.Store.Get(ctx, id)
	if _, ok := doc.Meta.(*model.Meeting); !ok {
		t.Fatalf("an older meeting was rewritten as %T", doc.Meta)
	}
	if !strings.Contains(doc.Body, "Rescheduled from 2026-11-04T10:00:00-05:00 to 2026-11-05T10:00:00-05:00") {
		t.Errorf("no history line:\n%s", doc.Body)
	}
	must(t, MeetingHold, d, MeetingHoldInput{ID: id, Notes: "Done."})
	doc, _ = d.Store.Get(ctx, id)
	if m, ok := doc.Meta.(*model.Meeting); !ok || m.Status != model.MeetingHeld {
		t.Errorf("after hold: %#v", doc.Meta)
	}
	// A plain task command does not write an older meeting.
	if _, err := TaskDone.Run(ctx, d, TaskDoneInput{ID: id}); err == nil {
		t.Error("task_done on an older meeting must fail")
	}
}

func TestMeetingIsATaskOfKindMeeting(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	add := must(t, MeetingAdd, d, MeetingAddInput{Title: "Launch sync", With: []string{f.person}, RFX: []string{f.rfx},
		Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00"})
	doc, _ := d.Store.Get(ctx, add.ID)
	tk, ok := doc.Meta.(*model.Task)
	if !ok || tk.Kind != model.KindMeeting || tk.Scheduled != "2026-11-04T10:00:00-05:00" || len(tk.RFX) != 1 {
		t.Fatalf("a new meeting is %#v", doc.Meta)
	}
	mustLintClean(t, d)
	// Closing it the plain way is refused; hold records it done.
	if _, err := TaskDone.Run(ctx, d, TaskDoneInput{ID: add.ID}); err == nil || !strings.Contains(err.Error(), "meeting_hold") {
		t.Errorf("task_done err = %v", err)
	}
	if _, err := TaskResolve.Run(ctx, d, TaskResolveInput{ID: add.ID}); err == nil {
		t.Error("task_resolve on an open meeting must fail")
	}
	must(t, MeetingHold, d, MeetingHoldInput{ID: add.ID, Notes: "Agreed."})
	doc, _ = d.Store.Get(ctx, add.ID)
	if tk := doc.Meta.(*model.Task); tk.Status != model.TaskDone || tk.Outcome != model.OutcomeDone {
		t.Errorf("held meeting = %s %s", tk.Status, tk.Outcome)
	}
	mustLintClean(t, d)
	// Cancelled maps to dropped.
	c := must(t, MeetingAdd, d, MeetingAddInput{Title: "Second", With: []string{f.person}})
	must(t, MeetingCancel, d, MeetingCancelInput{ID: c.ID, Reason: "Out."})
	doc, _ = d.Store.Get(ctx, c.ID)
	if tk := doc.Meta.(*model.Task); tk.Status != model.TaskDropped || tk.Outcome != model.OutcomeDropped {
		t.Errorf("cancelled meeting = %s %s", tk.Status, tk.Outcome)
	}
	mustLintClean(t, d)
}

func TestCommitmentTagReadsAsKind(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderProjects, Title: "Send the audit plan",
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask, Tags: []string{"commitment"}}, Status: model.TaskTodo,
			Project: model.LinkTo(f.project), Due: "2026-11-10"}})
	if err != nil {
		t.Fatal(err)
	}
	if v := must(t, TaskShow, d, TaskShowInput{ID: doc.ID}).Task; v.Kind != model.KindCommitment || !v.Commitment {
		t.Errorf("tagged commitment reads as %s", v.Kind)
	}
	// A new one is written as kind, with no tag; it closes with an outcome.
	n := must(t, TaskAdd, d, TaskAddInput{Title: "Owe a reply", Project: f.project, Commitment: true, Due: "2026-11-12", With: []string{f.person}})
	nd, _ := d.Store.Get(ctx, n.ID)
	if tk := nd.Meta.(*model.Task); tk.Kind != model.KindCommitment || len(tk.Tags) != 0 {
		t.Errorf("new commitment = %#v", tk)
	}
	if _, err := TaskDone.Run(ctx, d, TaskDoneInput{ID: n.ID}); err == nil || !strings.Contains(err.Error(), "task_resolve") {
		t.Errorf("task_done err = %v", err)
	}
	must(t, TaskResolve, d, TaskResolveInput{ID: n.ID, Outcome: model.OutcomeDone})
	if _, err := TaskAdd.Run(ctx, d, TaskAddInput{Title: "Work", Project: f.project, Kind: model.KindWorkWith, With: []string{f.person}}); err == nil || !strings.Contains(err.Error(), "done_when") {
		t.Errorf("work_with without done_when: %v", err)
	}
	if _, err := TaskAdd.Run(ctx, d, TaskAddInput{Title: "Work", Project: f.project, Kind: model.KindWorkWith, DoneWhen: "Shipped."}); err == nil {
		t.Error("work_with without with must fail")
	}
	mustLintClean(t, d)
}

func TestCallAttempts(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	call := must(t, TaskAdd, d, TaskAddInput{Title: "Call Ada", Kind: model.KindCall, With: []string{f.person}, Scheduled: "2026-11-04"})
	mustLintClean(t, d)
	if _, err := TaskAttempt.Run(ctx, d, TaskAttemptInput{ID: call.ID, Result: CallNoAnswer}); err == nil {
		t.Error("an attempt with no retry must fail")
	}
	o := must(t, TaskAttempt, d, TaskAttemptInput{ID: call.ID, Result: CallNoAnswer, Retry: "2026-11-05"})
	if o.Attempts != 1 || o.Scheduled != "2026-11-05" || o.Stuck {
		t.Fatalf("attempt 1 = %+v", o)
	}
	must(t, TaskAttempt, d, TaskAttemptInput{ID: call.ID, Result: CallLeftMessage, Retry: "2026-11-06T09:00:00-05:00"})
	o = must(t, TaskAttempt, d, TaskAttemptInput{ID: call.ID, Result: CallNoAnswer})
	if o.Attempts != 3 || !o.Stuck {
		t.Fatalf("attempt 3 = %+v", o)
	}
	v := must(t, TaskShow, d, TaskShowInput{ID: call.ID}).Task
	if v.Attempts != 3 || v.LastAttempt != CallNoAnswer || !v.Stuck || v.Scheduled != "2026-11-06T09:00:00-05:00" {
		t.Errorf("view = %+v", v)
	}
	body := must(t, TaskShow, d, TaskShowInput{ID: call.ID}).Body
	if !strings.Contains(body, "## Attempts") || !strings.Contains(body, "was 2026-11-05") {
		t.Errorf("history lost:\n%s", body)
	}
	mustLintClean(t, d)
	// Reached ends the stuck flag; a non-call is refused.
	must(t, TaskAttempt, d, TaskAttemptInput{ID: call.ID, Result: CallReached})
	if v := must(t, TaskShow, d, TaskShowInput{ID: call.ID}).Task; v.Stuck || v.Attempts != 4 {
		t.Errorf("after reached = %+v", v)
	}
	plain := must(t, TaskAdd, d, TaskAddInput{Title: "Plain", Project: f.project})
	if _, err := TaskAttempt.Run(ctx, d, TaskAttemptInput{ID: plain.ID, Result: CallReached}); err == nil {
		t.Error("task_attempt on a task must fail")
	}
}
