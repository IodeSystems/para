package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

// streamTDParityCases are the parity rows of tracked documents. Mutating ops
// use doc-td-a for Invoke and doc-td-b for the CLI.
func streamTDParityCases() (map[string]parityCase, map[string]string) {
	cases := map[string]parityCase{
		"doc_track":           {`{"url":"https://wiki.acme.example/td/new","text":"New page."}`, []string{"doc", "track", "--url", "https://wiki.acme.example/td/new2"}, false},
		"doc_version_add":     {`{"id":"doc-td-a","text":"One.\nTwo, changed."}`, []string{"doc", "version", "add", "doc-td-b"}, false},
		"doc_tracked":         {`{}`, []string{"doc", "tracked"}, false},
		"doc_tracked_show":    {`{"id":"doc-td-c"}`, []string{"doc", "tracked", "show", "doc-td-c"}, true},
		"doc_tracked_status":  {`{"id":"doc-td-a","status":"paused"}`, []string{"doc", "tracked", "status", "doc-td-b", "paused"}, false},
		"doc_diff":            {`{"id":"doc-td-c"}`, []string{"doc", "diff", "doc-td-c"}, true},
		"doc_seen":            {`{"id":"doc-td-a"}`, []string{"doc", "seen", "doc-td-b"}, false},
		"doc_comment_add":     {`{"id":"doc-td-a","by":"Ada","text":"Why?","quote":"One."}`, []string{"doc", "comment", "add", "doc-td-b", "--by", "Ada", "--text", "Why?", "--quote", "One."}, false},
		"doc_comments":        {`{"id":"doc-td-c"}`, []string{"doc", "comments", "doc-td-c"}, true},
		"doc_comment_resolve": {`{"id":"doc-td-a","comment":"c1","resolution":"resolved"}`, []string{"doc", "comment", "resolve", "doc-td-b", "c1", "resolved"}, false},
		"doc_comment_file":    {`{"id":"doc-td-a","comment":"c2","to":"question"}`, []string{"doc", "comment", "file", "doc-td-b", "c2", "question"}, false},
	}
	cli := map[string]string{
		"doc_track":       "New page, from the CLI.",
		"doc_version_add": "One.\nTwo, from the CLI.",
	}
	return cases, cli
}

// seedStreamTDParity tracks three documents, each with two versions and two
// open comments.
func seedStreamTDParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []string{"doc-td-a", "doc-td-b", "doc-td-c"} {
		if _, err := ops.DocTrack.Run(ctx, deps, ops.DocTrackInput{ID: id, URL: "https://wiki.acme.example/td/" + id, Text: "One.\nTwo.", Goal: "project-a"}); err != nil {
			t.Fatal(err)
		}
		if _, err := ops.DocVersionAdd.Run(ctx, deps, ops.DocVersionAddInput{ID: id, Text: "One.\nTwo.\nThree."}); err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{"One.", "Two."} {
			if _, err := ops.DocCommentAdd.Run(ctx, deps, ops.DocCommentAddInput{ID: id, By: "Ada", Text: "About " + q, Quote: q, At: "2026-11-01T09:00:00Z"}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestTrackDocFromTheCLI: text on stdin is version 1; the same text again
// stores nothing; a changed text is version 2 with a diff; seen clears it.
func TestTrackDocFromTheCLI(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.stdin = "# Runbook\n\nStep one.\nStep two.\n"
	var tracked ops.DocTrackOutput
	e.json(&tracked, "doc", "track", "--url", "https://wiki.acme.example/rb", "--system", "confluence", "--id", "runbook")
	if tracked.ID != "runbook" || tracked.Version != 1 {
		t.Fatalf("track = %+v", tracked)
	}
	var v ops.DocVersionAddOutput
	e.stdin = "# Runbook\n\nStep one.\nStep two.\n"
	e.json(&v, "doc", "version", "add", "runbook")
	if v.Result != ops.VersionUnchanged || v.Version != 1 {
		t.Fatalf("same text = %+v", v)
	}
	e.stdin = "# Runbook\n\nStep one.\nStep two, then wait.\n"
	e.json(&v, "doc", "version", "add", "runbook")
	if v.Result != ops.VersionAdded || v.Version != 2 || v.Unseen != 2 {
		t.Fatalf("changed = %+v", v)
	}
	out := e.ok("doc", "diff", "runbook", "--from", "1")
	if !strings.Contains(out, "- Step two.") || !strings.Contains(out, "+ Step two, then wait.") {
		t.Fatalf("diff:\n%s", out)
	}
	var seen ops.DocSeenOutput
	e.json(&seen, "doc", "seen", "runbook")
	if seen.LastSeen != 2 || seen.Unseen != 0 {
		t.Fatalf("seen = %+v", seen)
	}
	// An agent cannot mark it seen.
	if _, _, err := e.run("--actor", "claude", "--why", "x", "doc", "seen", "runbook"); err == nil || !strings.Contains(err.Error(), "only a person") {
		t.Fatalf("agent seen: %v", err)
	}
	var show ops.DocTrackedShowOutput
	e.json(&show, "doc", "tracked", "show", "runbook", "--version", "1")
	if len(show.Versions) != 2 || show.Text != "## Runbook\n\nStep one.\nStep two." || !show.Versions[1].Seen {
		raw, _ := json.Marshal(show)
		t.Fatalf("show = %s", raw)
	}
	e.ok("lint")
}
