package ops

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func mustRun[I, O any](t *testing.T, op *Op[I, O], d Deps, in I) O {
	t.Helper()
	out, err := op.Run(t.Context(), d, in)
	if err != nil {
		t.Fatalf("%s: %v", op.Name(), err)
	}
	return out
}

func ruleIDs(rs []RuleInEffect) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.ID+"@"+r.Via)
	}
	return out
}

// TestRulesInEffect: the chain runs task, goal, home area, goal concepts,
// home area concepts; a refined rule gives way; not_in excludes; a draft
// convention is not in effect.
func TestRulesInEffect(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), "install-a")
	eng := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Software engineering", Kind: model.AreaKindConcept})
	law := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Legal writing", Kind: model.AreaKindConcept})
	repo := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Repo upkeep", Concepts: []string{eng.ID}})
	goal := mustRun(t, GoalAdd, d, GoalAddInput{Title: "Ship v2", DoneWhen: "Tagged.", Area: repo.ID, Concepts: []string{law.ID}})
	task := mustRun(t, TaskAdd, d, TaskAddInput{Goal: goal.ID, Title: "Write notes", DoneWhen: "Written."})

	general := mustRun(t, ConventionAdd, d, ConventionAddInput{Title: "Small commits", Status: model.ConventionAdopted, AppliesIn: []string{"[[" + eng.ID + "]]"}})
	mid := mustRun(t, ConventionAdd, d, ConventionAddInput{Title: "Small commits, signed", Status: model.ConventionAdopted, AppliesIn: []string{"unrelated"}, Refines: general.ID})
	specific := mustRun(t, ConventionAdd, d, ConventionAddInput{Title: "Small signed commits, v2", Status: model.ConventionAdopted, AppliesIn: []string{goal.ID}, Refines: mid.ID})
	mustRun(t, ConventionAdd, d, ConventionAddInput{Title: "Draft rule", AppliesIn: []string{task.ID}})
	plain := mustRun(t, ConventionAdd, d, ConventionAddInput{Title: "Plain words", Status: model.ConventionAdopted, AppliesIn: []string{law.ID}})
	src := "https://wiki.acme.example/rules"
	ok := mustRun(t, ConstraintAdd, d, ConstraintAddInput{Says: "No force push", Why: "History", By: "Jordan Example", Source: src, AppliesIn: []string{"[[" + repo.ID + "]]"}, ReviewBy: "2027-01-01"})
	mustRun(t, ConstraintAdd, d, ConstraintAddInput{Says: "No prod data", Why: "Privacy", By: "Jordan Example", Source: src, AppliesIn: []string{eng.ID}, NotIn: []string{"[[" + goal.ID + "]]"}, ReviewBy: "2027-01-01"})

	out := mustRun(t, RulesInEffect, d, RulesInEffectInput{ID: task.ID})
	if want := []string{task.ID, goal.ID, repo.ID, law.ID, eng.ID}; !slices.Equal(out.Scopes, want) {
		t.Fatalf("scopes = %v, want %v", out.Scopes, want)
	}
	// general is refined by specific through mid (not in effect itself): dropped.
	want := []string{specific.ID + "@" + goal.ID, ok.ID + "@" + repo.ID, plain.ID + "@" + law.ID}
	if got := ruleIDs(out.Rules); !slices.Equal(got, want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
	if out.Rules[0].Refines != mid.ID || out.Rules[0].Type != model.TypeConvention {
		t.Fatalf("row = %+v", out.Rules[0])
	}

	// From the area: no goal, so the specific rule and the not_in no longer apply.
	out = mustRun(t, RulesInEffect, d, RulesInEffectInput{ID: repo.ID})
	got := ruleIDs(out.Rules)
	if len(got) != 3 || !slices.Contains(got, general.ID+"@"+eng.ID) || slices.Contains(got, plain.ID+"@"+law.ID) {
		t.Fatalf("area rules = %v", got)
	}

	// Task and goal views carry the short list.
	ts := mustRun(t, TaskShow, d, TaskShowInput{ID: task.ID})
	if len(ts.Rules) != 3 || ts.Rules[0].ID != specific.ID || ts.Rules[0].Via != goal.ID {
		t.Fatalf("task_show rules = %+v", ts.Rules)
	}
}

