package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// needsNow is the clock of the table: Thursday 2026-11-05, noon UTC.
var needsNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// snapOf is a snapshot of the documents only, with no store.
func snapOf(t *testing.T, docs ...model.Document) *Snapshot {
	t.Helper()
	sn := &Snapshot{ctx: ctx, Today: needsNow.Format(dateLayout), Now: needsNow, DueSoonDays: DefaultDueSoonDays,
		docs: map[string]model.Document{}, missing: map[string]bool{}, agent: map[string]string{}, needs: map[string]Need{}}
	for _, d := range docs {
		sn.refs = append(sn.refs, model.Ref{ID: d.ID, Type: d.Meta.Header().Type, Folder: d.Folder, Title: d.Title})
		sn.docs[d.ID] = d
	}
	if err := sn.index(); err != nil {
		t.Fatal(err)
	}
	return sn
}

func common(typ model.Type) model.Common { return model.Common{Type: typ, Created: "2026-11-01"} }

func nTask(id string, f func(*model.Task)) model.Document {
	m := &model.Task{Common: common(model.TypeTask), Status: model.TaskTodo, Project: model.LinkTo("project-goal")}
	if f != nil {
		f(m)
	}
	return model.Document{ID: id, Folder: model.FolderProjects, Title: id, Meta: m}
}

func nMeeting(id, start, end string, f func(*model.Task)) model.Document {
	return nTask(id, func(m *model.Task) {
		m.Kind, m.Scheduled, m.End = model.KindMeeting, start, end
		if f != nil {
			f(m)
		}
	})
}

func nQuestion(id string, f func(*model.Question)) model.Document {
	m := &model.Question{Common: common(model.TypeQuestion), Status: model.QuestionOpen, Scope: model.LinkTo("project-goal")}
	if f != nil {
		f(m)
	}
	return model.Document{ID: id, Folder: model.FolderQuestions, Title: id, Meta: m}
}

func nRFX(id string, f func(*model.RFX)) model.Document {
	m := &model.RFX{Common: common(model.TypeRFX), Status: model.RFXOpen, Direction: model.RFXOut,
		Targets: []model.Target{{To: model.LinkTo("person-ada"), State: model.TargetSent}}}
	if f != nil {
		f(m)
	}
	return model.Document{ID: id, Title: id, Meta: m}
}

func nIO(id string, f func(*model.IO)) model.Document {
	m := &model.IO{Common: common(model.TypeIO), Direction: model.IOIn, State: model.IOUnread}
	if f != nil {
		f(m)
	}
	return model.Document{ID: id, Title: id, Meta: m}
}

func nRequest(id string, st model.RequestStatus, res model.RequestResolution, links ...string) model.Document {
	m := &model.Request{Common: common(model.TypeRequest), Status: st, Resolution: res}
	for _, l := range links {
		m.Links = append(m.Links, model.LinkTo(l))
	}
	return model.Document{ID: id, Folder: model.FolderRequests, Title: id, Meta: m}
}

func nArea(id string, f func(*model.Area)) model.Document {
	m := &model.Area{Common: common(model.TypeArea), Status: model.AreaActive, Cadence: "weekly"}
	if f != nil {
		f(m)
	}
	return model.Document{ID: id, Folder: model.FolderAreas, Title: id, Meta: m}
}

func nException(id string, f func(*model.PolicyException)) model.Document {
	m := &model.PolicyException{Common: common(model.TypePolicyException), Status: model.ExceptionOpen}
	if f != nil {
		f(m)
	}
	return model.Document{ID: id, Title: id, Meta: m}
}

