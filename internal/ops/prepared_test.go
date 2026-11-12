package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// rowOf is the value-order row of id, or fails.
func rowOf(t *testing.T, d Deps, id string) ValueRow {
	t.Helper()
	o, err := Order(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range o.Rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("%s not in order", id)
	return ValueRow{}
}

func orderList(t *testing.T, d Deps) []string {
	t.Helper()
	o, err := Order(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range o.Rows {
		out = append(out, r.ID)
	}
	return out
}

func TestPreparedReadyAndStale(t *testing.T) {
	d := gapVault(t)
	proj := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Renew cert", DoneWhen: "Renewed."}).ID
	if r := rowOf(t, d, task); r.Ready {
		t.Fatalf("raw task is ready: %+v", r)
	}
	must(t, TaskEdit, d, TaskEditInput{ID: task, Steps: "1. Open the console.\n2. Renew."})
	out := must(t, ItemPrepared, d, ItemPreparedInput{ID: task, Kind: model.PreparedPlan})
	if out.Prepared.By != "user" || out.Prepared.Hash == "" || out.Prepared.Kind != model.PreparedPlan {
		t.Fatalf("prepared = %+v", out.Prepared)
	}
	if r := rowOf(t, d, task); !r.Ready || r.ReadyKind != model.PreparedPlan || r.ReadyRef != "" {
		t.Fatalf("prepared task: %+v", r)
	}
	// An edit of the item after it makes it raw again.
	must(t, TaskEdit, d, TaskEditInput{ID: task, Effort: model.EffortS})
	if r := rowOf(t, d, task); r.Ready {
		t.Fatalf("stale task is ready: %+v", r)
	}
	// Bad input is refused.
	if _, err := ItemPrepared.Run(ctx, d, ItemPreparedInput{ID: task, Kind: "guess"}); err == nil {
		t.Error("unknown kind accepted")
	}
	if _, err := ItemPrepared.Run(ctx, d, ItemPreparedInput{ID: task, Kind: model.PreparedDraft, Ref: "io-missing"}); err == nil {
		t.Error("missing ref accepted")
	}
	mustLintClean(t, d)

	// A question with an agent's proposed answer is ready by itself.
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which CA?", Goal: proj}).ID
	qd, err := d.Store.Get(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	qd.Meta.(*model.Question).ProposedBy, qd.Meta.(*model.Question).ProposedAnswer = "agent:scribe", "The current one."
	if _, err := d.Store.Update(ctx, qd); err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, d, q); !r.Ready || r.ReadyKind != model.PreparedAnswer {
		t.Fatalf("question with a proposal: %+v", r)
	}
}

func TestPreparedReadyFirstWithinGroup(t *testing.T) {
	d := gapVault(t)
	proj := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Older", DoneWhen: "Done.", Effort: model.EffortS})
	must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Newer", DoneWhen: "Done.", Effort: model.EffortS})
	soon := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Due", DoneWhen: "Done.", Effort: model.EffortS, Due: "2026-11-03"}).ID
	// older and newer tie on everything but id (same created time on the test
	// clock), so the raw order between them is the id order: prepare the one
	// that sorts second.
	before := orderList(t, d)
	if len(before) != 3 || before[0] != soon {
		t.Fatalf("before: %v", before)
	}
	older, newer := before[1], before[2]
	must(t, ItemPrepared, d, ItemPreparedInput{ID: newer, Kind: model.PreparedPlan})
	// Ready first in its group; the group order is the same: due soon stays first.
	if got := orderList(t, d); !slices.Equal(got, []string{soon, newer, older}) {
		t.Fatalf("after: %v", got)
	}
	if g1, g2 := rowOf(t, d, newer).Group, rowOf(t, d, older).Group; g1 != g2 {
		t.Fatalf("groups %s, %s", g1, g2)
	}
}

// askIn adds an ask made to the owner (prep, no gap).
func askIn(t *testing.T, d Deps, ask string) string {
	t.Helper()
	return must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXIn, Ask: ask, Route: "domain-logging"}).ID
}

