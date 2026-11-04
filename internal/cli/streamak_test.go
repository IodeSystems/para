package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// streamAKParityCases are the parity rows of stream AK: area kinds. Ids come
// from seedStreamAKParity.
func streamAKParityCases() map[string]parityCase {
	return map[string]parityCase{
		"area_review":     {`{"id":"area-ak-a","note":"Fine."}`, []string{"area", "review", "area-ak-b", "--note", "Fine."}, false},
		"rules_in_effect": {`{"id":"area-ak-a"}`, []string{"rules-in-effect", "area-ak-a"}, true},
	}
}

func seedStreamAKParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	for _, id := range []string{"area-ak-a", "area-ak-b"} {
		d := model.Document{ID: id, Folder: model.FolderAreas, Title: "AK " + id,
			Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive, Cadence: "weekly"}}
		if _, err := deps.Store.Create(context.Background(), d); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

// TestAreaKindsCLI: a concept area, a goal homed in a project area that draws
// on it, a refining convention, and area review through the CLI.
func TestAreaKindsCLI(t *testing.T) {
	e := newEnv(t)
	fail := func(args ...string) string {
		t.Helper()
		_, stderr, err := e.run(args...)
		if err == nil {
			t.Fatalf("para %v: want an error", args)
		}
		return err.Error() + stderr
	}
	e.init("acme")
	var home, concept ops.AreaAddOutput
	e.json(&concept, "area", "add", "Legal writing", "--kind", "concept")
	e.json(&home, "area", "add", "Acme LLC", "--cadence", "quarterly", "--concepts", concept.ID)
	if out := fail("area", "add", "Bad", "--concepts", home.ID); !strings.Contains(out, "want kind concept") {
		t.Fatalf("project area as concept: %s", out)
	}
	var goal ops.ProjectAddOutput
	e.json(&goal, "project", "add", "File the annual report", "--done-when", "Filed.", "--area", home.ID)
	if out := fail("project", "edit", goal.ID, "--area", concept.ID); !strings.Contains(out, "want kind project") {
		t.Fatalf("concept as home: %s", out)
	}
	e.ok("project", "edit", goal.ID, "--concepts", concept.ID)
	var general, specific ops.ConventionAddOutput
	e.json(&general, "convention", "add", "Plain words", "--status", "adopted", "--applies-in", "[["+concept.ID+"]]")
	e.json(&specific, "convention", "add", "Plain words, state filings", "--status", "adopted", "--applies-in", home.ID, "--refines", general.ID)
	var r ops.RulesInEffectOutput
	e.json(&r, "rules-in-effect", goal.ID)
	if len(r.Rules) != 1 || r.Rules[0].ID != specific.ID || r.Rules[0].Via != home.ID || r.Rules[0].Refines != general.ID {
		t.Fatalf("rules = %+v", r)
	}
	var ps ops.ProjectShowOutput
	e.json(&ps, "project", "show", goal.ID)
	if len(ps.Rules) != 1 || ps.Rules[0].ID != specific.ID || len(ps.Project.Concepts) != 1 {
		t.Fatalf("project show = %+v", ps)
	}
	var as ops.AreaShowOutput
	e.json(&as, "area", "show", concept.ID)
	if as.Area.Kind != model.AreaKindConcept || len(as.DrawnOnBy) != 2 {
		t.Fatalf("concept show = %+v", as)
	}
	var rv ops.AreaReviewOutput
	e.json(&rv, "area", "review", home.ID, "--note", "Report is on track.")
	if rv.LastReviewed == "" || rv.NextReview == "" {
		t.Fatalf("review = %+v", rv)
	}
	if out := e.ok("area", "show", home.ID); !strings.Contains(out, "review\tquarterly\tlast "+string(rv.LastReviewed)) {
		t.Fatalf("area show:\n%s", out)
	}
	as = ops.AreaShowOutput{}
	e.json(&as, "area", "show", home.ID)
	if !strings.Contains(as.Body, "## Reviews\n\n- "+string(rv.LastReviewed)+": Report is on track.") {
		t.Fatalf("area body:\n%s", as.Body)
	}
}
