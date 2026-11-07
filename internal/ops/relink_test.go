package ops

import (
	"slices"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// Relinking dependents moves an optional edge too, and it stays optional.
func TestRelinkMovesOptionalEdges(t *testing.T) {
	d := dependsVault(t)
	goal := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Launch", Owner: "Jordan Example"}).ID
	task := func(title string) string {
		return must(t, TaskAdd, d, TaskAddInput{Project: goal, Title: title, DoneWhen: "Done."}).ID
	}
	deps := func(id string) ([]string, []string) {
		t.Helper()
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return targets(model.HardDeps(doc.Meta)), targets(model.OptionalDeps(doc.Meta))
	}

	// task_resolve duplicate: hard stays hard, optional stays optional.
	old, keep := task("Write the notes"), task("Write the notes again")
	hard, opt := task("Publish"), task("Tidy")
	setDep(t, d, hard, old, false)
	setDep(t, d, opt, old, true)
	out := must(t, TaskResolve, d, TaskResolveInput{ID: old, Outcome: model.OutcomeDuplicate, Link: keep})
	moved := slices.Sorted(slices.Values(out.Moved))
	if want := slices.Sorted(slices.Values([]string{hard, opt})); !slices.Equal(moved, want) {
		t.Errorf("moved = %v, want %v", moved, want)
	}
	if h, o := deps(hard); !slices.Equal(h, []string{keep}) || len(o) != 0 {
		t.Errorf("hard dependent: depends_on %v, optional_after %v", h, o)
	}
	if h, o := deps(opt); len(h) != 0 || !slices.Equal(o, []string{keep}) {
		t.Errorf("optional dependent: depends_on %v, optional_after %v", h, o)
	}

	// task_to_question: the optional edge moves to the question.
	q := task("Pick a host")
	later := task("Write the runbook")
	setDep(t, d, later, q, true)
	tq := must(t, TaskToQuestion, d, TaskToQuestionInput{ID: q})
	if !slices.Contains(tq.Relinked, later) {
		t.Errorf("relinked = %v", tq.Relinked)
	}
	if h, o := deps(later); len(h) != 0 || !slices.Equal(o, []string{tq.Question}) {
		t.Errorf("after task_to_question: depends_on %v, optional_after %v", h, o)
	}

	// An item already hard on the new target keeps only the hard edge.
	a, b := task("Draft"), task("Draft, take two")
	both := task("Review")
	setDep(t, d, both, a, true)
	setDep(t, d, both, b, false)
	must(t, TaskResolve, d, TaskResolveInput{ID: a, Outcome: model.OutcomeSuperseded, Link: b})
	if h, o := deps(both); !slices.Equal(h, []string{b}) || len(o) != 0 {
		t.Errorf("hard and optional: depends_on %v, optional_after %v", h, o)
	}
}
