package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func proposeAnswer(t *testing.T, v *testVault, text string) string {
	t.Helper()
	ctx := context.Background()
	q, err := ops.QuestionAdd.Run(ctx, v.env.Deps, ops.QuestionAddInput{Text: "Which port does prod use?", Project: v.ids["project"]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.TaskAdd.Run(ctx, v.env.Deps, ops.TaskAddInput{Project: v.ids["project"], Title: "Open the port", DependsOn: []string{q.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.QuestionAnswer.Run(ctx, v.agent, ops.QuestionAnswerInput{ID: q.ID, Text: text, Why: "The runbook says so",
		Sources: []model.AnswerSource{{Ref: v.ids["project"], Quote: "Port 7700."}, {Ref: "https://wiki.acme.example/ports"}}}); err != nil {
		t.Fatal(err)
	}
	return q.ID
}

// An agent's answer is listed first on Review with its sources, Today counts
// it, and a person accepts it (or rejects it with a reason).
func TestAnswersToCheck(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	q := proposeAnswer(t, v, "Port 7700.")
	d := newDrv(t, v, 120, 36)
	if !d.has("1 answer to check") || !d.has("check") {
		t.Fatalf("Today does not count the answer:\n%s", d.plainFrame())
	}
	d.key("2")
	f := d.plainFrame()
	i, j := strings.Index(f, "Answers to check (1)"), strings.Index(f, "Proposals (")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("Answers to check is not before Proposals:\n%s", f)
	}
	for _, want := range []string{"proposed: Port 7700.", "why: The runbook says so", "https://wiki.acme.example/ports", `"Port 7700."`} {
		if !strings.Contains(f, want) {
			t.Errorf("Review lacks %q:\n%s", want, f)
		}
	}
	if !d.has("Reject answer") {
		t.Errorf("no Reject on a proposed answer: %q", d.footer())
	}
	// Accept, with the text changed.
	d.key("a")
	d.typeText(" Use 7701 for staging.")
	d.key("enter")
	sh, err := ops.QuestionShow.Run(context.Background(), v.env.Deps, ops.QuestionShowInput{ID: q})
	if err != nil {
		t.Fatal(err)
	}
	if sh.Question.Status != model.QuestionAnswered || sh.Question.AnsweredBy != Actor || sh.Question.ProposedBy == "" ||
		!strings.Contains(sh.Body, "Use 7701 for staging.") || len(sh.Question.ProposedSources) != 2 {
		t.Fatalf("after accept: %+v\n%s", sh.Question, sh.Body)
	}

	// A second proposal is rejected with a reason the agent reads.
	q2 := proposeAnswer(t, v, "Port 9000.")
	d = newDrv(t, v, 120, 36)
	d.key("2")
	d.key("e")
	d.typeText("Wrong environment")
	d.key("enter")
	sh, _ = ops.QuestionShow.Run(context.Background(), v.agent, ops.QuestionShowInput{ID: q2})
	if sh.Question.HasProposal || sh.Question.RejectedReason != "Wrong environment" || sh.Question.Status != model.QuestionOpen {
		t.Fatalf("after reject: %+v", sh.Question)
	}
}
