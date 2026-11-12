package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// claimTurn posts an owner turn on the fix's task and has the agent claim it.
// The turn's body says the change goes on the item.
func (f *threadFix) claimTurn(t *testing.T, text string) string {
	t.Helper()
	turn := must(t, ThreadPost, f.owner, ThreadPostInput{ID: f.task, Text: text})
	f.rv.advance(time.Minute)
	next := must(t, RequestNext, f.agent, RequestNextInput{})
	if next.Request == nil || next.Request.ID != turn.Request {
		t.Fatalf("next = %+v", next.Request)
	}
	for _, want := range []string{"make the change on the item", "Pass request", "says in one or two lines what changed"} {
		if !strings.Contains(next.Request.Body, want) {
			t.Fatalf("turn body lacks %q:\n%s", want, next.Request.Body)
		}
	}
	return turn.Request
}

// A reply that says partly done on a task no write under the turn changed
// is refused; task_split under the turn makes it pass.
func TestRequestDoneRefusesPartlyDoneUnchanged(t *testing.T) {
	f := newThreadFix(t)
	req := f.claimTurn(t, "This appears to already have been done? check, if done, close it.")
	done := RequestDoneInput{ID: req, Resolution: model.ResolutionPartial, Result: "Checked.", Reply: "Partly done, left it open."}
	_, err := RequestDone.Run(t.Context(), f.agent, done)
	if want := f.task + " is partly done but unchanged: split it with task_split {id: " + f.task + ", done, remaining}"; err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("partly unchanged: %v\nwant %s", err, want)
	}
	sp := must(t, TaskSplit, f.agent, TaskSplitInput{ID: f.task,
		Done:      []SplitDone{{Title: "Draft the post", Evidence: &model.Evidence{Locator: "https://wiki.acme.example/post-draft"}}},
		Remaining: []SplitRemaining{{Title: "Publish the post"}}, Reason: "The draft is up; publishing remains."})
	done.Reply = "Partly done: the draft is up (" + sp.Done[0] + "); publishing remains (" + sp.Remaining[0] + ")."
	must(t, RequestDone, f.agent, done)
}

// A reply that names a vault id the item does not link is refused with the
// link call; a wikilink on the item makes it pass.
func TestRequestDoneRefusesUnlinkedIDs(t *testing.T) {
	f := newThreadFix(t)
	note := must(t, NoteAdd, f.owner, NoteAddInput{Title: "Launch checklist", Goal: f.proj}).ID
	req := f.claimTurn(t, "add more context/details/links")
	done := RequestDoneInput{ID: req, Resolution: model.ResolutionDone, Result: "Added context.", Reply: "The steps are in " + note + "."}
	_, err := RequestDone.Run(t.Context(), f.agent, done)
	want := "link {from: " + f.task + ", to: " + note + ", rel: depends_on} if it blocks " + f.task + ", else name [[" + note + "]] in the body or notes of " + f.task
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("unlinked id: %v\nwant %s", err, want)
	}
	if _, err := DocEdit.Run(writeCtx(t), f.agent, DocEditInput{ID: f.task, Append: "Steps: [[" + note + "]]."}); err != nil {
		t.Fatal(err)
	}
	must(t, RequestDone, f.agent, done)
}
