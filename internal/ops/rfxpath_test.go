package ops

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store/local"
)

var pathNow = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

// pathDeps has Ada (slack preferred, email, phone), Bo on the same team, and a
// stalled Ask that went to Ada by slack.
func pathDeps(t *testing.T) (Deps, string) {
	t.Helper()
	d := newDepsAt(t, pathNow, "m1")
	must(t, OrgTeamAdd, d, OrgAddInput{Name: "Atlas", Org: "acme.example"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example", Team: "team-atlas"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Bo Example", Org: "acme.example", Team: "team-atlas"})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "slack", Value: "@ada", Preferred: true})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "email", Value: "ada@acme.example"})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "phone", Value: "+1 555 0100"})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-bo-example", Method: "email", Value: "bo@acme.example"})
	id := addRFX(t, d, RFXAddInput{Ask: "Open the port", SelfServe: "searched",
		Targets: []TargetInput{{To: "person-ada-example", Medium: "slack"}}, NextNudge: "2026-11-08"})
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: id, To: "person-ada-example", State: model.TargetSent, Date: "2026-11-01"})
	return d, id
}

func moveOf(out RFXPathsOutput, kind, to string, medium model.Medium) *PathMove {
	for i, m := range out.Moves {
		if m.Kind == kind && m.To == to && (medium == "" || m.Medium == medium) {
			return &out.Moves[i]
		}
	}
	return nil
}

func TestRFXPathsListsNextMoves(t *testing.T) {
	d, id := pathDeps(t)
	out := must(t, RFXPaths, d, RFXPathsInput{ID: id})
	if !out.Stalled || !strings.Contains(out.StalledWhy, "2026-11-08") || out.StalledDays != 9 {
		t.Fatalf("stalled = %v %q %d", out.Stalled, out.StalledWhy, out.StalledDays)
	}
	if len(out.Log) != 1 || out.Log[0].Contacts != 1 || out.Log[0].Last != "2026-11-01" {
		t.Fatalf("log = %+v", out.Log)
	}
	if m := moveOf(out, MoveOtherContact, "person-ada-example", "email"); m == nil || m.Value != "ada@acme.example" {
		t.Fatalf("no email move: %+v", out.Moves)
	}
	if moveOf(out, MoveOtherContact, "person-ada-example", "slack") != nil {
		t.Error("slack was tried and must not be offered again")
	}
	if moveOf(out, MoveSameGroup, "person-bo-example", "") == nil {
		t.Errorf("no same-team move: %+v", out.Moves)
	}
	if moveOf(out, MoveMeeting, "person-ada-example", "meeting") == nil {
		t.Error("no meeting move")
	}
	kinds := map[string]bool{}
	for _, m := range out.Moves {
		kinds[m.Kind] = true
	}
	for _, k := range []string{MoveIntroduction, MoveSelfServe, MoveBlocker} {
		if !kinds[k] {
			t.Errorf("no %s move", k)
		}
	}
}

func TestRFXPathsQuietAndAway(t *testing.T) {
	d, id := pathDeps(t)
	must(t, DocEdit, d, DocEditInput{ID: "person-bo-example", Fields: map[string]any{"away_until": "2026-11-20"}})
	must(t, DocEdit, d, DocEditInput{ID: "person-ada-example", Fields: map[string]any{"quiet_days": 14}})
	out := must(t, RFXPaths, d, RFXPathsInput{ID: id})
	if m := moveOf(out, MoveSameGroup, "person-bo-example", ""); m == nil || !strings.Contains(m.Hold, "away until 2026-11-20") {
		t.Fatalf("bo: %+v", m)
	}
	if m := moveOf(out, MoveOtherContact, "person-ada-example", "email"); m == nil || !strings.Contains(m.Hold, "free 2026-11-15") {
		t.Fatalf("ada: %+v", m)
	}
	// A note in the body is not read.
	must(t, DocEdit, d, DocEditInput{ID: "person-bo-example", Fields: map[string]any{"away_until": nil}, Body: ptr("Away until Dec 1.")})
	out = must(t, RFXPaths, d, RFXPathsInput{ID: id})
	if m := moveOf(out, MoveSameGroup, "person-bo-example", ""); m == nil || m.Hold != "" {
		t.Fatalf("free text held bo back: %+v", m)
	}
}