// The table: type × state × deferral × linked request → (needs_you, reason).
func TestNeedsYouTable(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	goal := model.Document{ID: "project-goal", Folder: model.FolderProjects, Title: "Goal",
		Meta: &model.Project{Common: common(model.TypeProject), Status: model.ProjectActive}}
	offGoal := model.Document{ID: "project-off", Folder: model.FolderProjects, Title: "Off",
		Meta: &model.Project{Common: common(model.TypeProject), Status: model.ProjectActive, Deferral: model.Deferral{DeferUntil: model.SomedayWord}}}
	person := model.Document{ID: "person-ada", Folder: model.FolderOrg, Title: "Ada", Meta: &model.Person{Common: common(model.TypePerson)}}
	openQ := nQuestion("question-open-dep", nil)
	doneQ := nQuestion("question-done-dep", func(q *model.Question) { q.Status = model.QuestionAnswered })
	cases := []struct {
		doc    model.Document
		extra  []model.Document
		want   bool
		reason string // a part of the reason
	}{
		// Tasks.
		{doc: nTask("task-todo", nil), want: true, reason: "task to do"},
		{doc: nTask("task-doing", func(m *model.Task) { m.Status = model.TaskDoing }), want: true, reason: "task to do"},
		{doc: nTask("task-done", func(m *model.Task) { m.Status = model.TaskDone }), reason: "closed"},
		{doc: nTask("task-failed", func(m *model.Task) {
			m.Status, m.Outcome, m.RetryAfter = model.TaskDropped, model.OutcomeFailed, "2026-11-05"
		}), reason: "closed"},
		{doc: nTask("task-later", func(m *model.Task) { m.DeferUntil = "2026-11-10" }), reason: "later"},
		{doc: nTask("task-back", func(m *model.Task) { m.DeferUntil = "2026-11-01" }), want: true, reason: "task to do"},
		{doc: nTask("task-someday", func(m *model.Task) { m.DeferUntil = model.SomedayWord }), reason: "later"},
		{doc: nTask("task-off-goal", func(m *model.Task) { m.Project = model.LinkTo("project-off") }), extra: []model.Document{offGoal}, reason: "later"},
		{doc: nTask("task-wait-after", func(m *model.Task) { m.DeferredAfter = []string{"question-open-dep"} }), extra: []model.Document{openQ}, reason: "later"},
		{doc: nTask("task-person", func(m *model.Task) { m.Assignee = "person-ada" }), reason: "waiting on person-ada"},
		{doc: nTask("task-agent", func(m *model.Task) { m.Assignee = "agent:claude" }), reason: "assigned to agent:claude"},
		{doc: nTask("task-dep-open", func(m *model.Task) { m.DependsOn = []model.Link{model.LinkTo("question-open-dep")} }), extra: []model.Document{openQ}, reason: "waits on question-open-dep"},
		{doc: nTask("task-dep-done", func(m *model.Task) { m.DependsOn = []model.Link{model.LinkTo("question-done-dep")} }), extra: []model.Document{doneQ}, want: true},
		{doc: nTask("task-queued", nil), extra: []model.Document{nRequest("request-q", model.RequestQueued, "", "task-queued")}, reason: "an agent has it (request-q)"},
		{doc: nTask("task-claimed", nil), extra: []model.Document{nRequest("request-c", model.RequestClaimed, "", "task-claimed")}, reason: "an agent has it"},
		{doc: nTask("task-proposed-req", nil), extra: []model.Document{nRequest("request-p", model.RequestProposed, "", "task-proposed-req")}, want: true},
		{doc: nTask("task-req-done", nil), extra: []model.Document{nRequest("request-d", model.RequestDone, model.ResolutionDone, "task-req-done")}, want: true},
		{doc: nTask("task-blocked", func(m *model.Task) { m.Status = model.TaskBlocked }), want: true, reason: "blocked"},
		{doc: nTask("task-blocked-until", func(m *model.Task) { m.Status, m.DeferUntil = model.TaskBlocked, "2026-11-09" }), reason: "later"},
		{doc: nTask("task-owed-far", func(m *model.Task) { m.Kind, m.Due = model.KindCommitment, "2026-11-20" }), want: true, reason: "owed"},
		{doc: nTask("task-owed-near", func(m *model.Task) { m.Kind, m.Due = model.KindCommitment, "2026-11-06" }), want: true, reason: "owed"},
		// Meetings (task kind meeting).
		{doc: nMeeting("meeting-today", "2026-11-05T15:00:00Z", "2026-11-05T16:00:00Z", nil), want: true, reason: "meeting 2026-11-05"},
		{doc: nMeeting("meeting-tomorrow", "2026-11-06T15:00:00Z", "2026-11-06T16:00:00Z", nil), want: true},
		{doc: nMeeting("meeting-next-week", "2026-11-10T15:00:00Z", "2026-11-10T16:00:00Z", nil), reason: "meeting 2026-11-10"},
		{doc: nMeeting("meeting-passed", "2026-11-05T09:00:00Z", "2026-11-05T10:00:00Z", nil), want: true, reason: "record what happened"},
		{doc: nMeeting("meeting-optional", "2026-11-05T15:00:00Z", "2026-11-05T16:00:00Z", func(m *model.Task) { m.Optional = true }), reason: "optional"},
		{doc: nMeeting("meeting-optional-maybe", "2026-11-05T15:00:00Z", "2026-11-05T16:00:00Z", func(m *model.Task) {
			m.Optional, m.Attend = true, model.AttendMaybe
		}), reason: "optional"},
		{doc: nMeeting("meeting-optional-yes", "2026-11-05T15:00:00Z", "2026-11-05T16:00:00Z", func(m *model.Task) {
			m.Optional, m.Attend = true, model.AttendYes
		}), want: true},
		{doc: nMeeting("meeting-optional-passed", "2026-11-05T09:00:00Z", "2026-11-05T10:00:00Z", func(m *model.Task) { m.Optional = true }), reason: "optional"},
		{doc: nMeeting("meeting-no", "2026-11-05T15:00:00Z", "2026-11-05T16:00:00Z", func(m *model.Task) { m.Attend = model.AttendNo }), reason: "you skip it"},
		{doc: nMeeting("meeting-held", "2026-11-05T09:00:00Z", "2026-11-05T10:00:00Z", func(m *model.Task) { m.Status = model.TaskDone }), reason: "closed"},
		{doc: nTask("meeting-requested", func(m *model.Task) { m.Kind = model.KindMeeting }), want: true, reason: "meeting to book"},
		{doc: nMeeting("meeting-later", "2026-11-05T15:00:00Z", "2026-11-05T16:00:00Z", func(m *model.Task) { m.DeferUntil = model.SomedayWord }), reason: "later"},
		// Questions.
		{doc: nQuestion("question-q", nil), want: true, reason: "question to answer"},
		{doc: nQuestion("question-proposal", func(q *model.Question) { q.ProposedBy = "agent" }), want: true, reason: "answer to check"},
		{doc: nQuestion("question-answered", func(q *model.Question) { q.Status = model.QuestionAnswered }), reason: "closed"},
		{doc: nQuestion("question-later", func(q *model.Question) { q.DeferUntil = "2026-11-06" }), reason: "later"},
		{doc: nQuestion("question-scope-off", func(q *model.Question) { q.Scope = model.LinkTo("task-off-scope") }),
			extra: []model.Document{nTask("task-off-scope", func(m *model.Task) { m.DeferUntil = model.SomedayWord })}, reason: "later"},
		{doc: nQuestion("question-sent-back", func(q *model.Question) {
			q.ReviewState, q.ReviewRequest = string(model.ItemSentBack), model.LinkTo("request-sb")
		}), extra: []model.Document{nRequest("request-sb", model.RequestQueued, "")}, reason: "an agent has it"},
		{doc: nQuestion("question-sent-back-ended", func(q *model.Question) {
			q.ReviewState, q.ReviewRequest = string(model.ItemSentBack), model.LinkTo("request-sbd")
		}), extra: []model.Document{nRequest("request-sbd", model.RequestDone, model.ResolutionDone)}, want: true},
		{doc: nQuestion("question-composer", nil), extra: []model.Document{nRequest("request-cq", model.RequestQueued, "", "question-composer")}, reason: "an agent has it"},
		// Asks.
		{doc: nRFX("rfx-in", func(m *model.RFX) { m.Direction = model.RFXIn }), want: true, reason: "asked of you"},
		{doc: nRFX("rfx-wait", func(m *model.RFX) { m.NextNudge = "2026-11-09" }), reason: "follow up 2026-11-09"},
		{doc: nRFX("rfx-wait-nodate", nil), reason: "waiting on a reply"},
		{doc: nRFX("rfx-nudge", func(m *model.RFX) { m.NextNudge = "2026-11-05" }), want: true, reason: "follow up"},
		{doc: nRFX("rfx-replied", func(m *model.RFX) { m.Targets[0].State = model.TargetReplied }), want: true, reason: "reply to sort"},
		{doc: nRFX("rfx-stalled", func(m *model.RFX) { m.Targets[0].State = model.TargetNoReply }), want: true, reason: "stalled"},
		{doc: nRFX("rfx-untargeted", func(m *model.RFX) { m.Targets = nil }), want: true, reason: "no one asked yet"},
		{doc: nRFX("rfx-closed", func(m *model.RFX) { m.Status = model.RFXClosed }), reason: "closed"},
		{doc: nRFX("rfx-declined", func(m *model.RFX) { m.Status = model.RFXDeclined }), reason: "closed"},
		// io.
		{doc: nIO("io-in", nil), want: true, reason: "to sort"},
		{doc: nIO("io-in-read", func(m *model.IO) { m.State = model.IORead }), want: true},
		{doc: nIO("io-filed", func(m *model.IO) { m.State = model.IOFiled }), reason: "closed"},
		{doc: nIO("io-later", func(m *model.IO) { m.DeferUntil = "2026-11-09" }), reason: "later"},
		{doc: nIO("io-draft", func(m *model.IO) { m.Direction, m.State = model.IOOut, model.IODraft }), want: true, reason: "draft to approve"},
		{doc: nIO("io-ready", func(m *model.IO) { m.Direction, m.State = model.IOOut, model.IOReady }), reason: "approved, not sent"},
		{doc: nIO("io-sent", func(m *model.IO) { m.Direction, m.State = model.IOOut, model.IOSent }), reason: "closed"},
		// Agent work.
		{doc: nRequest("request-proposed", model.RequestProposed, ""), want: true, reason: "agent work to accept"},
		{doc: nRequest("request-queued", model.RequestQueued, ""), reason: "an agent has it"},
		{doc: nRequest("request-claimed", model.RequestClaimed, ""), reason: "an agent has it"},
		{doc: nRequest("request-done", model.RequestDone, model.ResolutionDone), reason: "closed"},
		{doc: nRequest("request-partial", model.RequestDone, model.ResolutionPartial), reason: "closed"},
		{doc: nRequest("request-clarify", model.RequestDone, model.ResolutionNeedsClarification), want: true, reason: "needs_clarification"},
		{doc: nRequest("request-stuck", model.RequestDone, model.ResolutionStuck), want: true, reason: "stuck"},
		{doc: nRequest("request-blocked", model.RequestDone, model.ResolutionBlocked), want: true, reason: "blocked"},
		{doc: nRequest("request-failed", model.RequestFailed, model.ResolutionFailed), reason: "closed"},
		{doc: nRequest("request-cancelled", model.RequestCancelled, model.ResolutionCancelled), reason: "closed"},
		// Areas, exceptions, goals, people, incidents.
		{doc: nArea("area-never", nil), want: true, reason: "never reviewed"},
		{doc: nArea("area-due", func(a *model.Area) { a.LastReviewed = "2026-10-20" }), want: true, reason: "review weekly"},
		{doc: nArea("area-fresh", func(a *model.Area) { a.LastReviewed = "2026-11-04" }), reason: "review weekly"},
		{doc: nArea("area-no-cadence", func(a *model.Area) { a.Cadence = "" }), reason: "area"},
		{doc: nException("exception-soon", func(e *model.PolicyException) { e.ReviewBy = "2026-11-15" }), want: true, reason: "exception to review"},
		{doc: nException("exception-far", func(e *model.PolicyException) { e.ReviewBy = "2026-12-15" }), reason: "exception to review"},
		{doc: nException("exception-resolved", func(e *model.PolicyException) {
			e.ReviewBy, e.Status = "2026-11-06", model.ExceptionResolved
		}), reason: "closed"},
		{doc: goal, reason: "project"},
		{doc: person, reason: "person"},
		{doc: model.Document{ID: "incident-x", Title: "Outage", Meta: &model.Incident{Common: common(model.TypeIncident), Status: model.IncidentOpen, Severity: "sev2"}}, want: true, reason: "incident sev2"},
		{doc: model.Document{ID: "incident-r", Title: "Outage", Meta: &model.Incident{Common: common(model.TypeIncident), Status: model.IncidentResolved, Severity: "sev2"}}, reason: "closed"},
	}
	for _, c := range cases {
		docs := append([]model.Document{goal, person, c.doc}, c.extra...)
		sn := snapOf(t, docs...)
		got, why := NeedsYou(sn, c.doc, time.Time{})
		if got != c.want || !strings.Contains(why, c.reason) {
			t.Errorf("%s: needs_you %v (%s), want %v (%q)", c.doc.ID, got, why, c.want, c.reason)
		}
		// One answer: Set has the item exactly when NeedsYou says so.
		in := false
		for _, id := range sn.Set() {
			in = in || id == c.doc.ID
		}
		if in != got {
			t.Errorf("%s: in Set %v, NeedsYou %v", c.doc.ID, in, got)
		}
	}
}

