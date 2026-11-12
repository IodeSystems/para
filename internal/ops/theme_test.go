package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// themeFixture: a goal with tasks; a trip (an obligation) with an action
// item, a task that waits on it, one better after it, one put off until it,
// and one it waits on; a tagged task; and a task that only mentions the trip
// and the goal in its body.
type themeFixture struct {
	v                                     *verbVault
	trip, child, waits, optional, putOff  string
	serves, tagged, mention, closed, area string
	underGoal                             string
	question, onQuestion                  string
}

func newThemeFixture(t *testing.T) themeFixture {
	t.Helper()
	v := newVerbVault(t)
	f := themeFixture{v: v}
	ob := must(t, ObligationAdd, v.cli, ObligationAddInput{Title: "Trip to the coast", Kind: model.KindCommitment, Due: "2026-12-01", OwedTo: []string{v.person}, Goal: v.proj})
	f.trip = ob.ID
	add := func(in TaskAddInput) string {
		t.Helper()
		if in.Goal == "" && in.Area == "" {
			in.Goal = v.proj
		}
		in.DoneWhen = "It is done."
		return must(t, TaskAdd, v.cli, in).ID
	}
	f.child = add(TaskAddInput{Title: "Pack the bags", Parent: f.trip})
	f.waits = add(TaskAddInput{Title: "Post the photos", Area: "", Goal: v.proj})
	must(t, DependsSet, v.cli, DependsSetInput{ID: f.waits, On: f.trip})
	f.optional = add(TaskAddInput{Title: "Write the travel notes"})
	must(t, DependsSet, v.cli, DependsSetInput{ID: f.optional, On: f.trip, Optional: true})
	f.putOff = add(TaskAddInput{Title: "Plan the next one"})
	must(t, Defer, v.cli, DeferInput{IDs: []string{f.putOff}, After: f.trip})
	f.serves = add(TaskAddInput{Title: "Book the hotel"})
	must(t, DependsSet, v.cli, DependsSetInput{ID: f.trip, On: f.serves})
	f.tagged = add(TaskAddInput{Title: "Buy sunscreen"})
	must(t, TagAdd, v.cli, TagInput{IDs: []string{f.tagged}, Tags: []string{"summer"}})
	f.mention = add(TaskAddInput{Title: "Mentions only", Body: "See [[" + f.trip + "]] and [[" + v.proj + "]]."})
	f.closed = add(TaskAddInput{Title: "Old item", Parent: f.trip})
	must(t, TaskDone, v.cli, TaskDoneInput{ID: f.closed})
	f.underGoal = f.mention
	f.area = must(t, AreaAdd, v.cli, AreaAddInput{Title: "Home"}).ID
	f.question = v.question(t, "Which coast?")
	f.onQuestion = add(TaskAddInput{Title: "Rent the car"})
	must(t, DependsSet, v.cli, DependsSetInput{ID: f.onQuestion, On: f.question})
	return f
}

