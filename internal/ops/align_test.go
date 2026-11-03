package ops

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

func alignIDs(out AlignScopeOutput) []string {
	ids := []string{}
	for _, it := range out.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestAlignScopeListsOpenItemsOfOneScope(t *testing.T) {
	f := newAgentFix(t)
	area := must(t, AreaAdd, f.owner, AreaAddInput{Title: "Platform"})
	goal := must(t, ProjectAdd, f.owner, ProjectAddInput{Title: "Ship the portal", Owner: "Jordan Example", DoneWhen: "Live.", Due: "2026-11-20", Area: area.ID})
	other := must(t, ProjectAdd, f.owner, ProjectAddInput{Title: "Unrelated goal", Owner: "Jordan Example", DoneWhen: "Done."})
	must(t, ProjectLinkAdd, f.owner, LinkAddInput{ID: goal.ID, Label: "Repo", Kind: model.PointerRepo, Target: "https://github.example/acme/portal"})
	task := must(t, TaskAdd, f.owner, TaskAddInput{Project: goal.ID, Title: "Cut the release", DoneWhen: "Tagged.", Due: "2026-11-10"})
	elsewhere := must(t, TaskAdd, f.owner, TaskAddInput{Project: other.ID, Title: "Elsewhere", DoneWhen: "Done."})
	ask := must(t, RFXAdd, f.owner, RFXAddInput{Direction: model.RFXOut, Ask: "Is the cert renewed", Project: goal.ID, Route: "domain-x", SelfServe: "wiki", NextNudge: "2026-11-05"})
	q := must(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Which region", Task: task.ID})

	got := must(t, AlignScope, f.agent, AlignScopeInput{Scope: goal.ID})
	if got.Kind != "goal" || got.Title != "Ship the portal" || got.DoneWhen != "Live." || len(got.Links) != 1 || got.Links[0].Target != "https://github.example/acme/portal" {
		t.Fatalf("header = %+v", got)
	}
	ids := alignIDs(got)
	for _, want := range []string{goal.ID, task.ID, ask.ID, q.ID} {
		if !slices.Contains(ids, want) {
			t.Errorf("goal scope lacks %s: %v", want, ids)
		}
	}
	for _, not := range []string{other.ID, elsewhere.ID} {
		if slices.Contains(ids, not) {
			t.Errorf("goal scope holds %s", not)
		}
	}
	// Dated items come before the undated ones, as in the value order.
	if slices.Index(ids, task.ID) > slices.Index(ids, q.ID) {
		t.Errorf("not in value order: %v", ids)
	}
	for _, it := range got.Items {
		if it.ID == ask.ID && (it.Kind != KindAsk || it.Goal != goal.ID || it.State == "") {
			t.Errorf("ask = %+v", it)
		}
		if it.ID == task.ID && (it.Due != "2026-11-10" || it.DoneWhen != "Tagged." || it.State != "todo") {
			t.Errorf("task = %+v", it)
		}
	}

	// The area holds its goal and what is under it.
	byArea := must(t, AlignScope, f.agent, AlignScopeInput{Scope: area.ID})
	if byArea.Kind != "area" || !slices.Contains(alignIDs(byArea), task.ID) || slices.Contains(alignIDs(byArea), other.ID) {
		t.Errorf("area scope = %v", alignIDs(byArea))
	}
	all := must(t, AlignScope, f.agent, AlignScopeInput{Scope: AlignVault})
	if all.Kind != "vault" || !slices.Contains(alignIDs(all), other.ID) || !slices.Contains(alignIDs(all), elsewhere.ID) {
		t.Errorf("vault scope = %v", alignIDs(all))
	}
	// It only reads: no receipt, no write.
	if _, err := AlignScope.Run(t.Context(), f.agent, AlignScopeInput{Scope: task.ID}); err == nil || !strings.Contains(err.Error(), "not") && !strings.Contains(err.Error(), "goal") {
		t.Errorf("a task scope: %v", err)
	}
	if _, err := AlignScope.Run(t.Context(), f.agent, AlignScopeInput{}); err == nil {
		t.Error("an empty scope was accepted")
	}
}

func TestAlignRecordLeavesAReceiptEvenWhenNothingChanged(t *testing.T) {
	f := newAgentFix(t)
	for name, in := range map[string]AlignRecordInput{
		"no scope":      {Checked: 1, Matched: 1},
		"unknown scope": {Scope: "project-gone", Checked: 1, Matched: 1},
		"a note scope":  {Scope: f.source, Checked: 1, Matched: 1},
		"negative":      {Scope: f.proj, Checked: 1, Matched: -1},
		"more than all": {Scope: f.proj, Checked: 1, Matched: 1, Differ: 1},
		"nothing":       {Scope: f.proj},
	} {
		if _, err := AlignRecord.Run(t.Context(), f.agent, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if recs, _ := alignedDates(t.Context(), f.owner.Store); len(recs) != 0 {
		t.Fatalf("a refused call left a record: %v", recs)
	}

	// A clean run is a receipt.
	out := must(t, AlignRecord, f.agent, AlignRecordInput{Scope: f.proj, Checked: 5, Matched: 5})
	if !strings.Contains(out.Summary, "no change") || out.Date != "2026-11-01" {
		t.Errorf("clean run = %+v", out)
	}
	// A run with differences and an unreachable source names it.
	out2 := must(t, AlignRecord, f.agent, AlignRecordInput{Scope: AlignVault, Checked: 7, Matched: 5, Differ: 2, Unreachable: []string{"jira", " jira ", "slack"}})
	if !strings.Contains(out2.Summary, "2 unreachable (jira, slack)") {
		t.Errorf("summary = %q", out2.Summary)
	}
	rl := must(t, ReceiptList, f.owner, ReceiptListInput{Doc: f.proj})
	if len(rl.Items) != 1 || rl.Items[0].Op != "align_record" || rl.Items[0].Align == nil || rl.Items[0].Align.Checked != 5 || !rl.Items[0].SelfReported {
		t.Fatalf("receipts = %+v", rl.Items)
	}
	vl := must(t, ReceiptList, f.owner, ReceiptListInput{Doc: AlignVault})
	if len(vl.Items) != 1 || !slices.Equal(vl.Items[0].Align.Unreachable, []string{"jira", "slack"}) {
		t.Fatalf("vault receipts = %+v", vl.Items)
	}
	// It shows on the activity view.
	act := must(t, AgentActivity, f.owner, AgentActivityInput{})
	n := 0
	for _, r := range act.Items {
		if r.Op == "align_record" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("activity lists %d align records, want 2", n)
	}
	// The goal page reads the date.
	show := must(t, ProjectShow, f.owner, ProjectShowInput{ID: f.proj})
	if show.LastAligned != "2026-11-01" {
		t.Errorf("last aligned = %q", show.LastAligned)
	}
	// A receipt of this kind does not revert anything.
	if _, err := ReceiptUndo.Run(t.Context(), f.owner, ReceiptUndoInput{ID: rl.Items[0].ID}); err == nil {
		t.Error("an align receipt was reverted")
	}
}

func TestAlignedDateComesFromTheGoalItsAreaOrTheVault(t *testing.T) {
	f := newAgentFix(t)
	area := must(t, AreaAdd, f.owner, AreaAddInput{Title: "Platform"})
	inArea := must(t, ProjectAdd, f.owner, ProjectAddInput{Title: "In the area", Owner: "Jordan Example", DoneWhen: "Done.", Area: area.ID})
	if got, _ := LastAligned(t.Context(), f.owner.Store, inArea.ID); got != "" {
		t.Fatalf("before any run: %q", got)
	}
	must(t, AlignRecord, f.agent, AlignRecordInput{Scope: area.ID, Checked: 1, Matched: 1})
	if got, _ := LastAligned(t.Context(), f.owner.Store, inArea.ID); got != "2026-11-01" {
		t.Errorf("by area: %q", got)
	}
	if got, _ := LastAligned(t.Context(), f.owner.Store, f.proj); got != "" {
		t.Errorf("the other goal: %q", got)
	}
}

func TestHealthNotAlignedIsOffUnlessTheVaultSetsDays(t *testing.T) {
	rv := newReqVault(t)
	owner := rv.as("user", "cli", "install-a")
	goal := must(t, ProjectAdd, owner, ProjectAddInput{Title: "Portal", Owner: "Jordan Example", DoneWhen: "Done.", Due: "2026-12-01"})
	must(t, TaskAdd, owner, TaskAddInput{Project: goal.ID, Title: "Work", DoneWhen: "Done."})

	count := func(d Deps) int {
		out, err := Health(t.Context(), d.Store)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, g := range out.Groups {
			for _, fd := range g.Findings {
				if fd.Kind == HealthNotAligned {
					n++
				}
			}
		}
		return n
	}
	if n := count(owner); n != 0 {
		t.Fatalf("not_aligned without align.days: %d", n)
	}
	props := filepath.Join(rv.root, ".para", "vault.properties")
	cur, err := os.ReadFile(props)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(props, append(cur, []byte("align.days=7\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, agent := rv.as("user", "cli", "install-a"), rv.as("claude", "mcp", "install-a")
	if n := count(owner); n != 1 {
		t.Fatalf("never aligned: %d findings, want 1", n)
	}
	must(t, AlignRecord, agent, AlignRecordInput{Scope: goal.ID, Checked: 2, Matched: 2})
	if n := count(owner); n != 0 {
		t.Fatalf("just aligned: %d", n)
	}
	rv.advance(6 * 24 * time.Hour)
	if n := count(owner); n != 0 {
		t.Fatalf("6 days on: %d", n)
	}
	rv.advance(2 * 24 * time.Hour)
	if n := count(owner); n != 1 {
		t.Fatalf("8 days on: %d, want 1", n)
	}
	// A vault-wide run aligns every goal.
	must(t, AlignRecord, agent, AlignRecordInput{Scope: AlignVault, Checked: 2, Matched: 2})
	if n := count(owner); n != 0 {
		t.Fatalf("after a vault run: %d", n)
	}
}
