package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

// questionVault is a project with an area, and a task per use.
func questionVault(t *testing.T) (Deps, string, string) {
	t.Helper()
	d := directoryDeps(t)
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Atlas"}).ID
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example", Area: area}).ID
	return d, proj, area
}

func TestQuestionNeedsExactlyOneScope(t *testing.T) {
	d, proj, area := questionVault(t)
	task := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Write parser"}).ID
	for name, in := range map[string]QuestionAddInput{
		"none":          {Text: "Which port?"},
		"two":           {Text: "Which port?", Project: proj, Area: area},
		"root and task": {Text: "Which port?", Root: true, Task: task},
		"unknown":       {Text: "Which port?", Project: "project-nope"},
		"wrong type":    {Text: "Which port?", Project: area},
	} {
		if _, err := QuestionAdd.Run(ctx, d, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, in := range map[string]QuestionAddInput{
		"project": {Text: "Which port?", Project: proj},
		"task":    {Text: "Which port?", Task: task},
		"area":    {Text: "Which port?", Area: area},
		"root":    {Text: "Which port?", Root: true},
	} {
		must(t, QuestionAdd, d, in)
		_ = name
	}
	if got := must(t, QuestionList, d, QuestionListInput{}); len(got.Questions) != 4 {
		t.Fatalf("questions = %+v", got.Questions)
	}
	mustLintClean(t, d)
}

func TestQuestionBlocksTasksUntilAnswered(t *testing.T) {
	d, proj, _ := questionVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the window date", Project: proj}).ID
	other := must(t, QuestionAdd, d, QuestionAddInput{Text: "Who owns it?", Root: true}).ID
	a := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Schedule", DependsOn: []string{q}}).ID
	b := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Announce", DependsOn: []string{a}}).ID
	free := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Free"}).ID

	ids := func(ts []TaskView) []string {
		out := []string{}
		for _, t := range ts {
			out = append(out, t.ID)
		}
		return out
	}
	if got := ids(must(t, Next, d, NextInput{}).Tasks); len(got) != 1 || got[0] != free {
		t.Fatalf("next = %v, want only %s", got, free)
	}
	g := must(t, Graph, d, GraphInput{Project: proj})
	var node GraphNode
	for _, n := range g.Nodes {
		if n.ID == q {
			node = n
		}
	}
	if node.Type != model.TypeQuestion || node.Status != "open" || node.Scope != proj {
		t.Fatalf("question node = %+v", node)
	}
	for _, n := range g.Nodes {
		if n.ID == a && (n.State != StateWaiting || len(n.WaitingOn) != 1 || n.WaitingOn[0] != q) {
			t.Fatalf("task node = %+v", n)
		}
	}
	if _, err := TaskStart.Run(ctx, d, TaskStartInput{ID: a}); err == nil || !strings.Contains(err.Error(), q) {
		t.Fatalf("start err = %v", err)
	}

	// Open questions list by what they block: q blocks a and b, other blocks nothing.
	list := must(t, QuestionList, d, QuestionListInput{})
	if len(list.Questions) != 2 || list.Questions[0].ID != q || list.Questions[0].Blocking != 2 ||
		list.Questions[1].ID != other || list.Questions[1].Blocking != 0 {
		t.Fatalf("list = %+v", list.Questions)
	}
	if got := list.Questions[0].Blocks; len(got) != 1 || got[0] != a {
		t.Fatalf("blocks = %v", got)
	}

	out := must(t, QuestionAnswer, d, QuestionAnswerInput{ID: q, Text: "Saturday 2026-12-05.", Source: "Review on 2026-11-01"})
	if out.Status != model.QuestionAnswered || len(out.Unblocked) != 1 || out.Unblocked[0] != a || out.AnsweredBy != "user" {
		t.Fatalf("out = %+v", out)
	}
	if got := ids(must(t, Next, d, NextInput{}).Tasks); len(got) != 2 || got[0] != a && got[1] != a {
		t.Fatalf("next after answer = %v", got)
	}
	must(t, TaskStart, d, TaskStartInput{ID: a})
	if _, err := QuestionAnswer.Run(ctx, d, QuestionAnswerInput{ID: q, Text: "again"}); err == nil {
		t.Fatal("answered twice")
	}
	if open := must(t, QuestionList, d, QuestionListInput{}); len(open.Questions) != 1 {
		t.Fatalf("open = %+v", open.Questions)
	}
	if all := must(t, QuestionList, d, QuestionListInput{All: true}); len(all.Questions) != 2 {
		t.Fatalf("all = %+v", all.Questions)
	}
	doc, _, _ := getQuestion(ctx, d.Store, q)
	if !strings.Contains(doc.Body, "## Answer") || !strings.Contains(doc.Body, "Saturday 2026-12-05.") ||
		!strings.Contains(doc.Body, "- Source: Review on 2026-11-01") || !strings.Contains(doc.Body, "- By: user, via cli, on ") {
		t.Fatalf("body = %q", doc.Body)
	}
	_ = b
	mustLintClean(t, d)
}

func TestQuestionDropResolves(t *testing.T) {
	d, proj, _ := questionVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Use Redis?", Project: proj}).ID
	a := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Cache", DependsOn: []string{q}}).ID
	out := must(t, QuestionDrop, d, QuestionDropInput{ID: q, Reason: "Cache removed from scope."})
	if out.Status != model.QuestionDropped || len(out.Unblocked) != 1 || out.Unblocked[0] != a {
		t.Fatalf("out = %+v", out)
	}
	must(t, TaskStart, d, TaskStartInput{ID: a})
	mustLintClean(t, d)
}

func TestQuestionOptionsAndDecisions(t *testing.T) {
	d, proj, _ := questionVault(t)
	if _, err := QuestionAdd.Run(ctx, d, QuestionAddInput{Text: "Path?", Project: proj, Options: []string{"a"}, Recommended: "b"}); err == nil {
		t.Fatal("recommended outside the options")
	}
	if _, err := QuestionAdd.Run(ctx, d, QuestionAddInput{Text: "Path?", Project: proj, Kind: "maybe"}); err == nil {
		t.Fatal("bad kind")
	}
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Decide B-1: config path for prod", Project: proj, Kind: model.KindDecision,
		Options: []string{"/etc/atlas", "/opt/atlas"}, Recommended: "/etc/atlas"}).ID
	for name, in := range map[string]QuestionAnswerInput{
		"no reason":     {ID: q, Choice: "/etc/atlas"},
		"not an option": {ID: q, Choice: "/var/atlas", Why: "x"},
		"number range":  {ID: q, Choice: "3", Why: "x"},
		"empty":         {ID: q, Why: "x"},
	} {
		if _, err := QuestionAnswer.Run(ctx, d, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	out := must(t, QuestionAnswer, d, QuestionAnswerInput{ID: q, Choice: "2", Why: "Matches the container image."})
	if out.Choice != "/opt/atlas" {
		t.Fatalf("choice = %q", out.Choice)
	}
	sh := must(t, QuestionShow, d, QuestionShowInput{ID: q})
	if sh.Question.Status != model.QuestionAnswered || sh.Question.Kind != model.KindDecision ||
		!strings.Contains(sh.Body, "- Why: Matches the container image.") || !strings.Contains(sh.Body, "- Choice: /opt/atlas") {
		t.Fatalf("show = %+v", sh)
	}
	// Free text for a question without options; a choice is refused.
	free := must(t, QuestionAdd, d, QuestionAddInput{Text: "Give the missing prod keys", Project: proj, Kind: model.KindInput}).ID
	if _, err := QuestionAnswer.Run(ctx, d, QuestionAnswerInput{ID: free, Choice: "x"}); err == nil {
		t.Fatal("choice on a question with no options")
	}
	must(t, QuestionAnswer, d, QuestionAnswerInput{ID: free, Text: "Held in the team vault, item atlas-prod."})
	mustLintClean(t, d)
}

func TestQuestionAskAndReply(t *testing.T) {
	d := targetDeps(t)
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Can the portal launch before the audit?\n\nContext line.", Project: proj}).ID
	a := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Launch", DependsOn: []string{q}}).ID
	if _, err := QuestionAsk.Run(ctx, d, QuestionAskInput{ID: q, To: "person-ada-example"}); err == nil {
		t.Fatal("asked with no medium")
	}
	ask := must(t, QuestionAsk, d, QuestionAskInput{ID: q, To: "person-ada-example", Via: "email", Route: "domain-logging", SelfServe: "searched the wiki"})
	if !strings.HasPrefix(ask.RFX, "rfx-") {
		t.Fatalf("ask = %+v", ask)
	}
	if _, err := QuestionAsk.Run(ctx, d, QuestionAskInput{ID: q, To: "person-ada-example", Via: "email", Route: "domain-logging"}); err == nil {
		t.Fatal("asked twice")
	}
	_, m, _ := getRFX(ctx, d, ask.RFX)
	if m.Question.Target() != q || m.Project.Target() != proj || m.Direction != model.RFXOut || len(m.Targets) != 1 {
		t.Fatalf("rfx = %+v", m)
	}
	if sh := must(t, QuestionShow, d, QuestionShowInput{ID: q}); sh.Question.RFX != ask.RFX {
		t.Fatalf("question rfx = %q", sh.Question.RFX)
	}
	rdoc, _, _ := getRFX(ctx, d, ask.RFX)
	if !strings.Contains(rdoc.Body, "Context line.") {
		t.Fatalf("rfx body = %q", rdoc.Body)
	}
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: ask.RFX, To: "person-ada-example", State: "sent"})

	// A partial reply leaves the question open.
	if _, err := RFXReply.Run(ctx, d, RFXReplyInput{ID: ask.RFX, From: "person-ada-example", Outcome: ReplyPartial, Text: "Checking."}); err != nil {
		t.Fatal(err)
	}
	if sh := must(t, QuestionShow, d, QuestionShowInput{ID: q}); sh.Question.Status != model.QuestionOpen {
		t.Fatalf("partial reply answered it: %+v", sh.Question)
	}
	if _, err := RFXReply.Run(ctx, d, RFXReplyInput{ID: ask.RFX, From: "person-ada-example", Outcome: ReplyAnswered, Text: "Yes."}); err == nil {
		t.Fatal("second reply from the same target")
	}
	d2 := targetDeps(t)
	proj2 := must(t, ProjectAdd, d2, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	q2 := must(t, QuestionAdd, d2, QuestionAddInput{Text: "Can the portal launch?", Project: proj2}).ID
	a2 := must(t, TaskAdd, d2, TaskAddInput{Project: proj2, Title: "Launch", DependsOn: []string{q2}}).ID
	ask2 := must(t, QuestionAsk, d2, QuestionAskInput{ID: q2, To: "person-ada-example", Via: "email", Route: "domain-logging"})
	must(t, RFXTargetState, d2, RFXTargetStateInput{ID: ask2.RFX, To: "person-ada-example", State: "sent"})
	out := must(t, RFXReply, d2, RFXReplyInput{ID: ask2.RFX, From: "person-ada-example", Outcome: ReplyAnswered, Text: "Yes, ship it.", URL: "https://chat.acme.example/t/1"})
	if out.Question != q2 {
		t.Fatalf("reply out = %+v", out)
	}
	sh := must(t, QuestionShow, d2, QuestionShowInput{ID: q2})
	if sh.Question.Status != model.QuestionAnswered || sh.Question.AnsweredBy != "person-ada-example" || !strings.Contains(sh.Body, "Yes, ship it.") {
		t.Fatalf("question = %+v\n%s", sh.Question, sh.Body)
	}
	must(t, TaskStart, d2, TaskStartInput{ID: a2})
	_ = a
	mustLintClean(t, d)
	mustLintClean(t, d2)
}

func TestTaskToQuestion(t *testing.T) {
	d, proj, _ := questionVault(t)
	old := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Decide B-1: config path for prod", Body: "Two candidates."}).ID
	dep := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Deploy", DependsOn: []string{old}}).ID
	rfx := addRFX(t, d, RFXAddInput{DependsOn: []string{old}})
	out := must(t, TaskToQuestion, d, TaskToQuestionInput{ID: old, Kind: model.KindDecision, Options: []string{"/etc/atlas", "/opt/atlas"}})
	if !strings.HasPrefix(out.Question, "question-") || out.Task != old || len(out.Relinked) != 2 {
		t.Fatalf("out = %+v", out)
	}
	_, ot, _ := getTask(ctx, d.Store, old)
	odoc, _, _ := getTask(ctx, d.Store, old)
	if ot.Status != model.TaskDropped || !strings.Contains(odoc.Body, "[["+out.Question+"]]") {
		t.Fatalf("old task = %+v\n%s", ot, odoc.Body)
	}
	_, dt, _ := getTask(ctx, d.Store, dep)
	if len(dt.DependsOn) != 1 || dt.DependsOn[0].Target() != out.Question {
		t.Fatalf("dependent = %+v", dt.DependsOn)
	}
	_, rm, _ := getRFX(ctx, d, rfx)
	if len(rm.DependsOn) != 1 || rm.DependsOn[0].Target() != out.Question {
		t.Fatalf("rfx deps = %+v", rm.DependsOn)
	}
	qd, q, _ := getQuestion(ctx, d.Store, out.Question)
	if q.Scope.Target() != proj || q.AsKind() != model.KindDecision || qd.Title != "Decide B-1: config path for prod" ||
		!strings.Contains(qd.Body, "Two candidates.") {
		t.Fatalf("question = %+v\n%s", q, qd.Body)
	}
	if _, err := TaskToQuestion.Run(ctx, d, TaskToQuestionInput{ID: old}); err == nil {
		t.Fatal("converted a dropped task")
	}
	if _, err := TaskStart.Run(ctx, d, TaskStartInput{ID: dep}); err == nil {
		t.Fatal("started while the question is open")
	}
	must(t, QuestionAnswer, d, QuestionAnswerInput{ID: out.Question, Choice: "1", Why: "Default path."})
	must(t, TaskStart, d, TaskStartInput{ID: dep})
	mustLintClean(t, d)
}

func TestTaskDoneNote(t *testing.T) {
	d, proj, _ := questionVault(t)
	id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Rotate keys"}).ID
	must(t, TaskDone, d, TaskDoneInput{ID: id, Note: "Rotated; old keys revoked on 2026-11-01."})
	doc, _, _ := getTask(ctx, d.Store, id)
	if !strings.Contains(doc.Body, "## Outcome\n\nRotated; old keys revoked on 2026-11-01.") {
		t.Fatalf("body = %q", doc.Body)
	}
	plain := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Plain"}).ID
	must(t, TaskDone, d, TaskDoneInput{ID: plain})
	if doc, _, _ := getTask(ctx, d.Store, plain); strings.Contains(doc.Body, "Outcome") {
		t.Fatalf("body = %q", doc.Body)
	}
	mustLintClean(t, d)
}

func TestInboxTriageToQuestionNeedsScope(t *testing.T) {
	d, proj, _ := questionVault(t)
	in := must(t, InboxAdd, d, InboxAddInput{Text: "Which vendor for the audit?"}).ID
	if _, err := InboxTriage.Run(ctx, d, InboxTriageInput{ID: in, To: TriageQuestion}); err == nil {
		t.Fatal("triaged to an unscoped question")
	}
	if _, err := InboxTriage.Run(ctx, d, InboxTriageInput{ID: in, To: TriageQuestion, Root: true, Project: proj}); err == nil {
		t.Fatal("triaged to a question with two scopes")
	}
	if _, err := InboxTriage.Run(ctx, d, InboxTriageInput{ID: in, To: TriageTask, Project: proj, Root: true}); err == nil {
		t.Fatal("root applies to a question only")
	}
	out := must(t, InboxTriage, d, InboxTriageInput{ID: in, To: TriageQuestion, Root: true})
	if !strings.HasPrefix(out.Created, "question-") {
		t.Fatalf("out = %+v", out)
	}
	if got := must(t, QuestionList, d, QuestionListInput{Root: true}); len(got.Questions) != 1 || got.Questions[0].ScopeType != ScopeRoot {
		t.Fatalf("root list = %+v", got.Questions)
	}
	mustLintClean(t, d)
}

func TestQuestionsInScope(t *testing.T) {
	d, proj, area := questionVault(t)
	task := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Write parser"}).ID
	qp := must(t, QuestionAdd, d, QuestionAddInput{Text: "P?", Project: proj}).ID
	qt := must(t, QuestionAdd, d, QuestionAddInput{Text: "T?", Task: task}).ID
	qa := must(t, QuestionAdd, d, QuestionAddInput{Text: "A?", Area: area}).ID
	must(t, QuestionAdd, d, QuestionAddInput{Text: "R?", Root: true})
	ids := func(vs []QuestionView) []string {
		out := []string{}
		for _, v := range vs {
			out = append(out, v.ID)
		}
		return out
	}
	has := func(vs []QuestionView, want ...string) bool {
		got := ids(vs)
		if len(got) != len(want) {
			return false
		}
		for _, w := range want {
			found := false
			for _, g := range got {
				found = found || g == w
			}
			if !found {
				return false
			}
		}
		return true
	}
	if got := must(t, ProjectShow, d, ProjectShowInput{ID: proj}).Questions; !has(got, qp, qt) {
		t.Errorf("project show = %v", ids(got))
	}
	if got := must(t, TaskShow, d, TaskShowInput{ID: task}).Questions; !has(got, qt) {
		t.Errorf("task show = %v", ids(got))
	}
	if got := must(t, AreaShow, d, AreaShowInput{ID: area}).Questions; !has(got, qp, qt, qa) {
		t.Errorf("area show = %v", ids(got))
	}
	if got := must(t, QuestionList, d, QuestionListInput{Project: proj}).Questions; !has(got, qp, qt) {
		t.Errorf("list --project = %v", ids(got))
	}
	if got := must(t, QuestionList, d, QuestionListInput{Task: task}).Questions; !has(got, qt) {
		t.Errorf("list --task = %v", ids(got))
	}
	w := must(t, Where, d, WhereInput{Query: "portal"})
	if len(w.Matches) != 1 || !has(w.Matches[0].Questions, qp, qt) {
		t.Errorf("where = %+v", w.Matches)
	}
}
