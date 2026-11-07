package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// The fixture day is Thursday 2026-11-05, 12:00 UTC (obFixture).

func partIDs(ps []ObligationPart) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

// A task's, an ask's, and an obligation's parent may be an open obligation;
// a closed one, or a person, is refused. A child with no project or area
// takes the obligation's.
func TestParentMayBeAnObligation(t *testing.T) {
	d, proj, person := obFixture(t)
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send Ada the plan", OwedTo: []string{person}, Due: "2026-11-11", Project: proj})
	task := must(t, TaskAdd, d, TaskAddInput{Title: "Draft the plan", Parent: ob.ID, DoneWhen: "Draft in the folder."})
	if doc, err := d.Store.Get(ctx, task.ID); err != nil {
		t.Fatal(err)
	} else if tk := doc.Meta.(*model.Task); tk.Parent != model.LinkTo(ob.ID) || tk.Project != model.LinkTo(proj) {
		t.Fatalf("task = %+v", tk)
	}
	ask := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Numbers for the plan", Route: "domain-billing", Parent: ob.ID,
		Targets: []TargetInput{{To: "person-cy-example", Medium: model.MediumEmail}}})
	if doc, err := d.Store.Get(ctx, ask.ID); err != nil {
		t.Fatal(err)
	} else if r := doc.Meta.(*model.RFX); r.Parent != model.LinkTo(ob.ID) {
		t.Fatalf("rfx parent = %q", r.Parent)
	}
	follow := must(t, ObligationAdd, d, ObligationAddInput{Title: "Review the plan with Ada", OwedTo: []string{person}, Due: "2026-11-20", Parent: ob.ID})
	if _, o := getOb(t, d, follow.ID); o.Parent != model.LinkTo(ob.ID) || o.Project != model.LinkTo(proj) {
		t.Fatalf("obligation parent = %+v", o)
	}
	other := must(t, TaskAdd, d, TaskAddInput{Title: "Book the room", Project: proj, DoneWhen: "Booked."})
	if out := must(t, TaskEdit, d, TaskEditInput{ID: other.ID, Parent: ob.ID}); out.Parent != ob.ID {
		t.Fatalf("task_edit parent = %+v", out)
	}
	if out := must(t, TaskEdit, d, TaskEditInput{ID: other.ID, ClearParent: true}); out.Parent != "" {
		t.Fatalf("task_edit clear_parent = %+v", out)
	}
	if _, err := TaskAdd.Run(ctx, d, TaskAddInput{Title: "x", Parent: person, Project: proj, DoneWhen: "x."}); err == nil || !strings.Contains(err.Error(), "want a task or an obligation") {
		t.Fatalf("parent person: %v", err)
	}
	must(t, Close, d, CloseInput{ID: follow.ID, Reason: "Reviewed."})
	if _, err := TaskAdd.Run(ctx, d, TaskAddInput{Title: "x", Parent: follow.ID, DoneWhen: "x."}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("parent closed: %v", err)
	}
	// Prep of an obligation is owed through it.
	c := must(t, ItemContext, d, ItemContextInput{ID: task.ID})
	if !slices.ContainsFunc(c.Reasons, func(r Reason) bool { return r.Kind == ReasonOwed && r.Ref == person }) {
		t.Fatalf("reasons of prep = %+v", c.Reasons)
	}
	if c := must(t, ItemContext, d, ItemContextInput{ID: ask.ID}); !slices.ContainsFunc(c.Reasons, func(r Reason) bool { return r.Kind == ReasonOwed }) {
		t.Fatalf("reasons of an ask under it = %+v", c.Reasons)
	}
}

