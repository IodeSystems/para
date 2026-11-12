package ops

import (
	"strings"
	"testing"
)

func activeFindings(t *testing.T, d Deps) []HealthFinding {
	t.Helper()
	out, err := Health(t.Context(), d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var fs []HealthFinding
	for _, g := range out.Groups {
		for _, f := range g.Findings {
			if f.Kind == HealthTooManyActive {
				fs = append(fs, f)
			}
		}
	}
	return fs
}

func TestActiveGoalLimit(t *testing.T) {
	d, first := laterVault(t) // one active goal
	ctx := t.Context()
	today := d.Store.Now().Format(dateLayout)

	// Off by default: no finding, no warning.
	b := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Second", Owner: "Jordan Example"})
	if len(b.Warnings) != 0 || len(activeFindings(t, d)) != 0 {
		t.Fatalf("limit off: warnings %v", b.Warnings)
	}

	// A deferred goal is not counted.
	c := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Third", Owner: "Jordan Example"})
	must(t, Defer, d, DeferInput{IDs: []string{c.ID}})
	if n, err := activeGoalCount(ctx, d.Store, today); err != nil || n != 2 {
		t.Fatalf("count = %d, %v; want 2 (deferred excluded)", n, err)
	}

	if err := d.Store.SetConfig(ctx, activeLimitKey, "2"); err != nil {
		t.Fatal(err)
	}
	if fs := activeFindings(t, d); len(fs) != 0 {
		t.Fatalf("at the limit: %v", fs)
	}

	// Adding a third active goal succeeds and warns.
	e := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Fourth", Owner: "Jordan Example"})
	if e.ID == "" || len(e.Warnings) != 1 || !strings.Contains(e.Warnings[0], "3") || !strings.Contains(e.Warnings[0], "limit of 2") {
		t.Fatalf("add over the limit: %+v", e)
	}
	fs := activeFindings(t, d)
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "3 active goals") || !strings.Contains(fs[0].Message, "limit of 2") {
		t.Fatalf("finding: %+v", fs)
	}
	for _, id := range []string{first, b.ID, e.ID} {
		if !strings.Contains(fs[0].Message, id) {
			t.Fatalf("candidate %s missing: %s", id, fs[0].Message)
		}
	}
	if strings.Contains(fs[0].Message, c.ID) {
		t.Fatalf("deferred goal listed: %s", fs[0].Message)
	}

	// Limit 0 turns it off again.
	if err := d.Store.SetConfig(ctx, activeLimitKey, "0"); err != nil {
		t.Fatal(err)
	}
	if fs := activeFindings(t, d); len(fs) != 0 {
		t.Fatalf("limit 0: %v", fs)
	}
}
