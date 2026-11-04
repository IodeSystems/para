package ops

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

var azNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)

// azClock is the clock of laterVault; a test moves it to pass days.
var azClock = azNow

func laterVault(t *testing.T) (Deps, string) {
	t.Helper()
	azClock = azNow
	root := filepath.Join(t.TempDir(), "v")
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "acme", tpl, vault.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	noGate(t, root)
	st, err := local.Open(root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "m1",
		StateDir: filepath.Join(filepath.Dir(root), "state"), Now: func() time.Time { return azClock }})
	if err != nil {
		t.Fatal(err)
	}
	d := Deps{Store: st}
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example", Due: "2026-11-12"}).ID
	return d, proj
}

func listedIDs(q QuestionListOutput) []string {
	out := []string{}
	for _, x := range q.Questions {
		out = append(out, x.ID)
	}
	return out
}

func TestDeferHidesFromListsAndStillBlocks(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the window", Project: proj}).ID
	free := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Free"}).ID
	waits := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Waits", DependsOn: []string{q}, Due: "2026-11-10"}).ID

	out := must(t, Defer, d, DeferInput{IDs: []string{q, free}, Until: "2026-12-01", Reason: "After the audit."})
	if len(out.Changed) != 2 || out.Until != "2026-12-01" {
		t.Fatalf("defer = %+v", out)
	}
	if got := listedIDs(must(t, QuestionList, d, QuestionListInput{})); len(got) != 0 {
		t.Errorf("deferred question listed: %v", got)
	}
	got := must(t, QuestionList, d, QuestionListInput{Deferred: true})
	if len(got.Questions) != 1 || !got.Questions[0].Deferred || got.Questions[0].DeferUntil != "2026-12-01" || got.Questions[0].DeferredReason != "After the audit." || got.Questions[0].Blocking != 1 {
		t.Errorf("deferred list = %+v", got.Questions)
	}
	for _, tk := range must(t, TaskList, d, TaskListInput{}).Tasks {
		if tk.ID == free {
			t.Errorf("deferred task in task_list")
		}
	}
	if tl := must(t, TaskList, d, TaskListInput{Deferred: true}).Tasks; len(tl) != 2 {
		t.Errorf("task_list --deferred = %d tasks", len(tl))
	}
	for _, tk := range must(t, Next, d, NextInput{}).Tasks {
		if tk.ID == free || tk.ID == waits {
			t.Errorf("next lists %s", tk.ID)
		}
	}
	// Blocking is unchanged: the question still holds back its task.
	for _, n := range must(t, Graph, d, GraphInput{}).Nodes {
		if n.ID == waits && n.State != StateWaiting {
			t.Errorf("task state = %s, want waiting", n.State)
		}
	}
	// The put-off question holds up a task due before it returns.
	o := must(t, OrderOp, d, OrderInput{})
	// The deferred task also holds up its goal, due Nov 12.
	if len(o.HeldUp) != 2 || o.HeldUp[0].ID != q || o.HeldUp[0].Holds != waits || o.HeldUp[1].ID != free || o.HeldUp[1].Holds != proj {
		t.Errorf("held up = %+v", o.HeldUp)
	}
	for _, r := range o.Rows {
		if r.ID == q || r.ID == free {
			t.Errorf("order lists deferred %s", r.ID)
		}
	}
	// Undefer brings both back.
	if u := must(t, Undefer, d, UndeferInput{IDs: []string{q, free}}); len(u.Changed) != 2 {
		t.Errorf("undefer = %+v", u)
	}
	if got := listedIDs(must(t, QuestionList, d, QuestionListInput{})); len(got) != 1 {
		t.Errorf("after undefer: %v", got)
	}
	mustLintClean(t, d)
}

