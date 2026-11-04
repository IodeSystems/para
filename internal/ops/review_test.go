package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func responseItem(t *testing.T, d Deps, id, item string) ResponseItem {
	t.Helper()
	for _, it := range mustRun(t, ProposalResponse, d, ProposalIDInput{ID: id}).Items {
		if it.ID == item {
			return it
		}
	}
	t.Fatalf("%s has no item %s", id, item)
	return ResponseItem{}
}

// Ask agent: the note is required; the item waits as sent_back, agent work is
// queued with the note and refs, and proposal_response shows them.
func TestStackAskAgent(t *testing.T) {
	f := newAgentFix(t)
	id := fileDossierMD(t, f, dossierMD(f))
	ref := mustRun(t, NoteAdd, f.owner, NoteAddInput{Title: "Earlier booking", Body: "Room A is booked."}).ID
	one := []StackItemRef{{Proposal: id, Item: "a1"}}
	if _, err := StackDecide.Run(t.Context(), f.owner, StackDecideInput{Decision: "ask", Items: one}); err == nil || !strings.Contains(err.Error(), "comment") {
		t.Fatalf("ask with no note: %v", err)
	}
	if _, err := StackDecide.Run(t.Context(), f.owner, StackDecideInput{Decision: "approve", Items: one, Refs: []string{ref}}); err == nil {
		t.Fatal("refs with approve were accepted")
	}
	if _, err := StackDecide.Run(t.Context(), f.agent, StackDecideInput{Decision: "ask", Items: one, Comment: "x"}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("an agent asked: %v", err)
	}
	out := mustRun(t, StackDecide, f.owner, StackDecideInput{Decision: "ask", Items: one, Comment: "Duplicate of the earlier booking.", Refs: []string{ref}})
	if len(out.Requests) != 1 || out.Requests[0] == "" {
		t.Fatalf("out = %+v", out)
	}
	doc, r, err := getRequest(t.Context(), f.owner.Store, out.Requests[0])
	if err != nil || r.Status != model.RequestQueued || !strings.Contains(doc.Body, "Duplicate of the earlier booking.") || !strings.Contains(doc.Body, "[["+ref+"]]") ||
		!slices.Contains(targets(r.Links), id) || !slices.Contains(targets(r.Context), ref) {
		t.Fatalf("request = %+v %+v %v\n%s", doc.Title, r, err, doc.Body)
	}
	ri := responseItem(t, f.agent, id, "a1")
	if ri.State != model.ItemSentBack || ri.Stack != StackProposed || ri.Decision != model.AnswerAsk || ri.Answer == nil ||
		ri.Answer.Comment != "Duplicate of the earlier booking." || !slices.Equal(ri.Answer.Refs, []string{ref}) {
		t.Fatalf("response a1 = %+v", ri)
	}
	// Through submit: ask in the draft.
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerAsk}}); err == nil {
		t.Fatal("a draft ask with no note was accepted")
	}
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerAsk, Comment: "Find Ada's address first.", Refs: []string{ref}}}); err != nil {
		t.Fatal(err)
	}
	sub := mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	if len(sub.Requests) != 1 || responseItem(t, f.agent, id, "m1").State != model.ItemSentBack {
		t.Fatalf("submit = %+v", sub)
	}
	// The agent revises the item: it comes back as proposed and pending.
	md := strings.Replace(dossierMD(f), "what: Book room A", "what: Book room C", 1)
	mustRun(t, ProposalRevise, f.agent, ProposalReviseInput{ID: id, Markdown: md})
	if ri := responseItem(t, f.agent, id, "a1"); ri.State != model.ItemPending {
		t.Fatalf("after revise a1 = %+v", ri)
	}
	mustLintClean(t, f.owner)
}

