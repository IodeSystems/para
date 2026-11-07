package today

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

// A firing idea pool is a row of Next, so Today counts it and lists it.
func TestTodayCountsFiringPool(t *testing.T) {
	in, d := fixture(t)
	root := filepath.Join(in.Home, "vaults", "acme")
	must(t, ops.AreaAdd, d, ops.AreaAddInput{Title: "Tinkering", ID: "area-tinkering"})
	if err := os.WriteFile(filepath.Join(root, ".para", "pools.yaml"), []byte("pools:\n  - name: reading\n    area: area-tinkering\n    when: {date: 2020-01-01}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	must(t, ops.IdeaAdd, d, ops.IdeaAddInput{Pool: "reading", Title: "Read about compilers"})
	rep, err := Read(t.Context(), in, "user", "cli", day(0))
	if err != nil || rep.Failed() != nil {
		t.Fatalf("today: %v %v", err, rep.Failed())
	}
	vt := rep.Vaults[0]
	if vt.NeedsYou != 1 {
		t.Errorf("needs_you = %d, want 1 (the pool card)", vt.NeedsYou)
	}
	found := false
	for _, it := range vt.Agenda.Items {
		found = found || it.ID == "pool:reading"
	}
	if !found {
		t.Errorf("agenda lacks pool:reading: %+v", vt.Agenda.Items)
	}
}