// The hub of a meeting: its prep (children and what it depends on) before,
// with backward dates, and the action items meeting_hold files after.
func TestObligationPartsBeforeAndCameOut(t *testing.T) {
	d, proj, person := obFixture(t)
	m := must(t, ObligationAdd, d, ObligationAddInput{Title: "Plan review", With: []string{person}, Start: "2026-11-10T14:00:00Z", End: "2026-11-10T15:00:00Z", Project: proj})
	prep := must(t, TaskAdd, d, TaskAddInput{Title: "Write the review notes", Parent: m.ID, Effort: model.EffortL, DoneWhen: "Notes written."})
	small := must(t, TaskAdd, d, TaskAddInput{Title: "Print the agenda", Parent: m.ID, Effort: model.EffortS, DoneWhen: "Printed."})
	dep := must(t, TaskAdd, d, TaskAddInput{Title: "Get the numbers", Project: proj, Effort: model.EffortM, DoneWhen: "Numbers in."})
	doc, o := getOb(t, d, m.ID)
	o.DependsOn = []model.Link{model.LinkTo(dep.ID)}
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	must(t, Close, d, CloseInput{ID: small.ID, Reason: "Printed."})
	parts := must(t, ObligationParts, d, ObligationPartsInput{ID: m.ID})
	if got := partIDs(parts.Before); !slices.Equal(got, []string{dep.ID, prep.ID, small.ID}) {
		t.Fatalf("before = %v", got)
	}
	back := map[string]string{}
	for _, p := range parts.Before {
		back[p.ID] = p.Back
	}
	if back[prep.ID] != "2026-11-08" || back[dep.ID] != "2026-11-09" || back[small.ID] != "" {
		t.Fatalf("backward dates = %v", back)
	}
	if parts.Date != "2026-11-10" || len(parts.Parties) != 1 || len(parts.CameOut) != 0 {
		t.Fatalf("parts = %+v", parts)
	}
	held := must(t, MeetingHold, d, MeetingHoldInput{ID: m.ID, Notes: "Agreed the plan.", Actions: []HoldAction{{Title: "Send the minutes", DoneWhen: "Sent."}}})
	if len(held.Tasks) != 1 {
		t.Fatalf("hold = %+v", held)
	}
	if adoc, err := d.Store.Get(ctx, held.Tasks[0]); err != nil {
		t.Fatal(err)
	} else if adoc.Meta.(*model.Task).Parent != model.LinkTo(m.ID) {
		t.Fatalf("action item parent = %q", adoc.Meta.(*model.Task).Parent)
	}
	parts = must(t, ObligationParts, d, ObligationPartsInput{ID: m.ID})
	if got := partIDs(parts.CameOut); !slices.Equal(got, held.Tasks) {
		t.Fatalf("came out = %v, want %v", got, held.Tasks)
	}
	if slices.Contains(partIDs(parts.Before), held.Tasks[0]) {
		t.Fatalf("an action item is not prep: %v", partIDs(parts.Before))
	}
}

