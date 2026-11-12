package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// An ask to the owner folded into an obligation keeps its place in the value
// order: it is owed, prepared by an agent, waits since it came, and what it
// blocks waits on it.
func TestAskInFoldKeepsItsOrderSignals(t *testing.T) {
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	d := clockDeps(t, &now)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "6"); err != nil {
		t.Fatal(err)
	}
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"})
	goal := must(t, GoalAdd, d, GoalAddInput{Title: "Report", Owner: "Jordan Example", DoneWhen: "Sent."}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Goal: goal, Title: "Ship the report", DoneWhen: "Sent.", Due: "2026-11-09"}).ID
	ask := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXIn, Ask: "Can you send the numbers", Route: "email", SelfServe: "none",
		Targets: []TargetInput{{To: "person-ada-example", Medium: model.MediumEmail, Value: "ada@acme.example"}}, Blocks: []string{task}}).ID
	row := func(rows []ValueRow, id string) ValueRow {
		for _, r := range rows {
			if r.ID == id {
				return r
			}
		}
		t.Fatalf("no row %s in %v", id, rowIDs(rows))
		return ValueRow{}
	}
	before := must(t, OrderOp, d, OrderInput{}).Rows
	if _, err := upgrade.RunForced(ctx, d.Store, false); err != nil {
		t.Fatal(err)
	}
	if doc, _ := d.Store.Get(ctx, ask); doc.Meta.Header().Type != model.TypeObligation {
		t.Fatalf("%s not folded: %s", ask, doc.Meta.Header().Type)
	}
	after := must(t, OrderOp, d, OrderInput{}).Rows
	b, a := row(before, ask), row(after, ask)
	if a.Owed != b.Owed || a.Prep != b.Prep || a.Blocks != b.Blocks || a.WaitingDays != b.WaitingDays || a.Blocks != 1 {
		t.Errorf("ask row before %+v\nafter %+v", b, a)
	}
	if bt, at := row(before, task), row(after, task); at.Bucket != bt.Bucket || at.After != bt.After {
		t.Errorf("blocked task before %+v\nafter %+v", bt, at)
	}
}

func obligationDoc(id, title string, o *model.Obligation) model.Document {
	o.Common = model.Common{Type: model.TypeObligation}
	if o.State == "" {
		o.State = model.StateOpen
	}
	return model.Document{ID: id, Folder: model.FolderObligations, Title: title, Meta: o}
}

// align_scope, grill_context, recheck, and calendar_compare read obligations
// as they read the tasks they were before format 7.
func TestReadersReadObligations(t *testing.T) {
	d := grillDeps(t)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "7"); err != nil {
		t.Fatal(err)
	}
	g := must(t, GoalAdd, d, GoalAddInput{Title: "Launch", Owner: "Jordan Example", DoneWhen: "Live."})
	seedDoc(t, d, obligationDoc("obligation-owed", "Send the numbers", &model.Obligation{Kind: model.KindCommitment, Schedule: model.ScheduleOnce,
		Due: "2026-11-04", Goal: model.LinkTo(g.ID), DoneWhen: "Sent."}))
	seedDoc(t, d, obligationDoc("obligation-vendor", "Vendor sends keys", &model.Obligation{Kind: model.KindCommitment, Schedule: model.ScheduleOnce,
		Due: "2026-11-20", Goal: model.LinkTo(g.ID), OwedBy: []model.Link{model.LinkTo("person-riley")}}))
	seedDoc(t, d, obligationDoc("obligation-review", "Keep the rota", &model.Obligation{Kind: model.KindCommitment, Schedule: model.ScheduleOngoing,
		Cadence: "monthly", LastReviewed: "2026-09-01", Goal: model.LinkTo(g.ID)}))
	seedDoc(t, d, obligationDoc("obligation-fresh", "Keep the backups", &model.Obligation{Kind: model.KindCommitment, Schedule: model.ScheduleOngoing,
		Cadence: "monthly", LastReviewed: "2026-11-01", Goal: model.LinkTo(g.ID)}))

	scope := must(t, AlignScope, d, AlignScopeInput{Scope: g.ID})
	i := slices.IndexFunc(scope.Items, func(it AlignItem) bool { return it.ID == "obligation-owed" })
	if i < 0 || scope.Items[i].Goal != g.ID || scope.Items[i].State == "" || scope.Items[i].DoneWhen != "Sent." {
		t.Errorf("align_scope of the goal: %+v", scope.Items)
	}

	grill := must(t, GrillContext, d, GrillContextInput{ID: g.ID})
	if !slices.ContainsFunc(grill.Probes, func(p GrillProbe) bool {
		return p.Kind == ProbeOutside && p.About == "obligation-vendor" && strings.Contains(p.Known, "person-riley")
	}) {
		t.Errorf("grill_context: no probe for the obligation owed by another: %+v", grill.Probes)
	}

	re := must(t, Recheck, d, RecheckInput{Scope: g.ID, DryRun: true})
	var got []string
	for _, it := range re.Items {
		if it.Kind == RecheckObligation {
			got = append(got, it.ID)
		}
	}
	if !slices.Equal(got, []string{"obligation-review"}) {
		t.Fatalf("recheck obligations = %v, want the one whose review has come", got)
	}
	rec := must(t, RecheckRecord, d, RecheckRecordInput{ID: "obligation-review", Result: model.AssumptionHolds, Source: "the owner in chat, 2026-11-04"})
	if rec.Question != "" {
		t.Errorf("holds made a question: %+v", rec)
	}
	doc, err := d.Store.Get(ctx, "obligation-review")
	if err != nil {
		t.Fatal(err)
	}
	if o := doc.Meta.(*model.Obligation); o.LastReviewed != "2026-11-04" || !strings.Contains(doc.Body, rechecksHeading) {
		t.Errorf("after holds: last_reviewed %s, body %q", o.LastReviewed, doc.Body)
	}
	if n := len(must(t, Recheck, d, RecheckInput{Scope: g.ID, DryRun: true}).Items); n != 0 {
		t.Errorf("after holds: %d items", n)
	}
	if _, err := RecheckRecord.Run(ctx, d, RecheckRecordInput{ID: "obligation-owed", Result: model.AssumptionHolds, Source: "x"}); err == nil {
		t.Error("recheck_record took an obligation that is not ongoing")
	}
}

// calendar_compare matches a meeting stored as an obligation.
func TestCalendarCompareReadsObligations(t *testing.T) {
	d := grillDeps(t)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "7"); err != nil {
		t.Fatal(err)
	}
	m := must(t, ObligationAdd, d, ObligationAddInput{Kind: model.KindMeeting, Title: "Design review", Attendees: []string{"ada@acme.example"},
		Start: "2026-11-05T15:30:00Z", End: "2026-11-05T16:30:00Z", CalendarUID: "review@acme.example"})
	doc, err := d.Store.Get(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Meta.(*model.Obligation); !ok {
		t.Fatalf("stored as %T", doc.Meta)
	}
	ics := "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:review@acme.example\nSUMMARY:Design review\nDTSTART:20261105T153000Z\nDTEND:20261105T163000Z\nEND:VEVENT\nEND:VCALENDAR\n"
	out := must(t, CalendarCompare, d, CalendarCompareInput{ICS: ics, From: "2026-11-04", To: "2026-11-10", Zone: "UTC"})
	if len(out.Matched) != 1 || out.Matched[0].ID != m.ID || out.Matched[0].MatchedBy != "uid" {
		t.Errorf("matched = %+v, para only %+v", out.Matched, out.ParaOnly)
	}
}
