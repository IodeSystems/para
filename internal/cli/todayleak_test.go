package cli

import (
	"testing"

	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/today"
)

// Today lists no task that is done, or put off (Later with a future date or
// someday, or blocked until a future date), in any of its sections: the agenda,
// overdue, due soon, and blocked.
func TestTodayLeavesDoneAndDeferred(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Atlas")
	add := func(title string) string {
		var o ops.TaskAddOutput
		e.json(&o, "task", "add", title, "--area", area.ID, "--due", "2020-01-01")
		return o.ID
	}
	open, done, until, someday, blocked := add("Open one"), add("Done one"), add("Until one"), add("Someday one"), add("Blocked one")
	e.ok("task", "done", done)
	e.ok("defer", until, "--until", "2999-01-01")
	e.ok("defer", someday)
	e.ok("task", "block", blocked, "--reason", "waiting", "--until", "2999-01-01")

	var rep today.Report
	e.json(&rep, "today")
	v := rep.Vaults[0]
	sections := map[string][]today.Item{"agenda": v.Agenda.Items, "overdue": v.Overdue.Items, "soon": v.DueSoon.Items, "blocked": v.Blocked.Items}
	for name, items := range sections {
		for _, it := range items {
			if it.ID == done || it.ID == until || it.ID == someday || it.ID == blocked {
				t.Errorf("%s lists %s (%s)", name, it.ID, it.Title)
			}
		}
	}
	found := false
	for _, it := range v.Overdue.Items {
		found = found || it.ID == open
	}
	if !found {
		t.Errorf("overdue lacks the open task: %+v", v.Overdue.Items)
	}
}
