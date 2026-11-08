package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func lifeNow(t *testing.T, d Deps, id string) Lifecycle {
	t.Helper()
	sn, err := NewSnapshot(t.Context(), d.Store)
	if err != nil {
		t.Fatal(err)
	}
	return sn.Need(id).Life
}

func bulkStates(out BulkOutput) map[string]string {
	m := map[string]string{}
	for _, it := range out.Items {
		m[it.ID] = it.State
	}
	return m
}

func TestBulkClosesIDsAndUndoesTheBatch(t *testing.T) {
	v := newVerbVault(t)
	a, b := v.task(t, "First"), v.task(t, "Second")
	out, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkClose, IDs: []string{a, b}, Reason: "See the last task."})
	if err != nil {
		t.Fatal(err)
	}
	if out.Applied != 2 || out.Refused != 0 || out.Batch == "" {
		t.Fatalf("bulk = %+v", out)
	}
	for _, it := range out.Items {
		if it.Receipt == "" || it.Op != "task_done" {
			t.Errorf("item %+v: want task_done with a receipt", it)
		}
		if lifeNow(t, v.pwa, it.ID) != LifeClosed {
			t.Errorf("%s not closed", it.ID)
		}
	}
	rl, err := ReceiptList.Run(t.Context(), v.pwa, ReceiptListInput{Batch: out.Batch})
	if err != nil || len(rl.Items) != 2 {
		t.Fatalf("receipts of the batch = %+v, %v", rl.Items, err)
	}
	undo, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkUndo, Batch: out.Batch})
	if err != nil || undo.Applied != 2 || undo.Refused != 0 {
		t.Fatalf("undo = %+v, %v", undo, err)
	}
	for _, id := range []string{a, b} {
		if lifeNow(t, v.pwa, id) != LifeOpen {
			t.Errorf("%s not open after the batch undo", id)
		}
	}
	if _, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkUndo, Batch: out.Batch}); err == nil {
		t.Error("a second undo of the same batch passed")
	}
}

func TestBulkLaterByTagAndDryRunWritesNothing(t *testing.T) {
	v := newVerbVault(t)
	a, b, other := v.task(t, "Tagged one"), v.task(t, "Tagged two"), v.task(t, "Not tagged")
	if _, err := TagAdd.Run(t.Context(), v.cli, TagInput{IDs: []string{a, b}, Tags: []string{"optional-work"}}); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, id := range []string{a, b, other} {
		doc, _ := v.cli.Store.Get(t.Context(), id)
		before[id] = renderText(doc)
	}
	receipts := func() int {
		l, err := listReceipts(t.Context(), v.cli.Store)
		if err != nil {
			t.Fatal(err)
		}
		return len(l)
	}
	n := receipts()
	dry, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkLater, Tag: "optional-work", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Applied != 2 || dry.Batch != "" || len(dry.Items) != 2 {
		t.Fatalf("dry run = %+v", dry)
	}
	for _, it := range dry.Items {
		if it.State != BulkWouldApply || it.Receipt != "" {
			t.Errorf("dry item %+v", it)
		}
	}
	for id, txt := range before {
		doc, _ := v.cli.Store.Get(t.Context(), id)
		if renderText(doc) != txt {
			t.Errorf("dry run changed %s", id)
		}
	}
	if receipts() != n {
		t.Error("dry run filed a receipt")
	}
	out, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkLater, Tag: "optional-work", Reason: "Optional."})
	if err != nil || out.Applied != 2 {
		t.Fatalf("later by tag = %+v, %v", out, err)
	}
	if lifeNow(t, v.pwa, a) != LifeLater || lifeNow(t, v.pwa, b) != LifeLater || lifeNow(t, v.pwa, other) != LifeOpen {
		t.Error("later by tag changed the wrong items")
	}
}

func TestBulkRefusedItemDoesNotStopOthers(t *testing.T) {
	v := newVerbVault(t)
	a, b := v.task(t, "First"), v.task(t, "Second")
	q := v.question(t, "Which window?")
	// A question does not close as done: close refuses it, the tasks close.
	out, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkClose, IDs: []string{a, q, "task-missing", b}})
	if err != nil {
		t.Fatal(err)
	}
	st := bulkStates(out)
	if out.Applied != 2 || out.Refused != 2 || st[a] != BulkApplied || st[b] != BulkApplied || st[q] != BulkRefused || st["task-missing"] != BulkRefused {
		t.Fatalf("bulk = %+v", out)
	}
	for _, it := range out.Items {
		if it.State == BulkRefused && it.Error == "" {
			t.Errorf("refused %s without a reason", it.ID)
		}
	}
	if lifeNow(t, v.pwa, q) != LifeOpen {
		t.Error("the refused question changed")
	}
}

