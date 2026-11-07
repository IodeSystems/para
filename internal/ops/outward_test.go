package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func unreviewedFindings(t *testing.T, d Deps) []string {
	t.Helper()
	h, err := Health(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			if f.Kind == HealthUnreviewedOutward {
				out = append(out, f.Message)
			}
		}
	}
	return out
}

// TestUnreviewedOutward: a sweep reports what went out; an act with no
// approved item and no record is a health finding until one records it.
func TestUnreviewedOutward(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	d, _ := sourceClockDeps(t, &now)
	src := addGitHubSource(t, d)
	claude := agentDeps(d, "claude")
	if _, err := SweepRecord.Run(ctx, claude, SweepRecordInput{Source: src, Outward: []SweepOutward{{URL: "javascript:x", What: "x"}}}); err == nil {
		t.Fatal("a bad url was kept")
	}
	if _, err := SweepRecord.Run(ctx, claude, SweepRecordInput{Source: src, Outward: []SweepOutward{{URL: "https://github.example/acme/portal/pull/7#c1"}}}); err == nil {
		t.Fatal("an act with no what was kept")
	}
	acts := []SweepOutward{
		{URL: "https://github.example/acme/portal/pull/7#c1", What: "reply to a review comment", At: "2026-11-04T09:00:00Z"},
		{URL: "https://github.example/acme/portal/commit/abc", What: "commit on main"},
	}
	rec, err := SweepRecord.Run(ctx, claude, SweepRecordInput{Source: src, Outward: acts, Counts: map[string]int{"out": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Unreviewed) != 2 {
		t.Fatalf("unreviewed = %v", rec.Unreviewed)
	}
	if f := unreviewedFindings(t, d); len(f) != 2 || !strings.Contains(f[0]+f[1], "reply to a review comment") {
		t.Fatalf("findings = %v", f)
	}
	// The manager records the worker's commit as a sent action: one finding left.
	act, err := IOAdd.Run(ctx, claude, IOAddInput{Direction: model.IOOut, Text: "Committed the fix", To: []string{"github.example/acme/portal"},
		Medium: model.MediumGithub, Expects: model.ExpectsNone})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := IOSend.Run(ctx, claude, IOSendInput{ID: act.ID, URL: "https://github.example/acme/portal/commit/abc/", Observed: true}); err != nil {
		t.Fatal(err)
	}
	f := unreviewedFindings(t, d)
	if len(f) != 1 || !strings.Contains(f[0], "pull/7#c1") {
		t.Fatalf("after the record = %v", f)
	}
	// A later sweep keeps the acts, with no repeats, and the last sweep line.
	rec, err = SweepRecord.Run(ctx, claude, SweepRecordInput{Source: src, Outward: acts[:1]})
	if err != nil || !slices.Equal(rec.Unreviewed, []string{acts[0].URL}) {
		t.Fatalf("again = %+v %v", rec, err)
	}
	doc, _ := d.Store.Get(ctx, src)
	if strings.Count(doc.Body, "pull/7#c1") != 1 || strings.Count(doc.Body, "## Last sweep") != 1 || !strings.HasSuffix(strings.TrimSpace(doc.Body), "by claude") {
		t.Fatalf("body = %q", doc.Body)
	}
}
