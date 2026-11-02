package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func taskByTitle(t *testing.T, v *testVault, title string) ops.TaskView {
	t.Helper()
	o, err := ops.TaskList.Run(context.Background(), v.env.Deps, ops.TaskListInput{Deferred: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range o.Tasks {
		if tk.Title == title {
			return tk
		}
	}
	t.Fatalf("no task %q in %+v", title, o.Tasks)
	return ops.TaskView{}
}

func taskRowID(t *testing.T, v *testVault, title string) string {
	return taskByTitle(t, v, title).ID
}

// The task actions have keys that clash with no other action and no fixed key,
// on each tab and on an opened task, at three sizes.
func TestTaskActionKeysDoNotClash(t *testing.T) {
	v, _, _, _ := azVault(t)
	id := taskRowID(t, v, "Schedule the move")
	for _, size := range [][2]int{{120, 36}, {80, 24}, {60, 24}} {
		d := newDrv(t, v, size[0], size[1])
		check := func(where string) {
			t.Helper()
			seen := map[string]string{}
			for code, a := range d.m.keymap {
				if strings.HasPrefix(a, "act:") {
					if other, dup := seen[code]; dup && other != a {
						t.Errorf("%s: key %q is %s and %s", where, code, other, a)
					}
					seen[code] = a
				}
			}
			for _, want := range []string{"act:resolve", "act:start", "act:block", "act:assign", "act:delegate", "act:due", "act:later", "act:find"} {
				if !slices.Contains(mapValues(seen), want) {
					t.Errorf("%s: no key for %s", where, want)
				}
			}
			for _, reserved := range []string{"j", "k", "g", "q"} {
				if a := d.m.keymap[reserved]; strings.HasPrefix(a, "act:") {
					t.Errorf("%s: %s took the reserved key %s", where, a, reserved)
				}
			}
		}
		for tab, name := range map[string]string{"1": "Agenda", "2": "Review", "3": "Work"} {
			d.key(tab)
			check(name)
		}
		d.key("3")
		d.onRow("task", id)
		d.key("enter")
		check("opened task")
		// No link key of the document is also an action key.
		if p := d.m.docPage(); p != nil {
			for id, k := range d.m.renderDoc(p).Keys {
				if a := d.m.keymap[k.Code]; strings.HasPrefix(a, "act:") {
					t.Errorf("link %s has the key %q of %s", id, k.Code, a)
				}
			}
		}
	}
}

func mapValues(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// Resolve with its default is one key and enter.
func TestResolveDefaultIsDone(t *testing.T) {
	v, _, _, _ := azVault(t)
	id := taskRowID(t, v, "Schedule the move")
	d := newDrv(t, v, 120, 36)
	d.key("3")
	d.onRow("task", id)
	if !strings.Contains(d.footer(), "Resolve") || !strings.Contains(d.footer(), "Give to agent") {
		t.Fatalf("footer lacks the task actions: %s", d.footer())
	}
	d.key(d.keyFor("act:resolve"))
	d.key("enter")
	if got := taskByTitle(t, v, "Schedule the move"); got.Status != model.TaskDone || got.Outcome != model.OutcomeDone {
		t.Fatalf("task = %+v\n%s", got, d.plainFrame())
	}
}

func TestResolvePartialWithFollowUpFromAnOpenedTask(t *testing.T) {
	v, proj, _, _ := azVault(t)
	ctx := context.Background()
	first := taskRowID(t, v, "Schedule the move")
	dep, err := ops.TaskAdd.Run(ctx, v.env.Deps, ops.TaskAddInput{Project: proj, Title: "Announce the move", DependsOn: []string{first}})
	if err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 36)
	d.key("3")
	d.onRow("task", first)
	d.key("enter") // the opened task
	d.key(d.keyFor("act:resolve"))
	d.typeText("pa")
	d.key("enter")
	d.typeText("Window agreed, date not.")
	d.key("enter")
	d.typeText("Pick the date; Tell the team")
	d.key("enter")
	got := taskByTitle(t, v, "Schedule the move")
	if got.Outcome != model.OutcomePartial || got.Status != model.TaskDone {
		t.Fatalf("task = %+v\n%s", got, d.plainFrame())
	}
	fu := taskByTitle(t, v, "Pick the date")
	moved, _ := ops.TaskShow.Run(ctx, v.env.Deps, ops.TaskShowInput{ID: dep.ID})
	if !slices.Equal(moved.Task.DependsOn, []string{fu.ID}) {
		t.Errorf("dependent waits on %v, want %s", moved.Task.DependsOn, fu.ID)
	}
	taskByTitle(t, v, "Tell the team")
	// The list says how it ended, not only that it is closed.
	if d.m.top() != nil {
		d.key("esc")
	}
	if rows, _ := d.m.rows(120); !strings.Contains(rowText(rows), "partial") {
		t.Errorf("the Work list does not say partial:\n%s", rowText(rows))
	}
}

func TestResolveDroppedNeedsANote(t *testing.T) {
	v, _, _, _ := azVault(t)
	id := taskRowID(t, v, "Schedule the move")
	d := newDrv(t, v, 120, 36)
	d.key("3")
	d.onRow("task", id)
	d.key(d.keyFor("act:resolve"))
	d.typeText("dr")
	d.key("enter")
	d.key("enter") // the note is empty: the op refuses
	if got := taskByTitle(t, v, "Schedule the move"); got.Status == model.TaskDropped {
		t.Fatalf("dropped with no note: %+v", got)
	}
}

func TestGiveToAgentQueuesWorkAndTheRowSaysSo(t *testing.T) {
	v, _, _, _ := azVault(t)
	id := taskRowID(t, v, "Schedule the move")
	d := newDrv(t, v, 120, 36)
	d.key("3")
	d.onRow("task", id)
	d.key(d.keyFor("act:delegate"))
	d.key("enter") // no agent name
	d.typeText("start with the calendar")
	d.key("enter")
	reqs, err := ops.RequestList.Run(context.Background(), v.env.Deps, ops.RequestListInput{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range reqs.Items {
		found = found || r.Title == "Work on: Schedule the move" && r.Status == model.RequestQueued && r.Delegates
	}
	if !found {
		t.Fatalf("no queued work: %+v", reqs.Items)
	}
	if rows, _ := d.m.rows(120); !strings.Contains(rowText(rows), "Agent working") {
		t.Errorf("the row does not say an agent is working:\n%s", rowText(rows))
	}
}

func TestStartBlockAssignDueFromTheList(t *testing.T) {
	v, _, _, _ := azVault(t)
	id := taskRowID(t, v, "Schedule the move")
	d := newDrv(t, v, 120, 36)
	d.key("3")
	d.onRow("task", id)
	d.key(d.keyFor("act:assign"))
	d.typeText("agent:scribe")
	d.key("enter")
	if got := taskByTitle(t, v, "Schedule the move"); got.Assignee != "agent:scribe" {
		t.Errorf("assignee = %q", got.Assignee)
	}
	d.onRow("task", id)
	d.key(d.keyFor("act:due"))
	d.typeText("2026-12-01")
	d.key("enter")
	if got := taskByTitle(t, v, "Schedule the move"); got.Due != "2026-12-01" {
		t.Errorf("due = %q", got.Due)
	}
	d.onRow("task", id)
	d.key(d.keyFor("act:block"))
	d.typeText("Waiting on facilities.")
	d.key("enter")
	if got := taskByTitle(t, v, "Schedule the move"); got.Status != model.TaskBlocked {
		t.Errorf("status = %q", got.Status)
	}
	d.onRow("task", id)
	d.key(d.keyFor("act:start"))
	if got := taskByTitle(t, v, "Schedule the move"); got.Status != model.TaskDoing && !d.has("waits on") {
		t.Errorf("status = %q\n%s", got.Status, d.plainFrame())
	}
}
