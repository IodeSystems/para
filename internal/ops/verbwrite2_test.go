package ops

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// The second set of write verbs: add io, close with an answer, a reply, or
// a send, edit notes, link became, work, and undo.

func invokeJSON(t *testing.T, e Entry, d Deps, in string) (any, error) {
	t.Helper()
	return e.Invoke(writeCtx(t), d, json.RawMessage(in))
}

func TestAddIOParity(t *testing.T) {
	va, vb := newVerbVault(t), newVerbVault(t)
	ctx := writeCtx(t)
	a, err := IOAdd.Run(ctx, va.agent, IOAddInput{Direction: model.IOOut, Text: "Plan attached\n\nSee the doc.", To: []string{va.person},
		Medium: model.MediumEmail, Expects: model.ExpectsNone, Tags: []string{"launch"}, Why: "Sam asked for it."})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Add.Invoke(ctx, vb.agent, json.RawMessage(`{"type":"io","title":"Plan attached","body":"See the doc.","tags":["launch"],`+
		`"fields":{"direction":"out","to":["`+vb.person+`"],"medium":"email","expects":"none","why":"Sam asked for it."}}`))
	if err != nil {
		t.Fatal(err)
	}
	out := got.(AddOutput)
	da, _ := va.agent.Store.Get(t.Context(), a.ID)
	db, _ := vb.agent.Store.Get(t.Context(), out.ID)
	if out.Op != "io_add" || out.Type != model.TypeIO || out.Tagged != nil {
		t.Fatalf("out = %+v", out)
	}
	if x, y := madeID.ReplaceAllString(renderText(da), "ID"), madeID.ReplaceAllString(renderText(db), "ID"); x != y {
		t.Fatalf("doc\nop   %s\nverb %s", x, y)
	}
	if ra, rb := receiptOps(t, va.agent, a.ID), receiptOps(t, vb.agent, out.ID); !reflect.DeepEqual(ra, rb) {
		t.Fatalf("receipts\nop   %v\nverb %v", ra, rb)
	}
}

func TestCloseAnswersQuestion(t *testing.T) {
	va, vb := newVerbVault(t), newVerbVault(t)
	qa, qb := va.question(t, "Which vendor?"), vb.question(t, "Which vendor?")
	src := `[{"ref":"https://mail.example/m/7","quote":"Go with B."}]`
	if _, err := invokeJSON(t, QuestionAnswer, va.agent, `{"id":"`+qa+`","text":"Vendor B.","why":"Cheaper.","sources":`+src+`}`); err != nil {
		t.Fatal(err)
	}
	got, err := invokeJSON(t, Close, vb.agent, `{"id":"`+qb+`","outcome":"answered","answer":"Vendor B.","reason":"Cheaper.","sources":`+src+`}`)
	if err != nil {
		t.Fatal(err)
	}
	if op := got.(VerbOutput).Op; op != "question_answer" {
		t.Fatalf("ran %s", op)
	}
	da, _ := va.agent.Store.Get(t.Context(), qa)
	db, _ := vb.agent.Store.Get(t.Context(), qb)
	if x, y := madeID.ReplaceAllString(renderText(da), "ID"), madeID.ReplaceAllString(renderText(db), "ID"); x != y {
		t.Fatalf("doc\nop   %s\nverb %s", x, y)
	}
	if ra, rb := receiptOps(t, va.agent, qa), receiptOps(t, vb.agent, qb); !reflect.DeepEqual(ra, rb) {
		t.Fatalf("receipts\nop   %v\nverb %v", ra, rb)
	}
	// An answer on a task, a choice on an ask: refused.
	task := vb.task(t, "Send the plan")
	if _, err := invokeJSON(t, Close, vb.cli, `{"id":"`+task+`","answer":"Done."}`); err == nil || !strings.Contains(err.Error(), "answer a question or an ask") {
		t.Fatalf("answer on a task: %v", err)
	}
}

