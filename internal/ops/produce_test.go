package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// produceVault is a tick vault at the obligation format whose runner is ia.
func produceVault(t *testing.T, now string, props ...string) (*tickVault, Deps) {
	t.Helper()
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	tv, d := journalTick(t, now, props...)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "9"); err != nil {
		t.Fatal(err)
	}
	return tv, d
}

// produceReqs are the produce requests linked to id.
func produceReqs(t *testing.T, d Deps, id string) []model.Document {
	t.Helper()
	reqs, err := loadRequests(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var out []model.Document
	for _, r := range reqs {
		m := r.Meta.(*model.Request)
		if m.Kind == ProduceKind && slices.ContainsFunc(m.Links, func(l model.Link) bool { return l.Target() == id }) {
			out = append(out, r)
		}
	}
	return out
}

// claim claims the next request as the agent, and fails unless it is want.
func claim(t *testing.T, agent Deps, want string) {
	t.Helper()
	got := must(t, RequestNext, agent, RequestNextInput{})
	if got.Request == nil || got.Request.ID != want {
		t.Fatalf("request_next = %+v, want %s", got.Request, want)
	}
}

func agentWrite(why string) WriteMeta {
	return WriteMeta{Why: why, Confidence: model.ConfidenceHigh}
}

// A due occurrence of a series an agent owes gets one produce request and the
// next occurrence; passes after it make no second request, and the next
// occurrence gets its own when it comes due.
func TestProduceDueOccurrenceMakesOneRequest(t *testing.T) {
	tv, d := produceVault(t, "2026-11-06T09:00:00Z", "request.surfaces=cli,tick")
	tpl := must(t, NoteAdd, d, NoteAddInput{Title: "Status template", Body: "Done, next, blocked."}).ID
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Status note", OwedBy: []string{"agent:writer"}, Recur: "weekly", Due: "2026-11-06",
		Produce: &ProduceInput{Kind: model.ProduceReport, Template: tpl}}).ID
	_, o := getOb(t, d, ob)
	if o.Agent() != "agent:writer" || o.Produce == nil || o.Produce.Template != string(model.LinkTo(tpl)) || o.Schedule != model.ScheduleSeries {
		t.Fatalf("stored: %+v %+v", o, o.Produce)
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need(ob); n.Yes || n.Turn != TurnAgent {
		t.Fatalf("before it is produced: %+v", n)
	}

	out := tick(t, d)
	if len(out.Produce) != 1 || len(out.Occurrences) != 1 {
		t.Fatalf("tick: produce %v occurrences %v not_queued %q warnings %v", out.Produce, out.Occurrences, out.NotQueued, out.Warnings)
	}
	reqs := produceReqs(t, d, ob)
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	r := reqs[0]
	if st := r.Meta.(*model.Request).Status; st != model.RequestQueued {
		t.Errorf("status = %s", st)
	}
	for _, want := range []string{"[[" + tpl + "]]", "item_prepared", "kind: artifact", "Prompt: loop, part Produce", "agent:writer", "note_add", "2026-11-06"} {
		if !strings.Contains(r.Body, want) {
			t.Errorf("ask lacks %q:\n%s", want, r.Body)
		}
	}
	next := out.Occurrences[0]
	_, no := getOb(t, d, next)
	if no.Due != "2026-11-13" || no.Previous != model.LinkTo(ob) || no.Produce == nil || no.Agent() != "agent:writer" {
		t.Fatalf("next occurrence: %+v", no)
	}

	// Again, and the next day: no duplicate, no second next occurrence.
	if again := tick(t, d); len(again.Produce) != 0 || len(again.Occurrences) != 1 || again.Occurrences[0] != next {
		t.Fatalf("again: %+v", again)
	}
	tv.at("2026-11-07T09:00:00Z")
	if again := tick(t, d); len(again.Produce) != 0 {
		t.Fatalf("next day: %v", again.Produce)
	}
	if n := len(produceReqs(t, d, ob)); n != 1 {
		t.Fatalf("requests grew to %d", n)
	}

	// The agent writes the artifact and records it. A report is in the vault
	// and undoable: the occurrence closes done at once, with a receipt, and
	// never becomes the owner's turn. The next one is the one tick made.
	agent := tv.open("ia", "mcp")
	claim(t, agent, r.ID)
	wctx := WithWrite(ctx, agentWrite("Weekly status from the template."))
	note := must(t, NoteAdd, agent, NoteAddInput{Title: "Status Nov 6", Body: "Done: one thing."}).ID
	prep, err := ItemPrepared.Run(wctx, agent, ItemPreparedInput{ID: ob, Kind: model.PreparedArtifact, Ref: note})
	if err != nil {
		t.Fatal(err)
	}
	if !prep.Closed || prep.Next != next {
		t.Fatalf("item_prepared: %+v, want closed with next %s", prep, next)
	}
	must(t, RequestDone, agent, RequestDoneInput{ID: r.ID, Resolution: model.ResolutionDone, Result: "Wrote the status.", Links: []string{note}})
	if _, co := getOb(t, d, ob); co.Open() || co.Outcome != model.OutcomeDone {
		t.Fatalf("occurrence: state %s outcome %s", co.State, co.Outcome)
	}
	sn, err = NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need(ob); n.Yes {
		t.Fatalf("a produced report needs the owner: %+v", n)
	}
	if !closeReceipted(t, d, ob) {
		t.Fatal("no close receipt for the produced occurrence")
	}
	show := must(t, TaskShow, d, TaskShowInput{ID: ob})
	if p := show.Produce; p == nil || p.Artifact == nil || p.Artifact.ID != note || p.TemplateName != "Status template" || p.Agent != "agent:writer" {
		t.Fatalf("task_show produce: %+v", show.Produce)
	}
	// The next occurrence shows the last artifact, of the one before it.
	if p := must(t, TaskShow, d, TaskShowInput{ID: next}).Produce; p == nil || p.Ready || p.Artifact == nil || p.Artifact.Occurrence != ob {
		t.Fatalf("next occurrence produce view: %+v", p)
	}
	// The owner can take the close back.
	must(t, Reopen, d, ReopenInput{ID: ob, Reason: "Wrong week."})
	if _, co := getOb(t, d, ob); !co.Open() {
		t.Fatal("reopen left it closed")
	}

	tv.at("2026-11-13T09:00:00Z")
	out = tick(t, d)
	if len(out.Produce) != 1 || len(produceReqs(t, d, next)) != 1 {
		t.Fatalf("next due: %+v", out)
	}
}