func TestTickPrepareAheadAndOpenWork(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia", "request.surfaces=cli,pwa,tick", "prep.ahead=2", "gap.enabled=false")
	d := tv.open("ia", "cli")
	for _, ask := range []string{"Review the plan", "Sign the form", "Pick a date"} {
		askIn(t, d, ask)
	}
	rows := orderList(t, d)
	if len(rows) != 3 {
		t.Fatalf("rows %v", rows)
	}
	// Open side work on the first row (it stays the owner's, so it stays in the
	// window of two): it gets no prepare.
	busy := must(t, RequestAdd, d, RequestAddInput{Ask: "Fill it in", Links: []string{rows[0]}, Kind: FillGapKind}).ID
	out := tick(t, d)
	got := []string{}
	for _, id := range out.Prepares {
		_, r, err := getRequest(ctx, d.Store, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind != PrepareKind || r.Status != model.RequestQueued {
			t.Fatalf("%s: %+v", id, r)
		}
		got = append(got, r.Links[0].Target())
	}
	if !slices.Equal(got, []string{rows[1]}) {
		t.Fatalf("prepares for %v, want only %s (rows %v, busy %s)", got, rows[1], rows, busy)
	}
	// The prepared row stays the owner's while the agent works.
	if r := rowOf(t, d, rows[1]); !r.NeedsYou {
		t.Fatalf("row left needs-you: %+v", r)
	}
	// Again: open work, no second one.
	if again := tick(t, d); len(again.Prepares) != 0 {
		t.Fatalf("again: %v", again.Prepares)
	}
}

// A vault shaped unlike the owner's: no meetings, no goals with tasks; mail
// that expects replies. Prepare drafts the replies; a threaded draft makes the
// message ready, and a done prepare is not a result to check.
func TestTickPreparePersonaVault(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia", "request.surfaces=cli,pwa,tick")
	d := tv.open("ia", "cli")
	in := func(id, subject string) string {
		doc := model.Document{ID: id, Folder: model.FolderIO, Title: subject, Body: "Can you confirm?",
			Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOIn, Medium: model.MediumOther, State: model.IOUnread,
				From: "Neighbour", At: "2026-11-01T10:00:00Z", Subject: subject, Expects: model.ExpectsResponse}}
		saved, err := d.Store.Create(ctx, doc)
		if err != nil {
			t.Fatal(err)
		}
		return saved.ID
	}
	fence, bins := in("io-fence", "Fence repair"), in("io-bins", "Bin day")
	out := tick(t, d)
	if len(out.Prepares) != 2 || len(out.Gaps) != 0 {
		t.Fatalf("tick: %+v", out)
	}
	req, _, err := getRequest(ctx, d.Store, out.Prepares[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Body, "Never send") || !strings.Contains(req.Body, "thread ") {
		t.Errorf("body:\n%s", req.Body)
	}
	// The agent drafts the fence reply and ends the work.
	draft := model.Document{ID: "io-fence-reply", Folder: model.FolderIO, Title: "Re: Fence repair", Body: "Yes, Saturday works.",
		Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOOut, Medium: model.MediumOther, State: model.IODraft,
			To: []string{"Neighbour"}, Subject: "Re: Fence repair", Thread: model.LinkTo(fence), Expects: model.ExpectsNone}}
	if _, err := d.Store.Create(ctx, draft); err != nil {
		t.Fatal(err)
	}
	var fenceReq string
	for _, id := range out.Prepares {
		_, r, _ := getRequest(ctx, d.Store, id)
		if r.Links[0].Target() == fence {
			fenceReq = id
		}
	}
	agent := tv.open("ia", "mcp")
	for range out.Prepares {
		if _, err := RequestNext.Run(ctx, agent, RequestNextInput{}); err != nil {
			t.Fatal(err)
		}
	}
	must(t, RequestDone, agent, RequestDoneInput{ID: fenceReq, Resolution: model.ResolutionDone, Result: "Drafted the reply.", Links: []string{"io-fence-reply"}})
	if row := rowOf(t, d, fence); !row.Ready || row.ReadyKind != model.PreparedDraft || row.ReadyRef != "io-fence-reply" {
		t.Fatalf("fence row: %+v", row)
	}
	if row := rowOf(t, d, bins); row.Ready {
		t.Fatalf("bins row: %+v", row)
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need(fenceReq); n.Yes {
		t.Fatalf("a done prepare needs the owner: %+v", n)
	}
	// Ready, and the other has open work: nothing new.
	if again := tick(t, d); len(again.Prepares) != 0 {
		t.Fatalf("again: %v", again.Prepares)
	}
}