func TestDeferReturnsOnItsDateAndSomedayStays(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the window", Project: proj}).ID
	s := must(t, QuestionAdd, d, QuestionAddInput{Text: "Some day?", Project: proj}).ID
	must(t, Defer, d, DeferInput{IDs: []string{q}, Until: "2026-11-03"})
	must(t, Defer, d, DeferInput{IDs: []string{s}})
	azClock = azNow.AddDate(0, 0, 1)
	later := d
	if got := listedIDs(must(t, QuestionList, later, QuestionListInput{})); len(got) != 1 || got[0] != q {
		t.Errorf("on its date: %v, want only %s", got, q)
	}
	if got := must(t, QuestionList, later, QuestionListInput{Deferred: true}); len(got.Questions) != 2 {
		t.Errorf("with deferred: %+v", got.Questions)
	}
}

func TestDeferRules(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the window", Project: proj}).ID
	for name, in := range map[string]DeferInput{
		"past":       {IDs: []string{q}, Until: "2026-11-02"},
		"bad date":   {IDs: []string{q}, Until: "next week"},
		"nothing":    {},
		"not item":   {IDs: []string{"note-x"}},
		"area wrong": {Scope: q},
	} {
		if _, err := Defer.Run(ctx, d, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// An agent cannot hide work.
	a := agentOf(d)
	if _, err := Defer.Run(ctx, a, DeferInput{IDs: []string{q}}); !errors.Is(err, ErrPersonOnly) {
		t.Errorf("agent defer: %v", err)
	}
	if _, err := Undefer.Run(ctx, a, UndeferInput{IDs: []string{q}}); !errors.Is(err, ErrPersonOnly) {
		t.Errorf("agent undefer: %v", err)
	}
	// A finished item is skipped, not changed.
	must(t, QuestionAnswer, d, QuestionAnswerInput{ID: q, Text: "Nov 14."})
	if o := must(t, Defer, d, DeferInput{IDs: []string{q}}); len(o.Changed) != 0 || len(o.Skipped) != 1 {
		t.Errorf("answered: %+v", o)
	}
}

func TestDeferScopeTakesQuestionsThatBlockNothing(t *testing.T) {
	d, proj := laterVault(t)
	idle := must(t, QuestionAdd, d, QuestionAddInput{Text: "Idle one", Project: proj}).ID
	blocks := must(t, QuestionAdd, d, QuestionAddInput{Text: "Blocking one", Project: proj}).ID
	must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Waits", DependsOn: []string{blocks}})
	prop := must(t, QuestionAdd, d, QuestionAddInput{Text: "Has a proposal", Project: proj}).ID
	must(t, QuestionAnswer, agentOf(d), QuestionAnswerInput{ID: prop, Text: "Maybe.", Sources: []model.AnswerSource{{Ref: "https://wiki.example/x"}}})
	o := must(t, Defer, d, DeferInput{Scope: proj, Reason: "Not now."})
	if len(o.Changed) != 1 || o.Changed[0] != idle {
		t.Errorf("scope defer = %+v, want only %s", o, idle)
	}
}

func TestDeferringAGoalPushesOutItsWork(t *testing.T) {
	d, proj := laterVault(t)
	tk := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Inside"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Inside question", Task: tk}).ID
	must(t, Defer, d, DeferInput{IDs: []string{proj}, Until: "2027-01-01"})
	if got := listedIDs(must(t, QuestionList, d, QuestionListInput{})); len(got) != 0 {
		t.Errorf("question of a put-off goal listed: %v (%s)", got, q)
	}
	if got := must(t, Next, d, NextInput{}).Tasks; len(got) != 0 {
		t.Errorf("next = %+v", got)
	}
}

