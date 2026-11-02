package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// questionParityCases are the parity rows of the question ops. Mutating ops
// use one document for Invoke (a) and one for the CLI (b). Ids come from
// seedQuestionParity.
func questionParityCases() map[string]parityCase {
	return map[string]parityCase{
		"question_add":    {`{"text":"Which port?","project":"project-q-seed","kind":"decision","options":["80","8080"],"recommended":"80"}`, []string{"question", "add", "Which", "port?", "--project", "project-q-seed", "--kind", "decision", "--option", "80", "--option", "8080", "--recommended", "80"}, false},
		"question_list":   {`{"project":"project-q-seed"}`, []string{"question", "list", "--project", "project-q-seed"}, true},
		"question_show":   {`{"id":"question-q-show"}`, []string{"question", "show", "question-q-show"}, true},
		"question_answer": {`{"id":"question-q-ans-a","text":"Saturday.","why":"Agreed."}`, []string{"question", "answer", "question-q-ans-b", "--text", "Saturday.", "--why", "Agreed."}, false},
		"question_drop":   {`{"id":"question-q-drop-a","reason":"Not needed."}`, []string{"question", "drop", "question-q-drop-b", "--reason", "Not needed."}, false},
		"question_ask": {`{"id":"question-q-ask-a","to":"person-q-seed","via":"slack","route":"domain-seed","value":"@quinn"}`,
			[]string{"question", "ask", "question-q-ask-b", "--to", "person-q-seed", "--via", "slack", "--route", "domain-seed", "--value", "@quinn"}, false},
		"question_accept": {`{"id":"question-q-acc-a","text":"Changed."}`, []string{"question", "accept", "question-q-acc-b", "--text", "Changed."}, false},
		"question_reject": {`{"id":"question-q-rej-a","reason":"No."}`, []string{"question", "reject", "question-q-rej-b", "--reason", "No."}, false},
		"question_edit":   {`{"id":"question-q-edit-a","title":"Renamed"}`, []string{"question", "edit", "question-q-edit-b", "--title", "Renamed"}, false},
		"question_depends_add": {`{"id":"question-q-dep-a","depends_on":["task-q-dep"]}`,
			[]string{"question", "depends", "add", "question-q-dep-b", "task-q-dep"}, false},
		"question_depends_remove": {`{"id":"question-q-undep-a","depends_on":["task-q-dep"]}`,
			[]string{"question", "depends", "remove", "question-q-undep-b", "task-q-dep"}, false},
		"task_to_question": {`{"id":"task-q-conv-a","kind":"decision","options":["A","B"]}`, []string{"task", "to-question", "task-q-conv-b", "--kind", "decision", "--option", "A", "--option", "B"}, false},
	}
}

func seedQuestionParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{
		{ID: "project-q-seed", Folder: model.FolderProjects, Title: "Q project",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}},
		{ID: "person-q-seed", Folder: model.FolderOrg, Title: "Quinn Example",
			Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme.example"}},
	}
	for _, id := range []string{"question-q-show", "question-q-ans-a", "question-q-ans-b", "question-q-drop-a", "question-q-drop-b", "question-q-ask-a", "question-q-ask-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderQuestions, Title: "Q " + id,
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Kind: model.KindDecision, Scope: model.LinkTo("project-q-seed")}})
	}
	proposed := func(id string) model.Document {
		return model.Document{ID: id, Folder: model.FolderQuestions, Title: "Q " + id, Meta: &model.Question{
			Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.LinkTo("project-q-seed"),
			ProposedAnswer: "Proposed.", ProposedBy: "claude", ProposedSources: []model.AnswerSource{{Ref: "https://wiki.example/x"}}}}
	}
	docs = append(docs, proposed("question-q-acc-a"), proposed("question-q-acc-b"), proposed("question-q-rej-a"), proposed("question-q-rej-b"))
	for _, id := range []string{"question-q-edit-a", "question-q-edit-b", "question-q-dep-a", "question-q-dep-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderQuestions, Title: "Q " + id,
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.LinkTo("project-q-seed")}})
	}
	for _, id := range []string{"question-q-undep-a", "question-q-undep-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderQuestions, Title: "Q " + id,
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.LinkTo("project-q-seed"), DependsOn: []model.Link{model.LinkTo("task-q-dep")}}})
	}
	for _, id := range []string{"task-q-conv-a", "task-q-conv-b", "task-q-dep"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderProjects, Title: "Q " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-q-seed")}})
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}

