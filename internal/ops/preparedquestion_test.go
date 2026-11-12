package ops

import (
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// A draft threaded to a question links it, and the question reads as
// prepared by that draft, as an ask made to the owner does.
func TestDraftThreadedToQuestionPreparesIt(t *testing.T) {
	d := gapVault(t)
	proj := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which CA do we use?", Goal: proj}).ID
	if r := rowOf(t, d, q); r.Ready {
		t.Fatalf("raw question is ready: %+v", r)
	}
	draft := must(t, IOAdd, agentOf(d), IOAddInput{Direction: model.IOOut, Medium: model.MediumOther, Subject: "CA choice",
		Text: "Post the CA comparison to the wiki page.", To: []string{"wiki.acme.example/ca"}, Expects: model.ExpectsNone, Thread: q})
	sh := must(t, IOShow, d, IOShowInput{ID: draft.ID})
	if sh.Item.Thread != q {
		t.Fatalf("thread = %q, want %s", sh.Item.Thread, q)
	}
	if r := rowOf(t, d, q); !r.Ready || r.ReadyKind != model.PreparedDraft || r.ReadyRef != draft.ID {
		t.Fatalf("question with a threaded draft: %+v", r)
	}
	// A task is still refused as a thread.
	task := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Renew cert", DoneWhen: "Renewed."}).ID
	if _, err := IOAdd.Run(ctx, agentOf(d), IOAddInput{Direction: model.IOOut, Medium: model.MediumOther, Text: "x",
		To: []string{"wiki.acme.example/ca"}, Expects: model.ExpectsNone, Thread: task}); err == nil {
		t.Fatal("a task accepted as thread")
	}
	mustLintClean(t, d)
}
