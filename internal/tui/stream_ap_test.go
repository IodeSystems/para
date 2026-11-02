package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func (d *drv) onTarget(target string) {
	d.t.Helper()
	d.key("g")
	for range 80 {
		if r, ok := d.m.current(); ok && r.target == target {
			return
		}
		d.key("j")
	}
	d.t.Fatalf("cursor never reached %s:\n%s", target, d.plainFrame())
}

// Next moves opens from the Asks tab and from an opened Ask, lists the log and
// moves, and Take move adds a target with a reason.
func TestNextMovesOnAnAsk(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	if _, err := ops.OrgPersonContactAdd.Run(ctx, v.env.Deps, ops.PersonContactInput{ID: "person-ada-example", Method: model.MediumSlack, Value: "@ada"}); err != nil {
		t.Fatal(err)
	}
	id := v.ids["rfx"]
	for _, size := range [][2]int{{120, 36}, {80, 24}, {60, 24}} {
		d := newDrv(t, v, size[0], size[1])
		d.key("4")
		d.onTarget("doc:" + id)
		if k := d.keyFor("act:paths"); k == "" {
			t.Fatalf("%v: no key for Next moves:\n%s", size, d.plainFrame())
		}
		d.key(d.keyFor("act:paths"))
		for _, want := range []string{"Next moves", "Tried", "Stalled", "Try Ada Example by slack"} {
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
		if d.keyFor("act:take") == "" {
			t.Fatalf("no Take move key")
		}
	}
	d := newDrv(t, v, 120, 36)
	d.key("4")
	d.onTarget("doc:" + id)
	d.key(d.keyFor("act:paths"))
	d.key(d.keyFor("act:take"))
	d.typeText("1")
	d.key("enter")
	d.typeText("no_response")
	d.key("enter")
	d.typeText("No reply by email in two weeks.")
	d.key("enter")
	o, err := ops.RFXShow.Run(ctx, v.env.Deps, ops.RFXShowInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	ts := o.Doc.Meta.(*model.RFX).Targets
	if len(ts) != 2 || ts[1].Reason != model.FrictionNoResponse || ts[1].Medium != model.MediumSlack {
		t.Fatalf("targets = %+v\n%s", ts, d.plainFrame())
	}
}

// On an opened Ask the key for Next moves clashes with no other action and no link.
func TestNextMovesKeyOnAnOpenedAsk(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 120, 36)
	d.key("4")
	d.onTarget("doc:" + v.ids["rfx"])
	d.key("enter")
	if d.m.docPage() == nil {
		t.Fatalf("no document page:\n%s", d.plainFrame())
	}
	if d.keyFor("act:paths") == "" {
		t.Fatalf("no Next moves key:\n%s", d.plainFrame())
	}
	for id, k := range d.m.renderDoc(d.m.docPage()).Keys {
		if a := d.m.keymap[k.Code]; strings.HasPrefix(a, "act:") {
			t.Errorf("link %s has the key %q of %s", id, k.Code, a)
		}
	}
}
