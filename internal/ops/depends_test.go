package ops

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// dependsVault is a vault at the current format whose clock stands at
// 2026-11-04, with one person.
func dependsVault(t *testing.T) Deps {
	t.Helper()
	d, _ := agendaVault(t, "2026-11-04T12:00:00Z")
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, strconv.Itoa(upgrade.Latest())); err != nil {
		t.Fatal(err)
	}
	return d
}

func setDep(t *testing.T, d Deps, id, on string, optional bool) DependsSetOutput {
	t.Helper()
	return must(t, DependsSet, d, DependsSetInput{ID: id, On: on, Optional: optional})
}

// An optional dependency is shown but holds nothing: no wait, no inherited
// due date, no impact credit, no goal reason. Made hard, it does all of them.
func TestOptionalDependencyHoldsNothing(t *testing.T) {
	d := dependsVault(t)
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Platform"}).ID
	goal := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Launch", Owner: "Jordan Example", Due: "2026-12-01"}).ID
	a := must(t, TaskAdd, d, TaskAddInput{Project: goal, Title: "Ship it", Due: "2026-11-20", DoneWhen: "Shipped."}).ID
	b := must(t, TaskAdd, d, TaskAddInput{Area: area, Title: "Tidy the config", DoneWhen: "Tidy."}).ID

	check := func(label string, hard bool) {
		t.Helper()
		sn, err := NewSnapshot(ctx, d.Store)
		if err != nil {
			t.Fatal(err)
		}
		if waiting := sn.Need(a).Turn == TurnWaiting; waiting != hard {
			t.Errorf("%s: need of %s = %+v", label, a, sn.Need(a))
		}
		rows := orderIDs(t, d)
		rb := rows[b]
		if hard != (rb.EffDue == "2026-11-20") || hard != (rb.Blocks == 1) {
			t.Errorf("%s: row of %s = %+v", label, b, rb)
		}
		if _, listed := rows[a]; listed == hard {
			t.Errorf("%s: %s listed %v", label, a, listed)
		}
		var goalReason bool
		for _, r := range itemContext(t, d, b).Reasons {
			goalReason = goalReason || r.Kind == ReasonGoal && r.Ref == goal
		}
		if goalReason != hard {
			t.Errorf("%s: reasons of %s = %+v", label, b, itemContext(t, d, b).Reasons)
		}
	}
	out := setDep(t, d, a, b, true)
	if len(out.DependsOn) != 0 || !slices.Equal(out.OptionalAfter, []string{b}) {
		t.Fatalf("optional = %+v", out)
	}
	check("optional", false)
	out = setDep(t, d, a, b, false)
	if !slices.Equal(out.DependsOn, []string{b}) || len(out.OptionalAfter) != 0 {
		t.Fatalf("hard = %+v", out)
	}
	check("hard", true)
	// task_depends_add of an optional edge makes it hard too.
	setDep(t, d, a, b, true)
	td := must(t, TaskDependsAdd, d, TaskDependsInput{ID: a, DependsOn: []string{b}})
	if _, tk, _ := getTask(ctx, d.Store, a); !slices.Equal(td.DependsOn, []string{b}) || len(tk.OptionalAfter) != 0 {
		t.Fatalf("task_depends_add = %+v, optional_after %v", td, tk.OptionalAfter)
	}
}

// Optional after a meeting: the task is startable now, not "not yet".
func TestOptionalDependencyIsStartable(t *testing.T) {
	d, ada := agendaVault(t, "2026-11-04T12:00:00Z")
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Platform"}).ID
	m := meet(t, d, ada, "Design review", "2026-11-12T15:00:00Z", "2026-11-12T16:00:00Z")
	t1 := must(t, TaskAdd, d, TaskAddInput{Area: area, Title: "Apply review outcome", Due: "2026-11-13"}).ID
	setDep(t, d, t1, m, true)
	r, ok := orderIDs(t, d)[t1]
	if !ok || r.Bucket == BucketNotYet || strings.HasPrefix(r.Why, "not yet") {
		t.Fatalf("optional: row = %+v (listed %v)", r, ok)
	}
	setDep(t, d, t1, m, false)
	if r, ok := orderIDs(t, d)[t1]; ok {
		t.Fatalf("hard: listed %+v", r)
	}
}

// An obligation's optional dependency is no prep: no backward date.
func TestOptionalDependencyNoBackwardDate(t *testing.T) {
	d, proj, person := obFixture(t)
	m := must(t, ObligationAdd, d, ObligationAddInput{Title: "Plan review", With: []string{person}, Start: "2026-11-10T14:00:00Z", End: "2026-11-10T15:00:00Z", Project: proj}).ID
	dep := must(t, TaskAdd, d, TaskAddInput{Title: "Get the numbers", Project: proj, Effort: model.EffortM, DoneWhen: "Numbers in."}).ID
	setDep(t, d, m, dep, true)
	if got := partIDs(must(t, ObligationParts, d, ObligationPartsInput{ID: m}).Before); slices.Contains(got, dep) {
		t.Fatalf("optional: before = %v", got)
	}
	if r := orderIDs(t, d)[dep]; r.Back != "" || r.For == m {
		t.Fatalf("optional: row = %+v", r)
	}
	setDep(t, d, m, dep, false)
	parts := must(t, ObligationParts, d, ObligationPartsInput{ID: m})
	if i := slices.IndexFunc(parts.Before, func(p ObligationPart) bool { return p.ID == dep }); i < 0 || parts.Before[i].Back != "2026-11-09" {
		t.Fatalf("hard: before = %+v", parts.Before)
	}
}

