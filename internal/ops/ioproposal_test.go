package ops

import (
	"maps"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// propDraft adds an outgoing draft from the agent with proposal fields.
func propDraft(t *testing.T, f *agentFix, text, group, variantOf string) string {
	t.Helper()
	return mustRun(t, IOAdd, f.agent, IOAddInput{Direction: model.IOOut, Text: text, To: []string{"person-ada-example"}, Medium: model.MediumEmail,
		Expects: model.ExpectsNone, Why: "Ada asked for the quote.", Group: group, VariantOf: variantOf}).ID
}

func draftHash(t *testing.T, f *agentFix, id string) string {
	t.Helper()
	h := mustRun(t, IOShow, f.owner, IOShowInput{ID: id}).Item.DraftHash
	if h == "" {
		t.Fatalf("%s has no draft_hash", id)
	}
	return h
}

func ioState(t *testing.T, f *agentFix, id string) model.IOState {
	t.Helper()
	_, m, err := getIO(t.Context(), f.owner, id)
	if err != nil {
		t.Fatal(err)
	}
	return m.State
}

func TestIOAddProposalFields(t *testing.T) {
	f := ioFix(t)
	id := mustRun(t, IOAdd, f.agent, IOAddInput{Direction: model.IOOut, Text: "Hi Ada.", To: []string{"person-ada-example"}, Medium: model.MediumEmail,
		Expects: model.ExpectsNone, Why: "She asked.", Impact: "Ada gets the quote.", Group: "quotes",
		Assessments: []model.IOAssessment{{Name: "Confidence", Percent: 80, Basis: "She asked twice."}}}).ID
	it := mustRun(t, IOShow, f.owner, IOShowInput{ID: id}).Item
	if it.Why != "She asked." || it.Impact != "Ada gets the quote." || it.Group != "quotes" || len(it.Assessments) != 1 ||
		it.Assessments[0].Name != "confidence" || len(it.NotStated) != 2 {
		t.Fatalf("item = %+v", it)
	}
	// Missing fields are never refused.
	bare := outDraft(t, f, model.ExpectsNone, "")
	if it := mustRun(t, IOShow, f.owner, IOShowInput{ID: bare}).Item; len(it.NotStated) != 3 {
		t.Fatalf("not stated = %v", it.NotStated)
	}
	for _, in := range []IOAddInput{
		{Direction: model.IOOut, Text: "x", To: []string{"person-ada-example"}, Expects: model.ExpectsNone, Assessments: []model.IOAssessment{{Name: "risk", Percent: 120}}},
		{Direction: model.IOOut, Text: "x", To: []string{"person-ada-example"}, Expects: model.ExpectsNone, VariantOf: "io-nothing"},
		{Direction: model.IOIn, Text: "x", Group: "no"},
	} {
		if _, err := IOAdd.Run(t.Context(), f.owner, in); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
	// A proposal edit keeps the approval.
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: id})
	if o := mustRun(t, IOEdit, f.agent, IOEditInput{ID: id, Why: "Fix a word.", Impact: "Ada gets the quote today."}); o.State != model.IOReady {
		t.Fatalf("state after proposal edit = %s", o.State)
	}
	// An edit's why (the receipt reason) never replaces the stated why.
	if it := mustRun(t, IOShow, f.owner, IOShowInput{ID: id}).Item; it.Why != "She asked." || it.Impact != "Ada gets the quote today." {
		t.Fatalf("after edit = %q %q", it.Why, it.Impact)
	}
	// Incoming: why is only the write's reason.
	mustRun(t, IOAdd, f.agent, IOAddInput{Direction: model.IOIn, Text: "Hello.", Why: "Captured."})
	mustLintClean(t, f.owner)
}

