package ops

import (
	"slices"
	"testing"
)

// The tasks of a parked goal leave Next (order) and needs_you with no write
// to the tasks, and come back when the goal is unparked. The project
// bundle's icebox counts them.
func TestParkedGoalHidesTasks(t *testing.T) {
	rv := newReqVault(t)
	cli := rv.as("user", "cli", "install-a")
	ctx := writeCtx(t)
	format11(t, cli)

	p := must(t, ProjectAdd, cli, ProjectAddInput{Title: "Garden"})
	g := must(t, GoalAdd, cli, GoalAddInput{Title: "Pond", DoneWhen: "Pond holds water.", Project: p.ID})
	t1 := must(t, TaskAdd, cli, TaskAddInput{Goal: g.ID, Title: "Dig hole", DoneWhen: "Hole dug."})
	t2 := must(t, TaskAdd, cli, TaskAddInput{Goal: g.ID, Title: "Buy liner", DoneWhen: "Liner bought."})
	g2 := must(t, GoalAdd, cli, GoalAddInput{Title: "Beds", DoneWhen: "Beds planted.", Project: p.ID})
	other := must(t, TaskAdd, cli, TaskAddInput{Goal: g2.ID, Title: "Water beds", DoneWhen: "Beds watered."})

	needs := func() []string {
		var ids []string
		for _, r := range must(t, List, cli, ListInput{NeedsYou: true}).Rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	next := func() []string {
		var ids []string
		for _, r := range must(t, OrderOp, cli, OrderInput{}).Rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	for _, view := range []func() []string{needs, next} {
		if got := view(); !slices.Contains(got, t1.ID) || !slices.Contains(got, t2.ID) {
			t.Fatalf("before park: %v", got)
		}
	}
	before := map[string]string{}
	for _, id := range []string{t1.ID, t2.ID} {
		d, _ := cli.Store.Get(ctx, id)
		before[id] = d.Rev
	}

	invoke(t, Edit, cli, `{"id":"`+g.ID+`","fields":{"status":"parked","resume_when":"after the dry season"}}`)
	for name, view := range map[string]func() []string{"needs_you": needs, "next": next} {
		got := view()
		if slices.Contains(got, t1.ID) || slices.Contains(got, t2.ID) || !slices.Contains(got, other.ID) {
			t.Fatalf("parked, %s = %v", name, got)
		}
	}
	for id, rev := range before {
		if d, _ := cli.Store.Get(ctx, id); d.Rev != rev {
			t.Errorf("%s rewritten by park", id)
		}
	}
	if so := invoke(t, Show, cli, `{"id":"`+t1.ID+`"}`).(ShowOutput); so.NeedsYou || so.Reason != "goal "+g.ID+" is parked" {
		t.Fatalf("show parked task = %v %q", so.NeedsYou, so.Reason)
	}
	must(t, TaskDone, cli, TaskDoneInput{ID: t2.ID})
	sh := must(t, ProjectShow, cli, ProjectShowInput{ID: p.ID})
	if len(sh.Icebox) != 1 || sh.Icebox[0].HiddenTasks != 1 {
		t.Fatalf("icebox = %+v", sh.Icebox)
	}

	invoke(t, Edit, cli, `{"id":"`+g.ID+`","fields":{"status":"active"}}`)
	for name, view := range map[string]func() []string{"needs_you": needs, "next": next} {
		if got := view(); !slices.Contains(got, t1.ID) {
			t.Fatalf("unparked, %s = %v", name, got)
		}
	}
}