func TestDependsSetRules(t *testing.T) {
	d := dependsVault(t)
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Platform"}).ID
	task := func(title string) string {
		return must(t, TaskAdd, d, TaskAddInput{Area: area, Title: title, DoneWhen: "Done."}).ID
	}
	a, b := task("Ship it"), task("Tidy the config")
	fails := func(label string, in DependsSetInput, want string) {
		t.Helper()
		if _, err := DependsSet.Run(ctx, d, in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", label, err, want)
		}
	}
	fails("self", DependsSetInput{ID: a, On: a}, "cannot depend on itself")
	fails("self optional", DependsSetInput{ID: a, On: a, Optional: true}, "cannot depend on itself")
	fails("both", DependsSetInput{ID: a, On: b, Reason: "x"}, "not both")
	fails("neither", DependsSetInput{ID: a}, "not both")
	fails("missing", DependsSetInput{ID: a, On: "task-gone"}, "task-gone")
	fails("area", DependsSetInput{ID: a, On: area}, "want a task")
	fails("remove absent", DependsSetInput{ID: a, On: b, Remove: true}, "does not wait on")
	// A hard edge back is a cycle; an optional one cannot deadlock.
	setDep(t, d, a, b, false)
	fails("cycle", DependsSetInput{ID: b, On: a}, "dependency cycle")
	setDep(t, d, b, a, true)
	fails("cycle by toggle", DependsSetInput{ID: b, On: a}, "dependency cycle")
	out := must(t, DependsSet, d, DependsSetInput{ID: b, On: a, Remove: true})
	if len(out.DependsOn)+len(out.OptionalAfter) != 0 {
		t.Fatalf("remove = %+v", out)
	}
	// A text reason blocks the task; removing the last one unblocks it.
	r := must(t, DependsSet, d, DependsSetInput{ID: b, Reason: "  vendor   reply "})
	if _, tk, _ := getTask(ctx, d.Store, b); tk.Status != model.TaskBlocked || !slices.Equal(r.BlockedBy, []string{"vendor reply"}) {
		t.Fatalf("reason = %+v, status %s", r, tk.Status)
	}
	fails("optional reason", DependsSetInput{ID: b, Reason: "x", Optional: true}, "cannot be optional")
	must(t, DependsSet, d, DependsSetInput{ID: b, Reason: "vendor reply", Remove: true})
	if _, tk, _ := getTask(ctx, d.Store, b); tk.Status != model.TaskTodo || len(tk.BlockedBy) != 0 {
		t.Fatalf("unblocked = %+v", tk)
	}
	must(t, Close, d, CloseInput{ID: b, Reason: "Tidy."})
	fails("closed", DependsSetInput{ID: b, On: a, Optional: true}, "already")
}

// An agent's depends_set applies at once and its receipt undoes it.
func TestDependsSetUndo(t *testing.T) {
	d := dependsVault(t)
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Platform"}).ID
	a := must(t, TaskAdd, d, TaskAddInput{Area: area, Title: "Ship it", DoneWhen: "Done."}).ID
	b := must(t, TaskAdd, d, TaskAddInput{Area: area, Title: "Tidy the config", DoneWhen: "Done."}).ID
	setDep(t, d, a, b, false)
	ag := agentDeps(d, "agent")
	if _, err := DependsSet.Run(WithWrite(ctx, WriteMeta{Why: "The owner said it is optional."}), ag, DependsSetInput{ID: a, On: b, Optional: true}); err != nil {
		t.Fatal(err)
	}
	if _, tk, _ := getTask(ctx, d.Store, a); len(tk.DependsOn) != 0 || len(tk.OptionalAfter) != 1 {
		t.Fatalf("applied = %+v %+v", tk.DependsOn, tk.OptionalAfter)
	}
	recs := must(t, ReceiptList, d, ReceiptListInput{Op: "depends_set"})
	if len(recs.Items) != 1 {
		t.Fatalf("receipts = %+v", recs.Items)
	}
	must(t, ReceiptUndo, d, ReceiptUndoInput{ID: recs.Items[0].ID})
	if _, tk, _ := getTask(ctx, d.Store, a); !slices.Equal(tk.DependsOn, []model.Link{model.LinkTo(b)}) || len(tk.OptionalAfter) != 0 {
		t.Fatalf("undone = %+v %+v", tk.DependsOn, tk.OptionalAfter)
	}
}
