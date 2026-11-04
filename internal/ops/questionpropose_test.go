package ops

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func agentOf(d Deps) Deps { return Deps{Store: d.Store.ActAs("claude", "mcp")} }

func TestAgentAnswerNeedsSources(t *testing.T) {
	d, proj, _ := questionVault(t)
	agent := agentOf(d)
	note := must(t, NoteAdd, d, NoteAddInput{Title: "Runbook", Body: "Window is Saturday."}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the window", Project: proj, Options: []string{"Sat", "Sun"}}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Schedule", DependsOn: []string{q}}).ID

	// No source, no answer.
	for name, src := range map[string][]model.AnswerSource{
		"none":           nil,
		"empty":          {{Ref: " "}},
		"missing":        {{Ref: "note-nope"}},
		"bad url":        {{Ref: "https://"}},
		"chat, no quote": {{Ref: OwnerInChat}},
		"chat, blank":    {{Ref: OwnerInChat, Quote: "  "}},
	} {
		_, err := QuestionAnswer.Run(ctx, agent, QuestionAnswerInput{ID: q, Choice: "Sat", Sources: src})
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if name == "none" && !errors.Is(err, ErrNoSource) {
			t.Fatalf("none: %v", err)
		}
	}
	if v := must(t, QuestionShow, d, QuestionShowInput{ID: q}).Question; v.Status != model.QuestionOpen {
		t.Fatalf("a refused answer changed the question: %+v", v)
	}

	// With sources the answer is final and frees what waited on it.
	out := must(t, QuestionAnswer, agent, QuestionAnswerInput{ID: q, Choice: "sat", Why: "Runbook says so",
		Sources: []model.AnswerSource{{Ref: note, Quote: "Window is Saturday."}, {Ref: "https://wiki.acme.example/window"}}})
	if out.Status != model.QuestionAnswered || out.AnsweredBy != "claude" || len(out.Unblocked) != 1 || out.Unblocked[0] != task {
		t.Fatalf("out = %+v", out)
	}
	sh := must(t, QuestionShow, d, QuestionShowInput{ID: q})
	for _, want := range []string{"- Source: [[" + note + "]]", "- By: claude"} {
		if !strings.Contains(sh.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, sh.Body)
		}
	}

	// The owner's words in the chat are a source.
	q2 := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which team", Project: proj}).ID
	must(t, QuestionAnswer, agent, QuestionAnswerInput{ID: q2, Text: "Platform.", Sources: []model.AnswerSource{{Ref: "Owner in chat", Quote: "It is the platform team."}}})
	sh = must(t, QuestionShow, d, QuestionShowInput{ID: q2})
	if sh.Question.Status != model.QuestionAnswered || !strings.Contains(sh.Body, OwnerInChat) || !strings.Contains(sh.Body, "It is the platform team.") {
		t.Fatalf("chat source = %+v\n%s", sh.Question, sh.Body)
	}
	// Events hold ids and states, never the answer, the reason, or a quote.
	evs, err := d.Store.Events(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		blob := fmt.Sprintf("%+v", e)
		for _, secret := range []string{"Platform.", "Runbook says so", "Window is Saturday", "platform team"} {
			if strings.Contains(blob, secret) {
				t.Errorf("event holds content %q: %s", secret, blob)
			}
		}
	}
	mustLintClean(t, d)
}

