package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func grillDeps(t *testing.T) Deps {
	t.Helper()
	return newDepsAt(t, time.Date(2026, 11, 4, 12, 0, 0, 0, time.Local), "m1")
}

func seedDoc(t *testing.T, d Deps, doc model.Document) model.Document {
	t.Helper()
	saved, err := d.Store.Create(ctx, doc)
	if err != nil {
		t.Fatalf("seed %s: %v", doc.ID, err)
	}
	return saved
}

func constraintDoc(id, review string, appliesIn ...string) model.Document {
	return model.Document{ID: id, Folder: model.FolderConstraints, Title: "Rule " + id,
		Meta: &model.Constraint{Common: model.Common{Type: model.TypeConstraint}, Why: "audit scope", By: "Jordan Example",
			Source:    model.Source{URL: "https://wiki.acme.example/" + id, Retrieved: "2026-01-01", Fidelity: model.FidelityNone},
			AppliesIn: appliesIn, NotIn: []string{}, ReviewBy: model.Date(review)}}
}

func healthKinds(t *testing.T, d Deps, id string) []string {
	t.Helper()
	out, err := Health(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, g := range out.Groups {
		for _, f := range g.Findings {
			if f.ID == id {
				kinds = append(kinds, f.Kind)
			}
		}
	}
	return kinds
}

// TestGoalRiskSet: add, update by text, remove; a new risk needs a source; the
// goal stays lint clean; health flags a high impact with no mitigation.
func TestGoalRiskSet(t *testing.T) {
	d := grillDeps(t)
	g := must(t, GoalAdd, d, GoalAddInput{Title: "Launch", Owner: "Jordan Example", DoneWhen: "Live."})
	if _, err := GoalRiskSet.Run(ctx, d, GoalRiskSetInput{ID: g.ID, Text: "Vendor slips"}); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("no source: %v", err)
	}
	if _, err := GoalRiskSet.Run(ctx, d, GoalRiskSetInput{ID: g.ID, Text: "Vendor slips", Impact: "huge", Source: "chat"}); err == nil {
		t.Fatal("bad impact accepted")
	}
	must(t, GoalRiskSet, d, GoalRiskSetInput{ID: g.ID, Text: "Vendor slips", Likelihood: model.LevelMedium, Impact: model.LevelHigh, Source: "owner in chat"})
	if !slices.Contains(healthKinds(t, d, g.ID), HealthRiskUnmitigated) {
		t.Fatal("no risk_unmitigated finding")
	}
	out := must(t, GoalRiskSet, d, GoalRiskSetInput{ID: g.ID, Text: "vendor  SLIPS", Mitigation: "Second vendor on call", Owner: "Jordan Example"})
	if len(out.Risks) != 1 || out.Risks[0].Mitigation != "Second vendor on call" || out.Risks[0].Impact != model.LevelHigh || out.Risks[0].Source != "owner in chat" {
		t.Fatalf("update: %+v", out.Risks)
	}
	if slices.Contains(healthKinds(t, d, g.ID), HealthRiskUnmitigated) {
		t.Fatal("risk_unmitigated after mitigation")
	}
	mustLintClean(t, d)
	out = must(t, GoalRiskSet, d, GoalRiskSetInput{ID: g.ID, Text: "Vendor slips", Remove: true})
	if len(out.Risks) != 0 {
		t.Fatalf("remove: %+v", out.Risks)
	}
}

// TestAssumptionSet: add on a task, a check sets the date and who, changed
// needs a fact, and a goal takes them too.
func TestAssumptionSet(t *testing.T) {
	d := grillDeps(t)
	g := must(t, GoalAdd, d, GoalAddInput{Title: "Launch", Owner: "Jordan Example", DoneWhen: "Live."})
	task := must(t, TaskAdd, d, TaskAddInput{Goal: g.ID, Title: "Runbook", DoneWhen: "Merged."})
	if _, err := AssumptionSet.Run(ctx, d, AssumptionSetInput{ID: task.ID, Text: "API is stable"}); err == nil {
		t.Fatal("no source accepted")
	}
	must(t, AssumptionSet, d, AssumptionSetInput{ID: task.ID, Text: "API is stable", Source: "note"})
	out := must(t, AssumptionSet, d, AssumptionSetInput{ID: task.ID, Text: "API is stable", Status: model.AssumptionHolds})
	if a := out.Assumptions[0]; a.Checked != "2026-11-04" || a.By != "user" || a.Status != model.AssumptionHolds || a.Source != "note" {
		t.Fatalf("check: %+v", a)
	}
	if _, err := AssumptionSet.Run(ctx, d, AssumptionSetInput{ID: task.ID, Text: "API is stable", Status: model.AssumptionChanged}); err == nil {
		t.Fatal("changed with no fact accepted")
	}
	must(t, AssumptionSet, d, AssumptionSetInput{ID: g.ID, Text: "Budget holds", Source: "owner in chat"})
	if _, err := AssumptionSet.Run(ctx, d, AssumptionSetInput{ID: "constraint-x", Text: "x", Source: "y"}); err == nil {
		t.Fatal("missing document accepted")
	}
	mustLintClean(t, d)
}

