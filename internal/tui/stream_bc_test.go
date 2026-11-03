package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func findRequests(t *testing.T, v *testVault, title string) []ops.RequestView {
	t.Helper()
	reqs, err := ops.RequestList.Run(context.Background(), v.env.Deps, ops.RequestListInput{})
	if err != nil {
		t.Fatal(err)
	}
	var out []ops.RequestView
	for _, r := range reqs.Items {
		if r.Title == title {
			out = append(out, r)
		}
	}
	return out
}

func taskID(t *testing.T, v *testVault, title string) string {
	t.Helper()
	tasks, err := ops.TaskList.Run(context.Background(), v.env.Deps, ops.TaskListInput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tasks.Tasks {
		if tk.Title == title {
			return tk.ID
		}
	}
	t.Fatalf("no task %q", title)
	return ""
}

// Investigate works on a task row with an empty hint, says "agent looking",
// and a second press adds its hint to the open work.
func TestFindContextOnATaskRowAndSecondPressAppends(t *testing.T) {
	v, _, _, _ := azVault(t)
	task := taskID(t, v, "Schedule the move")
	d := newDrv(t, v, 120, 36)
	d.key("3")
	d.onRow("task", task)
	if !strings.Contains(d.footer(), "Ask agent") {
		t.Fatalf("no Ask agent on a task row: %s", d.footer())
	}
	d.key(d.keyFor("act:find"))
	d.key("enter") // an empty hint
	got := findRequests(t, v, "Investigate: Schedule the move")
	if len(got) != 1 || got[0].Status != model.RequestQueued || !got[0].Investigates {
		t.Fatalf("requests = %+v\n%s", got, d.plainFrame())
	}
	d.onRow("task", task)
	if !d.has("agent looking") {
		t.Errorf("task row does not say an agent is looking:\n%s", d.plainFrame())
	}
	d.key(d.keyFor("act:find"))
	if !d.has("looking already") {
		t.Errorf("no word on the open work:\n%s", d.plainFrame())
	}
	d.typeText("check the wiki")
	d.key("enter")
	if n := len(findRequests(t, v, "Investigate: Schedule the move")); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
	sh, err := ops.RequestShow.Run(context.Background(), v.env.Deps, ops.RequestShowInput{ID: got[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sh.Doc.Body, "- check the wiki") {
		t.Errorf("hint not added:\n%s", sh.Doc.Body)
	}
}

// Investigate is offered on an opened task and on Today, and not on a row
// that is no item.
func TestFindContextOnOpenedTaskAndToday(t *testing.T) {
	v, _, _, _ := azVault(t)
	task := taskID(t, v, "Schedule the move")
	d := newDrv(t, v, 120, 36)
	d.key("1")
	d.onRow("task", task)
	d.key("enter")
	if !strings.Contains(d.footer(), "Ask agent") {
		t.Fatalf("no Ask agent on an opened task: %s", d.footer())
	}
	d.key(d.keyFor("act:find"))
	d.key("enter")
	if n := len(findRequests(t, v, "Investigate: Schedule the move")); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
	d.key("esc")
	d.key("g")
	for range 40 {
		if r, ok := d.m.current(); ok && r.target == "act:health" {
			break
		}
		d.key("j")
	}
	if r, _ := d.m.current(); r.target != "act:health" {
		t.Fatalf("never reached the health row:\n%s", d.plainFrame())
	}
	if strings.Contains(d.footer(), "Ask agent") {
		t.Errorf("Ask agent on a row that is no item: %s", d.footer())
	}
}
