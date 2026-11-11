package ops

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// obFixture is a vault at the obligation format, 2026-11-05 12:00 UTC, with
// people and a goal.
func obFixture(t *testing.T) (Deps, string, string) {
	t.Helper()
	d, proj, person := attendFixture(t)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "7"); err != nil {
		t.Fatal(err)
	}
	return d, proj, person
}

func getOb(t *testing.T, d Deps, id string) (model.Document, *model.Obligation) {
	t.Helper()
	doc, o, err := getObligation(ctx, d, id)
	if err != nil {
		t.Fatal(err)
	}
	return doc, o
}

// A commitment series with no due is not due at once: its first occurrence
// is the start's day, else one cadence from today.
func TestCommitmentSeriesWithNoDue(t *testing.T) {
	d, proj, person := obFixture(t)
	weekly := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the invoice", OwedTo: []string{person}, Recur: "weekly", Project: proj})
	if _, o := getOb(t, d, weekly.ID); o.Due != "2026-11-12" {
		t.Errorf("weekly from today: due %q, want 2026-11-12", o.Due)
	}
	started := must(t, ObligationAdd, d, ObligationAddInput{Kind: model.KindCommitment, Title: "Send the report", OwedTo: []string{person}, Recur: "monthly", Start: "2026-11-20T09:00:00Z", Project: proj})
	if _, o := getOb(t, d, started.ID); o.Due != "2026-11-20" {
		t.Errorf("from start: due %q, want 2026-11-20", o.Due)
	}
	if got := needOf(t, d, weekly.ID); !strings.Contains(got.Reason, "2026-11-12") {
		t.Errorf("need of the series: %+v", got)
	}
}

