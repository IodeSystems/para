package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

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

// addPeople gives the fixture's people contacts: Ada by email, Bo by slack.
func addPeople(t *testing.T, d Deps) {
	t.Helper()
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: model.MediumEmail, Value: "ada@acme.example", Preferred: true})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-bo-example", Method: model.MediumSlack, Value: "@bo", Preferred: true})
}

func noticeOf(t *testing.T, d Deps, id string) (model.Document, *model.IO) {
	t.Helper()
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return doc, doc.Meta.(*model.IO)
}

// Push out writes one draft per medium to the interested parties, saying what
// changed and why; the receipt undoes the change and takes the drafts back.
func TestObligationChangePushNotifiesPerChannel(t *testing.T) {
	d, proj, person := obFixture(t)
	addPeople(t, d)
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the plan", OwedTo: []string{person}, Interested: []string{"person-bo-example"}, Due: "2026-11-11", Project: proj})
	urgent := must(t, TaskAdd, d, TaskAddInput{Title: "Fix the outage review", Project: proj, DoneWhen: "Done."})
	if _, err := ObligationChange.Run(ctx, d, ObligationChangeInput{ID: ob.ID, Action: ChangePush, Due: "2026-11-13"}); err == nil || !strings.Contains(err.Error(), "give reason") {
		t.Fatalf("no reason: %v", err)
	}
	out := must(t, ObligationChange, d, ObligationChangeInput{ID: ob.ID, Action: ChangePush, Due: "2026-11-13", Reason: "The numbers come Thursday", Because: urgent.ID})
	if len(out.Notices) != 2 || out.Receipt == "" || out.Due != "2026-11-13" || len(out.Told) != 2 {
		t.Fatalf("push = %+v", out)
	}
	media := map[model.Medium][]string{}
	for _, id := range out.Notices {
		doc, m := noticeOf(t, d, id)
		media[m.Medium] = m.To
		if m.Direction != model.IOOut || m.State != model.IODraft || m.Expects != model.ExpectsNone ||
			!strings.Contains(doc.Body, "from Nov 11 to Nov 13") || !strings.Contains(doc.Body, "The numbers come Thursday") || !strings.Contains(doc.Body, "Fix the outage review") {
			t.Fatalf("notice %s = %+v\n%s", id, m, doc.Body)
		}
	}
	if !slices.Equal(media[model.MediumEmail], []string{"[[person-ada-example]]"}) || !slices.Equal(media[model.MediumSlack], []string{"[[person-bo-example]]"}) {
		t.Fatalf("notices by medium = %v", media)
	}
	doc, o := getOb(t, d, ob.ID)
	if o.Due != "2026-11-13" || len(o.Notices) != 2 || !strings.Contains(doc.Body, "## Changes") || !strings.Contains(doc.Body, "pushed from Nov 11 to Nov 13") {
		t.Fatalf("after push: %+v\n%s", o, doc.Body)
	}
	// A second change threads on the last notice.
	again := must(t, ObligationChange, d, ObligationChangeInput{ID: ob.ID, Action: ChangePush, Due: "2026-11-16", Reason: "Still waiting"})
	if _, m := noticeOf(t, d, again.Notices[0]); m.Thread != o.Notices[1] {
		t.Fatalf("thread = %q, want %q", m.Thread, o.Notices[1])
	}
	must(t, ReceiptRevert, d, ReceiptIDInput{ID: again.Receipt})
	must(t, ReceiptRevert, d, ReceiptIDInput{ID: out.Receipt})
	if _, o := getOb(t, d, ob.ID); o.Due != "2026-11-11" || len(o.Notices) != 0 {
		t.Fatalf("after undo: %+v", o)
	}
	for _, id := range out.Notices {
		if doc, _ := noticeOf(t, d, id); doc.Folder != model.FolderArchive {
			t.Fatalf("undo left the draft %s in %s", id, doc.Folder)
		}
	}
	// notify false writes no notice.
	quiet := must(t, ObligationChange, d, ObligationChangeInput{ID: ob.ID, Action: ChangePush, Due: "2026-11-12", Reason: "Said so in person", Notify: new(bool)})
	if len(quiet.Notices) != 0 {
		t.Fatalf("notify false = %+v", quiet)
	}
	if doc, _ := getOb(t, d, ob.ID); !strings.Contains(doc.Body, "no notice") {
		t.Fatalf("body = %s", doc.Body)
	}
}