func TestInvestigateQueuesLinkedWork(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which port does prod use?", Project: proj}).ID
	if _, err := Investigate.Run(ctx, agentOf(d), InvestigateInput{ID: q, Hint: "wiki"}); !errors.Is(err, ErrPersonOnly) {
		t.Errorf("agent: %v", err)
	}
	o := must(t, Investigate, d, InvestigateInput{ID: q, Hint: "check the wiki, search #platform"})
	if o.Status != model.RequestQueued || o.Runner != "m1" || o.Appended {
		t.Errorf("out = %+v", o)
	}
	r := must(t, RequestShow, d, RequestShowInput{ID: o.Request}).Doc
	rm := r.Meta.(*model.Request)
	if r.Title != "Ask: Which port does prod use?" || len(rm.Links) != 1 || rm.Links[0].Target() != q ||
		!strings.Contains(r.Body, "## Ask\n\ncheck the wiki, search #platform") || !strings.Contains(r.Body, "doc_retype") ||
		!strings.Contains(r.Body, "a guess is not a source") {
		t.Errorf("request = %+v %s", r.Title, r.Body)
	}
	got := must(t, QuestionList, d, QuestionListInput{})
	if len(got.Questions) != 1 || !got.Questions[0].AgentLooking || got.Questions[0].Status != model.QuestionOpen {
		t.Errorf("question = %+v", got.Questions)
	}
	// A second press adds the hint to the open request. It adds no request.
	o2 := must(t, Investigate, d, InvestigateInput{ID: q, Hint: "also ask in #ops"})
	if !o2.Appended || o2.Request != o.Request {
		t.Errorf("second = %+v", o2)
	}
	o3 := must(t, Investigate, d, InvestigateInput{ID: q})
	if !o3.Appended || o3.Request != o.Request {
		t.Errorf("third = %+v", o3)
	}
	r = must(t, RequestShow, d, RequestShowInput{ID: o.Request}).Doc
	if n := strings.Count(r.Body, "## Hints"); n != 1 || !strings.Contains(r.Body, "- also ask in #ops") {
		t.Errorf("body after second press:\n%s", r.Body)
	}
	if n := len(must(t, RequestList, d, RequestListInput{}).Items); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
	// A runner on another install is the person's choice.
	q2 := must(t, QuestionAdd, d, QuestionAddInput{Text: "Another?", Project: proj}).ID
	if o2 := must(t, Investigate, d, InvestigateInput{ID: q2, Hint: "wiki", Runner: "m2"}); o2.Runner != "m2" {
		t.Errorf("runner = %s", o2.Runner)
	}
	mustLintClean(t, d)
}

func TestInvestigateAcceptsEachType(t *testing.T) {
	f := newMeetingFixture(t)
	d, proj, rfx := f.d, f.project, f.rfx
	task := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Write the plan"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which port?", Project: proj}).ID
	meet := must(t, MeetingAdd, d, MeetingAddInput{Title: "Launch sync", With: []string{f.person}}).ID
	other := must(t, RequestAdd, d, RequestAddInput{Title: "Check backups", Ask: "Check them."}).ID
	for name, id := range map[string]string{"task": task, "question": q, "meeting": meet, "ask": rfx, "goal": proj, "agent work": other} {
		o := must(t, Investigate, d, InvestigateInput{ID: id, Hint: "wiki"})
		r := must(t, RequestShow, d, RequestShowInput{ID: o.Request}).Doc
		rm := r.Meta.(*model.Request)
		want := "Ask: "
		if !strings.HasPrefix(r.Title, want) || len(rm.Links) != 1 || rm.Links[0].Target() != id {
			t.Errorf("%s: request = %q %v", name, r.Title, rm.Links)
		}
		if !strings.Contains(r.Body, "fix it") {
			t.Errorf("%s: body = %s", name, r.Body)
		}
	}
	// The list view marks each request, so rows can say who is looked into.
	n := 0
	for _, v := range must(t, RequestList, d, RequestListInput{}).Items {
		if v.Investigates {
			n++
		}
	}
	if n != 6 {
		t.Errorf("%d requests investigate, want 6", n)
	}
	// Other types are refused, and so is a search for context itself.
	note := must(t, NoteAdd, d, NoteAddInput{Title: "Runbook", Body: "Text."}).ID
	if _, err := Investigate.Run(ctx, d, InvestigateInput{ID: note}); err == nil {
		t.Error("a note was accepted")
	}
	first := must(t, RequestList, d, RequestListInput{Status: model.RequestQueued}).Items
	for _, v := range first {
		if v.Investigates {
			if _, err := Investigate.Run(ctx, d, InvestigateInput{ID: v.ID}); err == nil {
				t.Error("a search for context was investigated")
			}
			break
		}
	}
	if _, err := Investigate.Run(ctx, d, InvestigateInput{ID: "task-nope"}); err == nil {
		t.Error("an unknown id was accepted")
	}
	// Done questions are refused.
	must(t, QuestionDrop, d, QuestionDropInput{ID: q, Reason: "Not needed."})
	if _, err := Investigate.Run(ctx, d, InvestigateInput{ID: q}); err == nil {
		t.Error("a dropped question was accepted")
	}
	mustLintClean(t, d)
}