// noUpdated drops the updated line: an undo restores fields, and may stamp it.
func noUpdated(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if !strings.HasPrefix(l, "updated:") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

func partKinds(p []PartOfRef) []string {
	var out []string
	for _, r := range p {
		out = append(out, r.Kind+"/"+r.Via+"/"+r.ID)
	}
	return out
}

func TestPartOfNearestFirst(t *testing.T) {
	f := newThemeFixture(t)
	v := f.v
	for _, c := range []struct {
		id   string
		want []string
	}{
		{f.child, []string{PartParent + "/" + ViaParent + "/" + f.trip, PartGoal + "/" + ViaGoal + "/" + v.proj}},
		{f.waits, []string{PartObligation + "/" + ViaDepends + "/" + f.trip, PartGoal + "/" + ViaGoal + "/" + v.proj}},
		{f.optional, []string{PartObligation + "/" + ViaOptional + "/" + f.trip, PartGoal + "/" + ViaGoal + "/" + v.proj}},
		{f.putOff, []string{PartObligation + "/" + ViaPutOff + "/" + f.trip, PartGoal + "/" + ViaGoal + "/" + v.proj}},
		{f.serves, []string{PartObligation + "/" + ViaServes + "/" + f.trip, PartGoal + "/" + ViaGoal + "/" + v.proj}},
		{f.tagged, []string{PartGoal + "/" + ViaGoal + "/" + v.proj, PartTag + "/" + ViaTag + "/summer"}},
		{f.onQuestion, []string{PartQuestion + "/" + ViaDepends + "/" + f.question, PartGoal + "/" + ViaGoal + "/" + v.proj}},
		{f.mention, []string{PartGoal + "/" + ViaGoal + "/" + v.proj}},
	} {
		ctxOut := must(t, ItemContext, v.pwa, ItemContextInput{ID: c.id})
		if got := partKinds(ctxOut.PartOf); !slices.Equal(got, c.want) {
			t.Errorf("context %s part_of = %v, want %v", c.id, got, c.want)
		}
	}
	// The trip's title is what the row shows, and a task list fills it too.
	ctxOut := must(t, ItemContext, v.pwa, ItemContextInput{ID: f.child})
	if ctxOut.PartOf[0].Title != "Trip to the coast" {
		t.Errorf("parent title = %q", ctxOut.PartOf[0].Title)
	}
	list := must(t, TaskList, v.pwa, TaskListInput{Goal: v.proj})
	for _, tv := range list.Tasks {
		if tv.ID == f.tagged && !slices.Equal(partKinds(tv.PartOf), []string{PartGoal + "/" + ViaGoal + "/" + v.proj, PartTag + "/" + ViaTag + "/summer"}) {
			t.Errorf("task_list part_of of the tagged task = %v", partKinds(tv.PartOf))
		}
	}
}

func themeIDs(out ThemeItemsOutput) map[string][]string {
	m := map[string][]string{}
	for _, it := range out.Items {
		m[it.ID] = it.Why
	}
	return m
}

func TestThemeItemsInclusion(t *testing.T) {
	f := newThemeFixture(t)
	v := f.v
	trip := must(t, ThemeItems, v.pwa, ThemeItemsInput{Theme: f.trip})
	got := themeIDs(trip)
	want := map[string]string{f.child: ViaParent, f.waits: ViaDepends, f.optional: ViaOptional, f.putOff: ViaPutOff, f.serves: ViaServes}
	if len(got) != len(want) || trip.Kind != PartObligation || !trip.Open {
		t.Errorf("trip items = %v (kind %s)", got, trip.Kind)
	}
	for id, via := range want {
		if !slices.Contains(got[id], via) {
			t.Errorf("trip: %s why = %v, want %s", id, got[id], via)
		}
	}
	for _, id := range []string{f.mention, f.closed, f.tagged} {
		if _, ok := got[id]; ok {
			t.Errorf("trip lists %s", id)
		}
	}
	goal := themeIDs(must(t, ThemeItems, v.pwa, ThemeItemsInput{Theme: v.proj}))
	if _, ok := goal[f.mention]; !ok {
		t.Errorf("goal does not list a task under it: %v", goal)
	}
	if _, ok := goal[f.closed]; ok {
		t.Error("goal lists a closed task")
	}
	if _, ok := goal[f.trip]; !ok {
		t.Error("goal does not list the obligation under it")
	}
	tag := themeIDs(must(t, ThemeItems, v.pwa, ThemeItemsInput{Theme: "#Summer"}))
	if len(tag) != 1 || tag[f.tagged] == nil {
		t.Errorf("tag items = %v", tag)
	}
	q := themeIDs(must(t, ThemeItems, v.pwa, ThemeItemsInput{Theme: f.question}))
	if len(q) != 1 || q[f.onQuestion] == nil {
		t.Errorf("question items = %v", q)
	}
}

func TestThemeCloseIsOneBatchUndo(t *testing.T) {
	f := newThemeFixture(t)
	v := f.v
	before := map[string]string{}
	for _, id := range []string{f.child, f.waits, f.trip} {
		doc, _ := v.pwa.Store.Get(t.Context(), id)
		before[id] = noUpdated(renderText(doc))
	}
	out, err := ThemeClose.Run(t.Context(), v.pwa, ThemeCloseInput{Theme: f.trip, Outcome: string(model.OutcomeDropped), Reason: "The trip is off.",
		Drop: []string{f.child}, Keep: []ThemeKeep{{ID: f.waits, Area: f.area}, {ID: f.optional}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Batch == "" || out.Closed == "" || out.Refused != 0 || out.Applied != 2 {
		t.Fatalf("theme_close = %+v", out)
	}
	if !slices.Contains(out.Left, f.putOff) || slices.Contains(out.Left, f.child) {
		t.Errorf("left = %v", out.Left)
	}
	if lifeNow(t, v.pwa, f.trip) != LifeClosed || lifeNow(t, v.pwa, f.child) != LifeClosed {
		t.Fatal("the trip or its dropped item is open")
	}
	moved, _ := v.pwa.Store.Get(t.Context(), f.waits)
	// The goal stays: only what ends (the trip) is taken off a kept item.
	if tm := moved.Meta.(*model.Task); tm.Area.Target() != f.area || tm.Goal.Target() != v.proj {
		t.Errorf("moved task area %q project %q", tm.Area, tm.Goal)
	}
	if lifeNow(t, v.pwa, f.optional) != LifeOpen {
		t.Error("a kept item closed")
	}
	undo, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkUndo, Batch: out.Batch})
	if err != nil || undo.Refused != 0 || undo.Applied != 3 {
		t.Fatalf("undo = %+v, %v", undo, err)
	}
	for id, was := range before {
		doc, _ := v.pwa.Store.Get(t.Context(), id)
		if noUpdated(renderText(doc)) != was {
			t.Errorf("%s not restored by the batch undo:\n%s\nwant\n%s", id, renderText(doc), was)
		}
	}
}

func TestThemeCloseGoalForcesAndAnsweredQuestionOnlyDrops(t *testing.T) {
	f := newThemeFixture(t)
	v := f.v
	// goal_close alone refuses a goal with open tasks.
	if _, err := Close.Run(t.Context(), v.pwa, CloseInput{ID: v.proj}); err == nil {
		t.Fatal("close of a goal with open tasks passed")
	}
	out := must(t, ThemeClose, v.pwa, ThemeCloseInput{Theme: v.proj, Drop: []string{f.tagged}})
	if out.Closed != GoalClose.Name() || lifeNow(t, v.pwa, v.proj) != LifeClosed {
		t.Fatalf("goal close = %+v", out)
	}
	undo := must(t, Bulk, v.pwa, BulkInput{Verb: BulkUndo, Batch: out.Batch})
	if undo.Refused != 0 || lifeNow(t, v.pwa, v.proj) != LifeOpen || lifeNow(t, v.pwa, f.tagged) != LifeOpen {
		t.Fatalf("undo = %+v", undo)
	}
	// A question answered already is not closed again: only the drops run.
	if _, err := QuestionAnswer.Run(t.Context(), v.pwa, QuestionAnswerInput{ID: f.question, Text: "The north coast.", Sources: []model.AnswerSource{{Ref: "https://example.com/map"}}}); err != nil {
		t.Fatal(err)
	}
	q := must(t, ThemeClose, v.pwa, ThemeCloseInput{Theme: f.question, Drop: []string{f.onQuestion}})
	if q.Closed != "" || q.Applied != 1 || lifeNow(t, v.pwa, f.onQuestion) != LifeClosed {
		t.Fatalf("after-answer review = %+v", q)
	}
	// An item that does not hang on the theme is refused before anything runs.
	if _, err := ThemeClose.Run(t.Context(), v.pwa, ThemeCloseInput{Theme: f.trip, Drop: []string{f.mention}}); err == nil || !strings.Contains(err.Error(), "does not hang") {
		t.Fatalf("drop of a mention-only item: %v", err)
	}
}

func TestAgentCannotDropThemeItems(t *testing.T) {
	f := newThemeFixture(t)
	v := f.v
	if _, err := ThemeClose.Run(t.Context(), v.agent, ThemeCloseInput{Theme: f.trip, Drop: []string{f.child}}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent theme_close: %v", err)
	}
	if lifeNow(t, v.pwa, f.child) != LifeOpen {
		t.Fatal("an agent dropped an item")
	}
	// An agent's close of the trip lists what hangs on it and drops nothing.
	out, err := Close.Run(WithWrite(t.Context(), WriteMeta{Why: "The owner said the trip is off."}), v.agent, CloseInput{ID: f.trip, Outcome: string(model.OutcomeDropped), Reason: "Off."})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hanging) != 5 || out.HangingNote == "" {
		t.Fatalf("hanging = %+v note %q", out.Hanging, out.HangingNote)
	}
	for _, id := range []string{f.child, f.waits, f.optional} {
		if lifeNow(t, v.pwa, id) == LifeClosed {
			t.Errorf("%s closed by an agent's close of the theme", id)
		}
	}
}

// An item put off until an obligation starts is part of it, and hangs on it.
func TestPartOfUntilStart(t *testing.T) {
	d, proj, _ := obFixture(t)
	trip := must(t, ObligationAdd, d, ObligationAddInput{Kind: model.KindCommitment, Title: "Seattle trip", Goal: proj,
		Start: "2026-12-01T09:00:00-08:00", End: "2026-12-10T18:00:00-08:00"}).ID
	visit := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Visit the office", DoneWhen: "Badge picked up."}).ID
	must(t, Defer, d, DeferInput{IDs: []string{visit}, UntilStart: trip})
	got := partKinds(must(t, ItemContext, d, ItemContextInput{ID: visit}).PartOf)
	if !slices.Contains(got, PartObligation+"/"+ViaPutOff+"/"+trip) {
		t.Errorf("part_of = %v", got)
	}
	items := themeIDs(must(t, ThemeItems, d, ThemeItemsInput{Theme: trip}))
	if !slices.Contains(items[visit], ViaPutOff) {
		t.Errorf("theme_items = %v", items)
	}
}
