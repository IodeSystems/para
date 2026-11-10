package ops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// sourceClockDeps opens a vault whose clock reads *now, so a test can move it.
func sourceClockDeps(t *testing.T, now *time.Time) (Deps, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "v")
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "acme", tpl, vault.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	s, err := local.Open(root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "m1",
		StateDir: filepath.Join(filepath.Dir(root), "state"), Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	return Deps{Store: s}, root
}

func agentDeps(d Deps, actor string) Deps {
	d.Store = d.Store.ActAs(actor, "mcp")
	return d
}

func addGitHubSource(t *testing.T, d Deps) string {
	t.Helper()
	out, err := SourceAdd.Run(ctx, d, SourceAddInput{Title: "Code reviews", Example: model.SourceGitHub})
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

// A declaration lives on this machine, not in the vault; a plan lists the
// declared sources that are due; the cursor a sweep records comes back in
// the next plan, with since at the last scan.
func TestSweepPlanDueAndCursor(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	d, root := sourceClockDeps(t, &now)
	src := addGitHubSource(t, d)
	claude := agentDeps(d, "claude")

	plan, err := SweepPlan.Run(ctx, claude, SweepPlanInput{})
	if err != nil || plan.Declared || len(plan.Sources) != 0 {
		t.Fatalf("before declaring: %+v %v", plan, err)
	}
	decl, err := AgentSources.Run(ctx, claude, AgentSourcesInput{Sources: []string{src, "nowhere"}})
	if err != nil || !slices.Equal(decl.Sources, []string{src}) || !slices.Equal(decl.Unknown, []string{"nowhere"}) || !slices.Equal(decl.Due, []string{src}) {
		t.Fatalf("declare = %+v %v", decl, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "state", "reach", "claude.json")); err != nil {
		t.Fatalf("declaration not in the state directory: %v", err)
	}
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, _ error) error {
		if strings.Contains(p, "reach") || strings.Contains(p, "claude.json") {
			t.Errorf("declaration in the vault: %s", p)
		}
		return nil
	})
	src0, _, _ := getSource(ctx, d.Store, src)
	if src0.Meta.(*model.SourceDoc).ReachedBy != nil {
		t.Fatal("learn_reach is off: reached_by must not change")
	}

	plan, err = SweepPlan.Run(ctx, claude, SweepPlanInput{})
	if err != nil || !plan.Declared || len(plan.Sources) != 1 {
		t.Fatalf("plan = %+v %v", plan, err)
	}
	p := plan.Sources[0]
	if p.Since != now.Add(-7*24*time.Hour).UTC().Format(time.RFC3339) || !strings.Contains(p.ScanIn.Rendered, p.Since) || p.Fetch.Rendered != "gh issue view {url} --json title,body,state,comments" {
		t.Fatalf("source = %+v scan_in %+v", p, p.ScanIn)
	}
	cur := "c-9"
	rec, err := SweepRecord.Run(ctx, claude, SweepRecordInput{Source: src, Cursor: &cur, Counts: map[string]int{"in": 2}})
	if err != nil || rec.Cursor != "c-9" || rec.LastScan != now.UTC().Format(time.RFC3339) || rec.Due != "2026-11-05" {
		t.Fatalf("record = %+v %v", rec, err)
	}
	if plan, _ = SweepPlan.Run(ctx, claude, SweepPlanInput{}); len(plan.Sources) != 0 {
		t.Fatalf("swept today, still due: %+v", plan.Sources)
	}
	// Named, it is planned even when not due.
	if plan, _ = SweepPlan.Run(ctx, claude, SweepPlanInput{Sources: []string{src, "nowhere"}}); len(plan.Sources) != 1 || len(plan.Skipped) != 1 {
		t.Fatalf("named = %+v", plan)
	}
	now = now.Add(24 * time.Hour)
	plan, _ = SweepPlan.Run(ctx, claude, SweepPlanInput{})
	if len(plan.Sources) != 1 || plan.Sources[0].Cursor != "c-9" || plan.Sources[0].Since != rec.LastScan {
		t.Fatalf("next day = %+v", plan.Sources)
	}
	// A failed scan keeps last_scan and the cursor.
	other := "c-10"
	rec2, err := SweepRecord.Run(ctx, claude, SweepRecordInput{Source: src, Cursor: &other, Failed: true, Errors: []string{"gh timed out"}})
	if err != nil || rec2.LastScan != rec.LastScan || rec2.Cursor != "c-9" {
		t.Fatalf("failed = %+v %v", rec2, err)
	}
	doc, _, _ := getSource(ctx, d.Store, src)
	if !strings.Contains(doc.Body, "## Last sweep") || !strings.Contains(doc.Body, "gh timed out") || strings.Count(doc.Body, "## Last sweep") != 1 {
		t.Fatalf("body = %q", doc.Body)
	}
	// Another agent that did not declare it does not get it.
	if plan, _ = SweepPlan.Run(ctx, agentDeps(d, "codex"), SweepPlanInput{}); len(plan.Sources) != 0 || plan.Declared {
		t.Fatalf("other agent = %+v", plan)
	}
	mustLintClean(t, d)
}

