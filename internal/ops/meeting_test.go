package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// meetingFixture is a person, a project, an outbound request in the project,
// and two open questions: one in the project and one in another.
type meetingFixture struct {
	d                    Deps
	person, project, rfx string
	inScope, outOfScope  string
}

func newMeetingFixture(t *testing.T) meetingFixture {
	t.Helper()
	d := newDeps(t)
	seedOrg(t, d)
	f := meetingFixture{d: d, person: "person-ada-example"}
	f.project = must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	other := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Elsewhere"}).ID
	f.rfx = must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Can the portal launch before the audit?", Route: "domain-logging",
		SelfServe: "read policies", Project: f.project, Targets: []TargetInput{{To: f.person, Medium: model.MediumEmail}}}).ID
	f.inScope = must(t, QuestionAdd, d, QuestionAddInput{Text: "Which window does the audit use?", Project: f.project}).ID
	f.outOfScope = must(t, QuestionAdd, d, QuestionAddInput{Text: "Unrelated thing?", Project: other}).ID
	return f
}

func targetOf(t *testing.T, d Deps, rfx, person string, medium model.Medium) model.Target {
	t.Helper()
	_, m, err := getRFX(ctx, d, rfx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range m.Targets {
		if tg.To.Target() == person && tg.Medium == medium {
			return tg
		}
	}
	t.Fatalf("%s has no %s target for %s", rfx, medium, person)
	return model.Target{}
}

func waitingIDs(t *testing.T, d Deps) []string {
	t.Helper()
	out := must(t, RFXWaiting, d, RFXWaitingInput{Today: "2026-11-01"})
	var ids []string
	for _, it := range out.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestMeetingAskScheduleCancel(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	// Asking for a meeting is progress: the request has a requested target and
	// stays on the waiting list.
	add := must(t, MeetingAdd, d, MeetingAddInput{Title: "Launch window sync", With: []string{f.person}, RFX: []string{f.rfx}})
	if add.Status != model.MeetingRequested || !slices.Equal(add.RFX, []string{f.rfx}) {
		t.Fatalf("add = %+v", add)
	}
	if got := targetOf(t, d, f.rfx, f.person, model.MediumMeeting).State; got != model.TargetRequested {
		t.Errorf("target = %s, want requested", got)
	}
	if !slices.Contains(waitingIDs(t, d), f.rfx) {
		t.Error("a requested meeting must keep the request on the waiting list")
	}
	// The body gathers the request and the open question in scope, not the other.
	show := must(t, MeetingShow, d, MeetingShowInput{ID: add.ID})
	for _, want := range []string{questionsHeading, "[[" + f.rfx + "]]", "[[" + f.inScope + "]]", notesHeading} {
		if !strings.Contains(show.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, show.Body)
		}
	}
	if strings.Contains(show.Body, f.outOfScope) {
		t.Errorf("body lists a question out of scope:\n%s", show.Body)
	}
	if strings.Index(show.Body, questionsHeading) > strings.Index(show.Body, notesHeading) {
		t.Error("Questions must come before Notes")
	}

	// Booking moves the target to scheduled; the meeting is next, not a wait.
	sch := must(t, MeetingSchedule, d, MeetingScheduleInput{ID: add.ID, Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00"})
	if sch.Status != model.MeetingScheduled || sch.Start != "2026-11-04T10:00:00-05:00" {
		t.Fatalf("schedule = %+v", sch)
	}
	if got := targetOf(t, d, f.rfx, f.person, model.MediumMeeting).State; got != model.TargetScheduled {
		t.Errorf("target = %s, want scheduled", got)
	}
	if slices.Contains(waitingIDs(t, d), f.rfx) {
		t.Error("a scheduled meeting is not a wait; the email target is still drafted")
	}

	// Cancelling steps back, so the ask is open again.
	can := must(t, MeetingCancel, d, MeetingCancelInput{ID: add.ID, Reason: "Jordan is out."})
	if can.Status != model.MeetingCancelled {
		t.Fatalf("cancel = %+v", can)
	}
	if got := targetOf(t, d, f.rfx, f.person, model.MediumMeeting).State; got != model.TargetRequested {
		t.Errorf("target after cancel = %s, want requested", got)
	}
	if _, err := MeetingSchedule.Run(ctx, d, MeetingScheduleInput{ID: add.ID, Start: "2026-11-05T10:00:00Z", End: "2026-11-05T11:00:00Z"}); err == nil {
		t.Error("a cancelled meeting must not be scheduled again")
	}
	if _, err := MeetingCancel.Run(ctx, d, MeetingCancelInput{ID: add.ID}); err == nil {
		t.Error("cancelling twice must fail")
	}
	if !strings.Contains(must(t, MeetingShow, d, MeetingShowInput{ID: add.ID}).Body, "Cancelled. Jordan is out.") {
		t.Error("the cancel reason must reach the notes")
	}
	mustLintClean(t, d)
}

func TestMeetingAddScheduledAndDefaults(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	// With no `with`, the meeting is with whoever the request goes to.
	add := must(t, MeetingAdd, d, MeetingAddInput{Title: "Sync", RFX: []string{f.rfx}, Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00"})
	if add.Status != model.MeetingScheduled {
		t.Errorf("status = %s", add.Status)
	}
	if got := targetOf(t, d, f.rfx, f.person, model.MediumMeeting).State; got != model.TargetScheduled {
		t.Errorf("target = %s", got)
	}
	show := must(t, MeetingShow, d, MeetingShowInput{ID: add.ID})
	if !slices.Equal(show.Meeting.With, []string{f.person}) {
		t.Errorf("with = %v", show.Meeting.With)
	}
	for _, c := range []struct {
		name string
		in   MeetingAddInput
		want string
	}{
		{"no one", MeetingAddInput{Title: "Alone"}, "needs someone"},
		{"no title", MeetingAddInput{With: []string{f.person}}, "title"},
		{"start only", MeetingAddInput{Title: "x", With: []string{f.person}, Start: "2026-11-04T10:00:00Z"}, "both or neither"},
		{"not a person", MeetingAddInput{Title: "x", With: []string{f.project}}, "want one of"},
		{"not a request", MeetingAddInput{Title: "x", With: []string{f.person}, RFX: []string{f.project}}, "want one of"},
		{"missing request", MeetingAddInput{Title: "x", With: []string{f.person}, RFX: []string{"rfx-nope"}}, "rfx"},
	} {
		if _, err := MeetingAdd.Run(ctx, d, c.in); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	// A closed request refuses the meeting, and nothing is made.
	must(t, RFXClose, d, RFXCloseInput{ID: f.rfx})
	before := len(must(t, MeetingList, d, MeetingListInput{All: true}).Meetings)
	if _, err := MeetingAdd.Run(ctx, d, MeetingAddInput{Title: "x", With: []string{f.person}, RFX: []string{f.rfx}}); err == nil {
		t.Error("a closed request must refuse a meeting")
	}
	if after := len(must(t, MeetingList, d, MeetingListInput{All: true}).Meetings); after != before {
		t.Errorf("a refused add left %d meetings, had %d", after, before)
	}
	mustLintClean(t, d)
}

// Times are stored as written and compared as instants, so a meeting over
// midnight, or over a clock change, is valid and keeps its zone.
func TestMeetingSlotZones(t *testing.T) {
	cases := []struct {
		name, start, end string
		wantErr          string
	}{
		{"same zone", "2026-11-04T10:00:00-05:00", "2026-11-04T10:30:00-05:00", ""},
		{"over midnight", "2026-11-04T23:30:00-05:00", "2026-11-05T00:30:00-05:00", ""},
		{"midnight in UTC is the previous evening in New York", "2026-11-05T00:00:00Z", "2026-11-05T01:00:00Z", ""},
		{"different zones, one instant apart", "2026-11-04T23:00:00-05:00", "2026-11-05T05:00:00+01:00", "not after"},
		{"different zones, valid", "2026-11-04T23:00:00-05:00", "2026-11-05T05:30:00+01:00", ""},
		{"spring forward gap: 01:30 EST to 03:30 EDT is one hour", "2026-03-08T01:30:00-05:00", "2026-03-08T03:30:00-04:00", ""},
		{"fall back: end reads earlier on the clock", "2026-11-01T01:30:00-04:00", "2026-11-01T01:15:00-05:00", ""},
		{"fall back: wall clock later but instant earlier", "2026-11-01T01:30:00-05:00", "2026-11-01T01:45:00-04:00", "not after"},
		{"equal", "2026-11-04T10:00:00Z", "2026-11-04T10:00:00Z", "not after"},
		{"no zone", "2026-11-04T10:00:00", "2026-11-04T10:30:00", "RFC 3339"},
		{"date only", "2026-11-04", "2026-11-05", "RFC 3339"},
		{"end before start", "2026-11-04T11:00:00Z", "2026-11-04T10:00:00Z", "not after"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newDeps(t)
			seedOrg(t, d)
			add, err := MeetingAdd.Run(ctx, d, MeetingAddInput{Title: "Zone check", With: []string{"person-ada-example"}, Start: c.start, End: c.end})
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			doc, m, err := getMeeting(ctx, d.Store, add.ID)
			if err != nil {
				t.Fatal(err)
			}
			if m.Start != c.start || m.End != c.end {
				t.Errorf("stored %s .. %s, want the zone kept: %s .. %s", m.Start, m.End, c.start, c.end)
			}
			_ = doc
			mustLintClean(t, d)
		})
	}
}

func TestMeetingListOrder(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	late := must(t, MeetingAdd, d, MeetingAddInput{Title: "Late", With: []string{f.person}, Start: "2026-11-06T10:00:00Z", End: "2026-11-06T11:00:00Z"}).ID
	// 2026-11-05T01:00Z is earlier than 2026-11-05T00:30-05:00 (05:30Z), whatever the text order.
	early := must(t, MeetingAdd, d, MeetingAddInput{Title: "Early", With: []string{f.person}, Start: "2026-11-05T01:00:00Z", End: "2026-11-05T02:00:00Z"}).ID
	ask := must(t, MeetingAdd, d, MeetingAddInput{Title: "Ask", With: []string{f.person}}).ID
	zoned := must(t, MeetingAdd, d, MeetingAddInput{Title: "Zoned", With: []string{f.person}, Start: "2026-11-05T00:30:00-05:00", End: "2026-11-05T01:30:00-05:00"}).ID
	held := must(t, MeetingAdd, d, MeetingAddInput{Title: "Held", With: []string{f.person}, Start: "2026-10-01T10:00:00Z", End: "2026-10-01T11:00:00Z"}).ID
	doc, m, err := getMeeting(ctx, d.Store, held)
	if err != nil {
		t.Fatal(err)
	}
	m.Status = model.MeetingHeld
	m.sync()
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range must(t, MeetingList, d, MeetingListInput{}).Meetings {
		got = append(got, v.ID)
	}
	if want := []string{early, zoned, late, ask}; !slices.Equal(got, want) {
		t.Errorf("default list = %v, want %v", got, want)
	}
	got = nil
	for _, v := range must(t, MeetingList, d, MeetingListInput{All: true}).Meetings {
		got = append(got, v.ID)
	}
	if want := []string{early, zoned, late, ask, held}; !slices.Equal(got, want) {
		t.Errorf("all = %v, want %v", got, want)
	}
	if _, err := MeetingList.Run(ctx, d, MeetingListInput{Status: "soon"}); err == nil {
		t.Error("a bad status must fail")
	}
}

func TestTargetStateRequestedNeedsMeetingMedium(t *testing.T) {
	f := newMeetingFixture(t)
	_, err := RFXTargetState.Run(ctx, f.d, RFXTargetStateInput{ID: f.rfx, To: f.person, State: model.TargetRequested})
	if err == nil || !strings.Contains(err.Error(), "meeting") {
		t.Errorf("err = %v, want a refusal that names meeting", err)
	}
	must(t, RFXTargetAdd, f.d, RFXTargetAddInput{ID: f.rfx, To: f.person, Medium: model.MediumMeeting})
	out := must(t, RFXTargetState, f.d, RFXTargetStateInput{ID: f.rfx, To: f.person, Medium: model.MediumMeeting, State: model.TargetRequested})
	if !out.Waiting {
		t.Error("a requested meeting target is a wait")
	}
	mustLintClean(t, f.d)
}

// A meeting with several people puts a meeting target on a request only for
// the people that request already goes to.
func TestMeetingTargetsFollowTheRequest(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	other := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Second ask", Route: "domain-logging", SelfServe: "x",
		Targets: []TargetInput{{To: "person-bo-example", Medium: model.MediumEmail}}}).ID
	must(t, MeetingAdd, d, MeetingAddInput{Title: "Both", With: []string{f.person, "person-bo-example"}, RFX: []string{f.rfx, other}})
	_, m, _ := getRFX(ctx, d, f.rfx)
	for _, tg := range m.Targets {
		if tg.Medium == model.MediumMeeting && tg.To.Target() != f.person {
			t.Errorf("%s got a meeting target for %s", f.rfx, tg.To)
		}
	}
	targetOf(t, d, other, "person-bo-example", model.MediumMeeting)
	// A request that goes to nobody in the meeting asks everyone in it.
	third := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Third ask", Route: "domain-logging", SelfServe: "x"}).ID
	must(t, MeetingAdd, d, MeetingAddInput{Title: "Third", With: []string{f.person}, RFX: []string{third}})
	targetOf(t, d, third, f.person, model.MediumMeeting)
}
