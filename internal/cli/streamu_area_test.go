package cli

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// TestAreaShowsIncidentsAndRecurring: `where` lists the open incidents of an
// area, and `area show` lists its incidents (open first) and recurring tasks.
func TestAreaShowsIncidentsAndRecurring(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area, other ops.AreaAddOutput
	e.json(&area, "area", "add", "Atlas service")
	e.json(&other, "area", "add", "Billing service")
	var minor, major, elsewhere ops.IncidentAddOutput
	e.json(&minor, "incident", "add", "Slow search", "--severity", "sev3", "--area", area.ID)
	e.json(&major, "incident", "add", "Atlas down", "--severity", "sev1", "--area", area.ID)
	e.json(&elsewhere, "incident", "add", "Invoices late", "--severity", "sev2", "--area", other.ID)
	var rec, plain ops.TaskAddOutput
	e.json(&rec, "task", "add", "Test the restore", "--area", area.ID, "--due", "2026-12-01", "--recur", "monthly")
	e.json(&plain, "task", "add", "Tidy dashboards", "--area", area.ID)

	incidents := func(m ops.WhereMatch) []string {
		var ids []string
		for _, v := range m.Incidents {
			ids = append(ids, v.ID)
		}
		return ids
	}
	whereArea := func(query string) ops.WhereMatch {
		t.Helper()
		var w ops.WhereOutput
		e.json(&w, "where", query)
		for _, m := range w.Matches {
			if m.ID == area.ID {
				return m
			}
		}
		t.Fatalf("where %q: no match for %s in %+v", query, area.ID, w)
		return ops.WhereMatch{}
	}
	m := whereArea("atlas")
	if got := incidents(m); len(got) != 2 || got[0] != major.ID || got[1] != minor.ID {
		t.Fatalf("where incidents = %v, want %s then %s", got, major.ID, minor.ID)
	}
	if out := e.ok("where", "atlas"); !strings.Contains(out, "incident\t"+major.ID+"\tsev1\topen\tAtlas down") {
		t.Fatalf("where text:\n%s", out)
	}
	if got := incidents(whereArea("service")); len(got) != 2 {
		t.Fatalf("incidents of another area leaked: %v", got)
	}

	// A resolved incident leaves `where` and stays in `area show`.
	e.ok("incident", "status", major.ID, "resolved")
	if got := incidents(whereArea("atlas")); len(got) != 1 || got[0] != minor.ID {
		t.Fatalf("where after resolve = %v", got)
	}
	var as ops.AreaShowOutput
	e.json(&as, "area", "show", area.ID)
	if len(as.Incidents) != 2 || as.Incidents[0].ID != minor.ID || as.Incidents[1].Status != model.IncidentResolved {
		t.Fatalf("area show incidents = %+v", as.Incidents)
	}
	if len(as.Recurring) != 1 || as.Recurring[0].ID != rec.ID || len(as.Tasks) != 2 {
		t.Fatalf("area show recurring = %+v, tasks %d", as.Recurring, len(as.Tasks))
	}
	text := e.ok("area", "show", area.ID)
	for _, want := range []string{"incidents\n  " + minor.ID, "recurring\n  " + rec.ID + "\tmonthly\t2026-12-01\tself\tTest the restore"} {
		if !strings.Contains(text, want) {
			t.Errorf("area show lacks %q:\n%s", want, text)
		}
	}

	// A finished recurring task leaves the list; its successor takes its place.
	var done ops.TaskDoneOutput
	e.json(&done, "task", "done", rec.ID)
	as = ops.AreaShowOutput{}
	e.json(&as, "area", "show", area.ID)
	if len(as.Recurring) != 1 || as.Recurring[0].ID != done.Next {
		t.Fatalf("recurring after done = %+v, next %s", as.Recurring, done.Next)
	}
}
