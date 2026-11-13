package ops

import (
	"slices"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// rowPos is the index of each row id.
func rowPos(rows []ValueRow) map[string]int {
	out := map[string]int{}
	for i, r := range rows {
		out[r.ID] = i
	}
	return out
}

// An overdue chore that unblocks nothing ranks below an unblocker of impact 3
// at the same effort: the date is a multiplier (x3), not a group.
func TestScoreOverdueChoreBelowUnblocker(t *testing.T) {
	nodes := []ValueNode{
		{ID: "chore", Kind: KindTask, Due: "2026-11-01", Effort: model.EffortM},
		{ID: "unblock", Kind: KindTask, Effort: model.EffortM},
		{ID: "w1", Kind: KindTask, Deps: []string{"unblock"}},
		{ID: "w2", Kind: KindTask, Deps: []string{"unblock"}},
		{ID: "w3", Kind: KindTask, Deps: []string{"unblock"}},
	}
	o := RankValue(nodes, rankAt)
	g := groupOf(o)
	if g["unblock"].Impact != 3 || g["chore"].Impact != 0 {
		t.Fatalf("impact unblock %d chore %d", g["unblock"].Impact, g["chore"].Impact)
	}
	if p := rowPos(o.Rows); p["unblock"] > p["chore"] {
		t.Fatalf("order = %v", rowIDs(o.Rows))
	}
	if f := g["chore"].factors; f.Urgency != urgencyDueSoon || f.UrgencyWhy != "overdue" || g["chore"].Group != GroupImpact {
		t.Fatalf("chore: %+v group %s", f, g["chore"].Group)
	}
	if g["chore"].Score != 0.75 || g["unblock"].Score != 1 {
		t.Fatalf("scores chore %v unblock %v", g["chore"].Score, g["unblock"].Score)
	}
}

// A task for the top-ranked goal beats an equal task for an unranked one.
func TestScoreGoalRankWeight(t *testing.T) {
	nodes := []ValueNode{
		{ID: "goal-top", Kind: KindGoal, Rank: 1, Hidden: true},
		{ID: "goal-none", Kind: KindGoal, Hidden: true},
		{ID: "a-none", Kind: KindTask, Goal: "goal-none"},
		{ID: "b-top", Kind: KindTask, Goal: "goal-top"},
	}
	o := RankValue(nodes, rankAt)
	g := groupOf(o)
	if p := rowPos(o.Rows); p["b-top"] > p["a-none"] {
		t.Fatalf("order = %v", rowIDs(o.Rows))
	}
	if f := g["b-top"].factors; f.GoalRank != 1 || f.GoalWeight != 2 {
		t.Fatalf("top: %+v", f)
	}
	if f := g["a-none"].factors; f.GoalRank != 0 || f.GoalWeight != 1 {
		t.Fatalf("none: %+v", f)
	}
	if goalWeightOf(2) != 1.5 || goalWeightOf(4) != 1.25 {
		t.Fatal("goal weight is 1 + 1/rank")
	}
}

// A task on a goal of a project with a KPI off target scores x1.5.
func TestScoreKPIOffTarget(t *testing.T) {
	nodes := []ValueNode{
		{ID: "goal-kpi", Kind: KindGoal, KPIOff: true, Hidden: true},
		{ID: "goal-ok", Kind: KindGoal, Hidden: true},
		{ID: "a-ok", Kind: KindTask, Goal: "goal-ok"},
		{ID: "b-kpi", Kind: KindTask, Goal: "goal-kpi"},
	}
	o := RankValue(nodes, rankAt)
	g := groupOf(o)
	if f := g["b-kpi"].factors; f.KPI != kpiOffWeight || g["a-ok"].factors.KPI != 1 {
		t.Fatalf("kpi factors %+v %+v", f, g["a-ok"].factors)
	}
	if p := rowPos(o.Rows); p["b-kpi"] > p["a-ok"] {
		t.Fatalf("order = %v", rowIDs(o.Rows))
	}
}

// The vault marks the goal of a project whose KPI reading is off target.
func TestKPIOffProjectsInVault(t *testing.T) {
	tv, d := cronVault(t, "2026-11-01T09:00:00Z")
	agent := tv.open("ia", "mcp")
	format11(t, agent)
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Shop", StateLine: "Open."})
	must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: "Signups", Unit: "users", Target: f64(50), Direction: model.KPIUp})
	must(t, KPIReadingAdd, agent, KPIReadingAddInput{ID: p.ID, Name: "Signups", Value: f64(40), Source: "https://stats.acme.example/a", Date: "2026-10-30"})
	goal := must(t, GoalAdd, d, GoalAddInput{Title: "More signups", DoneWhen: "50 a week.", Project: p.ID}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Goal: goal, Title: "Fix the form", DoneWhen: "Fixed."}).ID
	o, err := OrderOp.Run(ctx, d, OrderInput{Explain: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range o.Rows {
		if r.ID == task {
			if r.Factors == nil || r.Factors.KPI != kpiOffWeight {
				t.Fatalf("task factors: %+v", r.Factors)
			}
			return
		}
	}
	t.Fatalf("%s not in order: %v", task, rowIDs(o.Rows))
}

// A ready draft that unblocks nothing waits in the quick approvals batch,
// after focus; a ready draft that unblocks something stays in the score.
func TestQuickApprovalsBatch(t *testing.T) {
	opt := rankAt
	opt.FocusCount = 1
	draft := &Readiness{Kind: model.PreparedDraft, Ref: "x"}
	nodes := []ValueNode{
		{ID: "draft-lone", Kind: KindIO, Effort: model.EffortXS, Prep: true, Ready: draft},
		{ID: "draft-blocks", Kind: KindIO, Effort: model.EffortXS, Prep: true, Ready: draft},
		{ID: "waiter", Kind: KindTask, Deps: []string{"draft-blocks"}},
		{ID: "plain", Kind: KindTask},
		{ID: "plan-lone", Kind: KindTask, Ready: &Readiness{Kind: model.PreparedPlan}},
	}
	o := RankValue(nodes, opt)
	g := groupOf(o)
	if g["draft-lone"].Group != GroupQuick || o.Summary.Quick != 1 {
		t.Fatalf("lone draft: %+v quick %d", g["draft-lone"], o.Summary.Quick)
	}
	if g["draft-blocks"].Group != GroupFocus {
		t.Fatalf("draft that unblocks: %s", g["draft-blocks"].Group)
	}
	if g["plan-lone"].Group == GroupQuick {
		t.Fatal("a ready plan is not a one-click approval")
	}
	ids := rowIDs(o.Rows)
	if p := rowPos(o.Rows); p["draft-lone"] != p["draft-blocks"]+1 || !slices.Contains(ids, "plain") || p["plain"] < p["draft-lone"] {
		t.Fatalf("order = %v", ids)
	}
}

// A promise to a person stays above the score: it beats an unblocker.
func TestOwedAboveScore(t *testing.T) {
	nodes := []ValueNode{
		{ID: "promise", Kind: KindTask, Promise: true, PromiseDate: "2026-11-08", Effort: model.EffortL},
		{ID: "unblock", Kind: KindTask, Effort: model.EffortXS, Due: "2026-11-04"},
		{ID: "w1", Kind: KindTask, Deps: []string{"unblock"}},
		{ID: "w2", Kind: KindTask, Deps: []string{"unblock"}},
		{ID: "w3", Kind: KindTask, Deps: []string{"unblock"}},
	}
	opt := rankAt
	opt.OwedSoonDays = DefaultOwedSoonDays
	o := RankValue(nodes, opt)
	g := groupOf(o)
	if g["promise"].Group != GroupOwed {
		t.Fatalf("promise group %s", g["promise"].Group)
	}
	if o.Rows[0].ID != "promise" || g["unblock"].Score <= g["promise"].Score {
		t.Fatalf("order = %v", rowIDs(o.Rows))
	}
}
