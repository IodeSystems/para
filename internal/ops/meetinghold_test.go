package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// holdFixture adds a second request with its own person, and a request that
// asks a question, to the meeting fixture.
type holdFixture struct {
	meetingFixture
	meeting string
	noRFX   string // a request the meeting will refuse
	askRFX  string // a request that asks a question
	asked   string // the question that request asks
	person2 string
}

func newHoldFixture(t *testing.T) holdFixture {
	t.Helper()
	f := holdFixture{meetingFixture: newMeetingFixture(t)}
	d := f.d
	f.person2 = "person-bo-example"
	f.noRFX = must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Can we skip the audit?", Route: "domain-logging",
		SelfServe: "read policies", Project: f.project, Targets: []TargetInput{{To: f.person2, Medium: model.MediumEmail}}}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Who signs off?", Project: f.project})
	f.asked = q.ID
	ask := must(t, QuestionAsk, d, QuestionAskInput{ID: q.ID, To: f.person, Via: model.MediumSlack, Route: "domain-logging"})
	f.askRFX = ask.RFX
	f.meeting = must(t, MeetingAdd, d, MeetingAddInput{
		Title: "Audit sync", With: []string{f.person, f.person2}, RFX: []string{f.rfx, f.noRFX, f.askRFX},
		Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00",
	}).ID
	return f
}