// Value order: a promise to people within order.owed_soon_days ranks in the
// owed tier, and its prep takes the backward date and rises with it a week
// ahead. A booked meeting after today stays not yet startable; its prep is
// startable and rises. Prep of a promise outside the window does not.
func TestOrderOwedTier(t *testing.T) {
	d, proj, person := obFixture(t)
	near := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send Ada the plan", OwedTo: []string{person}, Due: "2026-11-11", Project: proj})
	far := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send Ada the audit", OwedTo: []string{person}, Due: "2026-11-30", Project: proj})
	prep := must(t, TaskAdd, d, TaskAddInput{Title: "Draft the plan", Parent: near.ID, Effort: model.EffortL, DoneWhen: "Drafted."})
	farPrep := must(t, TaskAdd, d, TaskAddInput{Title: "Draft the audit", Parent: far.ID, Effort: model.EffortL, DoneWhen: "Drafted."})
	meet := must(t, ObligationAdd, d, ObligationAddInput{Title: "Plan review", With: []string{person}, Start: "2026-11-09T14:00:00Z", End: "2026-11-09T15:00:00Z"})
	mprep := must(t, TaskAdd, d, TaskAddInput{Title: "Read the plan", Parent: meet.ID, Effort: model.EffortM, Project: proj, DoneWhen: "Read."})
	plain := must(t, TaskAdd, d, TaskAddInput{Title: "Tidy the wiki", Project: proj, DoneWhen: "Tidy."})
	rows := orderIDs(t, d)
	if r, ok := rows[meet.ID]; ok && r.Bucket != BucketNotYet {
		t.Errorf("a booked meeting after today is not yet startable: %+v", r)
	}
	for id, want := range map[string]string{near.ID: GroupOwed, prep.ID: GroupOwed, mprep.ID: GroupOwed} {
		if rows[id].Group != want {
			t.Errorf("%s: group %q, want %q (%+v)", id, rows[id].Group, want, rows[id])
		}
	}
	if r := rows[prep.ID]; r.For != near.ID || r.Back != "2026-11-09" || r.EffDue != "2026-11-09" || !strings.HasPrefix(r.Why, "prep for Send Ada the plan on Nov 11, by Nov 9") {
		t.Errorf("prep row = %+v", r)
	}
	if r := rows[mprep.ID]; r.For != meet.ID || r.Back != "2026-11-08" {
		t.Errorf("meeting prep row = %+v", r)
	}
	for _, id := range []string{far.ID, farPrep.ID, plain.ID} {
		if g := rows[id].Group; g == GroupOwed || g == GroupDueSoon {
			t.Errorf("%s: group %q, want outside the owed tier", id, g)
		}
	}
	// Owed comes right after due soon: before every other actionable row.
	o, err := Order(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, r := range o.Rows {
		pos[r.ID] = i
	}
	if !(pos[mprep.ID] < pos[prep.ID] && pos[prep.ID] < pos[near.ID] && pos[near.ID] < pos[plain.ID]) {
		t.Errorf("order = %v", rowIDs(o.Rows))
	}
	// The window is a vault setting; 0 is off.
	if err := d.Store.SetConfig(ctx, OwedSoonKey, "0"); err != nil {
		t.Fatal(err)
	}
	if g := orderIDs(t, d)[prep.ID].Group; g == GroupOwed {
		t.Errorf("owed tier off: prep group %q", g)
	}
}

// A tie in score goes owed, then goal, then upkeep.
func TestRankValueTieGoesToOwed(t *testing.T) {
	nodes := []ValueNode{
		{ID: "a-upkeep", Kind: KindTask, Created: "2026-11-01", ReasonRank: 2},
		{ID: "b-goal", Kind: KindTask, Created: "2026-11-01", ReasonRank: 1},
		{ID: "c-owed", Kind: KindTask, Created: "2026-11-01", ReasonRank: 0},
		{ID: "d-none", Kind: KindTask, Created: "2026-11-01", ReasonRank: 3},
	}
	o := RankValue(nodes, RankOptions{Today: "2026-11-05", DueSoonDays: 2})
	if got := rowIDs(o.Rows); !slices.Equal(got, []string{"c-owed", "b-goal", "a-upkeep", "d-none"}) {
		t.Fatalf("order = %v", got)
	}
}

// A prep item's backward date is the promise's date minus its effort lead:
// xs and s 0 days, m 1, l 2.
func TestRankValueBackwardDate(t *testing.T) {
	nodes := []ValueNode{
		{ID: "ob", Kind: KindTask, Due: "2026-11-12", Promise: true, PromiseDate: "2026-11-12"},
		{ID: "p-xs", Kind: KindTask, Parent: "ob", Effort: model.EffortXS},
		{ID: "p-m", Kind: KindTask, Parent: "ob", Effort: model.EffortM},
		{ID: "p-l", Kind: KindTask, Effort: model.EffortL},
		{ID: "p-l2", Kind: KindTask, Parent: "p-l", Effort: model.EffortS},
	}
	nodes[0].Deps = []string{"p-l"}
	o := RankValue(nodes, RankOptions{Today: "2026-11-05", DueSoonDays: 2, OwedSoonDays: 7})
	got := map[string]string{}
	for _, r := range o.Rows {
		if r.Group == GroupOwed {
			got[r.ID] = r.Back
		}
	}
	want := map[string]string{"ob": "", "p-xs": "2026-11-12", "p-m": "2026-11-11", "p-l": "2026-11-10", "p-l2": "2026-11-12"}
	for id, w := range want {
		if b, ok := got[id]; !ok || b != w {
			t.Errorf("%s: back %q (in owed %v), want %q", id, b, ok, w)
		}
	}
}
