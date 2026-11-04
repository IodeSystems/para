package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestTaskUpdateAndParts(t *testing.T) {
	d, proj := laterVault(t)
	id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Ship the parser", DoneWhen: "Released."}).ID
	if _, err := TaskUpdate.Run(ctx, agentOf(d), TaskUpdateInput{ID: id, Text: "x"}); !errors.Is(err, ErrPersonOnly) {
		t.Errorf("agent: %v", err)
	}
	if _, err := TaskUpdate.Run(ctx, d, TaskUpdateInput{ID: id, Text: "  "}); err == nil {
		t.Error("empty text accepted")
	}
	text := "Met Sam.\n\n## Not a heading\nHe will review by Friday."
	o := must(t, TaskUpdate, d, TaskUpdateInput{ID: id, Text: text})
	if !o.Saved || o.Request == "" || o.Appended || !strings.Contains(o.Note, "request.runner") {
		t.Fatalf("first = %+v", o)
	}
	o2 := must(t, TaskUpdate, d, TaskUpdateInput{ID: id, Text: "Second note."})
	if !o2.Appended || o2.Request != o.Request {
		t.Errorf("second = %+v", o2)
	}
	r := must(t, RequestShow, d, RequestShowInput{ID: o.Request}).Doc
	if r.Title != "Sort update: Ship the parser" || strings.Count(r.Body, "## Hints") != 1 {
		t.Errorf("request = %q\n%s", r.Title, r.Body)
	}
	show := must(t, TaskShow, d, TaskShowInput{ID: id})
	if strings.Count(show.Body, "## Updates") != 1 || !strings.Contains(show.Updates, "Met Sam.") ||
		!strings.Contains(show.Updates, "Second note.") || !strings.Contains(show.Updates, "**Not a heading**") {
		t.Errorf("body = %q updates = %q", show.Body, show.Updates)
	}

	// Parts, computed from links.
	child := must(t, TaskAdd, d, TaskAddInput{Title: "Write tests", Parent: id, DoneWhen: "Tests pass."}).ID
	commit := must(t, TaskAdd, d, TaskAddInput{Title: "Send Sam the build", Parent: id, Commitment: true, Due: "2030-01-05", DoneWhen: "Sent."}).ID
	if _, err := TaskAdd.Run(ctx, d, TaskAddInput{Title: "No date", Parent: id, Commitment: true}); err == nil {
		t.Error("a commitment with no due date was accepted")
	}
	follow := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Announce", DependsOn: []string{id}, DoneWhen: "Posted."}).ID
	must(t, QuestionAdd, d, QuestionAddInput{Task: id, Text: "Keep the old parser?"})
	cv := must(t, TaskShow, d, TaskShowInput{ID: child}).Task
	if cv.Parent != id || cv.Project != proj {
		t.Errorf("child = %+v", cv)
	}
	show = must(t, TaskShow, d, TaskShowInput{ID: id})
	p := show.Parts
	if len(p.ActionItems) != 1 || p.ActionItems[0].ID != child || len(p.Commitments) != 1 || p.Commitments[0].ID != commit ||
		len(p.FollowUps) != 1 || p.FollowUps[0].ID != follow || len(show.Questions) != 1 || show.Task.Items != 2 {
		t.Errorf("parts = %+v items = %d", p, show.Task.Items)
	}
	// The parent does not wait on its children.
	if _, err := TaskStart.Run(ctx, d, TaskStartInput{ID: id}); err != nil {
		t.Errorf("start with open items: %v", err)
	}
	list := must(t, TaskList, d, TaskListInput{})
	for _, v := range list.Tasks {
		if v.ID == id && v.Items != 2 {
			t.Errorf("list items = %d", v.Items)
		}
	}

	// Closing the parent lists the open items and questions; they stay open.
	res := must(t, TaskResolve, d, TaskResolveInput{ID: id})
	if len(res.OpenItems) != 2 || len(res.OpenQuestions) != 1 || len(res.Warnings) == 0 {
		t.Errorf("resolve = %+v", res)
	}
	if v := must(t, TaskShow, d, TaskShowInput{ID: child}).Task; v.Status == model.TaskDone {
		t.Error("child closed with parent")
	}
	h, err := Health(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			found = found || (f.Kind == HealthClosedWithOpen && f.ID == id)
		}
	}
	if !found {
		t.Errorf("no closed_with_open_items finding: %+v", h)
	}
	mustLintClean(t, d)
}

func TestRankChildCountsTowardParent(t *testing.T) {
	nodes := []ValueNode{
		{ID: "goal", Kind: KindGoal, Due: "2030-01-10"},
		{ID: "parent", Kind: KindTask, Goal: "goal"},
		{ID: "child", Kind: KindTask, Goal: "goal", Parent: "parent"},
	}
	var child, parent ValueRow
	for _, r := range RankValue(nodes).Rows {
		switch r.ID {
		case "child":
			child = r
		case "parent":
			parent = r
		}
	}
	if child.Impact != 3 || child.Blocks != 0 || parent.Impact != 2 {
		t.Errorf("child = %+v parent = %+v", child, parent)
	}
}
