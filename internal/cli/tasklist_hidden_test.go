package cli

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

// TestTaskListSaysWhatItHides: task list reports the deferred tasks it leaves
// out, in text and in JSON.
func TestTaskListSaysWhatItHides(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Synthetic Area")
	var a, b ops.TaskAddOutput
	e.json(&a, "task", "add", "Shown task", "--done-when", "Done.", "--area", area.ID)
	e.json(&b, "task", "add", "Put off task", "--done-when", "Done.", "--area", area.ID)
	e.ok("defer", b.ID, "--until", "2999-01-01", "--reason", "Later.")
	var out ops.TaskListOutput
	e.json(&out, "task", "list")
	if len(out.Tasks) != 1 || out.DeferredHidden != 1 {
		t.Fatalf("list = %+v", out)
	}
	if txt := e.ok("task", "list"); !strings.Contains(txt, "1 shown; 1 put off") || !strings.Contains(txt, "--deferred") {
		t.Fatalf("text = %q", txt)
	}
	out = ops.TaskListOutput{}
	e.json(&out, "task", "list", "--deferred")
	if len(out.Tasks) != 2 || out.DeferredHidden != 0 {
		t.Fatalf("with --deferred = %+v", out)
	}
	if txt := e.ok("task", "list", "--deferred"); strings.Contains(txt, "shown;") {
		t.Fatalf("notice with nothing hidden: %q", txt)
	}
}
