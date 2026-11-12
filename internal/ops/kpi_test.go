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

// A KPI owner: self makes one Next row per KPI while it is off target or
// stale, gone when a reading brings it on target; agent:<name> makes agent
// work at tick, one open request per KPI and one per alert; a person is shown
// on the project and in health only.
func TestKPIOwners(t *testing.T) {
	tv, d := cronVault(t, "2026-11-01T09:00:00Z")
	if _, err := d.Store.Create(ctx, model.Document{ID: "person-ada", Folder: model.FolderOrg, Title: "Ada Example",
		Meta: &model.Person{Common: model.Common{Type: model.TypePerson}}}); err != nil {
		t.Fatal(err)
	}
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Shop", StateLine: "Open."})
	for _, c := range []struct{ owner, want string }{{"sometimes later", "want self"}, {"agent:Bad Name", "agent:<name>"}, {"person-nobody", "person-nobody"}, {p.ID, "is a project"}} {
		if _, err := KPIAdd.Run(ctx, d, KPIAddInput{ID: p.ID, Name: "Bad", Target: f64(1), Owner: c.owner}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("owner %q: %v", c.owner, err)
		}
	}
	must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: "Signups", Target: f64(50), Owner: "Self"})
	must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: "Refunds", Target: f64(2), Direction: model.KPIDown, Owner: "agent:Stats-Bot"})
	must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: "Stock", Target: f64(100), Owner: "person-ada"})
	must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: "Visits", Target: f64(10)})
	if _, err := Edit.Invoke(writeCtx(t), d, json.RawMessage(`{"id":"`+p.ID+`","fields":{"kpis":[{"name":"X","target":1,"direction":"up","owner":"person-nobody"}]}}`)); err == nil {
		t.Fatal("edit kpis with an owner that is no person")
	}
	kpiRows := func() []ValueRow {
		t.Helper()
		o, err := Order(ctx, d.Store)
		if err != nil {
			t.Fatal(err)
		}
		var out []ValueRow
		for _, r := range o.Rows {
			if r.Kind == KindKPI {
				out = append(out, r)
			}
		}
		return out
	}
	if rows := kpiRows(); len(rows) != 0 {
		t.Fatalf("rows with no reading = %+v", rows)
	}

	// Two off readings of Signups: one row; every KPI off.
	read := func(name string, v float64) {
		t.Helper()
		must(t, KPIReadingAdd, d, KPIReadingAddInput{ID: p.ID, Name: name, Value: f64(v), Source: "owner in chat, 2026-11-01"})
	}
	read("Signups", 40)
	read("Signups", 45)
	read("Refunds", 5)
	read("Stock", 10)
	read("Visits", 1)
	rows := kpiRows()
	if len(rows) != 1 || rows[0].ID != "kpi:"+p.ID+"/signups" || !strings.Contains(rows[0].Title, "Signups off") || rows[0].Bucket != "now" {
		t.Fatalf("rows = %+v", rows)
	}

	// Person owner: in health and the bundle line, not in Next or agent work.
	h := must(t, HealthOp, d, HealthInput{})
	var msgs []string
	for _, gr := range h.Groups {
		for _, f := range gr.Findings {
			if f.Kind == HealthKPIOffTarget {
				msgs = append(msgs, f.Message)
			}
		}
	}
	if !slices.ContainsFunc(msgs, func(m string) bool {
		return strings.HasPrefix(m, "Stock") && strings.HasSuffix(m, "; owner person-ada")
	}) {
		t.Fatalf("health = %v", msgs)
	}
	sh := must(t, ProjectShow, d, ProjectShowInput{ID: p.ID})
	if !slices.ContainsFunc(sh.KPIs, func(l string) bool {
		return strings.HasPrefix(l, "Stock:") && strings.HasSuffix(l, "; owner person-ada")
	}) {
		t.Fatalf("bundle kpis = %v", sh.KPIs)
	}

	// Agent owner: tick files one request, names the agent, and passes lint.
	out := tick(t, d)
	if len(out.KPIAlerts) != 1 {
		t.Fatalf("tick kpi alerts = %v warnings %v", out.KPIAlerts, out.Warnings)
	}
	req, err := d.Store.Get(ctx, out.KPIAlerts[0])
	if err != nil {
		t.Fatal(err)
	}
	if r := req.Meta.(*model.Request); r.Kind != KPIAlertKind || len(r.Links) != 1 || r.Links[0].Target() != p.ID || !strings.Contains(req.Body, "agent:stats-bot") {
		t.Fatalf("request = %+v %q", r, req.Body)
	}
	if finds, _ := d.Store.Lint(ctx); len(finds) != 0 {
		t.Fatalf("lint = %+v", finds)
	}
	// Open: a second tick and a new off reading file none.
	read("Refunds", 6)
	if out := tick(t, d); len(out.KPIAlerts) != 0 {
		t.Fatalf("second tick = %v", out.KPIAlerts)
	}
	// Ended with the KPI still off at the same reading: none; a new off reading: one.
	must(t, RequestCancel, d, RequestCancelInput{ID: req.ID})
	if out := tick(t, d); len(out.KPIAlerts) != 1 {
		t.Fatalf("after cancel, new reading since filing: %v", out.KPIAlerts)
	}
	again := tick(t, d)
	if len(again.KPIAlerts) != 0 {
		t.Fatalf("third tick = %v", again.KPIAlerts)
	}

	// A reading on target takes the owner's row away.
	read("Signups", 60)
	if rows := kpiRows(); len(rows) != 0 {
		t.Fatalf("rows on target = %+v", rows)
	}
	// Stale with no reading in its cadence: the row comes back, once.
	if _, err := Edit.Invoke(writeCtx(t), d, json.RawMessage(`{"id":"`+p.ID+`","fields":{"kpis":[{"name":"Signups","target":50,"direction":"up","cadence":"daily","owner":"self","readings":[{"date":"2026-11-01","value":60,"source":"owner in chat, 2026-11-01"}]}]}}`)); err != nil {
		t.Fatal(err)
	}
	tv.at("2026-11-04T09:00:00Z")
	if rows := kpiRows(); len(rows) != 1 || !strings.Contains(rows[0].Title, "stale") {
		t.Fatalf("stale rows = %+v", rows)
	}
}