// TestGrillContext: probes come in kind order, each with an ask; the vault's
// gaps, questions, owners, rules, outside work, assumptions, and the case
// against all show.
func TestGrillContext(t *testing.T) {
	d := grillDeps(t)
	g := must(t, GoalAdd, d, GoalAddInput{Title: "Launch", Owner: "Jordan Example", DoneWhen: "Live."})
	must(t, GoalRiskSet, d, GoalRiskSetInput{ID: g.ID, Text: "Vendor slips", Impact: model.LevelHigh, Source: "chat"})
	must(t, QuestionAdd, d, QuestionAddInput{Text: "Which region first?", Goal: g.ID})
	seedDoc(t, d, constraintDoc("constraint-freeze", "2027-01-01", g.ID))
	seedDoc(t, d, model.Document{ID: "task-vendor", Folder: model.FolderProjects, Title: "Vendor delivers keys", Body: "## Assumptions\n\n- Keys arrive by Friday",
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Goal: model.LinkTo(g.ID), Assignee: "person-riley"}})
	out := must(t, GrillContext, d, GrillContextInput{ID: g.ID})
	var kinds []string
	for i, p := range out.Probes {
		if p.Rank != i+1 || p.Ask == "" || p.Known == "" {
			t.Fatalf("probe %d: %+v", i, p)
		}
		kinds = append(kinds, p.Kind)
	}
	if !slices.IsSortedFunc(kinds, func(a, b string) int { return slices.Index(ProbeKinds, a) - slices.Index(ProbeKinds, b) }) {
		t.Fatalf("not in kind order: %v", kinds)
	}
	for _, k := range ProbeKinds {
		if !slices.Contains(kinds, k) {
			t.Errorf("no %s probe in %v", k, kinds)
		}
	}
	// Risks are recorded, so no gap asks for them; signals are missing.
	for _, p := range out.Probes {
		if p.Kind == ProbeBriefGap && strings.Contains(p.Known, "no risks") {
			t.Error("asks for risks the goal has")
		}
	}
	if !slices.ContainsFunc(out.Probes, func(p GrillProbe) bool { return p.Kind == ProbeRule && p.About == "constraint-freeze" }) {
		t.Error("constraint in effect not probed")
	}
	if !slices.ContainsFunc(out.Probes, func(p GrillProbe) bool {
		return p.Kind == ProbeAssumption && p.About == "task-vendor" && strings.Contains(p.Known, "Keys arrive")
	}) {
		t.Error("body assumption not probed")
	}
}

