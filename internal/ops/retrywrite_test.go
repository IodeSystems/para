package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func retryRequests(t *testing.T, d Deps) []model.Document {
	t.Helper()
	docs, err := loadRequests(context.Background(), d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var out []model.Document
	for _, doc := range docs {
		if doc.Meta.(*model.Request).Kind == RetryWriteKind {
			out = append(out, doc)
		}
	}
	return out
}

// refuse makes an agent write that lint refuses: raw HTML in the body.
func refuse(t *testing.T, d Deps, title string) error {
	t.Helper()
	_, err := NoteAdd.Run(context.Background(), d, NoteAddInput{Title: title, Body: "Plan:\n\n<heading>Big</heading>\n"})
	if err == nil {
		t.Fatal("raw HTML body accepted")
	}
	var lint *model.LintError
	if !errors.As(err, &lint) {
		t.Fatalf("want a lint error, got %v", err)
	}
	return err
}

func TestLintRefusalOfAnAgentFilesOneRetryRequest(t *testing.T) {
	rv := newReqVault(t)
	agent := rv.as("agent-1", "mcp", "inst-a")

	err := refuse(t, agent, "Plan")
	if !strings.Contains(err.Error(), "filed as agent work request-") || !strings.Contains(err.Error(), "retries it") {
		t.Fatalf("no note on the error: %v", err)
	}
	reqs := retryRequests(t, agent)
	if len(reqs) != 1 {
		t.Fatalf("filed %d requests, want 1", len(reqs))
	}
	doc := reqs[0]
	r := doc.Meta.(*model.Request)
	if doc.Title != "Retry a refused write: note_add" || r.Status != model.RequestQueued {
		t.Fatalf("title %q status %s", doc.Title, r.Status)
	}
	for _, want := range []string{"Op: note_add", "Actor: agent-1", "raw-html", "```json", `<heading>Big</heading>`, "call note_add again", "retry_propose"} {
		if !strings.Contains(doc.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, doc.Body)
		}
	}

	// A repeat adds nothing and says so.
	err = refuse(t, agent, "Plan")
	if !strings.Contains(err.Error(), "already filed as agent work "+doc.ID) {
		t.Fatalf("repeat note: %v", err)
	}
	if n := len(retryRequests(t, agent)); n != 1 {
		t.Fatalf("repeat filed: %d requests", n)
	}
	// A different input is another request.
	refuse(t, agent, "Other plan")
	if n := len(retryRequests(t, agent)); n != 2 {
		t.Fatalf("different input: %d requests, want 2", n)
	}
}

func TestLintRefusalOnOwnerSurfaceFilesNothing(t *testing.T) {
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	_, err := NoteAdd.Run(context.Background(), person, NoteAddInput{Title: "Plan", Body: "<heading>Big</heading>"})
	if err == nil {
		t.Fatal("accepted")
	}
	if strings.Contains(err.Error(), "filed as agent work") || len(retryRequests(t, person)) != 0 {
		t.Fatalf("owner surface filed: %v", err)
	}
}

func TestNonLintErrorFilesNothing(t *testing.T) {
	rv := newReqVault(t)
	agent := rv.as("agent-1", "mcp", "inst-a")
	if _, err := NoteAdd.Run(context.Background(), agent, NoteAddInput{Title: "  "}); err == nil {
		t.Fatal("empty title accepted")
	}
	if n := len(retryRequests(t, agent)); n != 0 {
		t.Fatalf("filed %d for a non-lint error", n)
	}
}