func TestKPIReadingOnTargetClosesAlert(t *testing.T) {
	ctx := t.Context()
	_, d := cronVault(t, "2026-11-01T09:00:00Z")
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Shop", StateLine: "Open."})
	must(t, KPIAdd, d, KPIAddInput{ID: p.ID, Name: "Refunds", Target: f64(2), Direction: model.KPIDown, Owner: "agent:stats-bot"})
	read := func(v float64) KPIReadingAddOutput {
		t.Helper()
		return must(t, KPIReadingAdd, d, KPIReadingAddInput{ID: p.ID, Name: "Refunds", Value: f64(v), Source: "owner in chat, 2026-11-01"})
	}
	read(5)
	out := tick(t, d)
	if len(out.KPIAlerts) != 1 {
		t.Fatalf("alerts = %v", out.KPIAlerts)
	}
	// Still off: the request stays open.
	read(4)
	req, err := d.Store.Get(ctx, out.KPIAlerts[0])
	if err != nil {
		t.Fatal(err)
	}
	if r := req.Meta.(*model.Request); r.Status != model.RequestQueued {
		t.Fatalf("status after an off reading = %s", r.Status)
	}
	// Back on target: closed in the same call as the reading.
	line := read(1).Line
	req, err = d.Store.Get(ctx, out.KPIAlerts[0])
	if err != nil {
		t.Fatal(err)
	}
	r := req.Meta.(*model.Request)
	if r.Status != model.RequestDone || r.Resolution != model.ResolutionDone || r.Seen == "" || requestOpen(r) {
		t.Fatalf("request = %+v", r)
	}
	if !strings.Contains(req.Body, "back on target: "+line) {
		t.Fatalf("body = %q, want reason with %q", req.Body, line)
	}
	if again := tick(t, d); len(again.KPIAlerts) != 0 {
		t.Fatalf("tick after close = %v", again.KPIAlerts)
	}
	// Off again: a new alert.
	read(9)
	if again := tick(t, d); len(again.KPIAlerts) != 1 {
		t.Fatalf("tick after a new off reading = %v", again.KPIAlerts)
	}
}

func TestProjectFlagRefusesGoal(t *testing.T) {
	ctx := t.Context()
	_, d := cronVault(t, "2026-11-01T09:00:00Z")
	// An id that does not look like a goal: the type decides, not the prefix.
	g := must(t, GoalAdd, d, GoalAddInput{ID: "project-launch", Title: "Ship it", DoneWhen: "shipped"})
	_, err := NoteAdd.Run(ctx, d, NoteAddInput{Title: "n", Project: g.ID})
	if err == nil || !strings.Contains(err.Error(), g.ID+" is a goal: use --goal") {
		t.Fatalf("err = %v", err)
	}
}
