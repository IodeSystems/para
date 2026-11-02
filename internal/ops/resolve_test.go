package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

func taskOf(t *testing.T, d Deps, id string) (TaskView, string) {
	t.Helper()
	o := must(t, TaskShow, d, TaskShowInput{ID: id})
	return o.Task, o.Body
}

func TestTaskResolveOutcomes(t *testing.T) {
	d, proj := laterVault(t)
	add := func(title string, deps ...string) string {
		return must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: title, DependsOn: deps}).ID
	}

	// Refusals first: nothing is written.
	a := add("Ship the parser")
	for name, in := range map[string]TaskResolveInput{
		"unknown outcome":      {ID: a, Outcome: "won"},
		"partial, no why":      {ID: a, Outcome: "partial"},
		"dropped, no note":     {ID: a, Outcome: "dropped"},
		"duplicate, no link":   {ID: a, Outcome: "duplicate"},
		"superseded, no link":  {ID: a, Outcome: "superseded", Note: "x"},
		"duplicate of itself":  {ID: a, Outcome: "duplicate", Link: a},
		"duplicate of nothing": {ID: a, Outcome: "duplicate", Link: "task-nope"},
		"bad due":              {ID: a, Outcome: "partial", FollowUps: []string{"x"}, Due: "soon"},
	} {
		if _, err := TaskResolve.Run(ctx, d, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if v, _ := taskOf(t, d, a); v.Status != model.TaskTodo {
		t.Fatalf("a refused resolve changed the task: %+v", v)
	}

	// done is task_done: the default outcome, and a recurring task gets its next.
	o := must(t, TaskResolve, d, TaskResolveInput{ID: a, Note: "Merged."})
	if o.Status != model.TaskDone || o.Outcome != model.OutcomeDone {
		t.Errorf("done = %+v", o)
	}
	if v, body := taskOf(t, d, a); v.Outcome != model.OutcomeDone || !strings.Contains(body, "## Outcome\n\nMerged.") {
		t.Errorf("done task = %+v %q", v, body)
	}
	rec := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Weekly", Recur: "weekly", Due: "2026-11-03"}).ID
	if r := must(t, TaskResolve, d, TaskResolveInput{ID: rec}); r.Next == "" {
		t.Errorf("recurring done made no next: %+v", r)
	}

	// partial: follow-ups carry the goal; dependents move to the first.
	p := add("Migrate")
	w1, w2 := add("Cut over", p), add("Announce", p)
	o = must(t, TaskResolve, d, TaskResolveInput{ID: p, Outcome: model.OutcomePartial, Note: "Data moved, DNS not.",
		FollowUps: []string{"Move DNS", "Remove old host"}, Due: "2026-12-01", DoneWhen: "Traffic is on the new host."})
	if o.Status != model.TaskDone || len(o.FollowUps) != 2 || len(o.Moved) != 2 {
		t.Fatalf("partial = %+v", o)
	}
	fu, _ := taskOf(t, d, o.FollowUps[0])
	if fu.Project != proj || fu.Due != "2026-12-01" || fu.Status != model.TaskTodo {
		t.Errorf("follow-up = %+v", fu)
	}
	for _, w := range []string{w1, w2} {
		if v, _ := taskOf(t, d, w); !slices.Equal(v.DependsOn, []string{o.FollowUps[0]}) {
			t.Errorf("%s depends on %v, want %s", w, v.DependsOn, o.FollowUps[0])
		}
	}
	v, body := taskOf(t, d, p)
	if v.Outcome != model.OutcomePartial || v.DoneWhen != "Traffic is on the new host." ||
		!strings.Contains(body, "Data moved, DNS not.") || !strings.Contains(body, "Done when changed. Was: (none)") ||
		!strings.Contains(body, "Follow-up: [["+o.FollowUps[1]+"]]") {
		t.Errorf("partial task = %+v\n%s", v, body)
	}
	if _, err := TaskResolve.Run(ctx, d, TaskResolveInput{ID: p, Outcome: model.OutcomeDropped, Note: "x"}); err == nil {
		t.Error("a closed task was resolved again")
	}
	// partial with a note and no follow-up releases the dependents.
	q := add("Review")
	w3 := add("Merge", q)
	o = must(t, TaskResolve, d, TaskResolveInput{ID: q, Outcome: model.OutcomePartial, Note: "Rest is not worth doing."})
	if len(o.FollowUps) != 0 || !slices.Equal(o.Released, []string{w3}) {
		t.Errorf("partial note = %+v", o)
	}

	// dropped: dependents are released and the body says so.
	x := add("Old idea")
	w4 := add("Needs old idea", x)
	o = must(t, TaskResolve, d, TaskResolveInput{ID: x, Outcome: model.OutcomeDropped, Note: "Not needed any more."})
	if o.Status != model.TaskDropped || !slices.Equal(o.Released, []string{w4}) || len(o.Moved) != 0 {
		t.Errorf("dropped = %+v", o)
	}
	if v, body := taskOf(t, d, x); v.Outcome != model.OutcomeDropped || !strings.Contains(body, "Released, no longer waiting: [["+w4+"]]") {
		t.Errorf("dropped task = %+v\n%s", v, body)
	}

	// duplicate and superseded: dependents move to the link, or to the first follow-up.
	keep := add("The real one")
	dup := add("Copy")
	w5 := add("Waits on copy", dup)
	o = must(t, TaskResolve, d, TaskResolveInput{ID: dup, Outcome: model.OutcomeDuplicate, Link: keep})
	if o.Status != model.TaskDropped || !slices.Equal(o.Moved, []string{w5}) {
		t.Errorf("duplicate = %+v", o)
	}
	if v, _ := taskOf(t, d, w5); !slices.Equal(v.DependsOn, []string{keep}) {
		t.Errorf("duplicate dependent = %v", v.DependsOn)
	}
	if _, body := taskOf(t, d, dup); !strings.Contains(body, "Duplicate of [["+keep+"]]") {
		t.Errorf("duplicate body = %s", body)
	}
	old := add("Old way")
	w6 := add("Waits on old way", old)
	o = must(t, TaskResolve, d, TaskResolveInput{ID: old, Outcome: model.OutcomeSuperseded, Link: keep, FollowUps: []string{"Port the old way"}})
	if v, _ := taskOf(t, d, w6); !slices.Equal(v.DependsOn, o.FollowUps) || o.Status != model.TaskDropped {
		t.Errorf("superseded = %+v dependent %v", o, v.DependsOn)
	}
	// A move to the link that would close a cycle is refused and the task stays open.
	loop := add("Loop")
	w7 := add("Waits on loop", loop)
	must(t, TaskDependsAdd, d, TaskDependsInput{ID: keep, DependsOn: []string{w7}})
	if _, err := TaskResolve.Run(ctx, d, TaskResolveInput{ID: loop, Outcome: model.OutcomeDuplicate, Link: keep}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("cycle: %v", err)
	}
	if v, _ := taskOf(t, d, loop); v.Status != model.TaskTodo {
		t.Errorf("a refused resolve closed the task: %+v", v)
	}

	// A recurring task: dropped skips this one; the rest are refused.
	rc := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Monthly", Recur: "monthly", Due: "2026-11-03"}).ID
	if _, err := TaskResolve.Run(ctx, d, TaskResolveInput{ID: rc, Outcome: model.OutcomePartial, Note: "x"}); err == nil {
		t.Error("partial on a recurring task")
	}
	if r := must(t, TaskResolve, d, TaskResolveInput{ID: rc, Outcome: model.OutcomeDropped, Note: "Skip this month."}); r.Next == "" || r.Status != model.TaskDropped {
		t.Errorf("recurring dropped = %+v", r)
	}
	mustLintClean(t, d)
}

func TestTaskResolveFromAnAgent(t *testing.T) {
	d, proj := laterVault(t)
	agent := agentOf(d)
	id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Check"}).ID
	for _, o := range []model.TaskOutcome{model.OutcomePartial, model.OutcomeDropped, model.OutcomeDuplicate, model.OutcomeSuperseded} {
		_, err := TaskResolve.Run(ctx, agent, TaskResolveInput{ID: id, Outcome: o, Note: "x", Link: proj, FollowUps: []string{"y"}})
		if !errors.Is(err, ErrNeedsProposal) {
			t.Errorf("%s from an agent: %v", o, err)
		}
	}
	if v, _ := taskOf(t, d, id); v.Status != model.TaskTodo {
		t.Fatalf("a refused outcome changed the task: %+v", v)
	}
	if o := must(t, TaskResolve, agent, TaskResolveInput{ID: id}); o.Outcome != model.OutcomeDone || o.Status != model.TaskDone {
		t.Errorf("agent done = %+v", o)
	}
	// An approved proposal item runs as the owner's decision.
	f := newPropFix(t)
	task := must(t, TaskAdd, f.owner, TaskAddInput{Project: f.proj, Title: "Old"}).ID
	if _, err := ProposalAdd.Run(ctx, f.agent, ProposalAddInput{Title: "Drop it", Source: f.source, Items: []ItemInput{{
		Kind: model.ItemOp, Op: "task_resolve", Input: map[string]any{"id": task, "outcome": "dropped", "note": "Not needed."}, Evidence: ev}}}); err != nil {
		t.Errorf("a proposal item for task_resolve: %v", err)
	}
}

func TestTaskDelegate(t *testing.T) {
	d, proj := laterVault(t)
	id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Draft the notes", DoneWhen: "Notes are in the wiki."}).ID
	if _, err := TaskDelegate.Run(ctx, agentOf(d), TaskDelegateInput{ID: id, Agent: "scribe"}); !errors.Is(err, ErrPersonOnly) {
		t.Errorf("agent: %v", err)
	}

	// No runner and no agent: queued, not assigned, and the note says how to set one.
	o := must(t, TaskDelegate, d, TaskDelegateInput{ID: id, Hint: "Start with the old runbook."})
	if o.Status != model.RequestQueued || o.Assignee != "" || o.Appended || !strings.Contains(o.Note, "request.runner") {
		t.Fatalf("no runner = %+v", o)
	}
	r := must(t, RequestShow, d, RequestShowInput{ID: o.Request}).Doc
	rm := r.Meta.(*model.Request)
	if r.Title != "Work on: Draft the notes" || len(rm.Links) != 1 || rm.Links[0].Target() != id ||
		!strings.Contains(r.Body, "- Start with the old runbook.") || !strings.Contains(r.Body, "Notes are in the wiki.") ||
		!strings.Contains(r.Body, "no external action without an approved proposal") || !strings.Contains(r.Body, "sources") ||
		!strings.Contains(r.Body, "request.runner") {
		t.Errorf("request = %q\n%s", r.Title, r.Body)
	}

	// A second press adds the hint; there is still one request, now with an agent.
	o2 := must(t, TaskDelegate, d, TaskDelegateInput{ID: id, Agent: "agent:Scribe", Hint: "Also check the chat."})
	if !o2.Appended || o2.Request != o.Request || o2.Assignee != "agent:scribe" {
		t.Errorf("second = %+v", o2)
	}
	r = must(t, RequestShow, d, RequestShowInput{ID: o.Request}).Doc
	if strings.Count(r.Body, "## Hints") != 1 || !strings.Contains(r.Body, "- Also check the chat.") {
		t.Errorf("body = %s", r.Body)
	}
	if v, _ := taskOf(t, d, id); v.Assignee != "agent:scribe" || v.Status == model.TaskDone {
		t.Errorf("task = %+v", v)
	}
	n := 0
	for _, it := range must(t, RequestList, d, RequestListInput{}).Items {
		if strings.HasPrefix(it.Title, "Work on: ") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d work requests, want 1", n)
	}
	if _, err := TaskDelegate.Run(ctx, d, TaskDelegateInput{ID: id, Agent: "bad name"}); err == nil {
		t.Error("a bad agent name was accepted")
	}
	// The agent's close of a non-done outcome is refused; done goes through.
	if _, err := TaskResolve.Run(ctx, agentOf(d), TaskResolveInput{ID: id, Outcome: model.OutcomeDropped, Note: "x"}); !errors.Is(err, ErrNeedsProposal) {
		t.Errorf("agent drop: %v", err)
	}
	// Closed tasks are not delegated.
	must(t, TaskResolve, d, TaskResolveInput{ID: id})
	if _, err := TaskDelegate.Run(ctx, d, TaskDelegateInput{ID: id}); err == nil {
		t.Error("a closed task was delegated")
	}
	mustLintClean(t, d)
}
