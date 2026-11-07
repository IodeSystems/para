package ops

import (
	"slices"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// context shows both directions: what an item waits on (hard, optional, text)
// and what waits on it.
func TestContextWaitsOnAndBlocks(t *testing.T) {
	d := dependsVault(t)
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Platform"}).ID
	task := func(title string) string {
		return must(t, TaskAdd, d, TaskAddInput{Area: area, Title: title, DoneWhen: "Done."}).ID
	}
	a, b, c := task("Ship it"), task("Tidy the config"), task("Get the cert")
	setDep(t, d, a, b, true)
	setDep(t, d, a, c, false)
	must(t, DependsSet, d, DependsSetInput{ID: a, Reason: "vendor reply"})
	got := itemContext(t, d, a)
	want := []DepRow{
		{ID: c, Title: "Get the cert", Type: model.TypeTask, State: "todo", Open: true, Via: DepHard},
		{ID: b, Title: "Tidy the config", Type: model.TypeTask, State: "todo", Open: true, Optional: true, Via: DepOptional},
		{Title: "vendor reply", Reason: "vendor reply", State: "blocked", Open: true, Via: DepReason},
	}
	if !slices.Equal(got.WaitsOn, want) || len(got.Blocks) != 0 {
		t.Fatalf("a: waits_on %+v blocks %+v", got.WaitsOn, got.Blocks)
	}
	if bl := itemContext(t, d, b).Blocks; len(bl) != 1 || bl[0].ID != a || !bl[0].Optional || bl[0].State != "blocked" {
		t.Fatalf("b blocks = %+v", bl)
	}
	if bl := itemContext(t, d, c).Blocks; len(bl) != 1 || bl[0].ID != a || bl[0].Optional {
		t.Fatalf("c blocks = %+v", bl)
	}
	// A closed waiter blocks nothing.
	must(t, Close, d, CloseInput{ID: a, Reason: "Shipped."})
	if bl := itemContext(t, d, c).Blocks; len(bl) != 0 {
		t.Fatalf("c blocks after close = %+v", bl)
	}
}
