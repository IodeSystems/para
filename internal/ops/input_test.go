package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// Every write an agent makes while ingesting an io item cites it in input.
func TestReceiptInputCitesTheIO(t *testing.T) {
	f := newAgentFix(t)
	msg, err := IOAdd.Run(t.Context(), f.owner, IOAddInput{Direction: model.IOIn, Text: "Three things for the portal", From: "ops@acme.example"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithWrite(t.Context(), WriteMeta{Why: "From the message.", Sources: []string{f.source}, Input: msg.ID})
	var tasks []string
	for _, title := range []string{"Rotate the portal keys", "Write the billing parser", "Book the vendor review"} {
		r, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: title, DoneWhen: "It is done."})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, r.ID)
	}
	for _, id := range tasks {
		if _, err := TaskEdit.Run(ctx, f.agent, TaskEditInput{ID: id, Effort: model.Effort("s")}); err != nil {
			t.Fatal(err)
		}
		if _, err := TaskDone.Run(ctx, f.agent, TaskDoneInput{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range tasks {
		list, err := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: id})
		if err != nil || len(list.Items) != 3 {
			t.Fatalf("%s receipts = %+v %v", id, list.Items, err)
		}
		for _, r := range list.Items {
			if r.Input != msg.ID {
				t.Fatalf("%s %s input = %q, want %s", id, r.Op, r.Input, msg.ID)
			}
		}
	}

	// io_file sets it itself.
	other, err := IOAdd.Run(t.Context(), f.owner, IOAddInput{Direction: model.IOIn, Text: "Renew the domain", From: "ops@acme.example"})
	if err != nil {
		t.Fatal(err)
	}
	plain := WithWrite(t.Context(), WriteMeta{Why: "Filed from the inbox.", Sources: []string{f.source}})
	filed, err := IOFile.Run(plain, f.agent, IOFileInput{ID: other.ID, To: TriageTask, Project: f.proj, DoneWhen: "Renewed."})
	if err != nil {
		t.Fatal(err)
	}
	list, err := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Op: "io_file"})
	if err != nil || len(list.Items) == 0 {
		t.Fatalf("io_file receipts = %+v %v", list.Items, err)
	}
	docs := map[string]bool{}
	for _, r := range list.Items {
		docs[r.Doc] = true
		if r.Input != other.ID {
			t.Fatalf("io_file receipt %s input = %q", r.Doc, r.Input)
		}
	}
	if !docs[filed.Became] || !docs[other.ID] {
		t.Fatalf("io_file receipts cover %v, want %s and %s", docs, filed.Became, other.ID)
	}

	// A source that names one io item sets it.
	cited := WithWrite(t.Context(), WriteMeta{Why: "The message says so.", Sources: []string{"[[" + msg.ID + "]]"}})
	r, err := TaskAdd.Run(cited, f.agent, TaskAddInput{Project: f.proj, Title: "Call the registrar", DoneWhen: "Called."})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: r.ID}); len(got.Items) != 1 || got.Items[0].Input != msg.ID {
		t.Fatalf("cited receipts = %+v", got.Items)
	}

	// An input that is not an io item is refused, and nothing is written.
	for _, bad := range []string{f.source, "io-nothing-here"} {
		ctx := WithWrite(t.Context(), WriteMeta{Why: "w", Sources: []string{f.source}, Input: bad})
		if _, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "Never written " + bad, DoneWhen: "No."}); err == nil || !strings.Contains(err.Error(), "not an io item") {
			t.Fatalf("input %s: %v", bad, err)
		}
	}
}

// A write under a held request that links one io item cites it.
func TestReceiptInputFromHeldRequest(t *testing.T) {
	f := newAgentFix(t)
	msg, err := IOAdd.Run(t.Context(), f.owner, IOAddInput{Direction: model.IOIn, Text: "Ship the portal", From: "ops@acme.example"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := RequestAdd.Run(t.Context(), f.owner, RequestAddInput{Title: "Ingest the message", Ask: "Take it in.", Links: []string{msg.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if next, err := RequestNext.Run(t.Context(), f.agent, RequestNextInput{}); err != nil || next.Request == nil || next.Request.ID != req.ID {
		t.Fatalf("next = %+v %v", next, err)
	}
	ctx := WithWrite(t.Context(), WriteMeta{Why: "From the message.", Sources: []string{f.source}})
	r, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "Ship the portal build", DoneWhen: "Shipped."})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: r.ID})
	if len(got.Items) != 1 || got.Items[0].Input != msg.ID || got.Items[0].Request != req.ID {
		t.Fatalf("receipts = %+v", got.Items)
	}
}
