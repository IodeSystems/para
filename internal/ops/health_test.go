package ops

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

func hc(t model.Type) model.Common { return model.Common{Type: t, Created: "2026-11-01"} }

func healthSeed(t *testing.T) Deps {
	t.Helper()
	d, _ := laterVault(t)
	link := model.LinkTo
	docs := []model.Document{
		// A dated goal: a clean task under it needs no date; a task with no done_when is flagged.
		{ID: "project-dated", Folder: model.FolderProjects, Title: "Dated",
			Meta: &model.Project{Common: hc(model.TypeProject), Status: model.ProjectActive, Owner: "Jordan Example", DoneWhen: "Shipped.", Due: "2026-12-01"}},
		{ID: "task-dated-ok", Folder: model.FolderProjects, Title: "Clean",
			Meta: &model.Task{Common: hc(model.TypeTask), Status: model.TaskTodo, Project: link("project-dated"), DoneWhen: "Done."}},
		{ID: "task-dated-nodw", Folder: model.FolderProjects, Title: "No done when",
			Meta: &model.Task{Common: hc(model.TypeTask), Status: model.TaskTodo, Project: link("project-dated"), DoneWhen: "Done."}},
		// A blocker under the dated goal inherits its date; a short reason is incomplete.
		{ID: "task-dated-blocked", Folder: model.FolderProjects, Title: "Blocked",
			Meta: &model.Task{Common: hc(model.TypeTask), Status: model.TaskBlocked, Project: link("project-dated"), DoneWhen: "Done.", BlockedBy: []string{"legal"}}},
		// An undated goal with no open task, no done_when.
		{ID: "project-bare", Folder: model.FolderProjects, Title: "Bare",
			Meta: &model.Project{Common: hc(model.TypeProject), Status: model.ProjectActive, Owner: "Jordan Example"}},
		// A blocker with a full reason but no date and no dated goal.
		{ID: "project-open", Folder: model.FolderProjects, Title: "Open",
			Meta: &model.Project{Common: hc(model.TypeProject), Status: model.ProjectActive, Owner: "Jordan Example", DoneWhen: "Done.", Due: "2026-12-15"}},
		{ID: "task-cyc-a", Folder: model.FolderProjects, Title: "Cycle A",
			Meta: &model.Task{Common: hc(model.TypeTask), Status: model.TaskTodo, Project: link("project-open"), DoneWhen: "Done.", DependsOn: []model.Link{link("task-cyc-b")}}},
		{ID: "task-cyc-b", Folder: model.FolderProjects, Title: "Cycle B",
			Meta: &model.Task{Common: hc(model.TypeTask), Status: model.TaskTodo, Project: link("project-open"), DoneWhen: "Done.", DependsOn: []model.Link{link("task-cyc-a")}}},
		{ID: "task-dangling", Folder: model.FolderProjects, Title: "Dangling",
			Meta: &model.Task{Common: hc(model.TypeTask), Status: model.TaskTodo, Project: link("project-open"), DoneWhen: "Done.", DependsOn: []model.Link{link("task-gone")}}},
		// Deferred: skipped.
		{ID: "task-later", Folder: model.FolderProjects, Title: "Later",
			Meta: &model.Task{Common: hc(model.TypeTask), Status: model.TaskTodo, Project: link("project-open"),
				Deferral: model.Deferral{DeferUntil: model.SomedayWord}}},
		// Dismissed: counted, not listed.
		{ID: "task-dismissed", Folder: model.FolderProjects, Title: "Dismissed",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask, Created: "2026-11-01", HealthDismissed: []model.HealthDismissal{{Kind: HealthTaskNoDoneWhen, Reason: "Obvious.", Date: "2026-11-01"}}},
				Status: model.TaskTodo, Project: link("project-open")}},
		// Area, person, constraint with no place filled.
		{ID: "area-bare", Folder: model.FolderAreas, Title: "Bare area",
			Meta: &model.Area{Common: hc(model.TypeArea), Status: model.AreaActive}},
		{ID: "person-bare", Folder: model.FolderOrg, Title: "Bare person",
			Meta: &model.Person{Common: hc(model.TypePerson), Org: "acme"}},
		{ID: "constraint-nosrc", Folder: model.FolderConstraints, Title: "Past review",
			Meta: &model.Constraint{Common: hc(model.TypeConstraint), Why: "Audit", By: "Jordan Example", ReviewBy: "2026-10-01",
				Source: model.Source{URL: "https://wiki.acme.example/a", Retrieved: "2026-10-01", Fidelity: model.FidelityNone}}},
		// An ask with no follow-up and no target.
		{ID: "rfx-bare", Folder: model.FolderProjects, Title: "Bare ask",
			Meta: &model.RFX{Common: hc(model.TypeRFX), Direction: model.RFXOut, Status: model.RFXOpen, Route: "domain-x", SelfServeCheck: "wiki", Project: link("project-open")}},
		// Questions: stale and blocking nothing; recent; stale but blocking a task.
		{ID: "question-stale", Folder: model.FolderQuestions, Title: "Stale",
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion, Created: "2026-09-01"}, Status: model.QuestionOpen, Scope: link("project-open")}},
		{ID: "question-fresh", Folder: model.FolderQuestions, Title: "Fresh",
			Meta: &model.Question{Common: hc(model.TypeQuestion), Status: model.QuestionOpen, Scope: link("project-open")}},
		{ID: "question-blocking", Folder: model.FolderQuestions, Title: "Blocking",
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion, Created: "2026-09-01"}, Status: model.QuestionOpen, Scope: link("project-open")}},
		{ID: "task-waits", Folder: model.FolderProjects, Title: "Waits",
			Meta: &model.Task{Common: hc(model.TypeTask), Status: model.TaskTodo, Project: link("project-open"), DoneWhen: "Done.", DependsOn: []model.Link{link("question-blocking")}}},
		// Answered decision with and without a source.
		{ID: "question-decided", Folder: model.FolderQuestions, Title: "Decided",
			Meta: &model.Question{Common: hc(model.TypeQuestion), Kind: model.KindDecision, Status: model.QuestionAnswered, Scope: link("project-open")}},
		{ID: "question-decided-src", Folder: model.FolderQuestions, Title: "Decided with source", Body: "## Answer\n\n- Source: https://wiki.acme.example/x\n",
			Meta: &model.Question{Common: hc(model.TypeQuestion), Kind: model.KindDecision, Status: model.QuestionAnswered, Scope: link("project-open")}},
	}
	for _, doc := range docs {
		if _, err := d.Store.Create(ctx, doc); err != nil {
			t.Fatalf("seed %s: %v", doc.ID, err)
		}
	}
	return d
}

