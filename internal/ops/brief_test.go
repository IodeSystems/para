package ops

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// briefFixture is a weekly meeting with Ada and Bo whose last occurrence was
// held with notes, and an attendee kept as text who is not in the directory.
// Ada has an ask of the owner's, an ask of hers, a commitment owed to her, a
// task waiting on her reply, and a question asked of her; Bo has a task.
type briefFixture struct {
	d                                  Deps
	meeting, prev                      string
	ada, bo                            string
	askOut, askIn, owed, waits, bosJob string
	question, settle, action, oldItem  string
}

func newBriefFixture(t *testing.T) briefFixture {
	t.Helper()
	d := newDeps(t)
	seedOrg(t, d)
	f := briefFixture{d: d, ada: "person-ada-example", bo: "person-bo-example"}
	project := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	f.question = must(t, QuestionAdd, d, QuestionAddInput{Text: "Which window does the audit use?", Project: project}).ID
	f.askOut = must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Can the portal launch before the audit?", Route: "domain-logging",
		SelfServe: "read policies", Project: project, Question: f.question, Targets: []TargetInput{{To: f.ada, Medium: model.MediumEmail}}}).ID
	f.settle = must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Who signs off the launch?", Route: "domain-logging",
		SelfServe: "read policies", Project: project, Targets: []TargetInput{{To: f.bo, Medium: model.MediumEmail}}}).ID
	f.askIn = must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXIn, Ask: "Send the retention numbers", Route: "domain-logging",
		Targets: []TargetInput{{To: f.ada, Medium: model.MediumEmail}}}).ID
	f.owed = must(t, TaskAdd, d, TaskAddInput{Project: project, Title: "Draft the rollout note for Ada", Kind: model.KindCommitment, With: []string{f.ada}, Due: "2026-11-20"}).ID
	f.waits = must(t, TaskAdd, d, TaskAddInput{Project: project, Title: "Book the launch slot", DependsOn: []string{f.askOut}}).ID
	f.bosJob = must(t, TaskAdd, d, TaskAddInput{Project: project, Title: "Check the log volume", Assignee: f.bo}).ID

	// A weekly meeting: holding one occurrence makes the next.
	f.prev = must(t, TaskAdd, d, TaskAddInput{Project: project, Title: "Portal sync", Kind: model.KindMeeting, Recur: "weekly",
		Scheduled: "2026-10-28T10:00:00-04:00", End: "2026-10-28T10:30:00-04:00", With: []string{f.ada, f.bo}}).ID
	f.oldItem = must(t, TaskAdd, d, TaskAddInput{Project: project, Title: "Send Ada the audit dates", Parent: f.prev}).ID
	held := must(t, MeetingHold, d, MeetingHoldInput{ID: f.prev, Notes: "Agreed the audit window comes first."})
	if held.Next == "" {
		t.Fatalf("hold made no next occurrence: %+v", held)
	}
	f.meeting = held.Next
	doc, err := d.Store.Get(ctx, f.meeting)
	if err != nil {
		t.Fatal(err)
	}
	m := doc.Meta.(*model.Task)
	m.Attendees = []string{"Zed Stranger <zed@elsewhere.example>"}
	m.RFX = []model.Link{model.LinkTo(f.settle)}
	m.DoneWhen = "We know who signs off."
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	f.action = must(t, TaskAdd, d, TaskAddInput{Project: project, Title: "Bring the launch checklist", Parent: f.meeting}).ID
	return f
}