func TestRetryDailyCap(t *testing.T) {
	rv := newReqVault(t)
	agent := rv.as("agent-1", "mcp", "inst-a")
	if err := agent.Store.SetConfig(context.Background(), RetryCapKey, "1"); err != nil {
		t.Fatal(err)
	}
	refuse(t, agent, "One")
	err := refuse(t, agent, "Two")
	if strings.Contains(err.Error(), "filed as agent work") {
		t.Fatalf("over the cap: %v", err)
	}
	if n := len(retryRequests(t, agent)); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

func TestRetryInputWithASecretIsWithheld(t *testing.T) {
	rv := newReqVault(t)
	agent := rv.as("agent-1", "mcp", "inst-a")
	body := "<heading>x</heading>\n\nSee https://files.example.com/a.pdf?X-Amz-Signature=abc123 for it."
	if _, err := NoteAdd.Run(context.Background(), agent, NoteAddInput{Title: "Plan", Body: body}); err == nil {
		t.Fatal("accepted")
	}
	reqs := retryRequests(t, agent)
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	if strings.Contains(reqs[0].Body, "abc123") || !strings.Contains(reqs[0].Body, "Withheld") {
		t.Fatalf("input not withheld:\n%s", reqs[0].Body)
	}
}

// claim has the agent take the retry request.
func claimRetry(t *testing.T, agent Deps) string {
	t.Helper()
	next := must(t, RequestNext, agent, RequestNextInput{})
	if next.Request == nil {
		t.Fatal("no request to claim")
	}
	return next.Request.ID
}

func TestFormFixRetryClosesWithTheReceipt(t *testing.T) {
	rv := newReqVault(t)
	agent := rv.as("agent-1", "mcp", "inst-a")
	refuse(t, agent, "Plan")
	id := claimRetry(t, agent)
	out := must(t, NoteAdd, agent, NoteAddInput{Title: "Plan", Body: "Plan:\n\n`<heading>`Big`</heading>`\n"})
	rcpts := must(t, ReceiptList, agent, ReceiptListInput{})
	var rid string
	for _, r := range rcpts.Items {
		if r.Doc == out.ID {
			rid = r.ID
		}
	}
	if rid == "" {
		t.Fatal("no receipt for the retried write")
	}
	must(t, RequestDone, agent, RequestDoneInput{ID: id, Resolution: model.ResolutionDone, Result: "Fixed: wrapped the tags in code.", Links: []string{rid}})
	_, r, err := getRequest(context.Background(), agent.Store, id)
	if err != nil || r.Status != model.RequestDone {
		t.Fatalf("status %v err %v", r, err)
	}
}

func proposeFix(t *testing.T, agent Deps, id, title string) {
	t.Helper()
	must(t, RetryPropose, agent, RetryProposeInput{Request: id, Input: map[string]any{"title": title, "body": "Plan, as a sentence."}, Why: "The heading changes what the note says."})
}

func TestProposeTurnsToTheOwnerAndEachDecision(t *testing.T) {
	rv := newReqVault(t)
	agent := rv.as("agent-1", "mcp", "inst-a")
	person := rv.as("user", "cli", "inst-a")

	// An agent cannot decide.
	refuse(t, agent, "Plan")
	id := claimRetry(t, agent)
	if _, err := RetryDecide.Run(ctx, agent, RetryDecideInput{Request: id, Action: RetryDrop}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent decided: %v", err)
	}
	// Not yet proposed: accept has no fix.
	if _, err := RetryDecide.Run(ctx, person, RetryDecideInput{Request: id, Action: RetryAccept}); err == nil {
		t.Fatal("accepted a fix that does not exist")
	}

	proposeFix(t, agent, id, "Plan")
	sn, err := NewSnapshot(ctx, person.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need(id); !n.Yes || n.Reason != "agent write refused: review the fix" {
		t.Fatalf("need %+v", n)
	}

	// Reject: back to the agent with the notes.
	must(t, RetryDecide, person, RetryDecideInput{Request: id, Action: RetryReject, Notes: "Keep the heading as bold text."})
	doc, r, _ := getRequest(ctx, person.Store, id)
	if r.Status != model.RequestQueued || !strings.Contains(doc.Body, "## Owner's notes") || !strings.Contains(doc.Body, "bold text") {
		t.Fatalf("reject: %s\n%s", r.Status, doc.Body)
	}
	if got := claimRetry(t, agent); got != id {
		t.Fatalf("claimed %s, want %s", got, id)
	}

	// Accept: the op runs with the proposed input on the owner's surface.
	proposeFix(t, agent, id, "Plan accepted")
	acc := must(t, RetryDecide, person, RetryDecideInput{Request: id, Action: RetryAccept})
	if !acc.Applied || acc.Doc == "" || acc.Status != model.RequestDone {
		t.Fatalf("accept: %+v", acc)
	}
	if _, err := person.Store.Get(ctx, acc.Doc); err != nil {
		t.Fatal(err)
	}
	if _, err := RetryDecide.Run(ctx, person, RetryDecideInput{Request: id, Action: RetryDrop}); err == nil {
		t.Fatal("decided a closed request")
	}

	// Edit: the owner's input runs, not the proposal.
	refuse(t, agent, "Second")
	id2 := claimRetry(t, agent)
	proposeFix(t, agent, id2, "Second")
	ed := must(t, RetryDecide, person, RetryDecideInput{Request: id2, Action: RetryEdit, Approve: true, Input: map[string]any{"title": "Second, edited", "body": "Owner words."}})
	got, err := person.Store.Get(ctx, ed.Doc)
	if err != nil || got.Title != "Second, edited" {
		t.Fatalf("edit: %v %v", got.Title, err)
	}

	// Edit without approve: only the proposal changes; Accept then runs it.
	refuse(t, agent, "Fourth")
	id4 := claimRetry(t, agent)
	proposeFix(t, agent, id4, "Fourth")
	sv := must(t, RetryDecide, person, RetryDecideInput{Request: id4, Action: RetryEdit, Input: map[string]any{"title": "Fourth, saved", "body": "Saved."}})
	if sv.Applied || sv.Status != model.RequestDone {
		t.Fatalf("save: %+v", sv)
	}
	d4, r4, _ := getRequest(ctx, person.Store, id4)
	if !r4.Resolution.NeedsOwner() || strings.Count(d4.Body, "## Proposed fix") != 2 {
		t.Fatalf("save: %s\n%s", r4.Resolution, d4.Body)
	}
	if _, err := RetryDecide.Run(ctx, person, RetryDecideInput{Request: id4, Action: RetryEdit}); err == nil {
		t.Fatal("saved an empty edit")
	}
	acc4 := must(t, RetryDecide, person, RetryDecideInput{Request: id4, Action: RetryAccept})
	if g, err := person.Store.Get(ctx, acc4.Doc); err != nil || g.Title != "Fourth, saved" {
		t.Fatalf("accept after save ran the old proposal: %v %v", g.Title, err)
	}

	// Drop: closed, nothing written.
	refuse(t, agent, "Third")
	id3 := claimRetry(t, agent)
	proposeFix(t, agent, id3, "Third")
	before := len(requestIDs(t, person))
	dr := must(t, RetryDecide, person, RetryDecideInput{Request: id3, Action: RetryDrop, Notes: "Not wanted."})
	if dr.Applied || dr.Status != model.RequestCancelled || len(requestIDs(t, person)) != before {
		t.Fatalf("drop: %+v", dr)
	}
}

func TestRetryDecideRunsOnlyAgentWrites(t *testing.T) {
	rv := newReqVault(t)
	agent := rv.as("agent-1", "mcp", "inst-a")
	person := rv.as("user", "cli", "inst-a")
	refuse(t, agent, "Plan")
	id := claimRetry(t, agent)
	doc, r, _ := getRequest(ctx, agent.Store, id)
	doc.Body = strings.Replace(doc.Body, "Op: note_add", "Op: request_accept", 1)
	if _, err := agent.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	_ = r
	proposeFix(t, agent, id, "Plan")
	if _, err := RetryDecide.Run(ctx, person, RetryDecideInput{Request: id, Action: RetryAccept}); err == nil || !strings.Contains(err.Error(), "only a write an agent may call") {
		t.Fatalf("ran a person op: %v", err)
	}
}