func TestRequestAddLinksTakeMeetingsAsksAndRequests(t *testing.T) {
	f := newMeetingFixture(t)
	d, rfx := f.d, f.rfx
	meet := must(t, MeetingAdd, d, MeetingAddInput{Title: "Launch sync", With: []string{f.person}}).ID
	for _, id := range []string{meet, rfx} {
		if _, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: "Look at it", Ask: "Look.", Links: []string{id}}); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestCutRecordsTheAssumption(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the window", Project: proj}).ID
	tk := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Schedule", DependsOn: []string{q}}).ID
	if _, err := DependsCut.Run(ctx, d, DependsCutInput{ID: tk, Dep: q}); err == nil {
		t.Error("cut without a reason accepted")
	}
	if _, err := DependsCut.Run(ctx, agentOf(d), DependsCutInput{ID: tk, Dep: q, Reason: "x"}); !errors.Is(err, ErrPersonOnly) {
		t.Errorf("agent cut: %v", err)
	}
	// Later does not unblock; cut does.
	must(t, Defer, d, DeferInput{IDs: []string{q}})
	if got := must(t, Next, d, NextInput{}).Tasks; len(got) != 0 {
		t.Fatalf("next while deferred = %+v", got)
	}
	o := must(t, DependsCut, d, DependsCutInput{ID: tk, Dep: q, Reason: "The default window is fine."})
	if len(o.DependsOn) != 0 {
		t.Errorf("depends_on = %v", o.DependsOn)
	}
	sh := must(t, TaskShow, d, TaskShowInput{ID: tk})
	if !strings.Contains(sh.Body, "## Assumptions") || !strings.Contains(sh.Body, "The default window is fine.") || !strings.Contains(sh.Body, q) {
		t.Errorf("body = %q", sh.Body)
	}
	if got := must(t, Next, d, NextInput{}).Tasks; len(got) != 1 || got[0].ID != tk {
		t.Errorf("next after cut = %+v", got)
	}
	if _, err := DependsCut.Run(ctx, d, DependsCutInput{ID: tk, Dep: q, Reason: "again"}); err == nil {
		t.Error("cutting a missing edge accepted")
	}
}

func TestPersonAnswersWithFreeTextSource(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the window", Project: proj}).ID
	must(t, QuestionAnswer, d, QuestionAnswerInput{ID: q, Text: "Nov 14.", Sources: []model.AnswerSource{
		{Ref: "Platform sync, Oct 29", Quote: "we ship on the 14th"}, {Ref: "https://wiki.example/window"}}})
	sh := must(t, QuestionShow, d, QuestionShowInput{ID: q})
	for _, w := range []string{"- Source: Platform sync, Oct 29", `"we ship on the 14th"`, "- Source: https://wiki.example/window"} {
		if !strings.Contains(sh.Body, w) {
			t.Errorf("body lacks %q:\n%s", w, sh.Body)
		}
	}
	// An agent still needs a real source: free text is refused.
	q2 := must(t, QuestionAdd, d, QuestionAddInput{Text: "Another?", Project: proj}).ID
	if _, err := QuestionAnswer.Run(ctx, agentOf(d), QuestionAnswerInput{ID: q2, Text: "x", Sources: []model.AnswerSource{{Ref: "a meeting I remember"}}}); err == nil {
		t.Error("agent free-text source accepted")
	}
	if _, err := QuestionAnswer.Run(ctx, agentOf(d), QuestionAnswerInput{ID: q2, Text: "x"}); !errors.Is(err, ErrNoSource) {
		t.Errorf("agent without source: %v", err)
	}
}