// Tick makes produce work only when it can queue it, and says so.
func TestProduceNotQueuedWithoutTickSurface(t *testing.T) {
	_, d := produceVault(t, "2026-11-06T09:00:00Z")
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Report", Produce: &ProduceInput{Kind: model.ProduceReport, Template: "Two lines on what moved."}, Recur: "monthly", Due: "2026-11-06"}).ID
	_, o := getOb(t, d, ob)
	if o.Agent() != model.OwedByAgent || o.Produce.Template != "Two lines on what moved." {
		t.Fatalf("stored: %+v %+v", o.OwedBy, o.Produce)
	}
	out := tick(t, d)
	if len(out.Produce) != 0 || out.NotQueued == "" || !strings.Contains(out.NotQueued, "produce") {
		t.Fatalf("tick: %+v", out)
	}
	if n := len(produceReqs(t, d, ob)); n != 0 {
		t.Fatalf("requests = %d", n)
	}
}

func TestProduceInputChecks(t *testing.T) {
	_, d := produceVault(t, "2026-11-06T09:00:00Z")
	person := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"}).ID
	for name, in := range map[string]ObligationAddInput{
		"agent without produce":    {Title: "x", OwedBy: []string{"agent:writer"}, Due: "2026-11-06"},
		"produce owed by a person": {Title: "x", OwedBy: []string{person}, Produce: &ProduceInput{Kind: model.ProduceReport}},
		"bad kind":                 {Title: "x", Produce: &ProduceInput{Kind: "poem"}},
		"message with no person":   {Title: "x", Produce: &ProduceInput{Kind: model.ProduceMessage, To: []string{"#general"}}},
		"to on a report":           {Title: "x", Produce: &ProduceInput{Kind: model.ProduceReport, To: []string{person}}},
		"missing template doc":     {Title: "x", Produce: &ProduceInput{Kind: model.ProduceReport, Template: "[[note-nothing]]"}},
		"bad agent name":           {Title: "x", OwedBy: []string{"agent:Two Words"}, Produce: &ProduceInput{Kind: model.ProduceReport}},
		"a meeting":                {Title: "x", Kind: model.KindMeeting, With: []string{person}, Start: "2026-11-06T10:00:00Z", End: "2026-11-06T10:30:00Z", Produce: &ProduceInput{Kind: model.ProduceReport}},
	} {
		if _, err := ObligationAdd.Run(ctx, d, in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// The journal narrative is the built-in daily obligation: made once per vault
// when journal.agent is set, producing a request that lists the journals that
// ended with events, skipped (closed) on a day with none, paused while
// journal.agent is unset.
func TestProduceJournalSummaryDefault(t *testing.T) {
	tv, d := produceVault(t, "2026-11-03T10:00:00Z", "request.surfaces=cli,tick")
	inboxAt(t, tv, d, "2026-11-03T10:00:00Z")

	// No journal.agent: no obligation, no request.
	tv.at("2026-11-04T08:00:00Z")
	if out := tick(t, d); len(out.Defaults) != 0 || len(out.Produce) != 0 {
		t.Fatalf("without journal.agent: %+v", out)
	}
	if got := journalReqs(t, d); len(got) != 0 {
		t.Fatalf("journal requests at the obligation format: %d", len(got))
	}

	tv.addProps("journal.agent=agent:claude")
	tv.at("2026-11-04T08:10:00Z")
	out := tick(t, d)
	if len(out.Defaults) != 1 || len(out.Produce) != 1 {
		t.Fatalf("first tick with journal.agent: %+v", out)
	}
	def := out.Defaults[0]
	_, o := getOb(t, d, def)
	if o.Produce.Builtin != JournalSummaryBuiltin || o.Produce.Kind != model.ProduceJournal || o.Agent() != "agent:claude" || o.Recur != "daily" || o.Due != "2026-11-04" {
		t.Fatalf("default: %+v %+v", o, o.Produce)
	}
	r := produceReqs(t, d, def)
	if len(r) != 1 || !strings.Contains(r[0].Body, "[[2026-11-03]] (day)") || strings.Contains(r[0].Body, "[[2026-11-02]]") || !strings.Contains(r[0].Body, "journal_summary") {
		t.Fatalf("journal ask: %v\n%s", len(r), r[0].Body)
	}
	if got := journalReqs(t, d); len(got) != 0 {
		t.Fatalf("old per-journal requests made: %d", len(got))
	}
	next := out.Occurrences[0]

	// Never a second default, even once the first is closed.
	must(t, obligationClose, d, obligationCloseInput{ID: def, Outcome: string(model.OutcomeDropped)})
	tv.at("2026-11-04T09:00:00Z")
	if out := tick(t, d); len(out.Defaults) != 0 {
		t.Fatalf("second default: %v", out.Defaults)
	}

	// The agent wrote the day's summary. The next day had no events but the
	// journal plumbing: its occurrence closes skipped, no request, and the
	// series goes on.
	must(t, JournalSummary, d, JournalSummaryInput{ID: "2026-11-03", Text: "One note came in."})
	tv.at("2026-11-05T08:00:00Z")
	out = tick(t, d)
	if len(out.Produce) != 0 {
		doc, _ := d.Store.Get(ctx, out.Produce[0])
		j, _ := d.Store.Get(ctx, "2026-11-04")
		t.Fatalf("quiet day produced %v:\n%s\n%s", out.Produce, doc.Body, j.Body)
	}
	if _, no := getOb(t, d, next); no.Open() || no.Outcome != model.OutcomeSkipped {
		t.Fatalf("quiet occurrence: state %s outcome %s", no.State, no.Outcome)
	}
	if len(out.Occurrences) != 1 {
		t.Fatalf("series stopped: %+v", out)
	}

	// journal.agent unset: the built-in waits.
	third := out.Occurrences[0]
	inboxAt(t, tv, d, "2026-11-05T10:00:00Z")
	tv.addProps("journal.agent=")
	tv.at("2026-11-06T08:00:00Z")
	if out := tick(t, d); len(out.Produce) != 0 || len(produceReqs(t, d, third)) != 0 {
		t.Fatalf("produced with journal.agent unset: %+v", out)
	}
}

// A vault below the obligation format keeps the old journal requests.
func TestProduceJournalBelowFormatKeepsRequests(t *testing.T) {
	tv, d := journalTick(t, "2026-11-03T10:00:00Z", "journal.agent=claude")
	inboxAt(t, tv, d, "2026-11-03T10:00:00Z")
	tv.at("2026-11-04T08:00:00Z")
	out := tick(t, d)
	if len(out.Defaults) != 0 || len(journalReqs(t, d)) == 0 {
		t.Fatalf("below format: %+v", out)
	}
}

// A second synthetic persona: a weekly status message to a group, no journal.
// The message stays a draft until the owner approves it, and its occurrence
// cannot close done before it is sent.
func TestProduceWeeklyMessageToAGroup(t *testing.T) {
	tv, d := produceVault(t, "2026-11-06T09:00:00Z", "request.surfaces=cli,pwa,tick")
	grp := must(t, OrgGroupAdd, d, GroupAddInput{Name: "Garden club", Org: "garden.example", Kind: model.GroupKinds[0]}).ID
	tpl := must(t, NoteAdd, d, NoteAddInput{Title: "Club update template", Body: "Planted, harvested, needs hands."}).ID
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Weekly club update", OwedBy: []string{"agent:helper"}, OwedTo: []string{grp},
		Recur: "weekly", Due: "2026-11-06", Produce: &ProduceInput{Kind: model.ProduceMessage, Template: tpl, To: []string{grp, "#club-chat"}, Medium: model.MediumEmail}}).ID
	out := tick(t, d)
	if len(out.Produce) != 1 {
		t.Fatalf("tick: %+v", out)
	}
	req := out.Produce[0]
	_, rm, _ := getRequest(ctx, d.Store, req)
	doc, _ := d.Store.Get(ctx, req)
	if rm.Kind != ProduceKind || !strings.Contains(doc.Body, "io_add") || !strings.Contains(doc.Body, "[["+grp+"]]") || !strings.Contains(doc.Body, "medium email") {
		t.Fatalf("ask:\n%s", doc.Body)
	}

	agent := tv.open("ia", "mcp")
	claim(t, agent, req)
	wctx := WithWrite(ctx, agentWrite("Weekly club update from the template."))
	draft, err := IOAdd.Run(wctx, agent, IOAddInput{Direction: model.IOOut, Text: "Club update\n\nPlanted garlic. Needs hands on Saturday.", Medium: model.MediumEmail, To: []string{grp}, Expects: model.ExpectsNone})
	if err != nil {
		t.Fatal(err)
	}
	if draft.State != model.IODraft {
		t.Fatalf("draft state = %s", draft.State)
	}
	if _, err := ItemPrepared.Run(wctx, agent, ItemPreparedInput{ID: ob, Kind: model.PreparedArtifact, Ref: draft.ID}); err != nil {
		t.Fatal(err)
	}
	must(t, RequestDone, agent, RequestDoneInput{ID: req, Resolution: model.ResolutionDone, Result: "Drafted the update.", Links: []string{draft.ID}})

	if row := rowOf(t, d, ob); !row.Ready || row.ReadyRef != draft.ID {
		t.Fatalf("occurrence row: %+v", row)
	}
	if p := must(t, TaskShow, d, TaskShowInput{ID: ob}).Produce; p == nil || p.Artifact == nil || p.Artifact.State != string(model.IODraft) {
		t.Fatalf("artifact before approval: %+v", p)
	}
	// The message stays open: the owner approves it.
	if _, mo := getOb(t, d, ob); !mo.Open() {
		t.Fatal("a message occurrence closed before approval")
	}
	if row := rowOf(t, d, ob); !row.NeedsYou || !strings.Contains(row.Reason, "ready: approve it") {
		t.Fatalf("message row: %+v", row)
	}

	// A week later the next one is ready too, while this one is unsent: only
	// the newest needs the owner; the older says it is superseded.
	tv.at("2026-11-13T09:00:00Z")
	out2 := tick(t, d)
	if len(out2.Produce) != 1 {
		t.Fatalf("week two: %+v", out2)
	}
	_, r2, _ := getRequest(ctx, d.Store, out2.Produce[0])
	week2 := r2.Links[0].Target()
	claim(t, agent, out2.Produce[0])
	draft2, err := IOAdd.Run(wctx, agent, IOAddInput{Direction: model.IOOut, Text: "Club update\n\nGarlic is up.", Medium: model.MediumEmail, To: []string{grp}, Expects: model.ExpectsNone})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ItemPrepared.Run(wctx, agent, ItemPreparedInput{ID: week2, Kind: model.PreparedArtifact, Ref: draft2.ID}); err != nil {
		t.Fatal(err)
	}
	must(t, RequestDone, agent, RequestDoneInput{ID: out2.Produce[0], Resolution: model.ResolutionDone, Result: "Drafted.", Links: []string{draft2.ID}})
	sn2, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn2.Need(ob); n.Yes || n.Reason != "superseded by "+week2 {
		t.Fatalf("older occurrence: %+v", n)
	}
	if n := sn2.Need(week2); !n.Yes {
		t.Fatalf("newest occurrence: %+v", n)
	}
	// Not sent, not approved: it cannot close done.
	if _, err := obligationClose.Run(ctx, d, obligationCloseInput{ID: ob}); err == nil || !strings.Contains(err.Error(), "io_approve") {
		t.Fatalf("close done before approval: %v", err)
	}
	// An agent cannot approve it without the owner's words.
	if _, err := IOApprove.Run(wctx, agent, IOApproveInput{ID: draft.ID}); err == nil {
		t.Fatal("agent approved the draft without owner_said")
	}
	if _, m := ioOf(t, d, draft.ID); m.State != model.IODraft {
		t.Fatalf("state after the agent's try = %s", m.State)
	}
	must(t, IOApprove, d, IOApproveInput{ID: draft.ID})
	if _, m := ioOf(t, d, draft.ID); m.State != model.IOReady {
		t.Fatalf("state after io_approve = %s", m.State)
	}
	if p := must(t, TaskShow, d, TaskShowInput{ID: ob}).Produce; p == nil || p.Artifact.State != string(model.IOReady) {
		t.Fatalf("artifact after approval: %+v", p)
	}
	must(t, IOSend, agent, IOSendInput{ID: draft.ID, URL: "https://mail.example/sent/1"})
	must(t, obligationClose, d, obligationCloseInput{ID: ob})
	// The series goes on to the next week; no journal obligation in this vault.
	if out := tick(t, d); len(out.Defaults) != 0 {
		t.Fatalf("defaults in a vault without journal.agent: %v", out.Defaults)
	}
}

func ioOf(t *testing.T, d Deps, id string) (model.Document, *model.IO) {
	t.Helper()
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return doc, doc.Meta.(*model.IO)
}

// An ongoing obligation an agent owes is produced once per review turn.
func TestProduceOngoingOncePerTurn(t *testing.T) {
	tv, d := produceVault(t, "2026-11-06T09:00:00Z", "request.surfaces=cli,tick")
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Runbook refresh", OwedBy: []string{"agent:writer"}, Cadence: "monthly", Produce: &ProduceInput{Kind: model.ProduceDoc}}).ID
	if out := tick(t, d); len(out.Produce) != 1 || len(out.Occurrences) != 0 {
		t.Fatalf("first: %+v", out)
	}
	r := produceReqs(t, d, ob)[0]
	must(t, RequestCancel, d, RequestCancelInput{ID: r.ID})
	if out := tick(t, d); len(out.Produce) != 0 {
		t.Fatalf("same turn: %v", out.Produce)
	}
	tv.at("2026-11-07T09:00:00Z")
	must(t, ObligationSchedule, d, ObligationScheduleInput{ID: ob, Reviewed: true})
	tv.at("2026-12-08T09:00:00Z")
	if out := tick(t, d); len(out.Produce) != 1 {
		t.Fatalf("next turn: %+v", out)
	}
}

// closeReceipted reports whether the item_prepared that closed id left a
// receipt of it (the close runs inside it, so its receipt undoes both).
func closeReceipted(t *testing.T, d Deps, id string) bool {
	t.Helper()
	rs, err := listReceipts(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Doc == id && r.Op == ItemPrepared.Name() {
			return true
		}
	}
	return false
}
