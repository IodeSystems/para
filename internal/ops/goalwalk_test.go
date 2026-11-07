package ops

import (
	"slices"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// valueGoals is the goals the value order reaches downstream of id.
func valueGoals(nodes []ValueNode, id string) []string {
	by, down, _ := valueGraph(nodes)
	seen := map[string]bool{id: true}
	queue := []string{id}
	var out []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, u := range down[cur] {
			if seen[u] {
				continue
			}
			seen[u] = true
			if by[u].Kind == KindGoal {
				out = append(out, u)
			}
			queue = append(queue, u)
		}
	}
	slices.Sort(out)
	return out
}

// reasonGoals is the goals the reason walker reports for id.
func reasonGoals(g *reasonGraph, id string) []string {
	var out []string
	for _, r := range g.reasons(id) {
		if r.Kind == ReasonGoal && r.Ref != id {
			out = append(out, r.Ref)
		}
	}
	slices.Sort(out)
	return out
}

// TestValueGoalsAreReasonGoals: the goal the value order ranks an item by is
// the goal its Why names, through a direct project, a parent, a depends_on
// chain, a question's scope, and an obligation's action item. An optional
// edge leads to no goal.
func TestValueGoalsAreReasonGoals(t *testing.T) {
	d := newDeps(t)
	g1 := must(t, ProjectAdd, d, ProjectAddInput{Title: "Ship the portal", Owner: "me", DoneWhen: "Shipped.", Due: "2026-12-01"}).ID
	g2 := must(t, ProjectAdd, d, ProjectAddInput{Title: "Move the office", Owner: "me", DoneWhen: "Moved."}).ID
	mk := func(title string, m *model.Task) string {
		t.Helper()
		m.Common, m.Status, m.DoneWhen = model.Common{Type: model.TypeTask}, model.TaskTodo, "Done."
		if m.Project == "" {
			// Lint wants a project; a gone one leads nowhere, as a hand edit
			// can leave it.
			m.Project = model.LinkTo("project-gone")
		}
		doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderProjects, Title: title, Meta: m})
		if err != nil {
			t.Fatal(err)
		}
		return doc.ID
	}
	direct := mk("Direct", &model.Task{Project: model.LinkTo(g1)})
	parent := mk("Parent", &model.Task{Project: model.LinkTo(g1)})
	child := mk("Child", &model.Task{Parent: model.LinkTo(parent)})
	// a0 <- a1 <- b (b is in g2): a0 and a1 lead to g2.
	a0 := mk("Chain start", &model.Task{})
	a1 := mk("Chain middle", &model.Task{DependsOn: []model.Link{model.LinkTo(a0)}})
	mk("Chain end", &model.Task{Project: model.LinkTo(g2), DependsOn: []model.Link{model.LinkTo(a1)}})
	// opt is only better before a g1 task: no goal.
	opt := mk("Better before", &model.Task{})
	mk("Takes the optional", &model.Task{Project: model.LinkTo(g1), OptionalAfter: []model.Link{model.LinkTo(opt)}})
	hub := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the move plan", Project: g2, Due: "2026-11-20", DoneWhen: "Sent."}).ID
	hubChild := mk("Draft the move plan", &model.Task{Parent: model.LinkTo(hub)})
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which login page?", Task: direct}).ID

	sn, err := SnapshotAt(ctx, d.Store, "2026-11-06")
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := valueNodes(sn, true)
	if err != nil {
		t.Fatal(err)
	}
	g := newReasonGraph(sn.docs)
	want := map[string][]string{
		direct: {g1}, child: {g1}, a0: {g2}, a1: {g2}, opt: nil, hubChild: {g2}, hub: {g2}, q: {g1},
	}
	for id, w := range want {
		if got := reasonGoals(g, id); !slices.Equal(got, w) {
			t.Errorf("%s: reason goals %v, want %v", id, got, w)
		}
	}
	for _, n := range nodes {
		if n.Kind == KindGoal {
			continue
		}
		if v, r := valueGoals(nodes, n.ID), reasonGoals(g, n.ID); !slices.Equal(v, r) {
			t.Errorf("%s: value order goals %v, reason goals %v", n.ID, v, r)
		}
	}
	// The effective due comes from the goal through the parent and the scope.
	rows := map[string]ValueRow{}
	for _, r := range RankValue(nodes, RankOptions{Today: "2026-11-06"}).Rows {
		rows[r.ID] = r
	}
	for _, id := range []string{child, q} {
		if rows[id].EffDue != "2026-12-01" {
			t.Errorf("%s: eff_due %q, want the goal's 2026-12-01", id, rows[id].EffDue)
		}
	}
	if rows[opt].EffDue != "" {
		t.Errorf("optional edge gave eff_due %q", rows[opt].EffDue)
	}
}
