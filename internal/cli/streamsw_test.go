package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/registry"
)

// streamSWParityCases are the parity rows of sources and sweeps. Mutating
// ops use src-sw-a for Invoke and src-sw-b for the CLI.
func streamSWParityCases() map[string]parityCase {
	return map[string]parityCase{
		"source_add": {`{"title":"Chat one","example":"slack"}`,
			[]string{"source", "add", "--title", "Chat two", "--example", "slack"}, false},
		"source_list":    {`{}`, []string{"source", "list"}, false},
		"source_show":    {`{"id":"src-sw-c"}`, []string{"source", "show", "src-sw-c"}, false},
		"source_status":  {`{"id":"src-sw-a","status":"paused"}`, []string{"source", "status", "src-sw-b", "paused"}, false},
		"source_example": {`{"system":"github"}`, []string{"source", "example", "github"}, false},
		"agent_sources":  {`{"sources":["src-sw-c"]}`, []string{"agent", "sources", "src-sw-c"}, false},
		"sweep_plan":     {`{"sources":["src-sw-c"]}`, []string{"sweep", "plan", "src-sw-c"}, false},
		"sweep_record": {`{"source":"src-sw-a","cursor":"c1","counts":{"in":1}}`,
			[]string{"sweep", "record", "src-sw-b", "--cursor", "c1", "--count", "in=1"}, false},
		"sweep_report":  {`{"since":"2026-01-01"}`, []string{"sweep", "report", "--since", "2026-01-01"}, false},
		"io_stats":      {`{}`, []string{"io", "stats"}, false},
		"io_send_check": {`{"id":"io-sw-ready"}`, []string{"io", "send-check", "io-sw-ready"}, false},
	}
}

// seedStreamSWParity adds three github sources.
func seedStreamSWParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	for _, id := range []string{"src-sw-a", "src-sw-b", "src-sw-c"} {
		if _, err := ops.SourceAdd.Run(context.Background(), deps, ops.SourceAddInput{ID: id, Title: "Tracker " + id, Example: model.SourceGitHub}); err != nil {
			t.Fatal(err)
		}
	}
	// An approved draft: io_send_check passes it, and may run twice.
	if _, err := ops.OrgPersonAdd.Run(context.Background(), deps, ops.OrgAddInput{ID: "person-sw", Name: "Sam Sweep", Org: "acme.example"}); err != nil {
		t.Fatal(err)
	}
	draft, err := ops.IOAdd.Run(context.Background(), deps, ops.IOAddInput{Direction: model.IOOut, Text: "Sure.", To: []string{"person-sw"}, Expects: model.ExpectsNone})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.DocRename.Run(context.Background(), deps, ops.DocRenameInput{ID: draft.ID, NewID: "io-sw-ready"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.IOApprove.Run(context.Background(), deps, ops.IOApproveInput{ID: "io-sw-ready"}); err != nil {
		t.Fatal(err)
	}
}

// A source from an example, declared by an agent: the declaration is in
// $PARA_HOME, not the vault; the plan names it; the record moves the cursor.
func TestSourcesAndSweepFromTheCLI(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	if out := e.ok("source", "example", "gmail"); !strings.Contains(out, "type: source") || !strings.Contains(out, "via: mcp") {
		t.Fatalf("example:\n%s", out)
	}
	var added ops.SourceAddOutput
	e.json(&added, "source", "add", "--title", "Code reviews", "--example", "github", "--reached-by", "claude")
	if added.ID != "code-reviews" {
		t.Fatalf("added = %+v", added)
	}
	if _, err := os.Stat(filepath.Join(path, "sources", "code-reviews.md")); err != nil {
		t.Fatal(err)
	}
	e.ok("lint")
	var decl ops.AgentSourcesOutput
	e.json(&decl, "--actor", "claude", "agent", "sources", "code-reviews")
	if decl.Actor != "claude" || len(decl.Due) != 1 {
		t.Fatalf("declare = %+v", decl)
	}
	if _, err := os.Stat(filepath.Join(e.home, "vaults", "acme", "reach", "claude.json")); err != nil {
		t.Fatalf("declaration not in PARA_HOME: %v", err)
	}
	var plan ops.SweepPlanOutput
	e.json(&plan, "--actor", "claude", "sweep", "plan")
	if len(plan.Sources) != 1 || plan.Sources[0].ID != "code-reviews" {
		t.Fatalf("plan = %+v", plan)
	}
	var rec ops.SweepRecordOutput
	e.json(&rec, "--actor", "claude", "sweep", "record", "code-reviews", "--cursor", "page-2", "--count", "in=3")
	if rec.Cursor != "page-2" || rec.LastScan == "" {
		t.Fatalf("record = %+v", rec)
	}
	e.json(&plan, "--actor", "claude", "sweep", "plan", "code-reviews")
	if plan.Sources[0].Cursor != "page-2" || plan.Sources[0].Since != rec.LastScan {
		t.Fatalf("plan again = %+v", plan.Sources[0])
	}
	if out := e.ok("source", "list"); !strings.Contains(out, "code-reviews\tactive\ttracker/github\tcli\tdaily") || !strings.Contains(out, "claude") {
		t.Fatalf("list = %q", out)
	}
	e.ok("lint")
}

// para doctor finds a via cli source's commands on PATH, and does not check
// a via mcp source.
func TestDoctorChecksSourceCommands(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.ok("source", "add", "--title", "Code reviews", "--example", "github")
	e.ok("source", "add", "--title", "Work mailbox", "--example", "gmail")
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	out, _, _ := e.run("doctor")
	if !strings.Contains(out, "warn  vault acme source code-reviews: via cli: not on PATH here: gh") {
		t.Fatalf("doctor without gh:\n%s", out)
	}
	if !strings.Contains(out, "ok    vault acme source work-mailbox: via mcp (server gmail): agent-only, not checked here") {
		t.Fatalf("doctor mcp:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, _, _ = e.run("doctor")
	if !strings.Contains(out, "ok    vault acme source code-reviews: via cli: "+filepath.Join(bin, "gh")) {
		t.Fatalf("doctor with gh:\n%s", out)
	}
}

// para tick queues one sweep for a due source, and only the agent that
// declared the source claims it.
func TestTickQueuesSweepFromTheCLI(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	id, err := registry.Install{Home: e.home}.ID()
	if err != nil {
		t.Fatal(err)
	}
	e.ok("vault", "config", "set", "request.runner", id)
	e.ok("vault", "config", "set", "request.surfaces", "cli,pwa,tick")
	e.ok("source", "add", "--title", "Code reviews", "--example", "github")
	if out := e.ok("tick"); !strings.Contains(out, "ran") {
		t.Fatalf("tick = %q", out)
	}
	e.ok("tick")
	var list ops.RequestListOutput
	e.json(&list, "request", "list")
	if len(list.Items) != 1 || list.Items[0].RequiresSource != "code-reviews" || list.Items[0].Kind != ops.SweepKind {
		t.Fatalf("requests = %+v", list.Items)
	}
	var next ops.RequestNextOutput
	e.json(&next, "--actor", "codex", "request", "next")
	if next.Request != nil {
		t.Fatal("an agent that did not declare the source claimed it")
	}
	e.ok("--actor", "claude", "agent", "sources", "code-reviews")
	e.json(&next, "--actor", "claude", "request", "next")
	if next.Request == nil || next.Request.RequiresSource != "code-reviews" {
		t.Fatalf("claim = %+v", next)
	}
}
