package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func rowText(rows []row) string {
	var b strings.Builder
	for _, r := range rows {
		if r.draw != nil {
			b.WriteString(r.draw(func(_, label string) string { return label }))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestWorkShowsAreasAndAreaTasks(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	a, err := ops.AreaAdd.Run(ctx, v.env.Deps, ops.AreaAddInput{Title: "Home"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.ProjectAdd.Run(ctx, v.env.Deps, ops.ProjectAddInput{DoneWhen: "Done.", Title: "Paint the hall", Area: a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.AreaLinkAdd.Run(ctx, v.env.Deps, ops.LinkAddInput{ID: a.ID, Label: "House runbook", Kind: model.PointerRunbook, Target: "~/doc/house.md"}); err != nil {
		t.Fatal(err)
	}
	due := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	if _, err := ops.TaskAdd.Run(ctx, v.env.Deps, ops.TaskAddInput{Area: a.ID, Title: "Water plants", Due: due}); err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 50)
	text := rowText(d.m.rowsWork(120))
	for _, want := range []string{"Areas (1)", "Home", "Paint the hall", "Water plants", "runbook: House runbook", "due " + dateWords(due, time.Time{}), "Projects with no area (1)", "Portal launch"} {
		if !strings.Contains(text, want) {
			t.Errorf("Work lacks %q:\n%s", want, text)
		}
	}
	today := rowText(d.m.rowsToday(120))
	if strings.Contains(today, "late ") || strings.Contains(today, "none") {
		t.Errorf("Today shows an empty section:\n%s", today)
	}
	if !strings.Contains(today, "due ") || !strings.Contains(today, "Water plants") {
		t.Errorf("Today lacks the due task:\n%s", today)
	}
}

func TestTodayShowsIncidentsFirstAndWaiting(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	deps := v.env.Deps
	a, err := ops.AreaAdd.Run(ctx, deps, ops.AreaAddInput{Title: "Atlas service"})
	if err != nil {
		t.Fatal(err)
	}
	inc, err := ops.IncidentAdd.Run(ctx, deps, ops.IncidentAddInput{Title: "Atlas down", Severity: model.Sev1, Area: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.TaskAdd.Run(ctx, deps, ops.TaskAddInput{Area: a.ID, Title: "Ask Ada for logs", Assignee: "person-ada-example"}); err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 50)
	text := rowText(d.m.rowsToday(120))
	i, o := strings.Index(text, "incident "), strings.Index(text, "waiting ")
	if i < 0 || o < 0 || i > o || !strings.Contains(text, "sev1 open") || !strings.Contains(text, "Atlas down") {
		t.Errorf("Today lacks incidents before the rest:\n%s", text)
	}
	if n := strings.Count(text, "waiting "); n != 1 { // the task; the seeded RFX shows once, as a nudge
		t.Errorf("Today has %d waiting rows, want 1:\n%s", n, text)
	}
	if _, err := ops.IncidentStatus.Run(ctx, deps, ops.IncidentStatusInput{ID: inc.ID, Status: model.IncidentResolved}); err != nil {
		t.Fatal(err)
	}
	d = newDrv(t, v, 120, 50)
	if text := rowText(d.m.rowsToday(120)); strings.Contains(text, "incident ") {
		t.Errorf("a resolved incident is still on Today:\n%s", text)
	}
}

func TestTodayAnswersAQuestion(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	deps := v.env.Deps
	a, err := ops.AreaAdd.Run(ctx, deps, ops.AreaAddInput{Title: "Atlas service"})
	if err != nil {
		t.Fatal(err)
	}
	q, err := ops.QuestionAdd.Run(ctx, deps, ops.QuestionAddInput{Text: "Decide B-1: config path for prod", Area: a.ID,
		Kind: model.KindDecision, Options: []string{"/etc/atlas", "/opt/atlas"}, Recommended: "/opt/atlas"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := ops.QuestionAdd.Run(ctx, deps, ops.QuestionAddInput{Text: "Who signs off?", Root: true})
	if err != nil {
		t.Fatal(err)
	}
	task, err := ops.TaskAdd.Run(ctx, deps, ops.TaskAddInput{Area: a.ID, Title: "Deploy to prod", DependsOn: []string{q.ID}})
	if err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 60)
	text := rowText(d.m.rowsToday(120))
	for _, want := range []string{"question ", "Decide B-1: config path for prod", "decision", "blocks 1", "Who signs off?"} {
		if !strings.Contains(text, want) {
			t.Errorf("Today lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(rowText(d.m.rowsToday(120)), "Deploy to prod") {
		t.Error("a task waiting on a question is ready")
	}
	onQuestion := func(id string) {
		t.Helper()
		for i := 0; i < 60; i++ {
			if r, ok := d.m.current(); ok && r.kind == "question" && r.id == id {
				return
			}
			d.key("j")
		}
		t.Fatalf("cursor never reached %s", id)
	}
	// A decision: the recommended option is prefilled, the reason is required.
	onQuestion(q.ID)
	d.key(d.keyFor("act:answer"))
	if !d.has("2  /opt/atlas") || !d.has("recommended") {
		t.Fatalf("no options shown:\n%s", d.plainFrame())
	}
	d.key("enter") // accept the prefilled 2
	d.key("enter") // no reason: stays
	if !d.has("why:") {
		t.Fatalf("a decision went through with no reason:\n%s", d.plainFrame())
	}
	d.typeText("Matches the image.")
	d.key("enter")
	got, err := ops.QuestionShow.Run(ctx, deps, ops.QuestionShowInput{ID: q.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Question.Status != model.QuestionAnswered || got.Question.Choice != "/opt/atlas" || !strings.Contains(got.Body, "- Why: Matches the image.") ||
		!strings.Contains(got.Body, "via tui") {
		t.Fatalf("question = %+v\n%s", got.Question, got.Body)
	}
	// The dependent is ready at once, and the answered question left the list.
	text = rowText(d.m.rowsToday(120))
	if strings.Count(text, "question \x1b") != 1 || !strings.Contains(text, "Deploy to prod") {
		t.Fatalf("Today after the answer:\n%s", text)
	}
	// Free text for a question with no options.
	onQuestion(root.ID)
	d.key(d.keyFor("act:answer"))
	d.typeText("Sam Example")
	d.key("enter")
	got, err = ops.QuestionShow.Run(ctx, deps, ops.QuestionShowInput{ID: root.ID})
	if err != nil || got.Question.Status != model.QuestionAnswered || !strings.Contains(got.Body, "Sam Example") {
		t.Fatalf("root = %+v, %v", got.Question, err)
	}
	_ = task
}

func TestAnswerInput(t *testing.T) {
	q := ops.QuestionView{ID: "question-x", Options: []string{"Nov 14", "Nov 21"}}
	for typed, want := range map[string]ops.QuestionAnswerInput{
		"2":       {ID: "question-x", Choice: "Nov 21"},
		"nov 14":  {ID: "question-x", Choice: "Nov 14"},
		"3":       {ID: "question-x", Text: "3"},
		"Dec 5th": {ID: "question-x", Text: "Dec 5th"},
	} {
		if got := answerInput(q, typed, ""); got.Choice != want.Choice || got.Text != want.Text {
			t.Errorf("%q: got %+v, want %+v", typed, got, want)
		}
	}
}
