package ops

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

func f64(v float64) *float64 { return &v }

// KPIs on a project: define, read with a source, surface in the bundle (header
// alerts, a section of at most 8 lines and 600 B), health (off target by
// direction, stale by cadence), the list row count, and tick's archive of
// readings past the last 12.
func TestProjectKPIs(t *testing.T) {
	tv, d := cronVault(t, "2026-11-01T09:00:00Z")
	agent := tv.open("ia", "mcp")
	format11(t, agent)
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Shop", StateLine: "Open."})

	must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: "Signups", Unit: "users", Target: f64(50), Direction: model.KPIUp, Cadence: "weekly"})
	must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: "Refunds", Target: f64(2), Direction: model.KPIDown})
	must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: "Stock", Target: f64(100), Direction: model.KPIHold, Tolerance: 10, Cadence: "daily"})
	if _, err := KPIAdd.Run(ctx, d, KPIAddInput{ID: p.ID, Name: "signups", Target: f64(1)}); err == nil {
		t.Fatal("a second KPI of the same name")
	}
	if _, err := KPIAdd.Run(ctx, d, KPIAddInput{ID: p.ID, Name: "Bad", Target: f64(1), Cadence: "sometimes"}); err == nil {
		t.Fatal("a cadence outside the recur grammar")
	}
	if _, err := KPIAdd.Run(ctx, d, KPIAddInput{ID: p.ID, Name: "Bad", Target: f64(1), Direction: "sideways"}); err == nil {
		t.Fatal("an unknown direction")
	}

	// A reading needs a value and a source; an unknown KPI names the defined ones.
	if _, err := KPIReadingAdd.Run(ctx, agent, KPIReadingAddInput{ID: p.ID, Name: "Signups", Value: f64(40)}); err == nil {
		t.Fatal("a reading with no source")
	}
	if _, err := KPIReadingAdd.Run(ctx, agent, KPIReadingAddInput{ID: p.ID, Name: "Visits", Value: f64(1), Source: "s"}); err == nil || !strings.Contains(err.Error(), "Signups") {
		t.Fatalf("unknown KPI: %v", err)
	}
	must(t, KPIReadingAdd, agent, KPIReadingAddInput{ID: p.ID, Name: "Signups", Value: f64(55), Source: "https://stats.acme.example/a", Date: "2026-10-30"})
	r := must(t, KPIReadingAdd, agent, KPIReadingAddInput{ID: p.ID, Name: "signups", Value: f64(42), Source: "https://stats.acme.example/b"})
	if r.Date != "2026-11-01" || r.Readings != 2 || r.Line != "Signups: 42 users ↓ vs >= 50; 2026-11-01 (0d); off" {
		t.Fatalf("reading = %+v", r)
	}
	must(t, KPIReadingAdd, d, KPIReadingAddInput{ID: p.ID, Name: "Refunds", Value: f64(1), Source: "owner in chat, 2026-11-01"})
	must(t, KPIReadingAdd, d, KPIReadingAddInput{ID: p.ID, Name: "Stock", Value: f64(95), Source: "inventory.csv"})

	// Day 3: Stock (daily) is stale; Signups is off; Refunds is on.
	tv.at("2026-11-03T09:00:00Z")
	sh := must(t, ProjectShow, d, ProjectShowInput{ID: p.ID})
	if want := []string{"Signups off", "Stock stale"}; !slices.Equal(sh.KPIAlerts, want) {
		t.Fatalf("alerts = %v, want %v", sh.KPIAlerts, want)
	}
	want := []string{
		"Signups: 42 users ↓ vs >= 50; 2026-11-01 (2d); off",
		"Stock: 95 vs = 100 ± 10; 2026-11-01 (2d); stale",
		"Refunds: 1 vs <= 2; 2026-11-01 (2d); on",
	}
	if !slices.Equal(sh.KPIs, want) {
		t.Fatalf("kpis =\n%s\nwant\n%s", strings.Join(sh.KPIs, "\n"), strings.Join(want, "\n"))
	}
	h := must(t, HealthOp, d, HealthInput{})
	kinds := map[string][]string{}
	for _, gr := range h.Groups {
		for _, f := range gr.Findings {
			kinds[f.Kind] = append(kinds[f.Kind], f.Message)
		}
	}
	if len(kinds[HealthKPIOffTarget]) != 1 || !strings.HasPrefix(kinds[HealthKPIOffTarget][0], "Signups is 42 users vs >= 50") ||
		len(kinds[HealthKPIStale]) != 1 || !strings.HasPrefix(kinds[HealthKPIStale][0], "Stock:") {
		t.Fatalf("health = off %v stale %v", kinds[HealthKPIOffTarget], kinds[HealthKPIStale])
	}
	ls := must(t, List, d, ListInput{Type: model.TypeProject})
	if len(ls.Rows) != 1 || ls.Rows[0].Project == nil || ls.Rows[0].Project.KPIAlerts != 2 {
		t.Fatalf("row = %+v", ls.Rows)
	}
	kl := must(t, KPIList, agent, KPIListInput{ID: p.ID})
	if len(kl.KPIs) != 3 || !kl.KPIs[0].Off || kl.KPIs[1].Due != "2026-11-02" || len(kl.KPIs[0].KPI.Readings) != 2 {
		t.Fatalf("kpi_list = %+v", kl.KPIs)
	}

	// edit {fields: {kpis}} goes through the same rules: a reading needs a source.
	if _, err := Edit.Invoke(writeCtx(t), d, json.RawMessage(`{"id":"`+p.ID+`","fields":{"kpis":[{"name":"X","target":1,"direction":"up","readings":[{"date":"2026-11-01","value":1}]}]}}`)); err == nil {
		t.Fatal("edit: a reading with no source")
	}

	// Tick keeps the last 12 readings of a KPI; older ones go to the
	// project's archive of their year, once.
	for i := range 14 {
		date := fmt.Sprintf("2026-11-%02d", 3+i)
		if i == 0 {
			date = "2025-12-31"
		}
		must(t, KPIReadingAdd, agent, KPIReadingAddInput{ID: p.ID, Name: "Refunds", Value: f64(float64(i)), Source: "ledger", Date: date})
	}
	out := tick(t, d)
	if len(out.KPIArchived) != 1 || !strings.Contains(out.KPIArchived[0], "3 readings to "+p.ID+"-kpis-2025, "+p.ID+"-kpis-2026") {
		t.Fatalf("tick = %v %v", out.KPIArchived, out.Warnings)
	}
	doc, _ := d.Store.Get(ctx, p.ID)
	if ks := doc.Meta.(*model.Project).KPIs; len(ks[1].Readings) != model.KPIReadingsKept || ks[1].Readings[0].Date != "2026-11-05" || len(ks[0].Readings) != 2 {
		t.Fatalf("kept = %+v", ks[1].Readings)
	}
	a, err := d.Store.Get(ctx, p.ID+"-kpis-2026")
	if err != nil || a.Folder != model.FolderArchive || strings.Count(a.Body, "| Refunds |") != 2 || !strings.Contains(a.Body, "| Refunds | 2026-11-01 | 1 | owner in chat, 2026-11-01 |") {
		t.Fatalf("archive = %v %s %q", err, a.Folder, a.Body)
	}
	if out := tick(t, d); len(out.KPIArchived) != 0 {
		t.Fatalf("second pass = %v", out.KPIArchived)
	}
	// The archive is hidden: not in lists, not in the bundle.
	if refs, _ := d.Store.List(ctx, store.Filter{Type: model.TypeNote}); slices.ContainsFunc(refs, func(r model.Ref) bool { return strings.Contains(r.ID, "-kpis-") }) {
		t.Fatal("archive listed")
	}
	if vs, err := d.Store.Lint(ctx); err != nil || len(vs) != 0 {
		t.Fatalf("lint = %v %+v", err, vs)
	}
}