// Redirect hands an obligation to someone else, or reroutes it and closes
// it; cancel closes it declined; a meeting is cancelled as meeting_cancel
// does, with a calendar draft for its event; a series occurrence moves alone.
func TestObligationChangeRedirectCancelSeries(t *testing.T) {
	d, proj, person := obFixture(t)
	addPeople(t, d)
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the plan", OwedTo: []string{person}, Due: "2026-11-11", Project: proj})
	r := must(t, ObligationChange, d, ObligationChangeInput{ID: ob.ID, Action: ChangeRedirect, To: "person-bo-example", Reason: "Bo owns the plan now"})
	if !slices.Equal(r.OwedBy, []string{"person-bo-example"}) || r.State != model.StateOpen || len(r.Notices) != 2 {
		t.Fatalf("redirect = %+v", r)
	}
	away := must(t, ObligationAdd, d, ObligationAddInput{Title: "File the expense", OwedTo: []string{person}, Due: "2026-11-11", Project: proj})
	rr := must(t, ObligationChange, d, ObligationChangeInput{ID: away.ID, Action: ChangeRedirect, To: "https://tracker.acme.example/EXP-4", Reason: "The tracker holds it", Notify: new(bool)})
	if _, o := getOb(t, d, away.ID); rr.State != model.StateClosed || o.Outcome != model.OutcomeRerouted {
		t.Fatalf("reroute = %+v / %+v", rr, o)
	}
	c := must(t, ObligationAdd, d, ObligationAddInput{Title: "Write the post", OwedTo: []string{person}, Due: "2026-11-11", Project: proj})
	cc := must(t, ObligationChange, d, ObligationChangeInput{ID: c.ID, Action: ChangeCancel, Reason: "The launch moved a quarter"})
	if _, o := getOb(t, d, c.ID); o.State != model.StateClosed || o.Outcome != model.OutcomeDeclined || len(cc.Notices) != 1 {
		t.Fatalf("cancel = %+v / %+v", cc, o)
	}
	m := must(t, ObligationAdd, d, ObligationAddInput{Title: "Plan review", With: []string{person}, Start: "2026-11-10T14:00:00Z", End: "2026-11-10T15:00:00Z", CalendarUID: "uid-plan-review@cal.example"})
	mc := must(t, ObligationChange, d, ObligationChangeInput{ID: m.ID, Action: ChangeCancel, Reason: "The plan is not ready"})
	if mc.Calendar == "" {
		t.Fatalf("meeting cancel = %+v", mc)
	}
	if doc, cal := noticeOf(t, d, mc.Calendar); cal.Medium != model.MediumMeeting || !strings.Contains(doc.Body, "uid-plan-review@cal.example") {
		t.Fatalf("calendar draft = %+v\n%s", cal, doc.Body)
	}
	if _, o := getOb(t, d, m.ID); o.State != model.StateClosed || len(o.Notices) != 2 {
		t.Fatalf("meeting after cancel = %+v", o)
	}
	s := must(t, ObligationAdd, d, ObligationAddInput{Title: "Weekly 1:1", With: []string{person}, Start: "2026-11-06T09:00:00Z", End: "2026-11-06T09:30:00Z", Recur: "weekly"})
	sp := must(t, ObligationChange, d, ObligationChangeInput{ID: s.ID, Action: ChangePush, Start: "2026-11-06T15:00:00Z", End: "2026-11-06T15:30:00Z", Reason: "A clash", Notify: new(bool)})
	if sp.Next == "" {
		t.Fatalf("series push = %+v", sp)
	}
	if _, next := getOb(t, d, sp.Next); next.Scheduled != "2026-11-13T09:00:00Z" {
		t.Fatalf("next occurrence starts %q, want the old slot a week on", next.Scheduled)
	}
	if _, cur := getOb(t, d, s.ID); cur.Scheduled != "2026-11-06T15:00:00Z" {
		t.Fatalf("pushed occurrence = %q", cur.Scheduled)
	}
}

