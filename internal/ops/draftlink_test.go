package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// draftFix has an open question q and an agent that holds a request on it.
func draftFix(t *testing.T) (*agentFix, string) {
	t.Helper()
	f := ioFix(t)
	q := mustRun(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Should we write to Ada?", Goal: f.proj}).ID
	req := mustRun(t, RequestAdd, f.owner, RequestAddInput{Title: "Answer the question", Ask: "Answer " + q, Links: []string{q}})
	n := nextReq(t, f.agent)
	if n.Request == nil || n.Request.ID != req.ID {
		t.Fatalf("claim = %+v", n.Request)
	}
	return f, q
}

func agentDraft(t *testing.T, f *agentFix) IOAddOutput {
	t.Helper()
	return mustRun(t, IOAdd, f.agent, IOAddInput{Direction: model.IOOut, Text: "Hi Ada,\n\ncan you send the quote?", Subject: "Quote",
		To: []string{"person-ada-example"}, Medium: model.MediumEmail, Expects: model.ExpectsNone})
}

func draftNeed(t *testing.T, f *agentFix, id string) Need {
	t.Helper()
	sn, err := NewSnapshot(t.Context(), f.owner.Store)
	if err != nil {
		t.Fatal(err)
	}
	return sn.Need(id)
}

func questionDeps(t *testing.T, f *agentFix, id string) []string {
	t.Helper()
	_, q, err := getQuestion(t.Context(), f.owner.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	return targets(q.DependsOn)
}

func TestDraftLinksHeldQuestion(t *testing.T) {
	f, q := draftFix(t)
	dr := agentDraft(t, f)
	if got := questionDeps(t, f, q); !slices.Equal(got, []string{dr.ID}) {
		t.Fatalf("%s depends_on = %v, want [%s]; warnings %v", q, got, dr.ID, dr.Warnings)
	}
	if !strings.Contains(strings.Join(dr.Warnings, "\n"), q+" depends_on "+dr.ID) {
		t.Fatalf("warnings = %v", dr.Warnings)
	}
	// Two drafts: both linked, none twice.
	dr2 := agentDraft(t, f)
	if got := questionDeps(t, f, q); !slices.Equal(got, []string{dr.ID, dr2.ID}) {
		t.Fatalf("depends_on = %v", got)
	}
	mustLintClean(t, f.owner)
}

func TestDraftWarnsWithTwoHeldRequests(t *testing.T) {
	f, q := draftFix(t)
	mustRun(t, RequestAdd, f.owner, RequestAddInput{Title: "Other work", Ask: "Other work"})
	nextReq(t, f.agent)
	dr := agentDraft(t, f)
	if got := questionDeps(t, f, q); len(got) != 0 || !strings.Contains(strings.Join(dr.Warnings, "\n"), "2 requests") {
		t.Fatalf("depends_on = %v, warnings = %v", got, dr.Warnings)
	}
}

// threadedDraft is a question with an out draft threaded to it, no agent
// holding it.
func threadedDraft(t *testing.T, f *agentFix) (q string, dr IOAddOutput) {
	t.Helper()
	q = mustRun(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Write to Ada?", Goal: f.proj}).ID
	dr = mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: "Hi Ada", Subject: "Hi", To: []string{"person-ada-example"},
		Medium: model.MediumEmail, Expects: model.ExpectsNone, Thread: q})
	return q, dr
}

func TestNextQuestionWaitsOnDraft(t *testing.T) {
	f := ioFix(t)
	q, dr := threadedDraft(t, f)
	if n := draftNeed(t, f, q); n.Turn != TurnWaiting || n.Reason != "draft to approve: "+dr.ID {
		t.Fatalf("question need = %+v", n)
	}
	if n := draftNeed(t, f, dr.ID); n.Turn != TurnOwner || n.Reason != "draft to approve" {
		t.Fatalf("draft need = %+v", n)
	}
	mustRun(t, IODrop, f.owner, IODropInput{ID: dr.ID, Reason: "not needed"})
	if n := draftNeed(t, f, q); n.Turn != TurnOwner || n.Reason != "question to answer" {
		t.Fatalf("after drop: %+v", n)
	}
}

func TestSendUnblocksQuestionAndClosesAnswered(t *testing.T) {
	f := ioFix(t)
	q, dr := threadedDraft(t, f)
	sent := mustRun(t, IOSend, f.owner, IOSendInput{ID: dr.ID, Locator: "mail:1", Observed: true})
	if n := draftNeed(t, f, q); n.Turn != TurnOwner || n.Reason != "question to answer" {
		t.Fatalf("after send: %+v (%v)", n, sent.Warnings)
	}
	// An answer that waited on the draft is closed by the send.
	q2 := mustRun(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Reply to Ada?", Goal: f.proj}).ID
	dr2 := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: "Hi Ada", Subject: "Hi", To: []string{"person-ada-example"},
		Medium: model.MediumEmail, Expects: model.ExpectsNone, Thread: q2})
	doc, qm, err := getQuestion(t.Context(), f.owner.Store, q2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(targets(qm.DependsOn), []string{dr2.ID}) {
		t.Fatalf("thread did not link: %v", qm.DependsOn)
	}
	qm.ProposedAnswer, qm.ProposedBy = "Yes, reply.", "claude"
	if _, err := f.owner.Store.Update(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	mustRun(t, IOSend, f.owner, IOSendInput{ID: dr2.ID, Locator: "mail:2", Observed: true})
	_, qm, err = getQuestion(t.Context(), f.owner.Store, q2)
	if err != nil || qm.Status != model.QuestionAnswered || qm.AnsweredBy != "claude" {
		t.Fatalf("q2 = %+v, %v", qm, err)
	}
}

func TestNotesNamingDraftLinksIt(t *testing.T) {
	f := ioFix(t)
	q := mustRun(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Write to Ada?", Goal: f.proj}).ID
	dr := outDraft(t, f, model.ExpectsNone, "")
	out := mustRun(t, NotesEdit, f.agent, NotesEditInput{ID: q, MD: "Draft is ready: [[" + dr + "]]."})
	if !slices.Equal(out.Linked, []string{dr}) || !slices.Equal(questionDeps(t, f, q), []string{dr}) {
		t.Fatalf("linked = %v, deps = %v", out.Linked, questionDeps(t, f, q))
	}
	if n := draftNeed(t, f, q); n.Turn != TurnWaiting {
		t.Fatalf("need = %+v", n)
	}
}