func TestOrderPutsABlockerOfADatedGoalFirst(t *testing.T) {
	d, proj := laterVault(t)
	idle := must(t, QuestionAdd, d, QuestionAddInput{Text: "Idle", Project: proj}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Blocker", Project: proj}).ID
	tk := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Waits", DependsOn: []string{q}}).ID
	rows := must(t, OrderOp, d, OrderInput{}).Rows
	var order []string
	for _, r := range rows {
		order = append(order, r.ID)
	}
	want := []string{q, tk, proj, idle}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
	if rows[0].Why != "blocks 1, goal due Nov 12" {
		t.Errorf("why = %q", rows[0].Why)
	}
}

func laterQuestion(t *testing.T, d Deps, id string) QuestionView {
	t.Helper()
	for _, q := range must(t, QuestionList, d, QuestionListInput{Deferred: true}).Questions {
		if q.ID == id {
			return q
		}
	}
	t.Fatalf("%s not listed", id)
	return QuestionView{}
}

func TestDeferAfterItemAndUndefer(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which AI vendor", Project: proj}).ID
	pol := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Policy notes updated"}).ID

	must(t, Defer, d, DeferInput{IDs: []string{q}, After: pol})
	if got := listedIDs(must(t, QuestionList, d, QuestionListInput{})); len(got) != 0 {
		t.Fatalf("waiting question listed: %v", got)
	}
	v := laterQuestion(t, d, q)
	if !v.Deferred || len(v.WaitsOn) != 1 || v.WaitsOn[0] != pol || v.DeferUntil != "" || len(v.DependsOn) != 1 {
		t.Fatalf("view = %+v", v)
	}
	// A cycle is refused with the depends message.
	if _, err := Defer.Run(ctx, d, DeferInput{IDs: []string{pol}, After: q}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("cycle err = %v", err)
	}
	// Done dependency: the question returns, with a ready reason, then loses it after 7 days.
	must(t, TaskDone, d, TaskDoneInput{ID: pol})
	if got := listedIDs(must(t, QuestionList, d, QuestionListInput{})); len(got) != 1 {
		t.Fatalf("question did not return: %v", got)
	}
	o, err := Order(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range o.Rows {
		if r.ID == q {
			found = true
			if !strings.HasPrefix(r.Why, "ready: Policy notes updated done") {
				t.Errorf("why = %q", r.Why)
			}
		}
	}
	if !found {
		t.Fatal("ready question not in the order")
	}
	azClock = azNow.AddDate(0, 0, 9)
	o, _ = Order(ctx, d.Store)
	for _, r := range o.Rows {
		if r.ID == q && strings.Contains(r.Why, "ready") {
			t.Errorf("ready reason after 9 days: %q", r.Why)
		}
	}
}

func TestDeferUndeferKeepsManualDependency(t *testing.T) {
	d, proj := laterVault(t)
	a := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "A"}).ID
	b := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "B"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which", Project: proj}).ID
	must(t, QuestionDependsAdd, d, QuestionDependsInput{ID: q, DependsOn: []string{a}})
	must(t, Defer, d, DeferInput{IDs: []string{q}, After: a})
	must(t, Defer, d, DeferInput{IDs: []string{q}, After: b})
	must(t, Undefer, d, UndeferInput{IDs: []string{q}})
	v := must(t, QuestionList, d, QuestionListInput{}).Questions
	if len(v) != 1 || len(v[0].DependsOn) != 1 || v[0].DependsOn[0] != a {
		t.Fatalf("after undefer: %+v", v)
	}
}