// The KPI section stays within 8 lines and 600 B, cut with a more line, and
// the bundle within its 8000 B cap; the header lists at most 8 alerts.
func TestProjectKPICaps(t *testing.T) {
	_, d := cronVault(t, "2026-11-01T09:00:00Z")
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Shop", StateLine: "Open."})
	for i := range 11 {
		name := fmt.Sprintf("Metric number %02d with a long name to fill the line", i)
		must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: name, Unit: "widgets", Target: f64(1000), Cadence: "daily"})
		must(t, KPIReadingAdd, d, KPIReadingAddInput{ID: p.ID, Name: name, Value: f64(float64(i)), Source: "https://stats.acme.example/" + name})
	}
	sh := must(t, ProjectShow, d, ProjectShowInput{ID: p.ID})
	b, _ := json.Marshal(sh.KPIs)
	if len(sh.KPIs) == 0 || len(sh.KPIs) > kpiLinesMax || len(b) > kpiBytesMax {
		t.Fatalf("kpis: %d lines, %d bytes", len(sh.KPIs), len(b))
	}
	if !slices.ContainsFunc(sh.More, func(m string) bool {
		return m == fmt.Sprintf("kpis: %d more: op_run {op: kpi_list, input: {id: %s}}", 11-len(sh.KPIs), p.ID)
	}) {
		t.Fatalf("more = %v", sh.More)
	}
	if len(sh.KPIAlerts) != kpiLinesMax+1 || sh.KPIAlerts[kpiLinesMax] != "+3 more" {
		t.Fatalf("alerts = %v", sh.KPIAlerts)
	}
	if n := bundleSize(&sh); n > projectBundleMax {
		t.Fatalf("bundle %d bytes", n)
	}
	t.Logf("kpis (%d B):\n%s", len(b), strings.Join(sh.KPIs, "\n"))
}
