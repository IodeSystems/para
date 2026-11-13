package ops

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// agentAs opens the tick vault as an agent actor on surface mcp.
func (tv *tickVault) agentAs(actor string) Deps {
	tv.t.Helper()
	s, err := local.Open(tv.root, vault.Options{ID: "acme", Actor: actor, Surface: "mcp", Install: "ia",
		StateDir: filepath.Join(filepath.Dir(tv.root), "state-ia"), Now: func() time.Time { return tv.now }})
	if err != nil {
		tv.t.Fatal(err)
	}
	return Deps{Store: s}
}

func closeVault(t *testing.T) (*tickVault, Deps, string) {
	tv, d, task, _ := closeVaultGoal(t)
	return tv, d, task
}

func closeVaultGoal(t *testing.T) (*tickVault, Deps, string, string) {
	t.Helper()
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia", "request.surfaces=cli,pwa,tick", "prep.ahead=5", "gap.enabled=false")
	d := tv.open("ia", "cli")
	goal := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Goal: goal, Title: "Renew cert", DoneWhen: "Renewed."}).ID
	return tv, d, task, goal
}

func TestTaskEntersPrepQueue(t *testing.T) {
	tv, d, task := closeVault(t)
	if r := rowOf(t, d, task); !r.Prep {
		t.Fatalf("task row is not prep: %+v", r)
	}
	out := tick(t, d)
	if len(out.Prepares) != 1 {
		t.Fatalf("prepares %v", out.Prepares)
	}
	req, r, err := getRequest(ctx, d.Store, out.Prepares[0])
	if err != nil {
		t.Fatal(err)
	}
	if r.Links[0].Target() != task || !strings.Contains(req.Body, "kind: close") || !strings.Contains(req.Body, "kind: none") {
		t.Fatalf("prepare request for %v:\n%s", r.Links, req.Body)
	}
	// The agent finds nothing obvious and says so; the request ends.
	agent := tv.agentAs("agent:scribe")
	if _, err := RequestNext.Run(ctx, agent, RequestNextInput{}); err != nil {
		t.Fatal(err)
	}
	must(t, ItemPrepared, agent, ItemPreparedInput{ID: task, Kind: model.PreparedNone})
	must(t, RequestDone, agent, RequestDoneInput{ID: out.Prepares[0], Resolution: model.ResolutionDone, Result: "No obvious close."})
	if r := rowOf(t, d, task); r.Ready {
		t.Fatalf("no obvious close reads ready: %+v", r)
	}
	// Archiving the request record keeps the no: the marker holds until the
	// task changes.
	if _, err := d.Store.Move(ctx, out.Prepares[0], model.FolderArchive); err != nil {
		t.Fatal(err)
	}
	if again := tick(t, d); len(again.Prepares) != 0 {
		t.Fatalf("asked again at the same version: %v", again.Prepares)
	}
	must(t, TaskEdit, d, TaskEditInput{ID: task, Effort: model.EffortS})
	if again := tick(t, d); len(again.Prepares) != 1 {
		t.Fatalf("a changed task is not asked again: %v", again.Prepares)
	}
}