// The whole flow through the commands: a question blocks a task, an answer
// frees it, and the shortcuts run the same ops.
func TestQuestionFlowThroughTheCLI(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var proj ops.ProjectAddOutput
	e.json(&proj, "project", "add", "--done-when", "Done.", "Portal", "--owner", "Jordan Example")
	if out, _, err := e.run("question", "add", "Which window?"); err == nil {
		t.Fatalf("an unscoped question was accepted: %s", out)
	}
	var q ops.QuestionAddOutput
	e.json(&q, "question", "add", "Pick the window date", "--project", proj.ID, "--kind", "decision", "--option", "Nov 14", "--option", "Nov 21", "--recommended", "Nov 21")
	var root ops.QuestionAddOutput
	e.json(&root, "question", "add", "Who signs off?", "--root")
	var task ops.TaskAddOutput
	e.json(&task, "task", "add", "Schedule the window", "-p", proj.ID, "--depends-on", q.ID)

	var next ops.NextOutput
	e.json(&next, "next")
	if len(next.Tasks) != 0 {
		t.Fatalf("a task waiting on a question is ready: %+v", next.Tasks)
	}
	var list ops.QuestionListOutput
	e.json(&list, "questions")
	if len(list.Questions) != 2 || list.Questions[0].ID != q.ID || list.Questions[0].Blocking != 1 {
		t.Fatalf("questions = %+v", list.Questions)
	}
	e.json(&list, "questions", "--project", proj.ID)
	if len(list.Questions) != 1 {
		t.Fatalf("questions --project = %+v", list.Questions)
	}
	if out := e.ok("task", "show", task.ID); !strings.Contains(out, q.ID) {
		t.Fatalf("task show: %s", out)
	}
	if out := e.ok("project", "show", proj.ID); !strings.Contains(out, "questions") || !strings.Contains(out, q.ID) {
		t.Fatalf("project show: %s", out)
	}
	if out := e.ok("graph"); !strings.Contains(out, "question:decision:"+proj.ID) {
		t.Fatalf("graph: %s", out)
	}
	if _, _, err := e.run("answer", q.ID, "--choice", "2"); err == nil {
		t.Fatal("a decision was answered with no reason")
	}
	out := e.ok("answer", q.ID, "--choice", "2", "--why", "Fits the freeze.")
	if !strings.Contains(out, "answered") || !strings.Contains(out, "Nov 21") || !strings.Contains(out, "unblocked\t"+task.ID) {
		t.Fatalf("answer: %s", out)
	}
	e.json(&next, "next")
	if len(next.Tasks) != 1 || next.Tasks[0].ID != task.ID {
		t.Fatalf("next after answer = %+v", next.Tasks)
	}
	e.stdin = "Sam Example signs off."
	e.ok("question", "answer", root.ID)
	if out := e.ok("question", "show", root.ID); !strings.Contains(out, "Sam Example signs off.") {
		t.Fatalf("show: %s", out)
	}
	e.ok("task", "done", task.ID, "--note", "Scheduled for Nov 21.")
	if out := e.ok("task", "show", task.ID); !strings.Contains(out, "## Outcome") {
		t.Fatalf("task show: %s", out)
	}
	if out := e.ok("lint"); !strings.Contains(out, "0 errors") {
		t.Fatalf("lint: %s", out)
	}
}

func TestQuestionsInToday(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var q ops.QuestionAddOutput
	e.json(&q, "question", "add", "Who signs off?", "--root")
	var task ops.TaskAddOutput
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Atlas")
	e.json(&task, "task", "add", "Ship", "--area", area.ID, "--depends-on", q.ID)
	out := e.ok("today")
	if !strings.Contains(out, "questions 1") || !strings.Contains(out, "question\t"+q.ID+"\tquestion root\tblocks 1") {
		t.Fatalf("today: %s", out)
	}
	e.ok("answer", q.ID, "--text", "Sam.")
	if out := e.ok("today"); !strings.Contains(out, "questions 0") {
		t.Fatalf("today after answer: %s", out)
	}
}
