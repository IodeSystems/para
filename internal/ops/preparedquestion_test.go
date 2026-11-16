package ops

import (
	"slices"
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
	// The question waits on its draft (it depends_on it); the draft is the
	// owner's row, to approve.
	if !slices.Contains(orderList(t, d), draft.ID) || slices.Contains(orderList(t, d), q) {
		t.Fatalf("order = %v, want the draft %s and not the question %s", orderList(t, d), draft.ID, q)
	}
	if r := rowOf(t, d, draft.ID); !r.Ready || r.ReadyKind != model.PreparedDraft {
		t.Fatalf("draft row: %+v", r)
	}
	// A task is a thread too: it waits on the draft.
	task := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Renew cert", DoneWhen: "Renewed."}).ID
	tdraft := must(t, IOAdd, agentOf(d), IOAddInput{Direction: model.IOOut, Medium: model.MediumOther, Text: "x",
		To: []string{"wiki.acme.example/ca"}, Expects: model.ExpectsNone, Thread: task})
	_, tm, err := getTask(ctx, d.Store, task)
	if err != nil || !slices.Equal(targets(tm.DependsOn), []string{tdraft.ID}) {
		t.Fatalf("task depends_on = %v, %v", tm, err)
	}
	mustLintClean(t, d)
}
