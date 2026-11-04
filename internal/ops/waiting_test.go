package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestReadsLikeWaiting(t *testing.T) {
	for title, want := range map[string]bool{
		"Wait for legal sign-off":             true,
		"Waiting on Ada for the budget":       true,
		"Awaiting the vendor quote":           true,
		"Pending reply from finance":          true,
		"Follow up with Bo on their response": true,
		"Hear back from security":             true,
		"Write the waiting-room page":         false,
		"Ship the parser":                     false,
		"Follow up with Bo about lunch":       false,
	} {
		if got := readsLikeWaiting(title); got != want {
			t.Errorf("%q: %v, want %v", title, got, want)
		}
	}
}

func TestTaskToAsks(t *testing.T) {
	d, proj := laterVault(t)
	ada := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"}).ID
	bo := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Bo Example", Org: "acme.example"}).ID
	added := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Waiting on Ada and Bo for the budget", Due: "2026-11-09"})
	if !slices.ContainsFunc(added.Warnings, func(w string) bool { return strings.Contains(w, "task_to_asks") }) {
		t.Fatalf("task_add warnings = %v", added.Warnings)
	}
	h := must(t, HealthOp, d, HealthInput{})
	found := false
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			found = found || (f.Kind == HealthTaskIsWaiting && f.ID == added.ID)
		}
	}
	if !found {
		t.Fatalf("health has no %s for %s: %+v", HealthTaskIsWaiting, added.ID, h)
	}
	dep := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Spend the budget", DependsOn: []string{added.ID}}).ID
	if _, err := TaskToAsks.Run(ctx, d, TaskToAsksInput{ID: added.ID}); err == nil {
		t.Fatal("no people was accepted")
	}
	out := must(t, TaskToAsks, d, TaskToAsksInput{ID: added.ID, People: []string{ada, bo}})
	if len(out.Asks) != 2 || !slices.Contains(out.Moved, dep) {
		t.Fatalf("to asks = %+v", out)
	}
	for i, a := range out.Asks {
		r := must(t, RFXShow, d, RFXShowInput{ID: a})
		m := r.Doc.Meta.(*model.RFX)
		if m.Direction != model.RFXOut || len(m.Targets) != 1 || m.Targets[0].To.Target() != []string{ada, bo}[i] || m.NextNudge != "2026-11-09" {
			t.Fatalf("ask %s = %+v", a, m)
		}
	}
	dv, _ := taskOf(t, d, dep)
	if !slices.Equal(dv.DependsOn, out.Asks) {
		t.Fatalf("dependent waits on %v, want %v", dv.DependsOn, out.Asks)
	}
	tv, body := taskOf(t, d, added.ID)
	if tv.Status != model.TaskDropped || tv.Outcome != model.OutcomeSuperseded || !strings.Contains(body, out.Asks[1]) {
		t.Fatalf("task = %+v\n%s", tv, body)
	}
}
