package ops

import (
	"strings"
	"testing"
)

func TestRFXDraft(t *testing.T) {
	d := targetDeps(t)
	id := addRFX(t, d, RFXAddInput{Ask: "Ask title\n\nSome context."})
	if show := must(t, RFXShow, d, RFXShowInput{ID: id}); show.Draft != "" {
		t.Fatalf("draft = %q", show.Draft)
	}
	must(t, RFXDraft, d, RFXDraftInput{ID: id, Text: "Hi Bo,\n\nCan we launch X?\n\nThanks"})
	show := must(t, RFXShow, d, RFXShowInput{ID: id})
	if show.Draft != "Hi Bo,\n\nCan we launch X?\n\nThanks" || !strings.Contains(show.Doc.Body, "Some context.") {
		t.Fatalf("show = %q / %q", show.Draft, show.Doc.Body)
	}
	// A new draft replaces the old; a reply section after it survives.
	must(t, RFXTargetAdd, d, RFXTargetAddInput{ID: id, To: "person-bo-example", Medium: "email"})
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: id, To: "person-bo-example", State: "sent"})
	must(t, RFXReply, d, RFXReplyInput{ID: id, From: "person-bo-example", Outcome: ReplyAnswered, Text: "ok", Date: "2026-11-01"})
	must(t, RFXDraft, d, RFXDraftInput{ID: id, Text: "Second draft"})
	show = must(t, RFXShow, d, RFXShowInput{ID: id})
	if show.Draft != "Second draft" || strings.Contains(show.Doc.Body, "Hi Bo") || !strings.Contains(show.Doc.Body, "## Replies") ||
		strings.Count(show.Doc.Body, "## Draft") != 1 {
		t.Fatalf("body = %q", show.Doc.Body)
	}
	for _, bad := range []RFXDraftInput{{ID: id}, {ID: id, Text: "ok\n## Heading"}, {ID: "rfx-none", Text: "x"}} {
		if _, err := RFXDraft.Run(ctx, d, bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	must(t, RFXClose, d, RFXCloseInput{ID: id})
	if _, err := RFXDraft.Run(ctx, d, RFXDraftInput{ID: id, Text: "late"}); err == nil {
		t.Error("closed request took a draft")
	}
	mustLintClean(t, d)
}