func TestIOApproveBatchOnlyTheNamedPairs(t *testing.T) {
	f := ioFix(t)
	a := propDraft(t, f, "Quote A.", "quotes", "")
	b := propDraft(t, f, "Quote B.", "quotes", "")
	ha, hb := draftHash(t, f, a), draftHash(t, f, b)
	// The owner clicked on a and b; c joins the group after.
	c := propDraft(t, f, "Quote C.", "quotes", "")
	if _, err := IOApprove.Run(t.Context(), f.owner, IOApproveInput{Items: []IOApproveItem{{ID: a, ExpectHash: ha}, {ID: b}}}); err == nil || !strings.Contains(err.Error(), "expect_hash is empty") {
		t.Fatalf("missing hash: %v", err)
	}
	out := mustRun(t, IOApprove, f.owner, IOApproveInput{Items: []IOApproveItem{{ID: a, ExpectHash: ha}, {ID: b, ExpectHash: hb}}})
	if len(out.Items) != 2 || out.ID != a {
		t.Fatalf("out = %+v", out)
	}
	if ioState(t, f, a) != model.IOReady || ioState(t, f, b) != model.IOReady || ioState(t, f, c) != model.IODraft {
		t.Fatalf("states = %s %s %s", ioState(t, f, a), ioState(t, f, b), ioState(t, f, c))
	}
	if list := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Op: "io_approve"}); len(list.Items) != 1 {
		t.Fatalf("receipts = %+v", list.Items)
	}
}

func TestIOApproveExpectHashMismatch(t *testing.T) {
	f := ioFix(t)
	a := propDraft(t, f, "Quote A.", "", "")
	b := propDraft(t, f, "Quote B.", "", "")
	ha, hb := draftHash(t, f, a), draftHash(t, f, b)
	mustRun(t, IOEdit, f.agent, IOEditInput{ID: b, Text: "Quote B, changed."})
	if _, err := IOApprove.Run(t.Context(), f.owner, IOApproveInput{ID: b, ExpectHash: hb}); err == nil || !strings.Contains(err.Error(), ErrStaleHash) {
		t.Fatalf("single: %v", err)
	}
	// One stale pair refuses the batch: nothing is approved.
	if _, err := IOApprove.Run(t.Context(), f.owner, IOApproveInput{Items: []IOApproveItem{{ID: a, ExpectHash: ha}, {ID: b, ExpectHash: hb}}}); err == nil {
		t.Fatal("stale batch approved")
	}
	if ioState(t, f, a) != model.IODraft || ioState(t, f, b) != model.IODraft {
		t.Fatalf("states = %s %s", ioState(t, f, a), ioState(t, f, b))
	}
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: b, ExpectHash: draftHash(t, f, b)})
}

func TestIOApproveVariationSupersedesAndUndo(t *testing.T) {
	f := ioFix(t)
	root := propDraft(t, f, "Quote, short.", "", "")
	v1 := propDraft(t, f, "Quote, long.", "", root)
	v2 := propDraft(t, f, "Quote, formal.", "", v1) // a variation of a variation joins the root's set
	if _, m, _ := getIO(t.Context(), f.owner, v2); m.VariantOf != root {
		t.Fatalf("v2 variant_of = %q", m.VariantOf)
	}
	other := propDraft(t, f, "Unrelated.", "", "")
	if _, err := IOApprove.Run(t.Context(), f.owner, IOApproveInput{Items: []IOApproveItem{
		{ID: v1, ExpectHash: draftHash(t, f, v1)}, {ID: v2, ExpectHash: draftHash(t, f, v2)}}}); err == nil || !strings.Contains(err.Error(), "variations of one draft") {
		t.Fatalf("two variations: %v", err)
	}
	out := mustRun(t, IOApprove, f.owner, IOApproveInput{ID: v1, ExpectHash: draftHash(t, f, v1)})
	if len(out.Superseded) != 2 {
		t.Fatalf("superseded = %v", out.Superseded)
	}
	for _, id := range []string{root, v2} {
		_, m, _ := getIO(t.Context(), f.owner, id)
		if m.State != model.IOSuperseded || m.Reason != "superseded by "+v1 {
			t.Fatalf("%s = %s %q", id, m.State, m.Reason)
		}
	}
	if ioState(t, f, other) != model.IODraft {
		t.Fatal("unrelated draft changed")
	}
	if l := mustRun(t, IOList, f.owner, IOListInput{Direction: model.IOOut}); len(l.Items) != 2 {
		t.Fatalf("superseded listed as open: %+v", l.Items)
	}
	mustLintClean(t, f.owner)
	list := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Op: "io_approve"})
	if len(list.Items) != 1 {
		t.Fatalf("receipts = %+v", list.Items)
	}
	mustRun(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: list.Items[0].ID})
	for _, id := range []string{root, v1, v2} {
		if s := ioState(t, f, id); s != model.IODraft {
			t.Fatalf("after undo %s = %s", id, s)
		}
	}
}