// NeedsYou at another time reads the same documents on that day: a meeting
// next week needs the owner from due_soon_days before it.
func TestNeedsYouAt(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	m := nMeeting("meeting-next-week", "2026-11-10T15:00:00Z", "2026-11-10T16:00:00Z", nil)
	sn := snapOf(t, m)
	if yes, _ := NeedsYou(sn, m, needsNow); yes {
		t.Error("meeting 5 days out needs the owner today")
	}
	if yes, why := NeedsYou(sn, m, needsNow.AddDate(0, 0, 3)); !yes {
		t.Errorf("meeting 2 days out does not need the owner: %s", why)
	}
}

func TestLifecycle(t *testing.T) {
	for _, c := range []struct {
		m    model.Meta
		want Lifecycle
	}{
		{&model.Task{Status: model.TaskBlocked}, LifeOpen},
		{&model.Task{Status: model.TaskDropped}, LifeClosed},
		{&model.Meeting{Status: model.MeetingHeld}, LifeClosed},
		{&model.Meeting{Status: model.MeetingScheduled}, LifeOpen},
		{&model.Project{Status: model.ProjectPaused}, LifeOpen},
		{&model.Project{Status: model.ProjectDone}, LifeClosed},
		{&model.RFX{Status: model.RFXDeclined}, LifeClosed},
		{&model.Request{Status: model.RequestDone, Resolution: model.ResolutionStuck}, LifeOpen},
		{&model.Request{Status: model.RequestDone, Resolution: model.ResolutionDone}, LifeClosed},
		{&model.IO{State: model.IOReady}, LifeOpen},
		{&model.IO{State: model.IOSent}, LifeClosed},
		{&model.Person{}, LifeOpen},
	} {
		if got := lifeOf(c.m); got != c.want {
			t.Errorf("%T %+v: %s, want %s", c.m, c.m, got, c.want)
		}
	}
}
