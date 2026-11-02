package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

func replyRFX(t *testing.T, d Deps) string {
	t.Helper()
	id := addRFX(t, d, RFXAddInput{Ask: "Can we launch X?", Targets: []TargetInput{
		{To: "person-bo-example", Medium: "email"}, {To: "person-ada-example", Medium: "slack"}, {To: "person-ada-example", Medium: "email"}}})
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: id, To: "person-bo-example", State: "sent"})
	return id
}

func TestRFXReplyAnswered(t *testing.T) {
	d := targetDeps(t)
	id := replyRFX(t, d)
	out := must(t, RFXReply, d, RFXReplyInput{ID: id, From: "person-bo-example", Outcome: ReplyAnswered, Text: "Yes, ship it on Friday.", Date: "2026-11-01"})
	if out.Constraint != "" || out.State != model.TargetReplied || !strings.HasPrefix(out.Interaction, "interaction-") {
		t.Fatalf("out = %+v", out)
	}
	doc, m, err := getRFX(ctx, d, id)
	if err != nil || m.Targets[0].State != model.TargetReplied {
		t.Fatalf("targets = %+v, %v", m.Targets, err)
	}
	if !strings.Contains(doc.Body, "## Replies") || !strings.Contains(doc.Body, "- 2026-11-01: [[person-bo-example]] answered: Yes, ship it on Friday. ([[") {
		t.Fatalf("body = %q", doc.Body)
	}
	ia, _ := d.Store.Get(ctx, out.Interaction)
	im := ia.Meta.(*model.Interaction)
	if im.Direction != model.InteractionIn || im.Medium != "email" || im.With[0] != "[[person-bo-example]]" || im.Date != "2026-11-01" {
		t.Fatalf("interaction = %+v", im)
	}
	if _, err := RFXReply.Run(ctx, d, RFXReplyInput{ID: id, From: "person-bo-example", Outcome: ReplyPartial, Text: "again"}); err == nil || !strings.Contains(err.Error(), "already replied") {
		t.Fatalf("err = %v", err)
	}
	if list := must(t, ConstraintList, d, ConstraintListInput{}); len(list.Constraints) != 0 {
		t.Fatalf("an answer made a constraint: %+v", list)
	}
	mustLintClean(t, d)
}

func TestRFXReplyNo(t *testing.T) {
	d := targetDeps(t)
	id := replyRFX(t, d)
	no := RFXReplyInput{ID: id, From: "person-bo-example", Outcome: ReplyNo, Text: "Not this quarter, audit freeze.",
		URL: "https://chat.acme.example/t/42", Why: "Audit freeze until January", Date: "2026-11-01", ReviewBy: "2027-01-15"}

	for name, mut := range map[string]func(*RFXReplyInput){
		"no why":     func(in *RFXReplyInput) { in.Why = "" },
		"multi-line": func(in *RFXReplyInput) { in.Why = "a\nb" },
		"no source":  func(in *RFXReplyInput) { in.URL = "" },
		"bad source": func(in *RFXReplyInput) { in.URL = "not a url" },
		"bad review": func(in *RFXReplyInput) { in.ReviewBy = "soon" },
	} {
		t.Run(name, func(t *testing.T) {
			in := no
			mut(&in)
			if _, err := RFXReply.Run(ctx, d, in); err == nil {
				t.Fatal("accepted")
			}
			// Nothing was written.
			if ia := must(t, InteractionList, d, InteractionListInput{}); len(ia.Items) != 0 {
				t.Fatalf("interactions = %+v", ia.Items)
			}
			if _, m, _ := getRFX(ctx, d, id); m.Targets[0].State != model.TargetSent {
				t.Fatalf("state = %s", m.Targets[0].State)
			}
		})
	}

	out := must(t, RFXReply, d, no)
	if out.Constraint == "" {
		t.Fatalf("out = %+v", out)
	}
	c := must(t, ConstraintShow, d, ConstraintShowInput{ID: out.Constraint})
	if c.Why != "Audit freeze until January" || c.By != "Bo Example" || c.SourceURL != "https://chat.acme.example/t/42" ||
		c.Says != "No: Can we launch X?" || c.ReviewBy != "2027-01-15" ||
		!strings.Contains(c.Body, "[["+id+"]]") || !strings.Contains(c.Body, "[["+out.Interaction+"]]") {
		t.Fatalf("constraint = %+v", c)
	}
	doc, _, _ := getRFX(ctx, d, id)
	if !strings.Contains(doc.Body, "[["+out.Constraint+"]]") {
		t.Fatalf("rfx body lacks the constraint link: %q", doc.Body)
	}
	mustLintClean(t, d)
}

func TestRFXReplyNoUsesTargetRef(t *testing.T) {
	d := targetDeps(t)
	id := replyRFX(t, d)
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: id, To: "person-bo-example", State: "sent", Ref: "https://chat.acme.example/t/7"})
	out := must(t, RFXReply, d, RFXReplyInput{ID: id, From: "person-bo-example", Outcome: ReplyNo, Text: "No.", Why: "Policy"})
	if c := must(t, ConstraintShow, d, ConstraintShowInput{ID: out.Constraint}); c.SourceURL != "https://chat.acme.example/t/7" || c.ReviewBy == "" {
		t.Fatalf("constraint = %+v", c)
	}
}

func TestRFXReplyRejects(t *testing.T) {
	d := targetDeps(t)
	id := replyRFX(t, d)
	in := addRFX(t, d, RFXAddInput{Direction: model.RFXIn, Ask: "inbound", Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}}})
	closed := addRFX(t, d, RFXAddInput{Ask: "closed", Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}}})
	must(t, RFXClose, d, RFXCloseInput{ID: closed})
	cases := []struct {
		name string
		in   RFXReplyInput
		want string
	}{
		{"inbound", RFXReplyInput{ID: in, From: "person-bo-example", Outcome: ReplyAnswered, Text: "x"}, "outbound"},
		{"closed", RFXReplyInput{ID: closed, From: "person-bo-example", Outcome: ReplyAnswered, Text: "x"}, "closed"},
		{"bad outcome", RFXReplyInput{ID: id, From: "person-bo-example", Outcome: "maybe", Text: "x"}, "outcome"},
		{"empty text", RFXReplyInput{ID: id, From: "person-bo-example", Outcome: ReplyAnswered}, "text"},
		{"not a target", RFXReplyInput{ID: id, From: "person-cy-example", Outcome: ReplyAnswered, Text: "x"}, "no target"},
		{"several mediums", RFXReplyInput{ID: id, From: "person-ada-example", Outcome: ReplyAnswered, Text: "x"}, "pass via"},
		{"why on answered", RFXReplyInput{ID: id, From: "person-bo-example", Outcome: ReplyAnswered, Text: "x", Why: "r"}, "outcome no only"},
		{"bad date", RFXReplyInput{ID: id, From: "person-bo-example", Outcome: ReplyAnswered, Text: "x", Date: "11/01"}, "date"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := RFXReply.Run(ctx, d, c.in); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	// Via picks one of several mediums.
	must(t, RFXReply, d, RFXReplyInput{ID: id, From: "person-ada-example", Via: "slack", Outcome: ReplyPartial, Text: "Half of it."})
}