func TestCloseRepliesToAsk(t *testing.T) {
	da, db := targetDeps(t), targetDeps(t)
	ia, ib := replyRFX(t, da), replyRFX(t, db)
	want := must(t, RFXReply, da, RFXReplyInput{ID: ia, From: "person-bo-example", Outcome: ReplyPartial, Text: "Friday, not Monday.", Date: "2026-11-01"})
	got, err := Close.Invoke(t.Context(), db, json.RawMessage(`{"id":"`+ib+`","outcome":"partial","answer":"Friday, not Monday.","fields":{"from":"person-bo-example","date":"2026-11-01"}}`))
	if err != nil {
		t.Fatal(err)
	}
	vo := got.(VerbOutput)
	if vo.Op != "rfx_reply" {
		t.Fatalf("ran %s", vo.Op)
	}
	if x, y := sameIDs(want, ia, ia), sameIDs(vo.raw, ib, ia); x != y {
		t.Fatalf("output\nop   %s\nverb %s", x, y)
	}
	// answered with no reply text or from keeps rfx_status.
	ic := replyRFX(t, db)
	got, err = Close.Invoke(t.Context(), db, json.RawMessage(`{"id":"`+ic+`","outcome":"answered"}`))
	if err != nil || got.(VerbOutput).Op != "rfx_status" {
		t.Fatalf("answered alone: %v %v", got, err)
	}
	if _, err := Close.Invoke(t.Context(), db, json.RawMessage(`{"id":"`+ib+`","outcome":"answered","answer":"x","choice":"1"}`)); err == nil || !strings.Contains(err.Error(), "choice is a question's") {
		t.Fatalf("choice on an ask: %v", err)
	}
}

