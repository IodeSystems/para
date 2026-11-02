package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// azVault has a goal, a question that blocks a task, and a question that
// blocks nothing.
func azVault(t *testing.T) (*testVault, string, string, string) {
	t.Helper()
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	deps := v.env.Deps
	proj, err := ops.ProjectAdd.Run(ctx, deps, ops.ProjectAddInput{Title: "Portal move", DoneWhen: "Live.", Due: "2026-11-12"})
	if err != nil {
		t.Fatal(err)
	}
	q, err := ops.QuestionAdd.Run(ctx, deps, ops.QuestionAddInput{Text: "Which window?", Project: proj.ID})
	if err != nil {
		t.Fatal(err)
	}
	idle, err := ops.QuestionAdd.Run(ctx, deps, ops.QuestionAddInput{Text: "Who owns the wiki page?", Root: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.TaskAdd.Run(ctx, deps, ops.TaskAddInput{Project: proj.ID, Title: "Schedule the move", DependsOn: []string{q.ID}}); err != nil {
		t.Fatal(err)
	}
	return v, proj.ID, q.ID, idle.ID
}

func (d *drv) onRow(kind, id string) {
	d.t.Helper()
	d.key("g")
	for range 80 {
		if r, ok := d.m.current(); ok && r.kind == kind && r.id == id {
			return
		}
		d.key("j")
	}
	d.t.Fatalf("cursor never reached %s %s:\n%s", kind, id, d.plainFrame())
}

func status(t *testing.T, v *testVault, id string) ops.QuestionShowOutput {
	t.Helper()
	o, err := ops.QuestionShow.Run(context.Background(), v.env.Deps, ops.QuestionShowInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// Today is one list in value order: the question that blocks work due soon
// comes before the one that blocks nothing, and each row says why.
func TestTodayOrdersQuestionsByWhatTheyBlock(t *testing.T) {
	v, _, q, idle := azVault(t)
	d := newDrv(t, v, 120, 36)
	text := rowText(d.m.rowsToday(120))
	a, b := strings.Index(text, "Which window?"), strings.Index(text, "Who owns the wiki page?")
	if a < 0 || b < 0 || a > b {
		t.Fatalf("order:\n%s", text)
	}
	if !strings.Contains(text, "blocks 1, goal due Nov 12") || !strings.Contains(text, "blocks nothing, no date") {
		t.Errorf("rows do not say why:\n%s", text)
	}
	_, _ = q, idle
}

// Every action works from the list and from an opened question. This is the
// bug that "a" did nothing on an opened question.
func TestQuestionActionsFromAnOpenedQuestion(t *testing.T) {
	v, _, q, _ := azVault(t)
	d := newDrv(t, v, 120, 36)
	d.onRow("question", q)
	d.key("enter")
	if d.m.docPage() == nil {
		t.Fatalf("no document page:\n%s", d.plainFrame())
	}
	foot := d.footer()
	for _, want := range []string{"Answer", "Drop", "Ask person", "Investigate", "Later", "Cut"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer lacks %q: %s", want, foot)
		}
	}
	d.key(d.keyFor("act:answer"))
	if !d.has("Answer: Which window?") {
		t.Fatalf("a did nothing on an opened question:\n%s", d.plainFrame())
	}
	d.typeText("Nov 14")
	d.key("enter")
	d.typeText("Platform sync, Oct 29")
	d.key("enter")
	d.typeText("we ship on the 14th")
	d.key("enter")
	d.key("enter") // no second source
	got := status(t, v, q)
	if got.Question.Status != model.QuestionAnswered || !strings.Contains(got.Body, "- Source: Platform sync, Oct 29") || !strings.Contains(got.Body, `"we ship on the 14th"`) {
		t.Fatalf("question = %+v\n%s", got.Question, got.Body)
	}
}

func TestDropAsksForAReasonFromTheList(t *testing.T) {
	v, _, _, idle := azVault(t)
	d := newDrv(t, v, 120, 36)
	d.onRow("question", idle)
	d.key(d.keyFor("act:drop"))
	d.key("enter") // a reason is required
	if !d.has("Drop: Who owns the wiki page?") {
		t.Fatalf("a drop went through with no reason:\n%s", d.plainFrame())
	}
	d.typeText("Nobody needs it.")
	d.key("enter")
	got := status(t, v, idle)
	if got.Question.Status != model.QuestionDropped || !strings.Contains(got.Body, "Nobody needs it.") {
		t.Fatalf("question = %+v\n%s", got.Question, got.Body)
	}
}

func TestLaterMovesAQuestionToItsSectionAndBack(t *testing.T) {
	v, _, q, idle := azVault(t)
	d := newDrv(t, v, 120, 36)
	d.onRow("question", idle)
	d.key(d.keyFor("act:later"))
	d.key("enter") // someday
	d.key("enter") // no reason
	d.key("enter") // no place
	if status(t, v, idle).Question.DeferUntil != model.SomedayWord {
		t.Fatalf("not deferred: %+v", status(t, v, idle).Question)
	}
	if strings.Contains(rowText(d.m.rowsToday(120)), "Who owns the wiki page?") {
		t.Error("a deferred question is still on Today")
	}
	// Work lists it under Later, and it keeps blocking: the blocker stays put.
	d.key("3")
	text := rowText(d.m.rowsWork(120))
	if !strings.Contains(text, "Questions: Later (1)") || !strings.Contains(text, "Questions: Blocking (1)") || !strings.Contains(text, "Who owns the wiki page?") {
		t.Fatalf("Work sections:\n%s", text)
	}
	d.onRow("question", idle)
	if !strings.Contains(d.footer(), "Not later") {
		t.Errorf("no way back on a put-off row: %s", d.footer())
	}
	d.key(d.keyFor("act:later"))
	if status(t, v, idle).Question.DeferUntil != "" {
		t.Errorf("still deferred")
	}
	_ = q
}

func TestFindContextQueuesAgentWork(t *testing.T) {
	v, _, q, _ := azVault(t)
	d := newDrv(t, v, 120, 36)
	d.onRow("question", q)
	d.key("enter")
	d.key(d.keyFor("act:find"))
	d.typeText("check the wiki")
	d.key("enter")
	reqs, err := ops.RequestList.Run(context.Background(), v.env.Deps, ops.RequestListInput{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range reqs.Items {
		found = found || r.Title == "Find evidence: Which window?" && r.Status == model.RequestQueued
	}
	if !found {
		t.Fatalf("no queued request: %+v", reqs.Items)
	}
	if got := status(t, v, q); got.Question.Status != model.QuestionOpen || !got.Question.AgentLooking {
		t.Errorf("question = %+v", got.Question)
	}
	d.key("esc")
	if rows, _ := d.m.rows(120); !strings.Contains(rowText(rows), "agent looking") {
		t.Errorf("Today does not say an agent is looking:\n%s", rowText(rows))
	}
}

func TestCutRemovesTheEdgeAndRecordsWhy(t *testing.T) {
	v, _, q, _ := azVault(t)
	d := newDrv(t, v, 120, 36)
	d.onRow("question", q)
	d.key(d.keyFor("act:cut"))
	if !d.has("Schedule the move stops waiting on Which window?") {
		t.Fatalf("no choice:\n%s", d.plainFrame())
	}
	d.key("enter") // the one choice, prefilled
	d.typeText("The default window is fine.")
	d.key("enter")
	tasks, _ := ops.TaskList.Run(context.Background(), v.env.Deps, ops.TaskListInput{})
	for _, tk := range tasks.Tasks {
		if tk.Title == "Schedule the move" {
			if len(tk.DependsOn) != 0 {
				t.Errorf("still waits: %v", tk.DependsOn)
			}
			sh, _ := ops.TaskShow.Run(context.Background(), v.env.Deps, ops.TaskShowInput{ID: tk.ID})
			if !strings.Contains(sh.Body, "The default window is fine.") {
				t.Errorf("no assumption:\n%s", sh.Body)
			}
		}
	}
	if status(t, v, q).Question.Status != model.QuestionOpen {
		t.Error("cut closed the question")
	}
}

func TestAskPersonFromTheList(t *testing.T) {
	v, _, _, idle := azVault(t)
	ctx := context.Background()
	role, err := ops.OrgRoleAdd.Run(ctx, v.env.Deps, ops.OrgAddInput{Name: "Wiki owner", Org: "acme.example"})
	if err != nil {
		t.Fatal(err)
	}
	dom, err := ops.OrgDomainAdd.Run(ctx, v.env.Deps, ops.OrgAddInput{Name: "Wiki", Owner: role.ID})
	if err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 36)
	d.onRow("question", idle)
	d.key(d.keyFor("act:ask"))
	d.typeText("person-ada-example")
	d.key("enter")
	d.typeText("email")
	d.key("enter")
	if !d.has("route") {
		t.Fatalf("no route step:\n%s", d.plainFrame())
	}
	d.typeText(dom.ID)
	d.key("enter")
	if got := status(t, v, idle); got.Question.RFX == "" {
		t.Fatalf("question not asked (route %s): %+v\n%s", dom.ID, got.Question, d.plainFrame())
	}
}

// The question actions have distinct keys, none from the fixed keys, on every
// tab that shows questions and on an opened question, at three sizes.
func TestQuestionActionKeysDoNotClash(t *testing.T) {
	v, _, q, _ := azVault(t)
	for _, size := range [][2]int{{120, 36}, {80, 24}, {60, 24}} {
		d := newDrv(t, v, size[0], size[1])
		check := func(where string) {
			t.Helper()
			seen := map[string]string{}
			for code, id := range d.m.keymap {
				if strings.HasPrefix(id, "act:") {
					if other, dup := seen[code]; dup && other != id {
						t.Errorf("%s: key %q is %s and %s", where, code, other, id)
					}
					seen[code] = id
				}
			}
			for _, id := range []string{"act:answer", "act:reword", "act:drop", "act:ask", "act:find", "act:later", "act:cut"} {
				found := false
				for _, got := range seen {
					found = found || got == id
				}
				if !found {
					t.Errorf("%s: no key for %s", where, id)
				}
			}
			for _, reserved := range []string{"j", "k", "g", "q"} {
				if id := d.m.keymap[reserved]; strings.HasPrefix(id, "act:") {
					t.Errorf("%s: %s took the reserved key %s", where, id, reserved)
				}
			}
		}
		for tab, name := range map[string]string{"1": "Agenda", "2": "Review", "3": "Work"} {
			d.key(tab)
			check(name)
		}
		d.key("3")
		d.onRow("question", q)
		d.key("enter")
		check("opened question")
	}
}