// TestAreaKindChecks: home must be a project area; concepts must be concept
// areas; refines must be the same type and not a cycle.
func TestAreaKindChecks(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), "install-a")
	concept := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Finances", Kind: model.AreaKindConcept})
	home := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Rental"})
	wantErr := func(err error, sub string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), sub) {
			t.Fatalf("err = %v, want %q", err, sub)
		}
	}
	_, err := GoalAdd.Run(t.Context(), d, GoalAddInput{Title: "G", DoneWhen: "Done.", Area: concept.ID})
	wantErr(err, "want kind project")
	_, err = GoalAdd.Run(t.Context(), d, GoalAddInput{Title: "G", DoneWhen: "Done.", Concepts: []string{home.ID}})
	wantErr(err, "want kind concept")
	_, err = AreaAdd.Run(t.Context(), d, AreaAddInput{Title: "X", Kind: model.AreaKindConcept, Concepts: []string{concept.ID}})
	wantErr(err, "concept area draws on no concepts")
	_, err = AreaAdd.Run(t.Context(), d, AreaAddInput{Title: "Y", Cadence: "sometimes"})
	wantErr(err, "cadence")
	goal := mustRun(t, GoalAdd, d, GoalAddInput{Title: "G", DoneWhen: "Done.", Area: home.ID})
	_, err = DocEdit.Run(t.Context(), d, DocEditInput{ID: home.ID, Fields: map[string]any{"kind": "concept"}})
	wantErr(err, "calls "+home.ID+" home")
	_, err = DocEdit.Run(t.Context(), d, DocEditInput{ID: goal.ID, Fields: map[string]any{"concepts": []any{"[[" + home.ID + "]]"}}})
	wantErr(err, "want kind concept")

	a := mustRun(t, ConventionAdd, d, ConventionAddInput{Title: "A"})
	b := mustRun(t, ConventionAdd, d, ConventionAddInput{Title: "B", Refines: a.ID})
	_, err = ConstraintAdd.Run(t.Context(), d, ConstraintAddInput{Says: "No", Why: "W", By: "Jo", Source: "https://wiki.acme.example/x", ReviewBy: "2027-01-01", Refines: a.ID})
	wantErr(err, "want a constraint")
	_, err = DocEdit.Run(t.Context(), d, DocEditInput{ID: a.ID, Fields: map[string]any{"refines": "[[" + b.ID + "]]"}})
	wantErr(err, "cycle")
}