func TestRFXPathAdd(t *testing.T) {
	d, id := pathDeps(t)
	out := must(t, RFXPathAdd, d, RFXPathAddInput{ID: id, To: "person-ada-example", Medium: "email", Reason: model.FrictionNoResponse,
		Why: "No answer on slack in 9 days.", Nudge: "2026-11-14"})
	if len(out.Targets) != 2 {
		t.Fatalf("targets = %+v", out.Targets)
	}
	n := out.Targets[1]
	if n.Reason != model.FrictionNoResponse || n.Why == "" || n.Added != "2026-11-10" || n.State != model.TargetDrafted || n.Value != "ada@acme.example" {
		t.Fatalf("target = %+v", n)
	}
	doc, _ := d.Store.Get(ctx, id)
	if !strings.Contains(doc.Body, "## Attempts") || !strings.Contains(doc.Body, "Next move: person-ada-example by email: no_response: No answer on slack") {
		t.Fatalf("body = %q", doc.Body)
	}
	if doc.Meta.(*model.RFX).NextNudge != "2026-11-14" {
		t.Errorf("nudge = %s", doc.Meta.(*model.RFX).NextNudge)
	}
	mustLintClean(t, d)
	for name, in := range map[string]RFXPathAddInput{
		"no reason":  {ID: id, To: "person-bo-example", Medium: "email", Why: "x"},
		"bad reason": {ID: id, To: "person-bo-example", Medium: "email", Reason: "blame", Why: "x"},
		"no why":     {ID: id, To: "person-bo-example", Medium: "email", Reason: model.FrictionWrongOwner},
		"bad about":  {ID: id, To: "person-bo-example", Medium: "email", Reason: model.FrictionWrongOwner, Why: "x", About: "person-ada-example"},
		"repeat":     {ID: id, To: "person-ada-example", Medium: "email", Reason: model.FrictionWrongOwner, Why: "x"},
	} {
		if _, err := RFXPathAdd.Run(ctx, d, in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestRFXPathAddHoldsBackUnlessAPersonOverrides(t *testing.T) {
	d, id := pathDeps(t)
	must(t, DocEdit, d, DocEditInput{ID: "person-bo-example", Fields: map[string]any{"away_until": "2026-11-20"}})
	in := RFXPathAddInput{ID: id, To: "person-bo-example", Medium: "email", Reason: model.FrictionWrongOwner, Why: "Ada does not own it."}
	if _, err := RFXPathAdd.Run(ctx, d, in); err == nil || !strings.Contains(err.Error(), "away until 2026-11-20") {
		t.Fatalf("err = %v", err)
	}
	agent := Deps{Store: d.Store.(*local.Store).As("claude", "api")}
	in.Override = true
	if _, err := RFXPathAdd.Run(ctx, agent, in); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent override err = %v", err)
	}
	out := must(t, RFXPathAdd, d, in)
	if out.Held == "" || len(out.Targets) != 2 {
		t.Fatalf("out = %+v", out)
	}
}

func TestNoReplyRecordsAReason(t *testing.T) {
	d, id := pathDeps(t)
	out := must(t, RFXTargetState, d, RFXTargetStateInput{ID: id, To: "person-ada-example", State: model.TargetNoReply})
	if out.Targets[0].Reason != model.FrictionNoResponse {
		t.Fatalf("reason = %q", out.Targets[0].Reason)
	}
	out = must(t, RFXTargetState, d, RFXTargetStateInput{ID: id, To: "person-ada-example", State: model.TargetNoReply, Reason: model.FrictionCapacity, Why: "On leave."})
	if out.Targets[0].Reason != model.FrictionCapacity || out.Targets[0].Why != "On leave." {
		t.Fatalf("target = %+v", out.Targets[0])
	}
	if _, err := RFXTargetState.Run(ctx, d, RFXTargetStateInput{ID: id, To: "person-ada-example", State: model.TargetNoReply, Reason: "who"}); err == nil {
		t.Error("bad reason accepted")
	}
	mustLintClean(t, d)
}
