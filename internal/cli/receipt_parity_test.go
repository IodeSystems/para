package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedReceiptParity creates four receipted notes: each case runs twice.
func seedReceiptParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c", "d"} {
		note, err := deps.Store.Create(ctx, model.Document{ID: "note-rc-" + id, Folder: model.FolderResources, Title: "Receipted " + id, Body: "Text.",
			Meta: &model.Note{Common: model.Common{Type: model.TypeNote}}})
		if err != nil {
			t.Fatal(err)
		}
		after, err := model.Render(note)
		if err != nil {
			t.Fatal(err)
		}
		body, err := model.RenderReceiptBody(model.ReceiptContent{After: strings.TrimRight(string(after), "\n")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := deps.Store.Create(ctx, model.Document{ID: "receipt-rc-" + id, Folder: model.FolderReceipts, Title: "Note add", Body: body,
			Meta: &model.Receipt{Common: model.Common{Type: model.TypeReceipt}, Actor: "agent", Op: "note_add", Doc: note.ID, Why: "Asked.", Check: model.ReceiptUnchecked}}); err != nil {
			t.Fatal(err)
		}
	}
}

func receiptParityCases() map[string]parityCase {
	return map[string]parityCase{
		"similar":        {`{"type":"task","title":"Sample"}`, []string{"similar", "task", "Sample"}, false},
		"agent_activity": {`{}`, []string{"agent", "activity"}, false},
		"receipt_list":   {`{}`, []string{"receipt", "list"}, false},
		"receipt_show":   {`{"id":"receipt-rc-a"}`, []string{"receipt", "show", "receipt-rc-a"}, false},
		"receipt_check":  {`{"id":"receipt-rc-a","verdict":"verified"}`, []string{"receipt", "check", "receipt-rc-b", "verified"}, false},
		"action_record":  {`{"what":"Opened a PR","where":"github.example/acme/portal","url":"https://github.example/acme/portal/pull/1","owner_said":"go ahead"}`, []string{"action", "record", "--what", "Opened a PR", "--where", "github.example/acme/portal", "--url", "https://github.example/acme/portal/pull/1", "--owner-said", "go ahead"}, false},
		"receipt_undo":   {`{"id":"receipt-rc-c"}`, []string{"receipt", "undo", "receipt-rc-d"}, false},
	}
}