// TestAreaNeglectedAndReview: an area with a cadence is neglected when never
// reviewed or reviewed more than one cadence ago; area_review clears it and
// shows on the agenda when due.
func TestAreaNeglectedAndReview(t *testing.T) {
	now := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	d := newDepsAt(t, now, "install-a")
	never := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Never", Cadence: "weekly"})
	old := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Old", Cadence: "monthly"})
	fresh := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Fresh", Cadence: "monthly"})
	plain := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Plain"})
	set := func(id, date string) {
		if _, err := DocEdit.Run(t.Context(), d, DocEditInput{ID: id, Fields: map[string]any{"last_reviewed": date}}); err != nil {
			t.Fatal(err)
		}
	}
	set(old.ID, "2026-10-03")   // next 2026-11-03: past
	set(fresh.ID, "2026-10-04") // next 2026-11-04: today, not yet neglected
	h, err := Health(context.Background(), d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			if f.Kind == HealthAreaNeglected {
				got = append(got, f.ID)
			}
		}
	}
	slices.Sort(got)
	if want := []string{never.ID, old.ID}; !slices.Equal(got, want) {
		t.Fatalf("neglected = %v, want %v (plain %s)", got, want, plain.ID)
	}
	ag := mustRun(t, Agenda, d, AgendaInput{Days: 7, Zone: "UTC"})
	areaItems := map[string]AgendaItem{}
	for _, it := range ag.Items {
		if it.Kind == AgendaArea {
			areaItems[it.ID] = it
		}
	}
	if len(areaItems) != 3 || !areaItems[old.ID].Overdue || areaItems[fresh.ID].Overdue || areaItems[never.ID].Date != "2026-11-04" {
		t.Fatalf("agenda area items = %+v", areaItems)
	}

	rv := mustRun(t, AreaReview, d, AreaReviewInput{ID: old.ID, Note: "Leases checked."})
	if rv.LastReviewed != "2026-11-04" || rv.NextReview != "2026-12-04" {
		t.Fatalf("review = %+v", rv)
	}
	rv = mustRun(t, AreaReview, d, AreaReviewInput{ID: old.ID, Note: "Again."})
	show := mustRun(t, AreaShow, d, AreaShowInput{ID: old.ID})
	if !strings.Contains(show.Body, "## Reviews\n\n- 2026-11-04: Leases checked.\n- 2026-11-04: Again.") {
		t.Fatalf("body:\n%s", show.Body)
	}
}

// TestAreaReviewReceipt: an agent's area_review files a receipt that undoes.
func TestAreaReviewReceipt(t *testing.T) {
	f := newAgentFix(t)
	area := mustRun(t, AreaAdd, f.owner, AreaAddInput{Title: "Home upkeep", Cadence: "quarterly"})
	if _, err := AreaReview.Run(writeCtx(t), f.agent, AreaReviewInput{ID: area.ID}); err != nil {
		t.Fatal(err)
	}
	list := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Doc: area.ID})
	if len(list.Items) != 1 || list.Items[0].Op != "area_review" || list.Items[0].Why != "The owner asked." {
		t.Fatalf("receipts = %+v", list.Items)
	}
	mustRun(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: list.Items[0].ID})
	show := mustRun(t, AreaShow, f.owner, AreaShowInput{ID: area.ID})
	if show.Area.LastReviewed != "" {
		t.Fatalf("after undo last_reviewed = %s", show.Area.LastReviewed)
	}
}

// TestAddBodyH1AndLastReviewed: a body H1 is demoted, not refused; a lint
// refusal names its findings on one line; area_add takes last_reviewed.
func TestAddBodyH1AndLastReviewed(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), "install-a")
	n := mustRun(t, NoteAdd, d, NoteAddInput{Title: "N", Body: "# Inner\n\ntext\n\n```\n# keep\n```"})
	doc, err := d.Store.Get(t.Context(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Body, "## Inner") || !strings.Contains(doc.Body, "\n# keep\n") {
		t.Errorf("body = %q", doc.Body)
	}
	_, err = NoteAdd.Run(t.Context(), d, NoteAddInput{Title: "H", Body: "<div>x</div>"})
	if err == nil {
		t.Fatal("raw HTML body accepted")
	}
	msg := PublicError(err, nil)
	if !strings.Contains(msg, "raw-html") || strings.Contains(msg, "\n") {
		t.Errorf("public error = %q", msg)
	}
	a := mustRun(t, AreaAdd, d, AreaAddInput{Title: "Rev", LastReviewed: "2026-10-01"})
	v, err := AreaShow.Run(t.Context(), d, AreaShowInput{ID: a.ID})
	if err != nil || v.Area.LastReviewed != "2026-10-01" {
		t.Errorf("last_reviewed = %q, %v", v.Area.LastReviewed, err)
	}
	if _, err := AreaAdd.Run(t.Context(), d, AreaAddInput{Title: "Bad", LastReviewed: "yesterday"}); err == nil || !strings.Contains(err.Error(), "last_reviewed") {
		t.Errorf("bad date err = %v", err)
	}
}