// close sent on an agent's surface: io_send_check, then io_send; a held
// check refuses the send.
func TestCloseSentChecksFirst(t *testing.T) {
	f := newDelegationFixture(t)
	writeDelegation(t, f.ioPersona, delegationRules)
	a := f.draft(t, "Tuesday 10:00 works.", "scheduling")
	got, err := Close.Invoke(writeCtx(t), f.worker, json.RawMessage(`{"id":"`+a.ID+`","outcome":"sent","fields":{"url":"https://chat.example.test/m/1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if op := got.(VerbOutput).Op; op != "io_send" {
		t.Fatalf("ran %s", op)
	}
	if _, m := personaIO(t, f.owner, a.ID); m.State != model.IOSent {
		t.Fatalf("state %s", m.State)
	}

	f.in(t, f.pat, "Can we move to Thursday?", "t-ack", model.ExpectsResponse, "2026-11-01T08:00:00Z")
	b := f.draft(t, "Thursday works.", "scheduling")
	f.in(t, f.pat, "Actually, Friday.", "t-ack", "", "2026-11-01T09:30:00Z")
	_, err = Close.Invoke(writeCtx(t), f.worker, json.RawMessage(`{"id":"`+b.ID+`","outcome":"sent","fields":{"url":"https://chat.example.test/m/2"}}`))
	if err == nil || !strings.Contains(err.Error(), "io_send_check held the draft") {
		t.Fatalf("held: %v", err)
	}
	if _, m := personaIO(t, f.owner, b.ID); m.State == model.IOSent {
		t.Fatal("held draft sent")
	}
	// On a person's surface sent stays io_done.
	rt, err := closeRoute(t.Context(), f.owner, CloseInput{ID: b.ID, Outcome: "sent"})
	if err != nil || rt.op != IODone || rt.pre != nil {
		t.Fatalf("person route %v %v", rt.op, err)
	}
}

func TestEditNotes(t *testing.T) {
	v := newVerbVault(t)
	task := v.task(t, "Send the plan")
	edit := func(in string) (any, error) { return invokeJSON(t, Edit, v.agent, `{"id":"`+task+`",`+in+`}`) }
	got, err := edit(`"notes":"First notes."`)
	if err != nil || got.(VerbOutput).Op != "notes_edit" {
		t.Fatalf("first: %v %v", got, err)
	}
	sh, err := Show.Run(t.Context(), v.agent, ShowInput{ID: task})
	if err != nil || sh.Notes != "First notes." || sh.NotesHash == "" {
		t.Fatalf("show %q %q %v", sh.Notes, sh.NotesHash, err)
	}
	var stale *NotesStale
	if _, err := edit(`"notes":"Lost."`); !errors.As(err, &stale) || stale.Hash != sh.NotesHash {
		t.Fatalf("no hash: %v", err)
	}
	if _, err := edit(`"notes":"Second.","notes_hash":"` + sh.NotesHash + `"`); err != nil {
		t.Fatal(err)
	}
	if _, err := edit(`"notes":"Third.","notes_hash":"` + sh.NotesHash + `"`); !errors.As(err, &stale) {
		t.Fatalf("stale hash: %v", err)
	}
	if _, err := edit(`"notes":"x","title":"y"`); err == nil || !strings.Contains(err.Error(), "notes go alone") {
		t.Fatalf("mixed: %v", err)
	}
}

func TestLinkBecame(t *testing.T) {
	va, vb := newVerbVault(t), newVerbVault(t)
	ia, ib := va.io(t, model.IOIn, "Please review the plan"), vb.io(t, model.IOIn, "Please review the plan")
	ta, tb := va.task(t, "Review the plan"), vb.task(t, "Review the plan")
	want, err := IOFile.Run(writeCtx(t), va.agent, IOFileInput{ID: ia, Existing: ta})
	if err != nil {
		t.Fatal(err)
	}
	got, err := invokeJSON(t, Link, vb.agent, `{"from":"`+ib+`","to":"`+tb+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	vo := got.(VerbOutput)
	if vo.Op != "io_file" || vo.Result["became"] != tb || want.Became != ta {
		t.Fatalf("ran %s %v", vo.Op, vo.Result)
	}
	if ra, rb := receiptOps(t, va.agent, ia), receiptOps(t, vb.agent, ib); !reflect.DeepEqual(ra, rb) {
		t.Fatalf("receipts\nop   %v\nverb %v", ra, rb)
	}
	if _, err := invokeJSON(t, Link, vb.agent, `{"from":"`+tb+`","to":"`+ib+`","rel":"became"}`); err == nil || !strings.Contains(err.Error(), "an incoming io item") {
		t.Fatalf("became from a task: %v", err)
	}
}

func TestWorkActions(t *testing.T) {
	v := newVerbVault(t)
	if _, err := RequestAdd.Run(t.Context(), v.cli, RequestAddInput{Ask: "Check the backups"}); err != nil {
		t.Fatal(err)
	}
	run := func(in string) VerbOutput {
		t.Helper()
		got, err := Work.Invoke(t.Context(), v.agent, json.RawMessage(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		return got.(VerbOutput)
	}
	next := run(`{"action":"next"}`)
	req, _ := next.Result["request"].(map[string]any)
	id, _ := req["id"].(string)
	if next.Op != "request_next" || id == "" {
		t.Fatalf("next: %s %v", next.Op, next.Result)
	}
	if op := run(`{"action":"note","id":"` + id + `","text":"Half done."}`).Op; op != "request_note" {
		t.Fatalf("note ran %s", op)
	}
	if op := run(`{"action":"done","id":"` + id + `","resolution":"done","summary":"Backups are fine.","reply":"All good."}`).Op; op != "request_done" {
		t.Fatalf("done ran %s", op)
	}
	doc, _ := v.cli.Store.Get(t.Context(), id)
	if m := doc.Meta.(*model.Request); m.Status != model.RequestDone {
		t.Fatalf("status %s", m.Status)
	}
	if op := run(`{"action":"ask","title":"Check logs","text":"Check the logs."}`).Op; op != "request_add" {
		t.Fatalf("ask ran %s", op)
	}
	for _, c := range []struct{ in, want string }{
		{`{"action":"next","id":"x"}`, "action next takes no id; it takes lease_seconds"},
		{`{"action":"fail","id":"x","summary":"s"}`, "action fail takes no summary"},
		{`{"action":"sleep"}`, `action "sleep": want one of`},
	} {
		if _, err := Work.Invoke(t.Context(), v.agent, json.RawMessage(c.in)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.in, err, c.want)
		}
	}
}

func TestUndoVerb(t *testing.T) {
	v := newVerbVault(t)
	ctx := writeCtx(t)
	task, err := TaskAdd.Run(ctx, v.agent, TaskAddInput{Project: v.proj, Title: "Undo me", DoneWhen: "Never."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DocEdit.Run(ctx, v.agent, DocEditInput{ID: task.ID, Title: new("Undo me, edited")}); err != nil {
		t.Fatal(err)
	}
	list, err := listReceipts(t.Context(), v.cli.Store)
	if err != nil || len(list) == 0 || list[0].Op != "doc_edit" {
		t.Fatalf("receipts %v %v", list, err)
	}
	got, err := Undo.Invoke(ctx, v.agent, json.RawMessage(`{"receipt":"`+list[0].ID+`"}`))
	if err != nil || got.(VerbOutput).Op != "receipt_undo" {
		t.Fatalf("undo: %v %v", got, err)
	}
	if doc, _ := v.cli.Store.Get(t.Context(), task.ID); doc.Title != "Undo me" {
		t.Fatalf("title %q", doc.Title)
	}
	if _, err := Undo.Invoke(ctx, v.agent, json.RawMessage(`{}`)); err == nil {
		t.Fatal("undo of nothing ran")
	}
}