// Open vault questions are rows of the stack, reviewed with question_review.
func TestStackVaultQuestions(t *testing.T) {
	f := newAgentFix(t)
	q := mustRun(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Which window?", Project: f.proj, Options: []string{"Sat", "Sun"}, Recommended: "Sat"}).ID
	row := func() StackRow {
		t.Helper()
		for _, r := range mustRun(t, ProposalStack, f.owner, ProposalStackInput{}).Rows {
			if r.Question == q {
				return r
			}
		}
		t.Fatalf("question %s is not in the stack", q)
		return StackRow{}
	}
	if r := row(); r.Proposal != "" || r.Kind != model.ItemQuestion || r.Stack != StackProposed || r.State != model.ItemPending {
		t.Fatalf("row = %+v", r)
	}
	if st := mustRun(t, ProposalStack, f.owner, ProposalStackInput{Kind: "para"}); slices.ContainsFunc(st.Rows, func(r StackRow) bool { return r.Question != "" }) {
		t.Fatal("kind para lists vault questions")
	}
	if _, err := QuestionReview.Run(t.Context(), f.owner, QuestionReviewInput{ID: q, Decision: ReviewDecline}); err == nil {
		t.Fatal("decline with no note was accepted")
	}
	if _, err := QuestionReview.Run(t.Context(), f.agent, QuestionReviewInput{ID: q, Decision: ReviewSkip}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("an agent reviewed: %v", err)
	}
	mustRun(t, QuestionReview, f.owner, QuestionReviewInput{ID: q, Decision: ReviewSkip, Note: "After the launch."})
	if r := row(); r.State != model.ItemSkipped || r.Reason != "After the launch." {
		t.Fatalf("skipped row = %+v", r)
	}
	ask := mustRun(t, QuestionReview, f.owner, QuestionReviewInput{ID: q, Decision: ReviewAsk, Note: "Check the ops calendar.", Refs: []string{f.source}})
	if r := row(); r.State != model.ItemSentBack || r.Request != ask.Request || !slices.Equal(r.Refs, []string{f.source}) {
		t.Fatalf("sent back row = %+v", r)
	}
	// Answer with an edited option and an added one.
	out := mustRun(t, QuestionReview, f.owner, QuestionReviewInput{ID: q, Decision: ReviewAnswer, Choices: []string{"Sat"},
		EditedOptions: map[string]string{"Sat": "Saturday 06:00"}, AddedOptions: []string{"Monday night"}, Note: "Quietest."})
	if out.Status != model.QuestionAnswered {
		t.Fatalf("answer = %+v", out)
	}
	show := mustRun(t, QuestionShow, f.owner, QuestionShowInput{ID: q})
	if show.Question.Choice != "Saturday 06:00" || !slices.Contains(show.Question.Options, "Monday night") {
		t.Fatalf("answered question = %+v", show.Question)
	}
	if slices.ContainsFunc(mustRun(t, ProposalStack, f.owner, ProposalStackInput{}).Rows, func(r StackRow) bool { return r.Question == q }) {
		t.Fatal("an answered question is still in the stack")
	}
	q2 := mustRun(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Need a new vendor?", Project: f.proj}).ID
	if out := mustRun(t, QuestionReview, f.owner, QuestionReviewInput{ID: q2, Decision: ReviewDecline, Note: "Not this year."}); out.Status != model.QuestionDropped {
		t.Fatalf("decline = %+v", out)
	}
	mustLintClean(t, f.owner)
}

// Done by me: a message card goes to done with exec.by the owner, its io draft
// is sent and filed, and a receipt says the owner did it.
func TestProposalDoneByOwner(t *testing.T) {
	f := ioFix(t)
	io1 := outDraft(t, f, model.ExpectsNone, "")
	id := fileDossierMD(t, f, dossierWith("```yaml para-message id=m1\nio: \"[["+io1+"]]\"\n"+bodyEv+"```\n"))
	if _, err := ProposalDone.Run(t.Context(), f.agent, ProposalDoneInput{ID: id, Item: "m1"}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("an agent marked it done: %v", err)
	}
	out := mustRun(t, ProposalDone, f.owner, ProposalDoneInput{ID: id, Item: "m1", URL: "https://mail.acme.example/sent/9", Note: "Sent from my phone."})
	if out.State != model.ItemDone || out.IO == nil || out.IO.State != model.IOSent || out.IO.BecameType != model.TypeInteraction {
		t.Fatalf("done = %+v", out)
	}
	show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	if ex := show.Items[0].Execution; ex == nil || ex.By != "user" || ex.URL != "https://mail.acme.example/sent/9" || ex.SelfReported {
		t.Fatalf("execution = %+v", ex)
	}
	rc, r, err := getReceipt(t.Context(), f.owner.Store, out.Receipt)
	if err != nil || r.Op != "proposal_done" || r.Why != "Sent from my phone." || r.Actor != "user" {
		t.Fatalf("receipt = %+v %v %v", rc.Title, r, err)
	}
	// An outbox draft outside a dossier.
	io2 := outDraft(t, f, model.ExpectsNone, "")
	if s := mustRun(t, IODone, f.owner, IODoneInput{ID: io2, Text: "Hi Ada, sent by hand."}); s.State != model.IOSent || s.BecameType != model.TypeInteraction {
		t.Fatalf("io_done = %+v", s)
	}
	mustLintClean(t, f.owner)
}