func TestObligationAddKindsAndSchedules(t *testing.T) {
	d, proj, person := obFixture(t)
	m := must(t, ObligationAdd, d, ObligationAddInput{Title: "Sync", With: []string{person}, Start: "2026-11-06T10:00:00Z", End: "2026-11-06T10:30:00Z"})
	if m.Type != model.TypeObligation || m.Folder != model.FolderObligations || m.Kind != model.KindMeeting || m.Schedule != model.ScheduleAt {
		t.Fatalf("meeting = %+v", m)
	}
	c := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the plan", OwedTo: []string{person}, Due: "2026-11-09", Project: proj})
	if _, o := getOb(t, d, c.ID); o.Kind != model.KindCommitment || o.Schedule != model.ScheduleOnce || len(o.OwedTo) != 1 || len(o.With) != 0 || o.Due != "2026-11-09" {
		t.Fatalf("commitment = %+v", o)
	}
	s := must(t, ObligationAdd, d, ObligationAddInput{Title: "Weekly 1:1", With: []string{person}, Start: "2026-11-06T09:00:00Z", End: "2026-11-06T09:30:00Z", Recur: "weekly"})
	if s.Schedule != model.ScheduleSeries {
		t.Fatalf("series = %+v", s)
	}
	g := must(t, ObligationAdd, d, ObligationAddInput{Title: "Keep the runbook current", Cadence: "monthly", OwedTo: []string{person}})
	if g.Schedule != model.ScheduleOngoing {
		t.Fatalf("ongoing = %+v", g)
	}
	// The reads of a task, a show, and a brief take an obligation.
	if v := must(t, TaskShow, d, TaskShowInput{ID: m.ID}); v.Task.Kind != model.KindMeeting {
		t.Fatalf("task_show = %+v", v.Task)
	}
	if v := must(t, TaskShow, d, TaskShowInput{ID: c.ID}); v.Task.Kind != model.KindCommitment || len(v.Task.With) != 1 {
		t.Fatalf("task_show commitment = %+v", v.Task)
	}
	if v := must(t, Show, d, ShowInput{ID: c.ID}); v.Type != model.TypeObligation || v.Status != "open" || !v.NeedsYou {
		t.Fatalf("show = %+v", v)
	}
	if b := must(t, Brief, d, BriefInput{ID: m.ID}); b.Type != "meeting" || len(b.People) != 1 {
		t.Fatalf("brief = %+v", b)
	}
	for name, in := range map[string]ObligationAddInput{
		"ongoing with a date": {Title: "x", Cadence: "monthly", Due: "2026-11-09"},
		"recur on once":       {Title: "x", Schedule: model.ScheduleOnce, Recur: "weekly"},
		"series with no rule": {Title: "x", Schedule: model.ScheduleSeries},
		"bad kind":            {Title: "x", Kind: model.KindCall},
		"meeting fields":      {Title: "x", Kind: model.KindCommitment, With: []string{person}},
		"meeting nobody":      {Title: "x", Kind: model.KindMeeting},
	} {
		if _, err := ObligationAdd.Run(ctx, d, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Below the obligation format the ops write the older types, so a para not
// yet upgraded reads the vault.
func TestObligationAddBelowTheFormatWritesATask(t *testing.T) {
	d, proj, person := attendFixture(t)
	out := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send it", OwedTo: []string{person}, Due: "2026-11-09", Project: proj})
	doc, err := d.Store.Get(ctx, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tk, ok := doc.Meta.(*model.Task); !ok || tk.EffectiveKind() != model.KindCommitment || len(tk.With) != 1 || out.Type != model.TypeTask || len(out.Warnings) == 0 {
		t.Fatalf("below format 7 = %+v %#v", out, doc.Meta)
	}
	if _, err := ObligationAdd.Run(ctx, d, ObligationAddInput{Title: "x", Cadence: "monthly"}); err == nil || !strings.Contains(err.Error(), "vault upgrade") {
		t.Fatalf("ongoing below format 7 = %v", err)
	}
	if m := must(t, MeetingAdd, d, MeetingAddInput{Title: "Sync", With: []string{person}}); m.Folder != model.FolderProjects {
		t.Fatalf("meeting below format 7 = %+v", m)
	}
}

// The old names store obligations at format 7: task_add kind commitment
// (parent and assignee kept as parent and owed_by), and rfx_add direction in.
func TestOlderAddsStoreObligations(t *testing.T) {
	d, proj, person := obFixture(t)
	parent := must(t, TaskAdd, d, TaskAddInput{Title: "Parent", Project: proj})
	c := must(t, TaskAdd, d, TaskAddInput{Title: "Ada sends the doc", Kind: model.KindCommitment, Due: "2026-11-09", Project: proj, Parent: parent.ID, Assignee: person})
	_, o := getOb(t, d, c.ID)
	if o.Parent != model.LinkTo(parent.ID) || len(o.OwedBy) != 1 || o.OwedBy[0] != model.LinkTo(person) || !strings.Contains(strings.Join(c.Warnings, " "), "alias of obligation_add") {
		t.Fatalf("task_add commitment = %+v %+v", o, c.Warnings)
	}
	parts, err := taskParts(ctx, d.Store, parent.ID)
	if err != nil || len(parts.Commitments) != 1 || parts.Commitments[0].Owner != person {
		t.Fatalf("parts = %+v, %v", parts, err)
	}
	r := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXIn, Ask: "Review my doc", Route: "logging", Targets: []TargetInput{{To: person, Medium: model.MediumEmail}}})
	_, ro := getOb(t, d, r.ID)
	if ro.Kind != model.KindCommitment || len(ro.OwedTo) != 1 || ro.FoldedFrom != "" || r.Folder != model.FolderObligations {
		t.Fatalf("rfx_add in = %+v %+v", ro, r)
	}
	if out := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Ask Ada", Route: "logging", Targets: []TargetInput{{To: person, Medium: model.MediumEmail}}}); out.Folder != model.FolderRFX {
		t.Fatalf("rfx_add out = %+v", out)
	}
}

// meeting_add and obligation_add of a meeting write the same file; the
// meeting aliases say alias_of; meeting_rsvp keeps its person check.
func TestMeetingAliasesParity(t *testing.T) {
	in := `{"title":"Sync","with":["person-ada-example"],"start":"2026-11-06T10:00:00Z","end":"2026-11-06T10:30:00Z","optional":true}`
	var metas []string
	for _, name := range []string{"meeting_add", "obligation_add"} {
		d, _, _ := obFixture(t)
		e := Alias(name)
		if e == nil {
			e = registry[name]
		}
		res, err := e.Invoke(ctx, d, json.RawMessage(in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := json.Marshal(res)
		if name == "meeting_add" && !strings.Contains(string(b), `"alias_of":"obligation_add"`) {
			t.Fatalf("meeting_add output = %s", b)
		}
		var out struct{ ID string }
		_ = json.Unmarshal(b, &out)
		doc, o := getOb(t, d, out.ID)
		o.Created, o.Updated = "", ""
		mb, _ := json.Marshal(o)
		metas = append(metas, string(mb)+"\n"+doc.Body)
	}
	if metas[0] != metas[1] {
		t.Fatalf("meeting_add and obligation_add differ:\n%s\n%s", metas[0], metas[1])
	}

	d, _, person := obFixture(t)
	id := addMeeting(t, d, "Standup", person, "2026-11-06T10:00:00Z", "2026-11-06T10:15:00Z", true)
	a := Alias("meeting_attend").(*aliasEntry)
	raw := json.RawMessage(`{"id":"` + id + `","attend":"yes"}`)
	if !a.Routed(ctx, d, raw) {
		t.Fatal("meeting_attend does not route to obligation_attend")
	}
	res, err := a.Invoke(ctx, d, raw)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(res); !strings.Contains(string(b), `"alias_of":"obligation_attend"`) {
		t.Fatalf("meeting_attend output = %s", b)
	}
	if _, o := getOb(t, d, id); o.Attend != model.AttendYes {
		t.Fatalf("attend = %q", o.Attend)
	}
	agent := d
	agent.Store = d.Store.ActAs("claude", "mcp")
	if _, err := Alias("meeting_rsvp").Invoke(ctx, agent, raw); err == nil || !strings.Contains(err.Error(), ErrPersonOnly.Error()) {
		t.Fatalf("meeting_rsvp from an agent = %v", err)
	}
	for _, name := range []string{"meeting_add", "meeting_schedule", "meeting_attend", "meeting_rsvp", "meeting_show", "meeting_hold", "meeting_cancel"} {
		if AliasOf(name) == "" {
			t.Errorf("%s is not an alias", name)
		}
	}
}

// A series makes its next occurrence when one closes, one at a time:
// occurrence_of the first, previous the one before, the slot a week on, attend
// from attend_default, optional carried.
func TestObligationSeriesNextOccurrence(t *testing.T) {
	d, _, person := obFixture(t)
	first := must(t, ObligationAdd, d, ObligationAddInput{Title: "Weekly", With: []string{person}, Start: "2026-11-05T09:00:00Z", End: "2026-11-05T09:30:00Z",
		Recur: "weekly", Optional: true}).ID
	must(t, ObligationAttend, d, MeetingAttendInput{ID: first, Attend: "yes", AllFuture: true})
	out := must(t, Close, d, CloseInput{ID: first, Outcome: "skipped", Reason: "Out sick."})
	if out.Op != "close" {
		t.Fatalf("close routed to %s", out.Op)
	}
	doc, o := getOb(t, d, first)
	if o.State != model.StateClosed || o.Outcome != model.OutcomeSkipped || o.Closed != "2026-11-05" || !strings.Contains(doc.Body, "Out sick.") {
		t.Fatalf("closed = %+v", o)
	}
	second, _ := successor(ctx, d, first)
	if second == "" {
		t.Fatal("no next occurrence")
	}
	_, n := getOb(t, d, second)
	if n.OccurrenceOf != model.LinkTo(first) || n.Previous != model.LinkTo(first) || n.Scheduled != "2026-11-12T09:00:00Z" || n.End != "2026-11-12T09:30:00Z" ||
		!n.Optional || n.Attend != model.AttendYes || n.State != model.StateOpen || n.Schedule != model.ScheduleSeries {
		t.Fatalf("second = %+v", n)
	}
	// Held through meeting_hold (close with no outcome) makes the third.
	must(t, Close, d, CloseInput{ID: second, Reason: "Agreed the plan."})
	third, _ := successor(ctx, d, second)
	if _, o3 := getOb(t, d, third); o3.OccurrenceOf != model.LinkTo(first) || o3.Previous != model.LinkTo(second) || o3.Scheduled != "2026-11-19T09:00:00Z" {
		t.Fatalf("third = %+v", o3)
	}
	if _, o2 := getOb(t, d, second); o2.Outcome != model.OutcomeDone {
		t.Fatalf("held = %+v", o2)
	}
	// Reopen and close again: the next one exists, no second is made.
	must(t, Reopen, d, ReopenInput{ID: first})
	must(t, Close, d, CloseInput{ID: first, Outcome: "declined"})
	if again, _ := successor(ctx, d, first); again != second {
		t.Fatalf("successor after reclose = %s", again)
	}
	if _, err := Close.Run(ctx, d, CloseInput{ID: first, Outcome: "declined"}); err == nil {
		t.Fatal("closing a closed obligation succeeded")
	}
}

// The attendance matrix and needs_you of an obligation meeting: a required
// one soon needs the owner; optional with no yes, and attend no, ask nothing;
// one that ended asks to record what happened.
func TestObligationAttendMatrix(t *testing.T) {
	d, _, person := obFixture(t)
	soon := func(title string, optional bool) string {
		return addMeeting(t, d, title, person, "2026-11-06T10:00:00Z", "2026-11-06T10:30:00Z", optional)
	}
	required, opt, optYes, optMaybe, skipped := soon("Required", false), soon("Optional", true), soon("Optional yes", true), soon("Optional maybe", true), soon("Skipped", false)
	must(t, ObligationAttend, d, MeetingAttendInput{ID: optYes, Attend: "yes"})
	must(t, ObligationAttend, d, MeetingAttendInput{ID: optMaybe, Attend: "maybe"})
	must(t, ObligationAttend, d, MeetingAttendInput{ID: skipped, Attend: "no"})
	past := addMeeting(t, d, "Past", person, "2026-11-04T10:00:00Z", "2026-11-04T10:30:00Z", false)
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{required: true, opt: false, optYes: true, optMaybe: false, skipped: false, past: true} {
		if n := sn.Need(id); n.Yes != want {
			t.Errorf("%s: needs_you %v (%s), want %v", id, n.Yes, n.Reason, want)
		}
	}
	if n := sn.Need(past); n.Reason != "record what happened" {
		t.Errorf("past: %q", n.Reason)
	}
	if !resolved(sn.docs[skipped]) {
		t.Error("a meeting the owner skips holds what waits on it")
	}
	// The agenda lists the required meeting with its time, the optional ones apart.
	items, sk, optional, err := agendaItems(ctx, d, newWindow(d.Store.Now(), time.UTC, 7), true)
	if err != nil {
		t.Fatal(err)
	}
	has := func(list []AgendaItem, id string) bool {
		for _, it := range list {
			if it.ID == id && it.Kind == AgendaMeeting && it.Time == "10:00" {
				return true
			}
		}
		return false
	}
	if !has(items, required) || !has(items, optYes) || !has(optional, opt) || !has(sk, skipped) {
		t.Fatalf("agenda items %+v optional %+v skipped %+v", items, optional, sk)
	}
}

// needs_you rows of obligations by kind and state.
func TestObligationNeedsYou(t *testing.T) {
	d, proj, person := obFixture(t)
	owed := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the plan", OwedTo: []string{person}, Due: "2026-11-20", Project: proj}).ID
	theirs := must(t, ObligationAdd, d, ObligationAddInput{Title: "Ada sends the doc", OwedBy: []string{person}, Due: "2026-11-20"}).ID
	ongoing := must(t, ObligationAdd, d, ObligationAddInput{Title: "Keep the runbook", Cadence: "monthly"}).ID
	reviewed := must(t, ObligationAdd, d, ObligationAddInput{Title: "Review access", Cadence: "monthly"}).ID
	must(t, ObligationSchedule, d, ObligationScheduleInput{ID: reviewed, Reviewed: true})
	later := must(t, ObligationAdd, d, ObligationAddInput{Title: "Later one", OwedTo: []string{person}}).ID
	must(t, Later, d, LaterInput{ID: later, Until: "2026-12-01"})
	closed := must(t, ObligationAdd, d, ObligationAddInput{Title: "Done one", OwedTo: []string{person}}).ID
	must(t, Close, d, CloseInput{ID: closed})
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id     string
		life   Lifecycle
		turn   Turn
		yes    bool
		reason string
	}{
		{owed, LifeOpen, TurnOwner, true, "owed, due 2026-11-20"},
		{theirs, LifeOpen, TurnWaiting, false, "owed by " + person},
		{ongoing, LifeOpen, TurnOwner, true, "review monthly; never reviewed"},
		{reviewed, LifeOpen, TurnOwner, false, "review monthly"},
		{later, LifeLater, TurnNone, false, "later"},
		{closed, LifeClosed, TurnNone, false, "closed"},
	} {
		n := sn.Need(c.id)
		if n.Life != c.life || n.Turn != c.turn || n.Yes != c.yes || n.Reason != c.reason {
			t.Errorf("%s: %+v, want %v %v %v %q", c.id, n, c.life, c.turn, c.yes, c.reason)
		}
	}
	if n := sn.Need(reviewed); n.On != "2026-12-05" {
		t.Errorf("reviewed: next review %q", n.On)
	}
	// The order lists what needs the owner; the commitment owed is an owed row.
	rows := orderIDs(t, d)
	if r, ok := rows[owed]; !ok || !r.Owed {
		t.Errorf("order row of %s = %+v", owed, r)
	}
	if _, ok := rows[theirs]; ok {
		t.Errorf("order lists %s, owed by someone else", theirs)
	}
}

// Tick marks a past optional obligation meeting skipped, as it does a task meeting.
func TestTickSkipsPastOptionalObligation(t *testing.T) {
	d, _, person := obFixture(t)
	id := addMeeting(t, d, "Optional past", person, "2026-11-04T10:00:00Z", "2026-11-04T10:30:00Z", true)
	out := TickOutput{}
	if err := tickOptionalMeetings(ctx, d, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.MeetingsSkipped) != 1 || out.MeetingsSkipped[0] != id {
		t.Fatalf("skipped = %v", out.MeetingsSkipped)
	}
	if _, o := getOb(t, d, id); o.Attend != model.AttendNo {
		t.Fatalf("attend = %q", o.Attend)
	}
}