func TestMeetingHold(t *testing.T) {
	f := newHoldFixture(t)
	d := f.d
	if body := must(t, MeetingShow, d, MeetingShowInput{ID: f.meeting}).Body; !strings.Contains(body, "[["+f.inScope+"]]") || !strings.Contains(body, "(question [["+f.asked+"]])") {
		t.Fatalf("questions not gathered:\n%s", body)
	}
	out, err := MeetingHold.Run(ctx, d, MeetingHoldInput{
		ID:    f.meeting,
		Notes: "# Decisions\nWe agreed to ship in January.\nJordan owns the plan.",
		Replies: []RFXReplyInput{
			{ID: f.askRFX, From: f.person, Via: model.MediumSlack, Outcome: ReplyAnswered, Text: "Ada signs off."},
			{ID: f.noRFX, From: f.person2, Via: model.MediumEmail, Outcome: ReplyNo, Text: "Not possible.", Why: "Audit freeze", URL: "https://acme.example/m/1"},
		},
		Answers: []QuestionAnswerInput{{ID: f.inScope, Text: "The audit window is December."}},
		Actions: []HoldAction{
			{Title: "Send the plan", Assignee: f.person, Due: "2026-11-10"},
			{Title: "Book the room"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != model.MeetingHeld || len(out.Replied) != 2 || len(out.Constraints) != 1 || len(out.Tasks) != 2 {
		t.Fatalf("out = %+v", out)
	}
	for _, q := range []string{f.asked, f.inScope} {
		if !slices.Contains(out.Answered, q) {
			t.Errorf("%s not answered: %v", q, out.Answered)
		}
		if _, qm, _ := getQuestion(ctx, d.Store, q); qm.Status != model.QuestionAnswered {
			t.Errorf("%s is %s", q, qm.Status)
		}
	}
	// A "no" made its constraint; the reply made an interaction.
	if _, err := d.Store.Get(ctx, out.Constraints[0]); err != nil {
		t.Error(err)
	}
	// Tasks: an assignee, and the meeting linked in the body.
	task, tm, err := getTask(ctx, d.Store, out.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	if tm.Assignee != f.person || tm.Due != "2026-11-10" || !strings.Contains(task.Body, "[["+f.meeting+"]]") || tm.Project.Target() != f.project {
		t.Errorf("task = %+v body %q", tm, task.Body)
	}
	if _, tm2, _ := getTask(ctx, d.Store, out.Tasks[1]); tm2.Assignee != "" {
		t.Errorf("an action with no assignee is for the owner, got %q", tm2.Assignee)
	}
	// The meeting is held and records the notes, flattened, and what came of it.
	show := must(t, MeetingShow, d, MeetingShowInput{ID: f.meeting})
	if show.Meeting.Status != model.MeetingHeld {
		t.Errorf("status = %s", show.Meeting.Status)
	}
	for _, want := range []string{"Held.", "**Decisions**", "Jordan owns the plan.", "Action: [[" + out.Tasks[0] + "]]", "Replied: [[" + f.noRFX + "]]"} {
		if !strings.Contains(show.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, show.Body)
		}
	}
	if strings.Contains(show.Body, "\n# ") {
		t.Errorf("notes opened a section:\n%s", show.Body)
	}
	// f.rfx got no reply: its meeting target was scheduled, so it is now sent
	// and the request is a wait again.
	if got := targetOf(t, d, f.rfx, f.person, model.MediumMeeting).State; got != model.TargetSent {
		t.Errorf("unanswered target = %s, want sent", got)
	}
	if !slices.Equal(out.Waiting, []string{f.rfx}) || !slices.Contains(waitingIDs(t, d), f.rfx) {
		t.Errorf("waiting = %v", out.Waiting)
	}
	for _, c := range []struct{ rfx, who string }{{f.noRFX, f.person2}, {f.askRFX, f.person}} {
		if got := targetOf(t, d, c.rfx, c.who, model.MediumMeeting).State; got != model.TargetReplied {
			t.Errorf("meeting target on %s = %s, want replied", c.rfx, got)
		}
	}
	mustLintClean(t, d)
	if _, err := MeetingHold.Run(ctx, d, MeetingHoldInput{ID: f.meeting}); err == nil {
		t.Error("a held meeting cannot be held again")
	}
	if _, err := MeetingCancel.Run(ctx, d, MeetingCancelInput{ID: f.meeting}); err == nil {
		t.Error("a held meeting cannot be cancelled")
	}
}

// A hold that fails a check writes nothing: no reply, no answer, no task.
func TestMeetingHoldChecksBeforeWriting(t *testing.T) {
	f := newHoldFixture(t)
	d := f.d
	reply := RFXReplyInput{ID: f.rfx, From: f.person, Via: model.MediumEmail, Outcome: ReplyAnswered, Text: "Yes."}
	cases := []struct {
		name string
		in   MeetingHoldInput
		want string
	}{
		{"bad assignee", MeetingHoldInput{Replies: []RFXReplyInput{reply}, Actions: []HoldAction{{Title: "x", Assignee: "person-nope"}}}, "actions[0]"},
		{"no scope for a task", MeetingHoldInput{Replies: []RFXReplyInput{reply}, Actions: []HoldAction{{Title: "x", Project: "", Area: "area-nope"}}}, "actions[0]"},
		{"bad due", MeetingHoldInput{Replies: []RFXReplyInput{reply}, Actions: []HoldAction{{Title: "x", Due: "soon"}}}, "actions[0]"},
		{"empty title", MeetingHoldInput{Replies: []RFXReplyInput{reply}, Actions: []HoldAction{{Title: " "}}}, "title"},
		{"raw html in notes", MeetingHoldInput{Notes: "see <b>this</b>", Replies: []RFXReplyInput{reply}}, "notes"},
		{"raw html in a reply", MeetingHoldInput{Replies: []RFXReplyInput{{ID: f.rfx, From: f.person, Outcome: ReplyAnswered, Text: "<script>x</script>"}}}, "replies[0].text"},
		{"unknown request", MeetingHoldInput{Replies: []RFXReplyInput{{ID: "rfx-nope", From: f.person, Outcome: ReplyAnswered, Text: "x"}}}, "replies[0]"},
		{"answered question already", MeetingHoldInput{Answers: []QuestionAnswerInput{{ID: f.asked, Text: "x"}}, Replies: []RFXReplyInput{{ID: f.askRFX, From: f.person, Via: model.MediumSlack, Outcome: ReplyAnswered, Text: "Ada."}}}, "twice"},
		{"question twice", MeetingHoldInput{Answers: []QuestionAnswerInput{{ID: f.inScope, Text: "a"}, {ID: f.inScope, Text: "b"}}}, "twice"},
		{"unknown question", MeetingHoldInput{Answers: []QuestionAnswerInput{{ID: "question-nope", Text: "a"}}}, "answers[0]"},
		{"time on a scheduled meeting", MeetingHoldInput{Start: "2026-11-04T10:00:00Z", End: "2026-11-04T11:00:00Z"}, "already scheduled"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.ID = f.meeting
			if _, err := MeetingHold.Run(ctx, d, c.in); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if _, m, _ := getMeeting(ctx, d.Store, f.meeting); m.Status != model.MeetingScheduled {
				t.Errorf("meeting is %s after a refused hold", m.Status)
			}
			if tg := targetOf(t, d, f.rfx, f.person, model.MediumEmail); tg.State == model.TargetReplied {
				t.Error("a refused hold replied to a request")
			}
			if _, qm, _ := getQuestion(ctx, d.Store, f.inScope); qm.Status != model.QuestionOpen {
				t.Error("a refused hold answered a question")
			}
			// The meeting is itself a task of kind meeting; a refused hold made no other.
			tasks, _ := listTasks(ctx, d.Store, "", "")
			if tasks = slices.DeleteFunc(tasks, func(v TaskView) bool { return v.Kind == model.KindMeeting }); len(tasks) != 0 {
				t.Errorf("a refused hold made %d tasks", len(tasks))
			}
		})
	}
}

func TestMeetingHoldUnscheduled(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	ask := must(t, MeetingAdd, d, MeetingAddInput{Title: "Hallway talk", With: []string{f.person}, RFX: []string{f.rfx}})
	if _, err := MeetingHold.Run(ctx, d, MeetingHoldInput{ID: ask.ID, Notes: "Talked."}); err == nil || !strings.Contains(err.Error(), "start and end") {
		t.Fatalf("err = %v", err)
	}
	out := must(t, MeetingHold, d, MeetingHoldInput{ID: ask.ID, Notes: "Talked.", Start: "2026-11-03T16:00:00Z", End: "2026-11-03T16:10:00Z"})
	if out.Status != model.MeetingHeld {
		t.Fatalf("out = %+v", out)
	}
	_, m, _ := getMeeting(ctx, d.Store, ask.ID)
	if m.Start != "2026-11-03T16:00:00Z" {
		t.Errorf("start = %s", m.Start)
	}
	// The request was only asked for, never answered: it is a wait.
	if got := targetOf(t, d, f.rfx, f.person, model.MediumMeeting).State; got != model.TargetSent {
		t.Errorf("target = %s", got)
	}
	mustLintClean(t, d)
}

// A recurring task for an agent that an action item creates queues its request.
func TestMeetingHoldActionForAgent(t *testing.T) {
	f := newMeetingFixture(t)
	d := f.d
	m := must(t, MeetingAdd, d, MeetingAddInput{Title: "x", With: []string{f.person}, Start: "2026-11-04T10:00:00Z", End: "2026-11-04T10:30:00Z"})
	out := must(t, MeetingHold, d, MeetingHoldInput{ID: m.ID, Project: f.project, Actions: []HoldAction{{Title: "Summarize the notes", Assignee: "agent:scribe"}}})
	_, tm, _ := getTask(ctx, d.Store, out.Tasks[0])
	if tm.Assignee != "agent:scribe" {
		t.Errorf("assignee = %q", tm.Assignee)
	}
	mustLintClean(t, d)
}

func TestThinHoldWarns(t *testing.T) {
	d, _ := laterVault(t)
	ada := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"}).ID
	m := must(t, MeetingAdd, d, MeetingAddInput{Title: "Sync", With: []string{ada}, Start: "2026-11-02T10:00:00Z", End: "2026-11-02T10:30:00Z"}).ID
	out := must(t, MeetingHold, d, MeetingHoldInput{ID: m})
	if !slices.ContainsFunc(out.Warnings, func(w string) bool {
		return strings.Contains(w, "no notes") && strings.Contains(w, "no actions") && strings.Contains(w, "no replies")
	}) {
		t.Fatalf("thin hold warnings = %v", out.Warnings)
	}
	m2 := must(t, MeetingAdd, d, MeetingAddInput{Title: "Sync two", With: []string{ada}, Start: "2026-11-02T11:00:00Z", End: "2026-11-02T11:30:00Z"}).ID
	if out := must(t, MeetingHold, d, MeetingHoldInput{ID: m2, Notes: "Agreed on the plan."}); slices.ContainsFunc(out.Warnings, func(w string) bool { return strings.Contains(w, "nothing of what came") }) {
		t.Fatalf("a hold with notes warned: %v", out.Warnings)
	}
}