func TestIODoneRecordsVariation(t *testing.T) {
	f := ioFix(t)
	root := propDraft(t, f, "Quote, short.", "", "")
	v := propDraft(t, f, "Quote, long.", "", root)
	out := mustRun(t, IODone, f.owner, IODoneInput{ID: v, Text: "Quote, long, as I sent it.", URL: "https://mail.acme.example/sent/9"})
	if out.State != model.IOSent || len(out.Superseded) != 1 || out.Superseded[0] != root {
		t.Fatalf("out = %+v", out)
	}
	doc, m, _ := getIO(t.Context(), f.owner, v)
	if doc.Body != "Quote, long, as I sent it." || m.Revision != 2 || len(m.Revisions) != 1 || len(revFiles(t, f, v)) != 1 {
		t.Fatalf("revision: body %q rev %d %+v", doc.Body, m.Revision, m.Revisions)
	}
	if len(m.Attempts) != 1 || m.Attempts[0].Result != model.AttemptSent || m.Attempts[0].URL != "https://mail.acme.example/sent/9" || m.Attempts[0].Actor != "user" {
		t.Fatalf("attempts = %+v", m.Attempts)
	}
	if r := mustRun(t, IORevisions, f.owner, IORevisionsInput{ID: v, N: 1}); !strings.Contains(r.Text, "Quote, long.") {
		t.Fatalf("r1 = %q", r.Text)
	}
	if ioState(t, f, root) != model.IOSuperseded {
		t.Fatal("root not superseded")
	}
	mustLintClean(t, f.owner)
}

func TestIOAttempts(t *testing.T) {
	f := ioFix(t)
	// A send appends a sent attempt.
	a := outDraft(t, f, model.ExpectsNone, "")
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: a})
	mustRun(t, IOSendCheck, f.agent, IOSendCheckInput{ID: a})
	mustRun(t, IOSend, f.agent, IOSendInput{ID: a, URL: "https://mail.acme.example/sent/1", Tool: "gmail"})
	if _, m, _ := getIO(t.Context(), f.owner, a); len(m.Attempts) != 1 || m.Attempts[0].Result != model.AttemptSent ||
		m.Attempts[0].Tool != "gmail" || m.Attempts[0].Actor != "claude" || m.Attempts[0].Medium != model.MediumEmail {
		t.Fatalf("send attempts = %+v", m.Attempts)
	}
	// A repeated report adds nothing.
	mustRun(t, IOSend, f.agent, IOSendInput{ID: a, URL: "https://mail.acme.example/sent/1"})
	if _, m, _ := getIO(t.Context(), f.owner, a); len(m.Attempts) != 1 {
		t.Fatalf("repeat attempts = %+v", m.Attempts)
	}

	// A hold appends a held attempt.
	b := outDraft(t, f, model.ExpectsNone, "")
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: b})
	mustRun(t, IOAdd, f.agent, IOAddInput{Direction: model.IOIn, Text: "Wait, new numbers.", From: "person-ada-example", Medium: model.MediumEmail,
		Ref: "https://mail.acme.example/in/2"})
	if chk := mustRun(t, IOSendCheck, f.agent, IOSendCheckInput{ID: b}); !chk.Held {
		t.Fatalf("check = %+v", chk)
	}
	if _, m, _ := getIO(t.Context(), f.owner, b); len(m.Attempts) != 1 || m.Attempts[0].Result != model.AttemptHeld || !strings.HasPrefix(m.Attempts[0].Error, "Situation changed") {
		t.Fatalf("held attempts = %+v", m.Attempts)
	}

	// An agent records a failed try; the draft keeps its state.
	c := outDraft(t, f, model.ExpectsNone, "")
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: c})
	if _, err := IOAttempt.Run(t.Context(), f.agent, IOAttemptInput{ID: c, Result: model.AttemptSent}); err == nil || !strings.Contains(err.Error(), "io_send") {
		t.Fatalf("sent: %v", err)
	}
	if _, err := IOAttempt.Run(t.Context(), f.agent, IOAttemptInput{ID: c, Result: model.AttemptFailed}); err == nil || !strings.Contains(err.Error(), "error is empty") {
		t.Fatalf("no error: %v", err)
	}
	o := mustRun(t, IOAttempt, f.agent, IOAttemptInput{ID: c, Result: model.AttemptFailed, Tool: "gmail", Error: "550 mailbox full"})
	if o.State != model.IOReady || o.Attempts != 1 {
		t.Fatalf("attempt out = %+v", o)
	}
	if _, m, _ := getIO(t.Context(), f.owner, c); m.Attempts[0].Error != "550 mailbox full" || m.Attempts[0].Result != model.AttemptFailed {
		t.Fatalf("failed attempts = %+v", m.Attempts)
	}
	if it := mustRun(t, IOShow, f.owner, IOShowInput{ID: c}).Item; len(it.Attempts) != 1 {
		t.Fatalf("show attempts = %+v", it.Attempts)
	}
	mustLintClean(t, f.owner)
}

