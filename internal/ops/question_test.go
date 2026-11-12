package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// questionVault is a project with an area, and a task per use.
func questionVault(t *testing.T) (Deps, string, string) {
	t.Helper()
	d := directoryDeps(t)
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Atlas"}).ID
	proj := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example", Area: area}).ID
	return d, proj, area
}

func TestQuestionNeedsExactlyOneScope(t *testing.T) {
	d, proj, area := questionVault(t)
	task := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Write parser"}).ID
	for name, in := range map[string]QuestionAddInput{
		"none":          {Text: "Which port?"},
		"two":           {Text: "Which port?", Goal: proj, Area: area},
		"root and task": {Text: "Which port?", Root: true, Task: task},
		"unknown":       {Text: "Which port?", Goal: "project-nope"},
		"wrong type":    {Text: "Which port?", Goal: area},
	} {
		if _, err := QuestionAdd.Run(ctx, d, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, in := range map[string]QuestionAddInput{
		"project": {Text: "Which port?", Goal: proj},
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
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the window date", Goal: proj}).ID
	other := must(t, QuestionAdd, d, QuestionAddInput{Text: "Who owns it?", Root: true}).ID
	a := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Schedule", DependsOn: []string{q}}).ID
	b := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Announce", DependsOn: []string{a}}).ID
	free := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Free"}).ID

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
	g := must(t, Graph, d, GraphInput{Goal: proj})
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
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Use Redis?", Goal: proj}).ID
	a := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Cache", DependsOn: []string{q}}).ID
	out := must(t, QuestionDrop, d, QuestionDropInput{ID: q, Reason: "Cache removed from scope."})
	if out.Status != model.QuestionDropped || len(out.Unblocked) != 1 || out.Unblocked[0] != a {
		t.Fatalf("out = %+v", out)
	}
	must(t, TaskStart, d, TaskStartInput{ID: a})
	mustLintClean(t, d)
}

func TestQuestionOptionsAndDecisions(t *testing.T) {
	d, proj, _ := questionVault(t)
	if _, err := QuestionAdd.Run(ctx, d, QuestionAddInput{Text: "Path?", Goal: proj, Options: []string{"a"}, Recommended: "b"}); err == nil {
		t.Fatal("recommended outside the options")
	}
	if _, err := QuestionAdd.Run(ctx, d, QuestionAddInput{Text: "Path?", Goal: proj, Kind: "maybe"}); err == nil {
		t.Fatal("bad kind")
	}
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Decide B-1: config path for prod", Goal: proj, Kind: model.KindDecision,
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
	// Free text for a question without options; a choice with text is refused,
	// a choice alone is the text.
	free := must(t, QuestionAdd, d, QuestionAddInput{Text: "Give the missing prod keys", Goal: proj, Kind: model.KindInput}).ID
	if _, err := QuestionAnswer.Run(ctx, d, QuestionAnswerInput{ID: free, Choice: "x", Text: "y"}); err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("choice and text on a question with no options: %v", err)
	}
	must(t, QuestionAnswer, d, QuestionAnswerInput{ID: free, Text: "Held in the team vault, item atlas-prod."})
	free2 := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which region?", Goal: proj, Kind: model.KindInput}).ID
	must(t, QuestionAnswer, d, QuestionAnswerInput{ID: free2, Choice: "eu-west"})
	if sh := must(t, QuestionShow, d, QuestionShowInput{ID: free2}); sh.Question.Status != model.QuestionAnswered || !strings.Contains(sh.Body, "eu-west") {
		t.Fatalf("choice as text: %+v", sh)
	}
	mustLintClean(t, d)
}

func TestQuestionAskAndReply(t *testing.T) {
	d := targetDeps(t)
	proj := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Can the portal launch before the audit?\n\nContext line.", Goal: proj}).ID
	a := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Launch", DependsOn: []string{q}}).ID
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
	if m.Question.Target() != q || m.Goal.Target() != proj || m.Direction != model.RFXOut || len(m.Targets) != 1 {
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
	proj2 := must(t, GoalAdd, d2, GoalAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	q2 := must(t, QuestionAdd, d2, QuestionAddInput{Text: "Can the portal launch?", Goal: proj2}).ID
	a2 := must(t, TaskAdd, d2, TaskAddInput{Goal: proj2, Title: "Launch", DependsOn: []string{q2}}).ID
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
	old := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Decide B-1: config path for prod", Body: "Two candidates."}).ID
	dep := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Deploy", DependsOn: []string{old}}).ID
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
	id := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Rotate keys"}).ID
	must(t, TaskDone, d, TaskDoneInput{ID: id, Note: "Rotated; old keys revoked on 2026-11-01."})
	doc, _, _ := getTask(ctx, d.Store, id)
	if !strings.Contains(doc.Body, "## Outcome\n\nRotated; old keys revoked on 2026-11-01.") {
		t.Fatalf("body = %q", doc.Body)
	}
	plain := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Plain"}).ID
	must(t, TaskDone, d, TaskDoneInput{ID: plain})
	if doc, _, _ := getTask(ctx, d.Store, plain); strings.Contains(doc.Body, "Outcome") {
		t.Fatalf("body = %q", doc.Body)
	}
	mustLintClean(t, d)
}

func TestIOFileToQuestionNeedsScope(t *testing.T) {
	d, proj, _ := questionVault(t)
	in := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Which vendor for the audit?"}).ID
	if _, err := IOFile.Run(ctx, d, IOFileInput{ID: in, To: TriageQuestion}); err == nil {
		t.Fatal("triaged to an unscoped question")
	}
	if _, err := IOFile.Run(ctx, d, IOFileInput{ID: in, To: TriageQuestion, Root: true, Goal: proj}); err == nil {
		t.Fatal("triaged to a question with two scopes")
	}
	if _, err := IOFile.Run(ctx, d, IOFileInput{ID: in, To: TriageTask, Goal: proj, Root: true}); err == nil {
		t.Fatal("root applies to a question only")
	}
	out := must(t, IOFile, d, IOFileInput{ID: in, To: TriageQuestion, Root: true})
	if !strings.HasPrefix(out.Became, "question-") {
		t.Fatalf("out = %+v", out)
	}
	if got := must(t, QuestionList, d, QuestionListInput{Root: true}); len(got.Questions) != 1 || got.Questions[0].ScopeType != ScopeRoot {
		t.Fatalf("root list = %+v", got.Questions)
	}
	mustLintClean(t, d)
}

func TestQuestionsInScope(t *testing.T) {
	d, proj, area := questionVault(t)
	task := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Write parser"}).ID
	qp := must(t, QuestionAdd, d, QuestionAddInput{Text: "P?", Goal: proj}).ID
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
	if got := must(t, GoalShow, d, GoalShowInput{ID: proj}).Questions; !has(got, qp, qt) {
		t.Errorf("project show = %v", ids(got))
	}
	if got := must(t, TaskShow, d, TaskShowInput{ID: task}).Questions; !has(got, qt) {
		t.Errorf("task show = %v", ids(got))
	}
	if got := must(t, AreaShow, d, AreaShowInput{ID: area}).Questions; !has(got, qp, qt, qa) {
		t.Errorf("area show = %v", ids(got))
	}
	if got := must(t, QuestionList, d, QuestionListInput{Goal: proj}).Questions; !has(got, qp, qt) {
		t.Errorf("list goal = %v", ids(got))
	}
	if got := must(t, QuestionList, d, QuestionListInput{Task: task}).Questions; !has(got, qt) {
		t.Errorf("list --task = %v", ids(got))
	}
	w := must(t, Where, d, WhereInput{Query: "portal"})
	if len(w.Matches) != 1 || !has(w.Matches[0].Questions, qp, qt) {
		t.Errorf("where = %+v", w.Matches)
	}
}

func TestAgentDecisionNeedsWeighing(t *testing.T) {
	f := newAgentFix(t)
	base := QuestionAddInput{Text: "Which path?", Goal: f.proj, Kind: model.KindDecision}
	bad := []struct {
		name string
		in   func(QuestionAddInput) QuestionAddInput
		want string
	}{
		{"no options", func(i QuestionAddInput) QuestionAddInput { return i }, "needs options"},
		{"bare options", func(i QuestionAddInput) QuestionAddInput {
			i.Options, i.Recommended = []string{"A", "B"}, "A"
			return i
		}, "weighs every option"},
		{"no recommendation", func(i QuestionAddInput) QuestionAddInput {
			i.OptionDetails = []model.OptionDetail{{Name: "A"}, {Name: "B"}}
			return i
		}, "recommendation"},
		{"no basis", func(i QuestionAddInput) QuestionAddInput {
			i.OptionDetails = []model.OptionDetail{{Name: "A", Recommendation: 60}, {Name: "B"}}
			return i
		}, "confidence_basis"},
		{"unsourced claim", func(i QuestionAddInput) QuestionAddInput {
			i.OptionDetails = []model.OptionDetail{{Name: "A", Pros: []model.Claim{{Text: "Fast."}}}, {Name: "B"}}
			return i
		}, "no source"},
		{"no source at high confidence", func(i QuestionAddInput) QuestionAddInput {
			i.OptionDetails = []model.OptionDetail{{Name: "A", Confidence: 90, Pros: []model.Claim{{Text: "Fast.", Source: model.NoSource}}}, {Name: "B"}}
			return i
		}, "must be low"},
	}
	for _, c := range bad {
		if _, err := QuestionAdd.Run(t.Context(), f.agent, c.in(base)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	good := base
	good.Context = "The installer writes one of these."
	good.OptionDetails = []model.OptionDetail{
		{Name: "A", Confidence: 40, ConfidenceBasis: "One note.", Recommendation: 70, Pros: []model.Claim{{Text: "Fast.", Source: model.NoSource}},
			Examples: []model.Example{{Kind: "failure", Text: "Beta broke.", Source: f.source}}},
		{Name: "B", Confidence: 20, ConfidenceBasis: "None.", Recommendation: 30},
	}
	out, err := QuestionAdd.Run(t.Context(), f.agent, good)
	if err != nil {
		t.Fatal(err)
	}
	show, err := QuestionShow.Run(t.Context(), f.owner, QuestionShowInput{ID: out.ID})
	if err != nil {
		t.Fatal(err)
	}
	if show.Question.Recommended != "A" || len(show.Question.Options) != 2 || show.Question.OptionDetails[0].Examples[0].Kind != "failure" {
		t.Fatalf("question = %+v", show.Question)
	}
	if _, err := QuestionAdd.Run(t.Context(), f.owner, base); err != nil {
		t.Fatalf("a person may file a bare decision: %v", err)
	}
}

// A question about an item lists under that item (the item page shows its options as buttons),
// and answering it by a choice works. The item may be any type, an io item here.
func TestQuestionAboutItemListsAndAnswers(t *testing.T) {
	d, proj, _ := questionVault(t)
	io := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Can we move the review to Friday?"}).ID
	other := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Unrelated note"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{
		Text: "How do we answer the review request?", Root: true, About: io,
		Options: []string{"Accept Friday", "Offer Monday"}, Recommended: "Offer Monday",
	}).ID
	must(t, QuestionAdd, d, QuestionAddInput{Text: "Unlinked", Goal: proj})

	got := must(t, QuestionList, d, QuestionListInput{About: io})
	if len(got.Questions) != 1 || got.Questions[0].ID != q || got.Questions[0].About != io ||
		len(got.Questions[0].Options) != 2 || got.Questions[0].Recommended != "Offer Monday" {
		t.Fatalf("about %s = %+v", io, got.Questions)
	}
	if got := must(t, QuestionList, d, QuestionListInput{About: other}); len(got.Questions) != 0 {
		t.Fatalf("about %s = %+v", other, got.Questions)
	}
	// A question scoped to an item lists under it too.
	if got := must(t, QuestionList, d, QuestionListInput{About: proj}); len(got.Questions) != 1 {
		t.Fatalf("about goal = %+v", got.Questions)
	}
	if _, err := QuestionAdd.Run(ctx, d, QuestionAddInput{Text: "Bad", Root: true, About: "io-nope"}); err == nil {
		t.Fatal("about an unknown item accepted")
	}

	r := must(t, QuestionReview, d, QuestionReviewInput{ID: q, Decision: ReviewAnswer, Choices: []string{"Offer Monday"}})
	if r.Status != model.QuestionAnswered {
		t.Fatalf("review = %+v", r)
	}
	if got := must(t, QuestionList, d, QuestionListInput{About: io}); len(got.Questions) != 0 {
		t.Fatalf("answered question still listed: %+v", got.Questions)
	}
	mustLintClean(t, d)
}
