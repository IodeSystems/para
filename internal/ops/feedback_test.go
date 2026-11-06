package ops

import (
	"slices"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestFeedbackAddFilesTaggedIO(t *testing.T) {
	d, _ := laterVault(t)
	ag := agentDeps(d, "scribe")
	out, err := FeedbackAdd.Run(WithWrite(ctx, WriteMeta{Why: "The error lacked the field."}), ag,
		FeedbackAddInput{Text: "proposal_decide refused decision with no hint.", Kind: FeedbackBug, Tool: "proposal_decide"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := d.Store.Get(ctx, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	m := doc.Meta.(*model.IO)
	if m.Direction != model.IOIn || !slices.Contains(m.Tags, FeedbackTag) || m.Subject != "para bug: proposal_decide" {
		t.Fatalf("feedback io = %+v", m)
	}
	if rs := must(t, ReceiptList, d, ReceiptListInput{Doc: out.ID}).Items; len(rs) != 1 {
		t.Fatalf("receipts = %d, want one", len(rs))
	}
	if _, err := FeedbackAdd.Run(ctx, d, FeedbackAddInput{Text: "x", Tool: "no_such_op"}); err == nil {
		t.Fatal("an unknown tool was accepted")
	}
}