// The promise score: kept when closed done by its date; renegotiated when a
// notice was approved before it; broken when its date passed with none, and
// health then says to tell them now.
func TestPromiseScoreAndHealth(t *testing.T) {
	d, proj, person := obFixture(t)
	addPeople(t, d)
	kept := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the plan", OwedTo: []string{person}, Due: "2026-11-11", Project: proj})
	must(t, Close, d, CloseInput{ID: kept.ID, Reason: "Sent."})
	if _, o := getOb(t, d, kept.ID); o.Promise != model.PromiseKept {
		t.Fatalf("kept = %q", o.Promise)
	}
	late := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the audit", OwedTo: []string{person}, Due: "2026-11-03", Project: proj})
	if got := healthOf(t, d, late.ID); !slices.Contains(got, HealthPromiseUntold) {
		t.Fatalf("health of a late promise = %v", got)
	}
	reneg := must(t, ObligationAdd, d, ObligationAddInput{Title: "Write the post", OwedTo: []string{person}, Due: "2026-11-09", Project: proj})
	ch := must(t, ObligationChange, d, ObligationChangeInput{ID: reneg.ID, Action: ChangeCancel, Reason: "The launch moved"})
	if _, o := getOb(t, d, reneg.ID); o.Promise != "" {
		t.Fatalf("before approval = %q", o.Promise)
	}
	must(t, IOApprove, d, IOApproveInput{ID: ch.Notices[0]})
	if err := d.Store.SetConfig(ctx, "request.runner", "a1"); err != nil {
		t.Fatal(err)
	}
	out, err := Tick(ctx, d, TickInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(out.Promises, late.ID+": broken") || !slices.Contains(out.Promises, reneg.ID+": renegotiated") {
		t.Fatalf("tick promises = %v", out.Promises)
	}
	// A notice of the late one, approved, settles the finding; broken stays.
	lc := must(t, ObligationChange, d, ObligationChangeInput{ID: late.ID, Action: ChangePush, Due: "2026-11-12", Reason: "Sorry, it slipped"})
	must(t, IOApprove, d, IOApproveInput{ID: lc.Notices[0]})
	if got := healthOf(t, d, late.ID); slices.Contains(got, HealthPromiseUntold) {
		t.Fatalf("health after the notice = %v", got)
	}
	if _, o := getOb(t, d, late.ID); o.Promise != model.PromiseBroken {
		t.Fatalf("late after push = %q", o.Promise)
	}
	tl := must(t, ObligationTimeline, d, ObligationTimelineInput{})
	// Closed ones count by the day they closed; the late one is dated ahead
	// now, so it is not in the last 30 days.
	if tl.Score.Kept != 1 || tl.Score.Renegotiated != 1 || tl.Score.Broken != 0 {
		t.Fatalf("score = %+v", tl.Score)
	}
}

// The timeline lists each day's fixed points with open prep, and marks one
// at risk when the prep takes more working time than is left (6 hours a
// weekday from 9:00, none at the weekend), or any prep is blocked.
func TestObligationTimelineAtRisk(t *testing.T) {
	d, proj, person := obFixture(t)
	// Thursday noon: 3 hours left today, 6 on Friday.
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the plan", OwedTo: []string{person}, Due: "2026-11-06", Project: proj})
	must(t, TaskAdd, d, TaskAddInput{Title: "Draft", Parent: ob.ID, Effort: model.EffortL, DoneWhen: "Drafted."})
	must(t, TaskAdd, d, TaskAddInput{Title: "Check", Parent: ob.ID, Effort: model.EffortS, DoneWhen: "Checked."})
	// Monday 11:00: Thursday 3 + Friday 6 + Monday 2 = 11 hours.
	m := must(t, ObligationAdd, d, ObligationAddInput{Title: "Plan review", With: []string{person}, Start: "2026-11-09T11:00:00Z", End: "2026-11-09T12:00:00Z"})
	blocked := must(t, TaskAdd, d, TaskAddInput{Title: "Get the room", Parent: m.ID, Effort: model.EffortXS, Project: proj, DoneWhen: "Booked."})
	tl := must(t, ObligationTimeline, d, ObligationTimelineInput{Days: 7})
	point := func(id string) TimelinePoint {
		for _, day := range tl.Days {
			for _, p := range day.Points {
				if p.ID == id {
					return p
				}
			}
		}
		t.Fatalf("%s not on the timeline: %+v", id, tl.Days)
		return TimelinePoint{}
	}
	p := point(ob.ID)
	if p.PrepHours != 9 || p.HoursLeft != 9 || p.AtRisk || len(p.Prep) != 2 {
		t.Fatalf("plan = %+v", p)
	}
	if len(tl.Days) != 7 || tl.Days[2].Hours != 0 || tl.Days[0].Hours != 6 {
		t.Fatalf("days = %+v", tl.Days)
	}
	must(t, TaskAdd, d, TaskAddInput{Title: "Proofread", Parent: ob.ID, Effort: model.EffortXS, DoneWhen: "Read."})
	tl = must(t, ObligationTimeline, d, ObligationTimelineInput{Days: 7})
	if p := point(ob.ID); !p.AtRisk || p.PrepHours != 9.25 || !strings.Contains(p.Risk, "9.25h") {
		t.Fatalf("plan with more prep = %+v", p)
	}
	if p := point(m.ID); p.HoursLeft != 11 || p.AtRisk {
		t.Fatalf("meeting = %+v", p)
	}
	must(t, TaskBlock, d, TaskBlockInput{ID: blocked.ID, Reason: "Facilities have not answered the room request yet"})
	tl = must(t, ObligationTimeline, d, ObligationTimelineInput{Days: 7})
	if p := point(m.ID); !p.AtRisk || !strings.Contains(p.Risk, "blocked") {
		t.Fatalf("meeting with blocked prep = %+v", p)
	}
}

// The pure working-time count.
func TestWorkHoursBetween(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{"2026-11-05T12:00:00Z", "2026-11-05T15:00:00Z", 3},
		{"2026-11-05T08:00:00Z", "2026-11-05T20:00:00Z", 6},
		{"2026-11-06T16:00:00Z", "2026-11-09T10:00:00Z", 1},
		{"2026-11-07T09:00:00Z", "2026-11-08T18:00:00Z", 0},
		{"2026-11-05T12:00:00Z", "2026-11-05T11:00:00Z", 0},
	} {
		if got := workHoursBetween(at(c.a), at(c.b), 6); got != c.want {
			t.Errorf("%s..%s = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
