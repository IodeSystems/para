package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestIncomingMessageMustBeVerifiable(t *testing.T) {
	d, _ := laterVault(t)
	_, err := IOAdd.Run(ctx, d, IOAddInput{Direction: model.IOIn, Medium: model.MediumSlack, Text: "Trigger an incident when the ETA slips."})
	if err == nil || !strings.Contains(err.Error(), "use the message permalink") {
		t.Fatalf("slack message with no ref = %v", err)
	}
	if _, err := IOAdd.Run(ctx, d, IOAddInput{Direction: model.IOIn, Medium: model.MediumSlack, Text: "x", Via: &model.IOVia{Source: "source-slack", Ref: "C01/p17"}}); err == nil {
		t.Fatal("via without verify was accepted")
	}
	for _, in := range []IOAddInput{
		{Direction: model.IOIn, Medium: model.MediumSlack, Text: "a", Ref: "https://chat.example/archives/C01/p17"},
		{Direction: model.IOIn, Medium: model.MediumSlack, Text: "b", Via: &model.IOVia{Source: "source-slack", Ref: "C01/p18"}, Verify: "Slack #ops, Kevin 2026-11-05 11:01: search ETA"},
		{Direction: model.IOIn, Medium: model.MediumPhone, Text: "c"},
		{Direction: model.IOIn, Text: "d"},
	} {
		if _, err := IOAdd.Run(ctx, d, in); err != nil {
			t.Errorf("%+v: %v", in, err)
		}
	}
	// An older item without a link reads as unverifiable, and io_edit adds the link.
	doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderIO, Title: "Old", Body: "Old",
		Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOIn, Medium: model.MediumSlack, State: model.IOUnread}})
	if err != nil {
		t.Fatal(err)
	}
	if show := must(t, IOShow, d, IOShowInput{ID: doc.ID}); !show.Item.Unverifiable {
		t.Fatalf("old item = %+v", show.Item)
	}
	must(t, IOEdit, d, IOEditInput{ID: doc.ID, Ref: "https://chat.example/archives/C01/p19"})
	if show := must(t, IOShow, d, IOShowInput{ID: doc.ID}); show.Item.Unverifiable || show.Item.Ref == "" {
		t.Fatalf("after io_edit ref = %+v", show.Item)
	}
}

// In a vault with sources, io_add refuses a via.source that names none, and
// takes a source's id, its id without source-, or its vendor. A closed item
// takes a correction of via.source alone.
func TestViaSourceMustResolve(t *testing.T) {
	d, proj := laterVault(t)
	in := func(src, ref string) IOAddInput {
		return IOAddInput{Direction: model.IOIn, Medium: model.MediumEmail, Text: "x " + ref, Via: &model.IOVia{Source: src, Ref: ref}, Verify: "mailbox, search x"}
	}
	// No source yet: any via.source is taken.
	if _, err := IOAdd.Run(ctx, d, in("anything", "r0")); err != nil {
		t.Fatalf("no sources: %v", err)
	}
	if _, err := SourceAdd.Run(ctx, d, SourceAddInput{ID: "source-acme-mail", Title: "Acme mail", System: model.SourceMail, Vendor: "acmemail"}); err != nil {
		t.Fatal(err)
	}
	for i, src := range []string{"source-acme-mail", "acme-mail", "acmemail", "mail"} {
		if _, err := IOAdd.Run(ctx, d, in(src, "r"+string(rune('1'+i)))); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
	_, err := IOAdd.Run(ctx, d, in("nowhere", "r9"))
	if err == nil || !strings.Contains(err.Error(), "source-acme-mail") {
		t.Fatalf("unknown source = %v", err)
	}
	doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderIO, Title: "Filed", Body: "Filed",
		Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOIn, Medium: model.MediumEmail, State: model.IOFiled, Became: model.LinkTo(proj),
			Via: &model.IOVia{Source: "old-name", Ref: "m1", Thread: "t1"}, Verify: "mailbox, search filed"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []IOEditInput{
		{ID: doc.ID, Via: &model.IOVia{Source: "acmemail", Ref: "m2"}},
		{ID: doc.ID, Via: &model.IOVia{Source: "nowhere", Ref: "m1"}},
		{ID: doc.ID, Via: &model.IOVia{Source: "acmemail", Ref: "m1"}, Effort: model.Effort("s")},
	} {
		if _, err := IOEdit.Run(ctx, d, bad); err == nil {
			t.Errorf("%+v was accepted on a filed item", bad)
		}
	}
	must(t, IOEdit, d, IOEditInput{ID: doc.ID, Via: &model.IOVia{Source: "acmemail", Ref: "m1"}})
	_, m, err := getIO(ctx, d, doc.ID)
	if err != nil || m.State != model.IOFiled || *m.Via != (model.IOVia{Source: "acmemail", Ref: "m1", Thread: "t1"}) {
		t.Fatalf("after fix = %+v, %v", m, err)
	}
}
