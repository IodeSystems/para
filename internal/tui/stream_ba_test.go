package tui

import (
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/ops"
)

// Today carries one health line with the count, outside the agenda; it opens
// the Health page, where a finding is dismissed with a reason.
func TestHealthLineOpensPageAndDismisses(t *testing.T) {
	v, _, _, _ := azVault(t)
	d := newDrv(t, v, 120, 36)
	before := d.m.snap.Health.Count
	if before == 0 || d.m.snap.Today.Health != before {
		t.Fatalf("health count: today %d, health %d", d.m.snap.Today.Health, before)
	}
	if !strings.Contains(rowText(d.m.rowsToday(120)), "health finding") {
		t.Fatalf("no health line on Today:\n%s", rowText(d.m.rowsToday(120)))
	}
	d.m.act("health")
	d.send(nil)
	if p := d.m.top(); p == nil || p.kind != pageHealth {
		t.Fatalf("health page not open:\n%s", d.plainFrame())
	}
	d.key("g")
	for range 40 {
		if r, ok := d.m.current(); ok && r.kind == "health" {
			break
		}
		d.key("j")
	}
	r, _ := d.m.current()
	if r.kind != "health" {
		t.Fatalf("no finding row:\n%s", d.plainFrame())
	}
	d.key(d.keyFor("act:dismiss"))
	d.key("enter") // a reason is required
	if !d.has("Dismiss: " + ops.HealthKindWords[r.hkind]) {
		t.Fatalf("dismissed with no reason:\n%s", d.plainFrame())
	}
	d.typeText("Fine as it is.")
	d.key("enter")
	if got := d.m.snap.Health; got.Count != before-1 || got.Dismissed != 1 {
		t.Fatalf("after dismiss: count %d (was %d), dismissed %d", got.Count, before, got.Dismissed)
	}
}