func TestReplyAndHoldNeedSourcesFromAgents(t *testing.T) {
	d := targetDeps(t)
	agent := agentOf(d)
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Can it launch?", Project: proj}).ID
	ask := must(t, QuestionAsk, d, QuestionAskInput{ID: q, To: "person-ada-example", Via: "email", Route: "domain-logging"})
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: ask.RFX, To: "person-ada-example", State: "sent"})
	in := RFXReplyInput{ID: ask.RFX, From: "person-ada-example", Outcome: ReplyAnswered, Text: "Yes."}
	if _, err := RFXReply.Run(ctx, agent, in); !errors.Is(err, ErrNoSource) {
		t.Fatalf("err = %v", err)
	}
	if _, m, _ := getRFX(ctx, d, ask.RFX); m.Targets[0].State == model.TargetReplied {
		t.Fatal("a refused reply was recorded")
	}
	in.URL = "https://chat.acme.example/t/9"
	out := must(t, RFXReply, agent, in)
	if out.Question != q {
		t.Fatalf("out = %+v", out)
	}
	sh := must(t, QuestionShow, d, QuestionShowInput{ID: q})
	if sh.Question.Status != model.QuestionAnswered || !strings.Contains(sh.Body, in.URL) {
		t.Fatalf("question = %+v\n%s", sh.Question, sh.Body)
	}

	q2 := must(t, QuestionAdd, d, QuestionAddInput{Text: "Who owns it?", Project: proj}).ID
	m := must(t, MeetingAdd, d, MeetingAddInput{Title: "Sync", With: []string{"person-ada-example"}, Start: "2026-11-05T10:00:00Z", End: "2026-11-05T10:30:00Z"})
	if _, err := MeetingHold.Run(ctx, agent, MeetingHoldInput{ID: m.ID, Answers: []QuestionAnswerInput{{ID: q2, Text: "Ada"}}}); !errors.Is(err, ErrNoSource) {
		t.Fatalf("hold err = %v", err)
	}
	held := must(t, MeetingHold, agent, MeetingHoldInput{ID: m.ID, Answers: []QuestionAnswerInput{{ID: q2, Text: "Ada", Sources: []model.AnswerSource{{Ref: m.ID}}}}})
	if len(held.Answered) != 1 {
		t.Fatalf("held = %+v", held)
	}
}

func TestQuestionDependsAndEdit(t *testing.T) {
	d, proj, _ := questionVault(t)
	a := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Measure"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the size", Project: proj}).ID
	q2 := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the vendor", Project: proj}).ID
	must(t, QuestionDependsAdd, d, QuestionDependsInput{ID: q, DependsOn: []string{a}})
	must(t, QuestionDependsAdd, d, QuestionDependsInput{ID: q2, DependsOn: []string{q}})
	if _, err := QuestionDependsAdd.Run(ctx, d, QuestionDependsInput{ID: q, DependsOn: []string{q2}}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle: %v", err)
	}
	if _, err := QuestionDependsAdd.Run(ctx, d, QuestionDependsInput{ID: q, DependsOn: []string{q}}); err == nil {
		t.Fatal("self dependency")
	}
	if _, err := TaskDependsAdd.Run(ctx, d, TaskDependsInput{ID: a, DependsOn: []string{q2}}); err == nil {
		t.Fatal("a task cycle through questions was accepted")
	}
	list := must(t, QuestionList, d, QuestionListInput{})
	if list.Questions[0].ID != q || len(list.Questions[1].WaitingOn) != 1 || len(list.Questions[0].WaitingOn) != 1 {
		t.Fatalf("list = %+v", list.Questions)
	}
	g := must(t, Graph, d, GraphInput{})
	var st string
	for _, n := range g.Nodes {
		if n.ID == q {
			st = n.State
		}
	}
	if st != StateWaiting {
		t.Fatalf("graph state of a waiting question = %q\n%+v", st, g.Nodes)
	}
	must(t, TaskStart, d, TaskStartInput{ID: a})
	must(t, TaskDone, d, TaskDoneInput{ID: a})
	if got := must(t, QuestionList, d, QuestionListInput{}).Questions; len(got[0].WaitingOn) != 0 {
		t.Fatalf("still waiting: %+v", got[0])
	}
	must(t, QuestionDependsRemove, d, QuestionDependsInput{ID: q2, DependsOn: []string{q}})

	e := must(t, QuestionEdit, d, QuestionEditInput{ID: q, Title: "Pick the cluster size", Options: []string{"S", "M"}, Recommended: "M"})
	if e.Title != "Pick the cluster size" || e.Recommended != "M" || len(e.Options) != 2 {
		t.Fatalf("edit = %+v", e)
	}
	if _, err := QuestionEdit.Run(ctx, d, QuestionEditInput{ID: q, Recommended: "XL"}); err == nil {
		t.Fatal("recommended outside the options")
	}
	mustLintClean(t, d)
}

func TestTaskToQuestionKeepsDependencies(t *testing.T) {
	d, proj, _ := questionVault(t)
	dep := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Measure"}).ID
	old := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Decide size", DependsOn: []string{dep}}).ID
	out := must(t, TaskToQuestion, d, TaskToQuestionInput{ID: old, Title: "Which size?"})
	sh := must(t, QuestionShow, d, QuestionShowInput{ID: out.Question})
	if sh.Question.Title != "Which size?" || len(sh.Question.DependsOn) != 1 || sh.Question.DependsOn[0] != dep {
		t.Fatalf("question = %+v", sh.Question)
	}
	mustLintClean(t, d)
}
