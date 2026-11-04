package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// brief fields as an agent sends them to doc_edit.
func briefFields() map[string]any {
	return map[string]any{
		"why":        map[string]any{"text": "Cuts the weekly manual check.", "benefit": map[string]any{"text": "Two hours a week.", "source": "note-x"}},
		"confidence": map[string]any{"percent": 60, "basis": "One pilot.", "history": []any{map[string]any{"date": "2026-10-01", "percent": 40}}},
		"alternatives": []any{
			map[string]any{"name": "do nothing", "cons": []any{map[string]any{"text": "The check stays manual.", "source": "note-x"}}},
			map[string]any{"name": "script it", "confidence": 50, "confidence_basis": "Similar work."},
		},
		"unknowns": []any{map[string]any{"text": "Does the vendor expose an API?"}},
		"signals": []any{
			map[string]any{"what": "Pilot runs", "how": "Count in the log", "every_days": 7, "expected": "3"},
		},
		"pivot":      map[string]any{"condition": "No pilot by the date.", "date": "2026-11-01", "action": "ask"},
		"extensions": []any{"A dashboard"},
		"ecosystem":  map[string]any{"replaces": []any{"the spreadsheet"}, "key_holder": true},
		"validation": "active",
	}
}

func TestGoalBriefThroughDocEdit(t *testing.T) {
	d, proj := laterVault(t)
	must(t, DocEdit, d, DocEditInput{ID: proj, Fields: briefFields()})
	v := must(t, ProjectShow, d, ProjectShowInput{ID: proj}).Project
	if v.Brief == nil || v.Brief.Why.Text == "" || len(v.Brief.Signals) != 1 || v.Brief.Pivot.Action != "ask" || !v.Brief.Ecosystem.KeyHolder {
		t.Fatalf("brief = %+v", v.Brief)
	}
	// do nothing is required among alternatives.
	bad := map[string]any{"alternatives": []any{map[string]any{"name": "script it"}}}
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: proj, Fields: bad}); err == nil || !strings.Contains(err.Error(), "do nothing") {
		t.Fatalf("alternatives without do nothing: %v", err)
	}
	// A claim needs a source.
	bad = map[string]any{"why": map[string]any{"text": "x", "pros": []any{map[string]any{"text": "y"}}}}
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: proj, Fields: bad}); err == nil {
		t.Fatal("claim without source accepted")
	}
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: proj, Fields: map[string]any{"validation": "maybe"}}); err == nil {
		t.Fatal("bad validation accepted")
	}
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: proj, Fields: map[string]any{"pivot": map[string]any{"condition": "c", "action": "quit"}}}); err == nil {
		t.Fatal("bad pivot action accepted")
	}
}

func TestGoalSignalAddAndRank(t *testing.T) {
	d, proj := laterVault(t)
	must(t, DocEdit, d, DocEditInput{ID: proj, Fields: briefFields()})
	agent := agentOf(d)
	out := must(t, GoalSignalAdd, agent, GoalSignalAddInput{ID: proj, What: "pilot runs", Value: "2", Source: "note-x"})
	if out.Readings != 1 || out.Date != "2026-11-02" {
		t.Fatalf("out = %+v", out)
	}
	if _, err := GoalSignalAdd.Run(ctx, agent, GoalSignalAddInput{ID: proj, What: "Nope", Value: "2", Source: "s"}); err == nil {
		t.Fatal("unknown signal accepted")
	}
	if _, err := GoalSignalAdd.Run(ctx, agent, GoalSignalAddInput{ID: proj, What: "Pilot runs", Value: "2"}); err == nil {
		t.Fatal("reading without a source accepted")
	}
	if _, err := GoalRank.Run(ctx, agent, GoalRankInput{ID: proj, Rank: 1}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent ranked a goal: %v", err)
	}
	must(t, GoalRank, d, GoalRankInput{ID: proj, Rank: 2})
	cmp := must(t, GoalCompare, d, GoalCompareInput{})
	if len(cmp.Goals) != 1 || cmp.Goals[0].Rank != 2 || cmp.Goals[0].Confidence != 60 || cmp.Goals[0].Signals[0].Last != "2026-11-02" {
		t.Fatalf("compare = %+v", cmp.Goals)
	}
}

func TestRankValueUsesGoalRankForTies(t *testing.T) {
	nodes := []ValueNode{
		{ID: "project-a", Kind: KindGoal, Title: "A", Created: "2026-10-01"},
		{ID: "project-b", Kind: KindGoal, Title: "B", Created: "2026-10-02", Rank: 1},
		{ID: "project-c", Kind: KindGoal, Title: "C", Created: "2026-10-03", Rank: 2},
		{ID: "project-d", Kind: KindGoal, Title: "D", Created: "2026-10-04", Due: "2026-12-01"},
	}
	var ids []string
	for _, r := range RankValue(nodes, RankOptions{Today: "2026-11-04", DueSoonDays: DefaultDueSoonDays}).Rows {
		ids = append(ids, r.ID)
	}
	// A due date comes first; the rank settles the undated tie; unranked goes last.
	if want := []string{"project-d", "project-b", "project-c", "project-a"}; !slices.Equal(ids, want) {
		t.Fatalf("order = %v, want %v", ids, want)
	}
}

func TestGoalWatchesHealthAndAgenda(t *testing.T) {
	d, proj := laterVault(t)
	must(t, DocEdit, d, DocEditInput{ID: proj, Fields: briefFields()})
	today := "2026-11-02"
	ws, err := goalWatches(ctx, d.Store, today)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, w := range ws {
		kinds = append(kinds, w.Kind)
		if w.Reason == "" {
			t.Errorf("no reason: %+v", w)
		}
	}
	// The pivot date has passed; the signal has no reading, so its count starts at the goal's creation.
	if !slices.Contains(kinds, WatchPivot) {
		t.Fatalf("watches = %+v", ws)
	}
	h, err := HealthAt(ctx, d.Store, today)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			got[f.Kind] = true
		}
	}
	if !got[HealthPivotPassed] || !got[HealthGoalNoSignal] {
		t.Fatalf("findings = %v", got)
	}
	ag := must(t, Agenda, d, AgendaInput{Days: 3})
	found := false
	for _, it := range ag.Items {
		if it.Kind == AgendaGoal && it.ID == proj && it.Overdue && strings.Contains(it.Detail, "change of course") {
			found = true
		}
	}
	if !found {
		t.Fatalf("agenda = %+v", ag.Items)
	}
	var out TickOutput
	if err := tickGoals(ctx, d, &out); err != nil || len(out.Goals) == 0 || !strings.HasPrefix(out.Goals[0], proj+": ") {
		t.Fatalf("tick goals = %v, %v", out.Goals, err)
	}
	// A stopped goal is settled: no row, no finding.
	must(t, DocEdit, d, DocEditInput{ID: proj, Fields: map[string]any{"validation": "stopped"}})
	if ws, _ := goalWatches(ctx, d.Store, today); len(ws) != 0 {
		t.Fatalf("stopped goal still watched: %+v", ws)
	}
}