func TestBulkOptionalMakesEdgesOptional(t *testing.T) {
	v := newVerbVault(t)
	gate := v.task(t, "Gate")
	w1, err := TaskAdd.Run(t.Context(), v.cli, TaskAddInput{Project: v.proj, Title: "Waits one", DependsOn: []string{gate}})
	if err != nil {
		t.Fatal(err)
	}
	free := v.task(t, "Waits on nothing")
	out, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkOptional, On: gate, IDs: []string{w1.ID, free}})
	if err != nil {
		t.Fatal(err)
	}
	st := bulkStates(out)
	if st[w1.ID] != BulkApplied || st[free] != BulkRefused {
		t.Fatalf("optional = %+v", out)
	}
	doc, _ := v.cli.Store.Get(t.Context(), w1.ID)
	hard, opt := model.DepLists(doc.Meta)
	if len(*hard) != 0 || len(*opt) != 1 {
		t.Errorf("edges after optional: hard %v optional %v", *hard, *opt)
	}
	if _, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkUndo, Batch: out.Batch}); err != nil {
		t.Fatal(err)
	}
	doc, _ = v.cli.Store.Get(t.Context(), w1.ID)
	if hard, _ := model.DepLists(doc.Meta); len(*hard) != 1 {
		t.Errorf("undo left hard edges %v", *hard)
	}
}

func TestBulkAgentGates(t *testing.T) {
	v := newVerbVault(t)
	open := v.task(t, "Open")
	put := v.task(t, "Put off")
	if _, err := Defer.Run(t.Context(), v.cli, DeferInput{IDs: []string{put}, Until: "2026-12-01"}); err != nil {
		t.Fatal(err)
	}
	// An agent may close a task, as close allows it.
	out, err := Bulk.Run(writeCtx(t), v.agent, BulkInput{Verb: BulkClose, IDs: []string{open}})
	if err != nil || out.Applied != 1 {
		t.Fatalf("agent close = %+v, %v", out, err)
	}
	// The agent's own receipts carry the batch, so its batch undoes too.
	undo, err := Bulk.Run(writeCtx(t), v.agent, BulkInput{Verb: BulkUndo, Batch: out.Batch})
	if err != nil || undo.Applied == 0 || undo.Refused != 0 || lifeNow(t, v.cli, open) != LifeOpen {
		t.Fatalf("agent batch undo = %+v, %v", undo, err)
	}
	// Bringing back an item put off is undefer, people only: refused per item.
	out, err = Bulk.Run(writeCtx(t), v.agent, BulkInput{Verb: BulkReopen, IDs: []string{put}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Refused != 1 || !strings.Contains(out.Items[0].Error, "only a person") {
		t.Fatalf("agent reopen of a put-off item = %+v", out)
	}
	if lifeNow(t, v.cli, put) != LifeLater {
		t.Error("agent brought back a put-off item")
	}
	// The same input on the owner's surface applies.
	out, err = Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkReopen, IDs: []string{put}})
	if err != nil || out.Applied != 1 {
		t.Fatalf("owner reopen = %+v, %v", out, err)
	}
}

func TestBulkRefusesInputOfAnotherVerb(t *testing.T) {
	v := newVerbVault(t)
	id := v.task(t, "T")
	for _, in := range []BulkInput{
		{Verb: "drop", IDs: []string{id}},
		{Verb: BulkClose, IDs: []string{id}, Until: "2026-12-01"},
		{Verb: BulkOptional, IDs: []string{id}},
		{Verb: BulkUndo},
		{Verb: BulkLater},
	} {
		if _, err := Bulk.Run(t.Context(), v.pwa, in); err == nil {
			t.Errorf("bulk %+v passed", in)
		}
	}
	_, err := Bulk.Run(t.Context(), v.pwa, BulkInput{Verb: BulkClose, Scope: id})
	if err == nil || errors.Is(err, ErrPersonOnly) {
		t.Errorf("scope of a task: %v", err)
	}
}
