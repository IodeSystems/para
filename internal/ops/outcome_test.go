package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestTaskResolveDeclinedFailedRerouted(t *testing.T) {
	d, proj := laterVault(t)
	add := func(title string, deps ...string) string {
		return must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: title, DependsOn: deps}).ID
	}
	ada := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"}).ID
	a := add("Ask for budget")

	// Required fields, and fields of another outcome, are refused.
	for name, in := range map[string]TaskResolveInput{
		"declined, no reason":      {ID: a, Outcome: model.OutcomeDeclined, DeclinedBy: "owner", Date: "2026-11-04"},
		"declined, no by":          {ID: a, Outcome: model.OutcomeDeclined, Reason: "No.", Date: "2026-11-04"},
		"declined, bad by":         {ID: a, Outcome: model.OutcomeDeclined, Reason: "No.", DeclinedBy: "boss", Date: "2026-11-04"},
		"declined, no source/date": {ID: a, Outcome: model.OutcomeDeclined, Reason: "No.", DeclinedBy: "other"},
		"declined, bad date":       {ID: a, Outcome: model.OutcomeDeclined, Reason: "No.", DeclinedBy: "other", Date: "today"},
		"declined, source missing": {ID: a, Outcome: model.OutcomeDeclined, Reason: "No.", DeclinedBy: "other", Source: "note-nope"},
		"declined, person a task":  {ID: a, Outcome: model.OutcomeDeclined, Reason: "No.", DeclinedBy: "other", DeclinedPerson: a, Date: "2026-11-04"},
		"failed, no tried":         {ID: a, Outcome: model.OutcomeFailed, StoppedBecause: "Locked out."},
		"failed, no stopped":       {ID: a, Outcome: model.OutcomeFailed, Tried: "Logged in."},
		"failed, bad retry":        {ID: a, Outcome: model.OutcomeFailed, Tried: "x", StoppedBecause: "y", RetryAfter: "later"},
		"rerouted, no to":          {ID: a, Outcome: model.OutcomeRerouted, Reason: "Theirs."},
		"rerouted, no reason":      {ID: a, Outcome: model.OutcomeRerouted, To: "https://jira.acme.example/browse/OPS-1"},
		"rerouted, to itself":      {ID: a, Outcome: model.OutcomeRerouted, To: a, Reason: "x"},
		"rerouted, to nothing":     {ID: a, Outcome: model.OutcomeRerouted, To: "task-nope", Reason: "x"},
		"rerouted, signed url":     {ID: a, Outcome: model.OutcomeRerouted, To: "https://jira.acme.example/x?token=abc", Reason: "x"},
		"reason on failed":         {ID: a, Outcome: model.OutcomeFailed, Tried: "x", StoppedBecause: "y", Reason: "z"},
		"to on dropped":            {ID: a, Outcome: model.OutcomeDropped, Note: "x", To: "https://jira.acme.example/browse/OPS-1"},
	} {
		if _, err := TaskResolve.Run(ctx, d, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if v, _ := taskOf(t, d, a); v.Status != model.TaskTodo {
		t.Fatalf("a refused resolve changed the task: %+v", v)
	}

	// declined: dependents are released; the Asks that hang on it alone close.
	w := add("Spend budget", a)
	only := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Budget sign-off", Route: "finance", Blocks: []string{a}}).ID
	after := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Tell finance the outcome", Route: "finance", DependsOn: []string{a}}).ID
	shared := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Shared ask", Route: "finance", Blocks: []string{a, w}}).ID
	o := must(t, TaskResolve, d, TaskResolveInput{ID: a, Outcome: model.OutcomeDeclined, Reason: "No budget this year.", DeclinedBy: "other", DeclinedPerson: ada, Date: "2026-11-03"})
	if o.Status != model.TaskDropped || o.Outcome != model.OutcomeDeclined || !slices.Contains(o.Released, w) {
		t.Fatalf("declined = %+v", o)
	}
	got := map[string]AskEffect{}
	for _, x := range o.Asks {
		got[x.ID] = x
	}
	if got[only].Action != askClosed || got[only].Relation != askBlocks || got[after].Action != askClosed || got[after].Relation != askDependsOn {
		t.Fatalf("asks = %+v", o.Asks)
	}
	if _, ok := got[shared]; ok {
		t.Fatalf("an Ask another open task waits on was touched: %+v", o.Asks)
	}
	if r := must(t, RFXShow, d, RFXShowInput{ID: only}); rfxStatus(r) != model.RFXClosed || !strings.Contains(r.Doc.Body, "No budget this year.") {
		t.Fatalf("closed ask = %+v", r)
	}
	v, body := taskOf(t, d, a)
	if v.DeclinedBy != model.DeclinedByOther || v.DeclinedPerson != ada || !strings.Contains(body, "Declined by other ([["+ada+"]]) on 2026-11-03: No budget this year.") {
		t.Fatalf("declined task = %+v\n%s", v, body)
	}

	// failed: findable by outcome; its Asks need a decision, none is closed.
	b := add("Renew the cert")
	ask := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Cert access", Route: "ops", Blocks: []string{b}}).ID
	o = must(t, TaskResolve, d, TaskResolveInput{ID: b, Outcome: model.OutcomeFailed, Tried: "Ran the renew script.", StoppedBecause: "The CA is down.", RetryAfter: "2026-11-01"})
	if len(o.Asks) != 1 || o.Asks[0].ID != ask || o.Asks[0].Action != askNeedsDecision || len(o.Warnings) == 0 {
		t.Fatalf("failed = %+v", o)
	}
	if r := must(t, RFXShow, d, RFXShowInput{ID: ask}); rfxStatus(r) != model.RFXOpen {
		t.Fatal("failed closed an Ask")
	}
	if l := must(t, TaskList, d, TaskListInput{Outcome: model.OutcomeFailed}); len(l.Tasks) != 1 || l.Tasks[0].ID != b || l.Tasks[0].RetryAfter != "2026-11-01" {
		t.Fatalf("task_list outcome=failed = %+v", l.Tasks)
	}

	// rerouted to a vault task: dependents move to it.
	c, there := add("Fix the build"), add("Platform fixes the build")
	wc := add("Release", c)
	o = must(t, TaskResolve, d, TaskResolveInput{ID: c, Outcome: model.OutcomeRerouted, To: there, Reason: "Platform owns CI."})
	if !slices.Equal(o.Moved, []string{wc}) || len(o.Released) != 0 {
		t.Fatalf("rerouted to a task = %+v", o)
	}
	if v, _ := taskOf(t, d, wc); !slices.Equal(v.DependsOn, []string{there}) {
		t.Fatalf("dependent = %v", v.DependsOn)
	}
	// rerouted to a URL: dependents are released, with a warning.
	e := add("Fix the portal")
	we := add("Demo the portal", e)
	o = must(t, TaskResolve, d, TaskResolveInput{ID: e, Outcome: model.OutcomeRerouted, To: "https://jira.acme.example/browse/OPS-7", Reason: "Ops tracks it."})
	if !slices.Equal(o.Released, []string{we}) || !slices.ContainsFunc(o.Warnings, func(s string) bool { return strings.Contains(s, "released") }) {
		t.Fatalf("rerouted to a URL = %+v", o)
	}
	if v, body := taskOf(t, d, e); v.ReroutedTo != "https://jira.acme.example/browse/OPS-7" || !strings.Contains(body, "Rerouted to https://jira.acme.example/browse/OPS-7: Ops tracks it.") {
		t.Fatalf("rerouted task = %+v\n%s", v, body)
	}

	// dropped now closes the Asks that hang on it only.
	f := add("Old plan")
	fa := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Old plan input", Route: "ops", Blocks: []string{f}}).ID
	if o := must(t, TaskResolve, d, TaskResolveInput{ID: f, Outcome: model.OutcomeDropped, Note: "Plan changed."}); len(o.Asks) != 1 || o.Asks[0].Action != askClosed {
		t.Fatalf("dropped asks = %+v", o.Asks)
	}
	if r := must(t, RFXShow, d, RFXShowInput{ID: fa}); rfxStatus(r) != model.RFXClosed {
		t.Fatal("dropped left its Ask open")
	}
	mustLintClean(t, d)
}

func rfxStatus(r RFXShowOutput) model.RFXStatus { return r.Doc.Meta.(*model.RFX).Status }

// A failed task with retry_after on or before today shows in health and agenda.
// A failed task is closed: health says it may be tried again, and the agenda
// (what needs the owner) does not list it.
func TestFailedRetryShowsInHealthOnly(t *testing.T) {
	d, proj := laterVault(t)
	id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Renew", DoneWhen: "Renewed."}).ID
	today := d.Store.Now().Format(dateLayout)
	must(t, TaskResolve, d, TaskResolveInput{ID: id, Outcome: model.OutcomeFailed, Tried: "Ran it.", StoppedBecause: "CA down.", RetryAfter: today})
	h := must(t, HealthOp, d, HealthInput{})
	found := false
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			found = found || (f.Kind == HealthRetryDue && f.ID == id)
		}
	}
	if !found {
		t.Fatalf("health has no retry_due for %s: %+v", id, h)
	}
	a := must(t, Agenda, d, AgendaInput{Days: 1})
	if slices.ContainsFunc(a.Items, func(it AgendaItem) bool { return it.ID == id }) {
		t.Fatalf("agenda lists a closed task: %+v", a.Items)
	}
}