// TestRecheck: collection by scope and age, one queued request, per-item
// recording, and the stale health finding.
func TestRecheck(t *testing.T) {
	d := grillDeps(t)
	g := must(t, GoalAdd, d, GoalAddInput{Title: "Launch", Owner: "Jordan Example", DoneWhen: "Live."})
	must(t, AssumptionSet, d, AssumptionSetInput{ID: g.ID, Text: "Budget holds", Source: "chat", Status: model.AssumptionHolds, Checked: "2026-09-01"})
	must(t, AssumptionSet, d, AssumptionSetInput{ID: g.ID, Text: "Team stays", Source: "chat", Status: model.AssumptionHolds, Checked: "2026-11-01"})
	seedDoc(t, d, model.Document{ID: "task-keys", Folder: model.FolderProjects, Title: "Get keys", Body: "## Assumptions\n\n- Keys arrive by Friday",
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Goal: model.LinkTo(g.ID)}})
	seedDoc(t, d, constraintDoc("constraint-due", "2026-10-01", g.ID))
	seedDoc(t, d, constraintDoc("constraint-later", "2027-06-01", g.ID))
	seedDoc(t, d, constraintDoc("constraint-elsewhere", "2026-10-01", "area-other"))

	// Off by default: no stale finding.
	if slices.Contains(healthKinds(t, d, g.ID), HealthAssumptionStale) {
		t.Fatal("assumption_stale with recheck.days unset")
	}
	if err := d.Store.SetConfig(ctx, recheckDaysKey, "30"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(healthKinds(t, d, g.ID), HealthAssumptionStale) {
		t.Fatal("no assumption_stale finding")
	}

	out := must(t, Recheck, d, RecheckInput{Scope: g.ID})
	var got []string
	for _, it := range out.Items {
		got = append(got, it.Kind+":"+it.ID+":"+it.Text)
	}
	want := []string{"assumption:" + g.ID + ":Budget holds", "assumption:task-keys:Keys arrive by Friday", "constraint:constraint-due:"}
	if !slices.Equal(got, want) || out.OlderThan != 30 {
		t.Fatalf("items %v, older_than %d; want %v", got, out.OlderThan, want)
	}
	if out.Request == "" || out.Status != model.RequestQueued {
		t.Fatalf("request: %+v", out)
	}
	req, err := d.Store.Get(ctx, out.Request)
	if err != nil {
		t.Fatal(err)
	}
	if r := req.Meta.(*model.Request); r.Kind != recheckKind || !strings.Contains(req.Body, "[[constraint-due]]") || !strings.Contains(req.Body, "recheck_record") {
		t.Fatalf("request %s: kind %q body %q", req.ID, r.Kind, req.Body)
	}
	// older_than 1 also takes the one checked three days ago; the vault scope adds the rule elsewhere.
	if n := len(must(t, Recheck, d, RecheckInput{Scope: g.ID, OlderThan: 1, DryRun: true}).Items); n != 4 {
		t.Fatalf("older_than 1: %d items", n)
	}
	vault := must(t, Recheck, d, RecheckInput{DryRun: true})
	if vault.Request != "" || !slices.ContainsFunc(vault.Items, func(it RecheckItem) bool { return it.ID == "constraint-elsewhere" }) {
		t.Fatalf("vault: %+v", vault)
	}
	if n := len(must(t, Recheck, d, RecheckInput{Scope: g.ID, Tag: "nothing", DryRun: true}).Items); n != 0 {
		t.Fatalf("tag filter: %d items", n)
	}

	// holds: checked today; the stale finding goes away.
	rec := must(t, RecheckRecord, d, RecheckRecordInput{ID: g.ID, Assumption: "budget holds", Result: model.AssumptionHolds, Source: "https://acme.example/budget"})
	if rec.Checked != "2026-11-04" || rec.Question != "" {
		t.Fatalf("holds: %+v", rec)
	}
	if slices.Contains(healthKinds(t, d, g.ID), HealthAssumptionStale) {
		t.Fatal("assumption_stale after the check")
	}
	// changed on a body line: the frontmatter takes it; the body stays; a question when given.
	if _, err := RecheckRecord.Run(ctx, d, RecheckRecordInput{ID: "task-keys", Assumption: "Keys arrive by Friday", Result: model.AssumptionChanged, Source: "chat"}); err == nil {
		t.Fatal("changed with no fact accepted")
	}
	rec = must(t, RecheckRecord, d, RecheckRecordInput{ID: "task-keys", Assumption: "Keys arrive by Friday", Result: model.AssumptionChanged,
		Fact: "Keys arrive in two weeks", Source: "owner in chat", Question: "Move the launch date?"})
	if rec.Question == "" {
		t.Fatal("no question for a changed item")
	}
	task, _ := d.Store.Get(ctx, "task-keys")
	if a := task.Meta.(*model.Task).Assumptions; len(a) != 1 || a[0].Status != model.AssumptionChanged || a[0].Fact != "Keys arrive in two weeks" || !strings.Contains(task.Body, "- Keys arrive by Friday") {
		t.Fatalf("task: %+v body %q", a, task.Body)
	}
	q, _ := d.Store.Get(ctx, rec.Question)
	if q.Meta.(*model.Question).Scope.Target() != "task-keys" {
		t.Fatalf("question scope %s", q.Meta.(*model.Question).Scope)
	}
	// A constraint: holds moves the review date by recheck.days; unknown opens a question.
	rec = must(t, RecheckRecord, d, RecheckRecordInput{ID: "constraint-due", Result: model.AssumptionHolds, Source: "https://wiki.acme.example/constraint-due"})
	if rec.ReviewBy != "2026-12-04" {
		t.Fatalf("review_by %s", rec.ReviewBy)
	}
	rec = must(t, RecheckRecord, d, RecheckRecordInput{ID: "constraint-elsewhere", Result: model.AssumptionUnknown, Scope: g.ID})
	if rec.Question == "" || rec.ReviewBy != "" {
		t.Fatalf("unknown: %+v", rec)
	}
	c, _ := d.Store.Get(ctx, "constraint-elsewhere")
	if !strings.Contains(c.Body, "## Rechecks") || !strings.Contains(c.Body, "2026-11-04: unknown") {
		t.Fatalf("constraint body %q", c.Body)
	}
	if _, err := RecheckRecord.Run(ctx, d, RecheckRecordInput{ID: g.ID, Assumption: "Nothing like it", Result: model.AssumptionHolds, Source: "x"}); err == nil || !strings.Contains(err.Error(), "Budget holds") {
		t.Fatalf("unknown assumption: %v", err)
	}
	if n := len(must(t, Recheck, d, RecheckInput{Scope: g.ID, DryRun: true}).Items); n != 0 {
		t.Fatalf("after recording: %d items left", n)
	}
	mustLintClean(t, d)
}