// TestHealthFindings checks one finding per seeded gap, and that clean and
// skipped items give none.
func TestHealthFindings(t *testing.T) {
	d := healthSeed(t)
	out, err := Health(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, g := range out.Groups {
		for _, f := range g.Findings {
			if f.Severity != SeverityWarning || f.Fix == "" || f.Message == "" {
				t.Errorf("incomplete finding %+v", f)
			}
			got[f.Kind+" "+f.ID] = true
		}
	}
	for _, want := range []string{
		"cycle task-cyc-a",
		"dangling_dependency task-dangling",
		"goal_no_open_task project-bare",
		"goal_no_due project-bare",
		"goal_no_done_when project-bare",
		"blocker_incomplete task-dated-blocked",
		"ask_no_followup rfx-bare",
		"ask_no_target rfx-bare",
		"area_no_links area-bare",
		"person_no_contact person-bare",
		"policy_review_due constraint-nosrc",
		"question_stale question-stale",
		"no_source question-decided",
	} {
		if !got[want] {
			t.Errorf("missing finding %q", want)
		}
	}
	for _, not := range []string{
		"goal_no_due project-dated",              // dated
		"task_no_done_when task-dated-ok",        // clean
		"blocker_no_followup task-dated-blocked", // inherits the goal's date
		"task_no_done_when task-later",           // deferred
		"task_no_done_when task-dismissed",       // dismissed
		"question_stale question-fresh",
		"question_stale question-blocking", // blocks a task
		"no_source question-decided-src",
	} {
		if got[not] {
			t.Errorf("unexpected finding %q", not)
		}
	}
	if out.Dismissed != 1 {
		t.Errorf("dismissed = %d, want 1", out.Dismissed)
	}
}

// TestHealthGroupOrder: dated goals first by the value order, other last.
func TestHealthGroupOrder(t *testing.T) {
	d := healthSeed(t)
	out, err := Health(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, g := range out.Groups {
		ids = append(ids, g.Goal)
	}
	// The Portal goal of laterVault is due Nov 12, so it comes first.
	if len(ids) > 0 {
		ids = ids[1:]
	}
	want := []string{"project-dated", "project-open", "project-bare", HealthOtherGroup}
	// project-dated has findings (blocker_incomplete); the order is by goal rank.
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("groups = %v, want %v", ids, want)
	}
}

func TestHealthDismissRoundTrip(t *testing.T) {
	d := healthSeed(t)
	if _, err := HealthDismiss.Run(ctx, d, HealthDismissInput{ID: "area-bare", Kind: HealthAreaNoLinks}); err == nil {
		t.Fatal("empty reason accepted")
	}
	if _, err := HealthDismiss.Run(ctx, d, HealthDismissInput{ID: "area-bare", Kind: "nope", Reason: "x"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
	before, _ := Health(ctx, d.Store)
	must(t, HealthDismiss, d, HealthDismissInput{ID: "area-bare", Kind: HealthAreaNoLinks, Reason: "Links live in the wiki."})
	after, _ := Health(ctx, d.Store)
	if after.Count != before.Count-1 || after.Dismissed != before.Dismissed+1 {
		t.Fatalf("count %d -> %d, dismissed %d -> %d", before.Count, after.Count, before.Dismissed, after.Dismissed)
	}
	must(t, HealthUndismiss, d, HealthUndismissInput{ID: "area-bare", Kind: HealthAreaNoLinks})
	back, _ := Health(ctx, d.Store)
	if back.Count != before.Count {
		t.Fatalf("undismiss: count %d, want %d", back.Count, before.Count)
	}
	// An agent cannot dismiss.
	agent := Deps{Store: d.Store.ActAs("agent", "mcp")}
	if _, err := HealthDismiss.Run(ctx, agent, HealthDismissInput{ID: "area-bare", Kind: HealthAreaNoLinks, Reason: "x"}); err == nil || !strings.Contains(err.Error(), ErrPersonOnly.Error()) {
		t.Fatalf("agent dismissed: %v", err)
	}
	if slices.Contains(HealthKinds, "") {
		t.Fatal("empty kind listed")
	}
}
