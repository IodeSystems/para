package cli

import (
	"strings"
	"testing"
)

// A CLI call counts once as the owner on surface cli; --unused lists the ops
// with no calls, and not one that was called.
func TestOpsStats(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.ok("lint")
	var out struct {
		Ops []struct {
			Op       string
			Calls    int
			Surfaces map[string]int
			Actors   map[string]int
		}
	}
	e.json(&out, "ops", "stats", "--surface", "cli")
	by := map[string]int{}
	for _, o := range out.Ops {
		by[o.Op] = o.Calls
		if o.Op == "lint" && (o.Surfaces["cli"] != o.Calls || o.Actors["owner"] != o.Calls) {
			t.Errorf("lint = %+v", o)
		}
	}
	if by["lint"] != 1 {
		t.Errorf("lint calls = %d, want 1", by["lint"])
	}
	if _, ok := by["events_tail"]; !ok {
		t.Errorf("an op never called is not listed")
	}
	un := e.ok("ops", "stats", "--unused")
	if strings.Contains(un, "\nlint\n") || !strings.Contains(un, "events_tail") {
		t.Errorf("--unused:\n%s", un)
	}
}