func briefPerson(t *testing.T, out BriefOutput, id string) BriefPerson {
	t.Helper()
	for _, p := range out.People {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("brief has no person %s: %+v", id, out.People)
	return BriefPerson{}
}

func lineIDs(ls []BriefLine) []string {
	out := []string{}
	for _, l := range ls {
		out = append(out, l.ID)
	}
	return out
}

func TestBriefMeeting(t *testing.T) {
	f := newBriefFixture(t)
	out := must(t, Brief, f.d, BriefInput{ID: f.meeting})
	if out.Type != "meeting" || out.DoneWhen != "We know who signs off." {
		t.Errorf("head = %s %q", out.Type, out.DoneWhen)
	}
	ada := briefPerson(t, out, f.ada)
	if !ada.Resolved || ada.Name != "Ada Example" {
		t.Errorf("ada = %+v", ada)
	}
	if got := lineIDs(ada.YouAsked); !slices.Equal(got, []string{f.askOut}) {
		t.Errorf("ada you_asked = %v, want %s", got, f.askOut)
	}
	if got := lineIDs(ada.TheyAsked); !slices.Contains(got, f.askIn) || !slices.Contains(got, f.owed) || len(got) != 2 {
		t.Errorf("ada they_asked = %v, want %s and %s", got, f.askIn, f.owed)
	}
	if got := lineIDs(ada.Tasks); !slices.Equal(got, []string{f.waits}) {
		t.Errorf("ada tasks = %v, want %s", got, f.waits)
	}
	if got := lineIDs(ada.Questions); !slices.Equal(got, []string{f.question}) {
		t.Errorf("ada questions = %v, want %s", got, f.question)
	}
	for _, l := range append(append(ada.YouAsked, ada.TheyAsked...), ada.Tasks...) {
		if l.Reason == "" || l.ID == "" {
			t.Errorf("line without id or reason: %+v", l)
		}
	}
	bo := briefPerson(t, out, f.bo)
	if got := lineIDs(bo.Tasks); !slices.Equal(got, []string{f.bosJob}) {
		t.Errorf("bo tasks = %v, want %s", got, f.bosJob)
	}
	// The ask the meeting should settle is linked, not listed under Bo's asks
	// twice in the linked list.
	if got := lineIDs(bo.YouAsked); !slices.Equal(got, []string{f.settle}) {
		t.Errorf("bo you_asked = %v", got)
	}
	// The attendee kept as text is still not in the directory.
	var zed *BriefPerson
	for i, p := range out.People {
		if p.Text != "" && !p.Resolved {
			zed = &out.People[i]
		}
	}
	if zed == nil || zed.ID != "" || !strings.Contains(zed.Unresolved, "not in the directory") {
		t.Fatalf("unresolved attendee = %+v", zed)
	}
	linked := lineIDs(out.Linked)
	for _, want := range []string{f.settle, f.action, f.oldItem} {
		if !slices.Contains(linked, want) {
			t.Errorf("linked %v lacks %s", linked, want)
		}
	}
	if out.Previous == nil || out.Previous.ID != f.prev || !strings.Contains(out.Previous.Notes, "audit window comes first") {
		t.Fatalf("previous = %+v", out.Previous)
	}
	for _, want := range []string{"# Brief: Portal sync", "[[" + f.askOut + "]]", "(unresolved)", "## Last time", "audit window comes first", "Done when: We know who signs off."} {
		if !strings.Contains(out.Markdown, want) {
			t.Errorf("markdown lacks %q:\n%s", want, out.Markdown)
		}
	}
	// The same vault gives the same brief.
	again := must(t, Brief, f.d, BriefInput{ID: f.meeting})
	if !reflect.DeepEqual(out, again) {
		t.Error("brief is not deterministic")
	}
}

func TestBriefResolvesAttendeeAddedLater(t *testing.T) {
	f := newBriefFixture(t)
	must(t, OrgPersonAdd, f.d, OrgAddInput{Name: "Zed Stranger", Org: "elsewhere.example"})
	out := must(t, Brief, f.d, BriefInput{ID: f.meeting})
	for _, p := range out.People {
		if !p.Resolved {
			t.Errorf("attendee in the directory now is unresolved: %+v", p)
		}
	}
}

func TestBriefRefusesItemWithoutPeople(t *testing.T) {
	d := newDeps(t)
	project := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Solo"}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Project: project, Title: "Think"}).ID
	if _, err := Brief.Run(ctx, d, BriefInput{ID: task}); err == nil || !strings.Contains(err.Error(), "names no people") {
		t.Errorf("err = %v", err)
	}
	if _, err := Brief.Run(ctx, d, BriefInput{ID: project}); err == nil {
		t.Error("a goal has no brief")
	}
}

func TestAgentSendFileNotes(t *testing.T) {
	f := newBriefFixture(t)
	d := f.d
	if _, err := AgentSend.Run(ctx, d, AgentSendInput{ID: f.meeting, FileNotes: true}); err == nil || !strings.Contains(err.Error(), "no notes") {
		t.Fatalf("no notes: err = %v", err)
	}
	if _, err := NotesEdit.Run(ctx, d, NotesEditInput{ID: f.meeting, MD: "Decided: Bo signs off.\nAda sends the numbers by Friday."}); err != nil {
		t.Fatal(err)
	}
	out, err := AgentSend.Run(ctx, d, AgentSendInput{ID: f.meeting, FileNotes: true})
	if err != nil {
		t.Fatal(err)
	}
	doc, r, err := getRequest(ctx, d.Store, out.Request)
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != MeetingNotesKind || !slices.Equal(targets(r.Links), []string{f.meeting}) {
		t.Errorf("request kind %q links %v", r.Kind, r.Links)
	}
	for _, want := range []string{"ingest_source", "Notes on a meeting", "`brief`", "`question_answer`", "`task_add`", "`rfx_add`", "`close`"} {
		if !strings.Contains(doc.Body, want) {
			t.Errorf("ask lacks %q:\n%s", want, doc.Body)
		}
	}
	if _, err := AgentSend.Run(ctx, d, AgentSendInput{ID: f.bosJob, FileNotes: true, Text: "x"}); err == nil {
		t.Error("file_notes on a task that is not a meeting must be refused")
	}
}