func TestPreparedCloseApproveAndUndo(t *testing.T) {
	tv, d, task := closeVault(t)
	agent := tv.agentAs("agent:scribe")
	ev := &model.Evidence{Quote: "Certificate renewed, valid to 2027-11-01", Locator: "https://example.com/certs/42"}
	out := must(t, ItemPrepared, agent, ItemPreparedInput{ID: task, Kind: model.PreparedClose, Close: &model.CloseProposal{
		Reason: "The console shows the new certificate.", Evidence: ev, Confidence: 90, ConfidenceBasis: "the console page names the new expiry"}})
	if out.Held != "" || out.Prepared.Close.Outcome != "done" || out.Prepared.Close.Confidence != 90 {
		t.Fatalf("prepared: %+v held %q", out.Prepared.Close, out.Held)
	}
	if r := rowOf(t, d, task); !r.Ready || r.ReadyKind != model.PreparedClose {
		t.Fatalf("row: %+v", r)
	}
	show := must(t, PreparedClose, d, PreparedCloseInput{ID: task})
	if !show.Ready || !show.High || show.By != "agent:scribe" || show.Check != model.CheckURL {
		t.Fatalf("show: %+v", show)
	}

	// An agent cannot approve it.
	if _, err := PreparedCloseApprove.Run(ctx, agent, PreparedCloseInput{ID: task}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent approve: %v", err)
	}

	ap := must(t, PreparedCloseApprove, d, PreparedCloseInput{ID: task})
	if ap.Outcome != "done" || ap.ProposedBy != "agent:scribe" || ap.Receipt == "" {
		t.Fatalf("approve: %+v", ap)
	}
	doc, err := d.Store.Get(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if lifeOf(doc.Meta) != LifeClosed {
		t.Fatalf("task not closed: %+v", doc.Meta)
	}
	rc := must(t, ReceiptShow, d, ReceiptIDInput{ID: ap.Receipt})
	if rc.Receipt.Actor != "user" || !strings.Contains(rc.Receipt.Why, "owner approved the close agent:scribe proposed") || !strings.Contains(strings.Join(rc.Receipt.Sources, " "), ev.Locator) {
		t.Fatalf("receipt: actor %q why %q sources %v", rc.Receipt.Actor, rc.Receipt.Why, rc.Receipt.Sources)
	}

	// Undo opens it again, with the proposal on it.
	must(t, ReceiptRevert, d, ReceiptIDInput{ID: ap.Receipt})
	doc, err = d.Store.Get(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if lifeOf(doc.Meta) != LifeOpen {
		t.Fatalf("undo left it %v", lifeOf(doc.Meta))
	}
	if r := rowOf(t, d, task); !r.Ready || r.ReadyKind != model.PreparedClose {
		t.Fatalf("after undo: %+v", r)
	}

	// An edit makes it stale: the approval is refused.
	must(t, TaskEdit, d, TaskEditInput{ID: task, Effort: model.EffortS})
	if r := rowOf(t, d, task); r.Ready {
		t.Fatalf("stale proposal reads ready: %+v", r)
	}
	if _, err := PreparedCloseApprove.Run(ctx, d, PreparedCloseInput{ID: task}); !errors.Is(err, ErrCloseStale) {
		t.Fatalf("stale approve: %v", err)
	}
	if s := must(t, PreparedClose, d, PreparedCloseInput{ID: task}); s.Ready || !s.Stale {
		t.Fatalf("stale show: %+v", s)
	}
	mustLintClean(t, d)
}

func TestPreparedCloseRules(t *testing.T) {
	tv, d, task, goal := closeVaultGoal(t)
	agent := tv.agentAs("agent:scribe")
	// No evidence: the confidence is held low.
	out := must(t, ItemPrepared, agent, ItemPreparedInput{ID: task, Kind: model.PreparedClose, Close: &model.CloseProposal{
		Outcome: "dropped", Reason: "The portal moved to a managed host.", Confidence: 85, ConfidenceBasis: "the owner said so last week"}})
	if out.Prepared.Close.Confidence != model.LowConfidence || out.Held == "" {
		t.Fatalf("no evidence: confidence %d held %q", out.Prepared.Close.Confidence, out.Held)
	}
	if s := must(t, PreparedClose, d, PreparedCloseInput{ID: task}); s.High {
		t.Fatalf("held proposal is high: %+v", s)
	}
	for name, cp := range map[string]*model.CloseProposal{
		"no reason":    {Confidence: 50, ConfidenceBasis: "x", Evidence: &model.Evidence{Locator: "https://example.com/a"}},
		"no basis":     {Reason: "r", Confidence: 50, Evidence: &model.Evidence{Locator: "https://example.com/a"}},
		"bad outcome":  {Outcome: "finished", Reason: "r", ConfidenceBasis: "x"},
		"duplicate":    {Outcome: "duplicate", Reason: "r", ConfidenceBasis: "x"},
		"over 100":     {Reason: "r", Confidence: 101, ConfidenceBasis: "x"},
		"free text ev": {Reason: "r", ConfidenceBasis: "x", Evidence: &model.Evidence{Quote: "q", Locator: "somewhere in slack"}},
	} {
		if _, err := ItemPrepared.Run(ctx, agent, ItemPreparedInput{ID: task, Kind: model.PreparedClose, Close: cp}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ItemPrepared.Run(ctx, agent, ItemPreparedInput{ID: task, Kind: model.PreparedPlan, Close: &model.CloseProposal{Reason: "r"}}); err == nil {
		t.Error("close on kind plan accepted")
	}
	// A duplicate with its link is kept, and approved as close would close it.
	other := must(t, TaskAdd, d, TaskAddInput{Goal: goal, Title: "Renew the cert again", DoneWhen: "Renewed."}).ID
	must(t, ItemPrepared, agent, ItemPreparedInput{ID: task, Kind: model.PreparedClose, Close: &model.CloseProposal{
		Outcome: "duplicate", Reason: "Same work as the newer task.", Fields: map[string]any{"link": other}, Evidence: &model.Evidence{Quote: "Renew the cert again", Locator: other},
		Confidence: 70, ConfidenceBasis: "same title and done_when"}})
	ap := must(t, PreparedCloseApprove, d, PreparedCloseInput{ID: task})
	if ap.Outcome != "duplicate" || ap.Op != "task_resolve" {
		t.Fatalf("duplicate approve: %+v", ap)
	}
	mustLintClean(t, d)
}
