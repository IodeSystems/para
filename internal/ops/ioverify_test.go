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