func TestDeferWhenMakesTask(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which AI vendor", Project: proj}).ID
	out := must(t, Defer, d, DeferInput{IDs: []string{q}, When: "AI policy notes updated"})
	if out.Created == "" || out.After != out.Created || len(out.Changed) != 1 {
		t.Fatalf("out = %+v", out)
	}
	doc, tk, err := getTask(ctx, d.Store, out.Created)
	if err != nil || tk.DoneWhen != "AI policy notes updated" || tk.Project.Target() != proj || doc.Title != "AI policy notes updated" {
		t.Fatalf("task = %+v %v", tk, err)
	}
	// A similar open task is offered, nothing changes.
	q2 := must(t, QuestionAdd, d, QuestionAddInput{Text: "Another AI question", Project: proj}).ID
	o2 := must(t, Defer, d, DeferInput{IDs: []string{q2}, When: "AI policy notes updated"})
	if len(o2.Similar) == 0 || len(o2.Changed) != 0 {
		t.Fatalf("similar = %+v", o2)
	}
	o3 := must(t, Defer, d, DeferInput{IDs: []string{q2}, When: "AI policy notes updated", New: true})
	if len(o3.Changed) != 1 {
		t.Fatalf("new = %+v", o3)
	}
	// A root question needs a place.
	root := must(t, QuestionAdd, d, QuestionAddInput{Text: "Root one", Root: true}).ID
	if _, err := Defer.Run(ctx, d, DeferInput{IDs: []string{root}, When: "Something else entirely"}); err == nil || !strings.Contains(err.Error(), "place") {
		t.Errorf("root err = %v", err)
	}
	must(t, Defer, d, DeferInput{IDs: []string{root}, When: "Something else entirely", Place: proj})
}

func TestDeferAfterWithDateReturnsWhenBothMet(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which", Project: proj}).ID
	pol := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Notes"}).ID
	must(t, Defer, d, DeferInput{IDs: []string{q}, After: pol, Until: "2026-11-10"})
	must(t, TaskDone, d, TaskDoneInput{ID: pol})
	if got := listedIDs(must(t, QuestionList, d, QuestionListInput{})); len(got) != 0 {
		t.Errorf("returned before the date: %v", got)
	}
	azClock = azNow.AddDate(0, 0, 9)
	if got := listedIDs(must(t, QuestionList, d, QuestionListInput{})); len(got) != 1 {
		t.Errorf("did not return on the date: %v", got)
	}
}

// With no text, investigate keeps its titles and instruction. The Ask title is
// read as find work, and a done find shows its resolution note as a reply.
func TestInvestigateEmptyAndReply(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which port?", Project: proj}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Fix it"}).ID
	oq := must(t, Investigate, d, InvestigateInput{ID: q})
	ot := must(t, Investigate, d, InvestigateInput{ID: task})
	if r := must(t, RequestShow, d, RequestShowInput{ID: oq.Request}).Doc; r.Title != "Find evidence: Which port?" {
		t.Errorf("question title = %q", r.Title)
	}
	if r := must(t, RequestShow, d, RequestShowInput{ID: ot.Request}).Doc; r.Title != "Investigate: Fix it" || !strings.Contains(r.Body, "Add the context") {
		t.Errorf("task = %q %s", r.Title, r.Body)
	}
	if !isFindTitle("Ask: x") || !isFindTitle("Find context: x") || isFindTitle("Work on: x") {
		t.Error("isFindTitle")
	}
	ask := must(t, Investigate, d, InvestigateInput{ID: must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Wrong type"}).ID, Hint: "should this be a question?"})
	doc := must(t, RequestShow, d, RequestShowInput{ID: ask.Request}).Doc
	for _, v := range must(t, RequestList, d, RequestListInput{}).Items {
		if v.Reply != "" {
			t.Errorf("open work has a reply: %+v", v)
		}
	}
	doc.Body = appendSection(doc.Body, "Result", "Yes: retype it. Source: question-x.")
	doc.Meta.(*model.Request).Status = model.RequestDone
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	var got RequestView
	for _, v := range must(t, RequestList, d, RequestListInput{}).Items {
		if v.ID == doc.ID {
			got = v
		}
	}
	if got.Reply != "Yes: retype it. Source: question-x." {
		t.Errorf("reply = %q", got.Reply)
	}
}
