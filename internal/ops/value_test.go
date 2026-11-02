package ops

import (
	"reflect"
	"testing"
)

func rowIDs(rows []ValueRow) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// The order is a rule, not a score. Each case states the rule it checks.
func TestRankValue(t *testing.T) {
	cases := []struct {
		name  string
		nodes []ValueNode
		want  []string
		why   map[string]string
		held  []string
	}{
		{"incident first, then dated, then undated",
			[]ValueNode{
				{ID: "t-undated", Kind: KindTask, Created: "2026-10-01"},
				{ID: "t-dated", Kind: KindTask, Due: "2026-11-20", Created: "2026-10-02"},
				{ID: "inc", Kind: KindIncident, Sev: "sev2", Created: "2026-11-01"},
				{ID: "t-sooner", Kind: KindTask, Due: "2026-11-10", Created: "2026-10-03"},
			},
			[]string{"inc", "t-sooner", "t-dated", "t-undated"}, nil, nil},
		{"worst incident first",
			[]ValueNode{{ID: "b", Kind: KindIncident, Sev: "sev2"}, {ID: "a", Kind: KindIncident, Sev: "sev1"}},
			[]string{"a", "b"}, nil, nil},
		{"a question inherits the due date of what it blocks, through the goal",
			[]ValueNode{
				{ID: "g", Kind: KindGoal, Due: "2026-11-12"},
				{ID: "task", Kind: KindTask, Goal: "g", Deps: []string{"q"}},
				{ID: "q", Kind: KindQuestion},
				{ID: "own", Kind: KindTask, Due: "2026-11-15"},
				{ID: "idle", Kind: KindQuestion},
			},
			[]string{"q", "task", "g", "own", "idle"},
			map[string]string{"q": "blocks 1, goal due Nov 12", "task": "goal due Nov 12, waits on q", "own": "due Nov 15", "idle": "blocks nothing, no date"}, nil},
		{"ties go to higher impact, a goal counts twice, then oldest, then id",
			[]ValueNode{
				{ID: "g", Kind: KindGoal, Due: "2026-11-12"},
				{ID: "a", Kind: KindQuestion, Created: "2026-10-01", Deps: nil},
				{ID: "t1", Kind: KindTask, Goal: "g", Deps: []string{"a"}, Created: "2026-10-01"},
				{ID: "b", Kind: KindQuestion, Created: "2026-10-02"},
				{ID: "t2", Kind: KindTask, Due: "2026-11-12", Deps: []string{"b"}, Created: "2026-10-03"},
				{ID: "t3", Kind: KindTask, Due: "2026-11-12", Deps: []string{"b"}, Created: "2026-10-03"},
			},
			// a: unblocks t1 and the goal (impact 3, due Nov 12); b: t2, t3 (impact 2, due Nov 12).
			[]string{"a", "t1", "b", "g", "t2", "t3"}, nil, nil},
		{"a deferred item adds no impact and gives no date, and is not listed",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion},
				{ID: "later", Kind: KindTask, Due: "2026-11-05", Deps: []string{"q"}, Deferred: true, DeferUntil: "2027-01-01"},
				{ID: "after", Kind: KindTask, Deps: []string{"later"}},
			},
			[]string{"after", "q"}, map[string]string{"q": "blocks nothing, no date"}, nil},
		{"a deferred question still blocks and is reported when it holds up earlier work",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion, Deferred: true, DeferUntil: "2026-12-01"},
				{ID: "task", Kind: KindTask, Due: "2026-11-10", Deps: []string{"q"}},
			},
			[]string{"task"}, nil, []string{"q"}},
		{"deferred until after the due date is not reported",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion, Deferred: true, DeferUntil: "2026-11-05"},
				{ID: "task", Kind: KindTask, Due: "2026-11-10", Deps: []string{"q"}},
			},
			[]string{"task"}, nil, nil},
		{"someday holds up anything dated",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion, Deferred: true, DeferUntil: "someday"},
				{ID: "task", Kind: KindTask, Due: "2030-01-01", Deps: []string{"q"}},
			},
			[]string{"task"}, nil, []string{"q"}},
		{"hidden work shapes the order and is not listed",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion},
				{ID: "theirs", Kind: KindTask, Hidden: true, Due: "2026-11-09", Deps: []string{"q"}},
			},
			[]string{"q"}, nil, nil},
		{"a dependency that is not open is ignored",
			[]ValueNode{{ID: "t", Kind: KindTask, Deps: []string{"gone"}}},
			[]string{"t"}, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RankValue(c.nodes)
			if !reflect.DeepEqual(rowIDs(got.Rows), c.want) {
				t.Errorf("order = %v, want %v", rowIDs(got.Rows), c.want)
			}
			for _, r := range got.Rows {
				if w, ok := c.why[r.ID]; ok && r.Why != w {
					t.Errorf("why(%s) = %q, want %q", r.ID, r.Why, w)
				}
			}
			var held []string
			for _, h := range got.Held {
				held = append(held, h.ID)
			}
			if !reflect.DeepEqual(held, c.held) && (len(held) != 0 || len(c.held) != 0) {
				t.Errorf("held up = %v, want %v", held, c.held)
			}
		})
	}
}

// The same nodes in any input order give the same rows.
func TestRankValueIsStable(t *testing.T) {
	nodes := []ValueNode{
		{ID: "a", Kind: KindQuestion, Created: "2026-10-01"},
		{ID: "b", Kind: KindQuestion, Created: "2026-10-01"},
		{ID: "c", Kind: KindTask, Due: "2026-11-01", Deps: []string{"a"}},
	}
	rev := []ValueNode{nodes[2], nodes[1], nodes[0]}
	if x, y := rowIDs(RankValue(nodes).Rows), rowIDs(RankValue(rev).Rows); !reflect.DeepEqual(x, y) {
		t.Errorf("%v != %v", x, y)
	}
}