func TestIOReviewKeepsNeedsYou(t *testing.T) {
	f := ioFix(t)
	in := mustRun(t, IOAdd, f.agent, IOAddInput{Direction: model.IOIn, Text: "Can you send the quote?", From: "person-ada-example", Medium: model.MediumEmail,
		Expects: model.ExpectsResponse, Ref: "https://mail.acme.example/in/1"}).ID
	a := propDraft(t, f, "Quote A.", "quotes", "")
	b := propDraft(t, f, "Quote B.", "quotes", "")
	ready := outDraft(t, f, model.ExpectsNone, "")
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: ready})
	needs := func() map[string]string {
		got := map[string]string{}
		for _, it := range mustRun(t, IOList, f.owner, IOListInput{All: true}).Items {
			got[it.ID] = map[bool]string{true: "yes", false: "no"}[it.NeedsYou] + ": " + it.NeedsYouReason
		}
		return got
	}
	before := needs()
	if _, err := IOReview.Run(t.Context(), f.agent, IOReviewInput{IDs: []string{in}}); err == nil {
		t.Fatal("agent reviewed")
	}
	if _, err := IOReview.Run(t.Context(), f.owner, IOReviewInput{}); err == nil {
		t.Fatal("empty review")
	}
	out := mustRun(t, IOReview, f.owner, IOReviewInput{IDs: []string{in, ready}, Group: "quotes"})
	if len(out.Reviewed) != 4 || out.At == "" {
		t.Fatalf("out = %+v", out)
	}
	for _, id := range []string{in, a, b, ready} {
		if it := mustRun(t, IOShow, f.owner, IOShowInput{ID: id}).Item; it.Reviewed != out.At {
			t.Fatalf("%s reviewed = %q", id, it.Reviewed)
		}
	}
	if after := needs(); !maps.Equal(before, after) {
		t.Fatalf("needs changed:\nbefore %v\nafter  %v", before, after)
	}
	if ioState(t, f, ready) != model.IOReady {
		t.Fatal("review changed the approval")
	}
	list := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Op: "io_review"})
	if len(list.Items) != 1 {
		t.Fatalf("receipts = %+v", list.Items)
	}
	mustRun(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: list.Items[0].ID})
	if it := mustRun(t, IOShow, f.owner, IOShowInput{ID: a}).Item; it.Reviewed != "" {
		t.Fatalf("after undo reviewed = %q", it.Reviewed)
	}
	// A thread key reviews the io items of that thread.
	reply := mustRun(t, IOAdd, f.agent, IOAddInput{Direction: model.IOOut, Text: "Here it is.", To: []string{"person-ada-example"}, Medium: model.MediumEmail,
		Expects: model.ExpectsNone, Thread: in}).ID
	got := mustRun(t, IOReview, f.owner, IOReviewInput{Key: in})
	if len(got.Reviewed) != 2 || !strings.Contains(strings.Join(got.Reviewed, " "), reply) {
		t.Fatalf("key review = %+v", got)
	}
	mustLintClean(t, f.owner)
}

func TestIOProposalsRowsCarryDraftHash(t *testing.T) {
	f := ioFix(t)
	a := propDraft(t, f, "Quote A.", "", "")
	b := propDraft(t, f, "Quote B.", "", "")
	mustRun(t, IOEdit, f.agent, IOEditInput{ID: b, Text: "Quote B, changed."})
	n := 0
	for _, g := range mustRun(t, IOProposals, f.owner, IOProposalsInput{}).Groups {
		for _, s := range g.Sets {
			for _, d := range s.Drafts {
				n++
				if d.DraftHash != draftHash(t, f, d.ID) {
					t.Fatalf("%s: row draft_hash %q != io_show %q", d.ID, d.DraftHash, draftHash(t, f, d.ID))
				}
			}
		}
	}
	if n != 2 {
		t.Fatalf("rows = %d, want 2 (%s, %s)", n, a, b)
	}
}