// With sources.learn_reach on, a declaration adds the actor to reached_by.
func TestAgentSourcesLearnReach(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	d, _ := sourceClockDeps(t, &now)
	src := addGitHubSource(t, d)
	if err := d.Store.SetConfig(ctx, learnReachConfig, "true"); err != nil {
		t.Fatal(err)
	}
	out, err := AgentSources.Run(ctx, agentDeps(d, "claude"), AgentSourcesInput{Sources: []string{src}})
	if err != nil || !slices.Equal(out.Learned, []string{src}) {
		t.Fatalf("declare = %+v %v", out, err)
	}
	_, m, _ := getSource(ctx, d.Store, src)
	if !slices.Equal(m.ReachedBy, []string{"claude"}) {
		t.Fatalf("reached_by = %v", m.ReachedBy)
	}
	if out, _ = AgentSources.Run(ctx, agentDeps(d, "claude"), AgentSourcesInput{Sources: []string{src}}); len(out.Learned) != 0 {
		t.Fatalf("learned twice: %+v", out)
	}
}

// Tick queues one sweep per due source, and no second while it is open. Only
// an agent that declared the source claims it; the dispatcher's watch sees it
// only once some agent here declared it.
func TestTickSweepQueuedOnceClaimNeedsDeclaration(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	d, _ := sourceClockDeps(t, &now)
	src := addGitHubSource(t, d)
	for k, v := range map[string]string{"request.runner": "m1", "request.surfaces": "cli,pwa,tick"} {
		if err := d.Store.SetConfig(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Tick(ctx, d, TickInput{})
	if err != nil || len(out.Sweeps) != 1 || out.Proposed != 0 {
		t.Fatalf("tick = %+v %v", out, err)
	}
	_, r, _ := getRequest(ctx, d.Store, out.Sweeps[0])
	if r.Status != model.RequestQueued || r.Kind != SweepKind || r.RequiresSource.Target() != src {
		t.Fatalf("request = %+v", r)
	}
	if out, _ = Tick(ctx, d, TickInput{}); len(out.Sweeps) != 0 {
		t.Fatalf("second tick queued again: %+v", out.Sweeps)
	}
	watch := NewRequestWatch(Deps{Store: d.Store.ActAs("dispatch", "dispatch")})
	if ids, err := watch.ClaimableIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("watch before any declaration = %v %v", ids, err)
	}
	if next, err := RequestNext.Run(ctx, agentDeps(d, "codex"), RequestNextInput{}); err != nil || next.Request != nil {
		t.Fatalf("undeclared claim = %+v %v", next, err)
	}
	if _, err := AgentSources.Run(ctx, agentDeps(d, "claude"), AgentSourcesInput{Sources: []string{src}}); err != nil {
		t.Fatal(err)
	}
	if ids, _ := watch.ClaimableIDs(ctx); len(ids) != 1 {
		t.Fatalf("watch after declaring = %v", ids)
	}
	if ids, _ := NewActorRequestWatch(agentDeps(d, "codex"), "codex").ClaimableIDs(ctx); len(ids) != 0 {
		t.Fatalf("codex's watch lists a sweep only claude can claim: %v", ids)
	}
	if ids, _ := NewActorRequestWatch(agentDeps(d, "claude"), "claude").ClaimableIDs(ctx); len(ids) != 1 {
		t.Fatalf("claude's watch = %v", ids)
	}
	if next, _ := RequestNext.Run(ctx, agentDeps(d, "codex"), RequestNextInput{}); next.Request != nil {
		t.Fatal("codex claimed a sweep it did not declare")
	}
	next, err := RequestNext.Run(ctx, agentDeps(d, "claude"), RequestNextInput{})
	if err != nil || next.Request == nil || next.Request.RequiresSource != src {
		t.Fatalf("claim = %+v %v", next, err)
	}
	rec, err := SweepRecord.Run(ctx, agentDeps(d, "claude"), SweepRecordInput{Source: src, Request: next.Request.ID})
	if err != nil || rec.Request != next.Request.ID {
		t.Fatalf("record = %+v %v", rec, err)
	}
	if _, r, _ = getRequest(ctx, d.Store, next.Request.ID); r.Status != model.RequestDone {
		t.Fatalf("request = %+v", r)
	}
	// Swept today: not due, so tick makes none. A declaration older than
	// sources.reach_days no longer claims.
	if out, _ = Tick(ctx, d, TickInput{}); len(out.Sweeps) != 0 {
		t.Fatalf("tick after sweep = %+v", out.Sweeps)
	}
	now = now.Add(8 * 24 * time.Hour)
	if out, _ = Tick(ctx, d, TickInput{}); len(out.Sweeps) != 1 {
		t.Fatalf("tick a week on = %+v", out.Sweeps)
	}
	if next, _ = RequestNext.Run(ctx, agentDeps(d, "claude"), RequestNextInput{}); next.Request != nil {
		t.Fatal("a stale declaration claimed")
	}
}

func sourceFindings(t *testing.T, d Deps) map[string][]string {
	t.Helper()
	h, err := Health(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			if strings.HasPrefix(f.Kind, "source_") {
				out[f.Kind] = append(out[f.Kind], f.ID)
			}
		}
	}
	return out
}

