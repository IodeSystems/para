package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// The friction report opens from the Asks tab, groups by reason, domain, or
// policy, and never by person.
func TestFrictionReportPage(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	if _, err := ops.RFXTargetState.Run(context.Background(), v.env.Deps, ops.RFXTargetStateInput{
		ID: v.ids["rfx"], To: "person-ada-example", State: model.TargetNoReply, Reason: model.FrictionCapacity, Why: "On leave."}); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{120, 36}, {80, 24}, {60, 24}} {
		d := newDrv(t, v, size[0], size[1])
		d.key("4")
		d.key(d.keyFor("act:friction"))
		for _, want := range []string{"Friction by reason", "capacity (1 asks)", "hops per ask"} {
			if !d.has(want) {
				t.Fatalf("%v: no %q:\n%s", size, want, d.plainFrame())
			}
		}
		seen := map[string]string{}
		for code, a := range d.m.keymap {
			if strings.HasPrefix(a, "act:") {
				if o, dup := seen[code]; dup && o != a {
					t.Errorf("key %q is %s and %s", code, o, a)
				}
				seen[code] = a
			}
		}
		d.key(d.keyFor("act:groupby"))
		if !d.has("Friction by domain") {
			t.Fatalf("%v: not grouped by domain:\n%s", size, d.plainFrame())
		}
		if d.has("Ada") {
			t.Errorf("the report names a person:\n%s", d.plainFrame())
		}
	}
}
