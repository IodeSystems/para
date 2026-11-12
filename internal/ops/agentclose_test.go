package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// The close and reply calls agents sent in the model harness, as they sent
// them: each either works or is refused with the one call that works.

const chatURL = "https://chat.example.test/t/1"

type agentCloseVault struct {
	*verbVault
	ctx context.Context
	in  string // the incoming io: Sam's message
}

func newAgentCloseVault(t *testing.T) agentCloseVault {
	t.Helper()
	v := newVerbVault(t)
	in, err := IOAdd.Run(t.Context(), v.cli, IOAddInput{Direction: model.IOIn, Text: "Quota approved for 4 nodes; region is eu-west.",
		Medium: model.MediumSlack, From: v.person, Ref: chatURL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithWrite(t.Context(), WriteMeta{Why: "Sam's message answers it: region is eu-west"})
	return agentCloseVault{verbVault: v, ctx: ctx, in: in.ID}
}

func (v agentCloseVault) ask(t *testing.T) string {
	t.Helper()
	out, err := RFXAdd.Run(t.Context(), v.cli, RFXAddInput{Direction: model.RFXOut, Ask: "Staging quota for the pilot", Route: "test",
		Targets: []TargetInput{{To: v.person, Medium: model.MediumSlack}}})
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func (v agentCloseVault) close(t *testing.T, in string) (VerbOutput, error) {
	t.Helper()
	got, err := Close.Invoke(v.ctx, v.agent, []byte(in))
	if err != nil {
		return VerbOutput{}, err
	}
	return got.(VerbOutput), nil
}

func TestAgentCloseQuestionSource(t *testing.T) {
	v := newAgentCloseVault(t)
	// evidence.locator is the answer's source.
	q := v.question(t, "Which region hosts the pilot?")
	out, err := v.close(t, `{"id":"`+q+`","outcome":"answered","answer":"eu-west","evidence":{"quote":"region is eu-west","locator":"`+v.in+`"}}`)
	if err != nil || out.Op != "question_answer" {
		t.Fatalf("evidence as source: %+v %v", out, err)
	}
	if doc, _ := v.cli.Store.Get(t.Context(), q); !strings.Contains(doc.Body, "- Source: [["+v.in+"]]") {
		t.Fatalf("body = %q", doc.Body)
	}
	// No source: refused, with the call that works and the caller's values.
	q2 := v.question(t, "Which region hosts the pilot, again?")
	v.ctx = WithWrite(t.Context(), WriteMeta{Why: "Sam on Slack " + chatURL + " says eu-west"})
	_, err = v.close(t, `{"id":"`+q2+`","outcome":"answered","answer":"eu-west","sources":[]}`)
	want := `close {id: ` + q2 + `, outcome: answered, answer: "eu-west", sources: [{ref: "` + chatURL + `"`
	if err == nil || !strings.Contains(err.Error(), "needs a source") || !strings.Contains(err.Error(), want) {
		t.Fatalf("no source: %v\nwant %s", err, want)
	}
	// question_answer by op_run with a free-text source: the shape of that op.
	_, err = QuestionAnswer.Invoke(WithWrite(t.Context(), WriteMeta{Why: "Sam answered"}), v.agent,
		[]byte(`{"id":"`+q2+`","text":"eu-west","source":"Sam Example, Slack (`+v.in+`): region is eu-west"}`))
	want = `question_answer {id: ` + q2 + `, text: "eu-west", sources: [{ref: "` + v.in + `"`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("op_run no source: %v\nwant %s", err, want)
	}
}

func TestAgentCloseTaskSources(t *testing.T) {
	v := newAgentCloseVault(t)
	task := v.task(t, "Request staging quota")
	out, err := v.close(t, `{"id":"`+task+`","outcome":"done","sources":[{"quote":"Quota approved for 4 nodes; region is eu-west.","ref":"`+chatURL+`"}]}`)
	if err != nil || out.Op != "task_resolve" {
		t.Fatalf("task done with sources: %+v %v", out, err)
	}
	list, err := ReceiptList.Run(t.Context(), v.cli, ReceiptListInput{Doc: task})
	if err != nil || len(list.Items) == 0 || !strings.Contains(strings.Join(list.Items[0].Sources, " "), chatURL) {
		t.Fatalf("receipt: %+v %v", list, err)
	}
	two := v.task(t, "Two sources")
	_, err = v.close(t, `{"id":"`+two+`","outcome":"done","sources":[{"ref":"`+chatURL+`"},{"ref":"`+v.in+`"}]}`)
	if err == nil || !strings.Contains(err.Error(), "close {id: "+two+", outcome: done, evidence: {quote:") {
		t.Fatalf("two sources: %v", err)
	}
}

func TestAgentCloseAskReply(t *testing.T) {
	v := newAgentCloseVault(t)
	rfx := v.ask(t)
	// answered with sources only: rfx_reply, from the Ask's one target, the
	// quote as the reply.
	out, err := v.close(t, `{"id":"`+rfx+`","outcome":"answered","sources":[{"quote":"Quota approved for 4 nodes; region is eu-west.","ref":"`+v.in+`"}]}`)
	if err != nil || out.Op != "rfx_reply" {
		t.Fatalf("sources only: %+v %v", out, err)
	}
	if r := out.raw.(RFXReplyOutput); r.From != v.person || len(r.Warnings) != 0 {
		t.Fatalf("reply = %+v", r)
	}
	// The same reply again, with fields.from: done, nothing written.
	before, _ := v.cli.Store.Get(t.Context(), rfx)
	out, err = v.close(t, `{"id":"`+rfx+`","outcome":"answered","answer":"Quota approved for 4 nodes; region is eu-west.","fields":{"from":"`+v.person+`"}}`)
	if err != nil || len(out.raw.(RFXReplyOutput).Warnings) != 1 {
		t.Fatalf("same reply: %+v %v", out, err)
	}
	// The same link, other words: done too.
	if _, err = v.close(t, `{"id":"`+rfx+`","outcome":"answered","answer":"Approved: 4 nodes in eu-west","sources":[{"ref":"`+chatURL+`"}]}`); err != nil {
		t.Fatalf("same link: %v", err)
	}
	if after, _ := v.cli.Store.Get(t.Context(), rfx); after.Rev != before.Rev {
		t.Fatalf("a repeated reply wrote: %q", after.Body)
	}
	// Another reply with no link: refused with the call that ends the Ask.
	_, err = v.close(t, `{"id":"`+rfx+`","outcome":"answered","answer":"Something else"}`)
	if err == nil || !strings.Contains(err.Error(), "close {id: "+rfx+", outcome: closed}") {
		t.Fatalf("other reply: %v", err)
	}
}

func TestAgentReplyFromName(t *testing.T) {
	v := newAgentCloseVault(t)
	rfx := v.ask(t)
	// from is the target's name, as rfx_show lists target_names.
	got, err := RFXReply.Invoke(v.ctx, v.agent, []byte(`{"id":"`+rfx+`","from":"Sam Example","via":"slack","outcome":"answered",`+
		`"text":"Quota approved for 4 nodes; region is eu-west.","url":"`+chatURL+`","heard":"message"}`))
	if err != nil || got.(RFXReplyOutput).From != v.person {
		t.Fatalf("from a name: %+v %v", got, err)
	}
	// Someone who is not a target: refused, with the targets to pick from.
	other := v.ask(t)
	_, err = RFXReply.Invoke(v.ctx, v.agent, []byte(`{"id":"`+other+`","from":"Cy Nobody","outcome":"answered","text":"x","url":"`+chatURL+`"}`))
	if err == nil || !strings.Contains(err.Error(), "its targets are "+v.person) || !strings.Contains(err.Error(), "rfx_target_add") {
		t.Fatalf("not a target: %v", err)
	}
}