func TestSourceHealth(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	d, _ := sourceClockDeps(t, &now)
	io := func(id, via string) {
		t.Helper()
		if _, err := d.Store.Create(ctx, model.Document{ID: id, Folder: model.FolderIO, Title: "Mail " + id,
			Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOIn, Medium: model.MediumEmail, State: model.IOUnread,
				Via: &model.IOVia{Source: via, Ref: "m1"}}}); err != nil {
			t.Fatal(err)
		}
	}
	io("io-before", "nowhere")
	if k := sourceFindings(t, d); len(k) != 0 {
		t.Fatalf("no sources, no findings: %v", k)
	}
	src := addGitHubSource(t, d)
	io("io-ok", src)
	io("io-system", "github")
	now = now.Add(3 * 24 * time.Hour)
	k := sourceFindings(t, d)
	if !slices.Equal(k[HealthSourceUnreached], []string{src}) || !slices.Equal(k[HealthSourceOverdue], []string{src}) || !slices.Equal(k[HealthSourceMissing], []string{"io-before"}) {
		t.Fatalf("findings = %v", k)
	}
	if _, err := AgentSources.Run(ctx, agentDeps(d, "claude"), AgentSourcesInput{Sources: []string{src}}); err != nil {
		t.Fatal(err)
	}
	if _, err := SweepRecord.Run(ctx, agentDeps(d, "claude"), SweepRecordInput{Source: src}); err != nil {
		t.Fatal(err)
	}
	if k = sourceFindings(t, d); len(k[HealthSourceUnreached])+len(k[HealthSourceOverdue]) != 0 {
		t.Fatalf("after declaring and sweeping = %v", k)
	}
	if _, err := SourceStatus.Run(ctx, d, SourceStatusInput{ID: src, Status: model.SourcePaused}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * 24 * time.Hour)
	if k = sourceFindings(t, d); len(k[HealthSourceUnreached])+len(k[HealthSourceOverdue]) != 0 {
		t.Fatalf("paused = %v", k)
	}
}

// Lint refuses a source with a secret in it, so source_add does too.
func TestSourceAddRefusesSecret(t *testing.T) {
	d := newDeps(t)
	_, err := SourceAdd.Run(ctx, d, SourceAddInput{Title: "Tracker", System: model.SourceGitHub, Via: model.SourceViaCLI,
		Fetch: &model.SourceStep{Command: "gh issue view {url} --token ghp_abcdefghijklmnopqrstuvwxyz0123"}})
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
	ex, err := SourceExample.Run(ctx, d, SourceExampleInput{System: model.SourceGmail})
	if err != nil || !strings.Contains(ex.Text, "type: source") || len(ex.Systems) != 8 {
		t.Fatalf("example = %+v %v", ex, err)
	}
	if _, err := SourceExample.Run(ctx, d, SourceExampleInput{System: "fax"}); err == nil {
		t.Fatal("no error for an unknown system")
	}
	for _, sys := range ex.Systems {
		if _, err := SourceAdd.Run(ctx, d, SourceAddInput{Title: "Example " + sys, Example: model.SourceSystem(sys)}); err != nil {
			t.Errorf("%s: %v", sys, err)
		}
	}
	mustLintClean(t, d)
}

func TestUnknownSourceHint(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	d, _ := sourceClockDeps(t, &now)
	claude := agentDeps(d, "claude")

	_, err := SweepRecord.Run(ctx, claude, SweepRecordInput{Source: "nowhere"})
	if err == nil || !errors.Is(err, model.ErrNotFound) || !strings.Contains(err.Error(), "this vault has no sources yet") || !strings.Contains(err.Error(), "source_add") {
		t.Fatalf("sweep_record in an empty vault: %v", err)
	}
	decl, err := AgentSources.Run(ctx, claude, AgentSourcesInput{Sources: []string{"nowhere"}})
	if err != nil || !strings.Contains(decl.Hint, "this vault has no sources yet") {
		t.Fatalf("agent_sources in an empty vault: %+v %v", decl, err)
	}

	src := addGitHubSource(t, d)
	_, err = SweepRecord.Run(ctx, claude, SweepRecordInput{Source: "nowhere"})
	if err == nil || strings.Contains(err.Error(), "no sources yet") || !strings.Contains(err.Error(), "source_list") {
		t.Fatalf("sweep_record with a source present: %v", err)
	}
	decl, err = AgentSources.Run(ctx, claude, AgentSourcesInput{Sources: []string{src}})
	if err != nil || decl.Hint != "" {
		t.Fatalf("agent_sources with no unknown id: %+v %v", decl, err)
	}
}
