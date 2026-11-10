package ops

import (
	"slices"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func madeRow(t *testing.T, out IOMadeOutput, id string) IOMadeRow {
	t.Helper()
	for _, r := range out.Rows {
		if r.Doc == id {
			return r
		}
	}
	t.Fatalf("no row for %s in %+v", id, out.Rows)
	return IOMadeRow{}
}

// One ingested io item: one task created, one edited, one closed.
func TestIOMadeListsWhatTheIOChanged(t *testing.T) {
	f := newAgentFix(t)
	setup := func(title string) string {
		r, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Project: f.proj, Title: title, DoneWhen: "Done."})
		if err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	toEdit, toClose := setup("Existing to edit"), setup("Existing to close")
	msg, err := IOAdd.Run(t.Context(), f.owner, IOAddInput{Direction: model.IOIn, Text: "Update on the portal", From: "ops@acme.example"})
	if err != nil {
		t.Fatal(err)
	}
	quiet, err := IOAdd.Run(t.Context(), f.owner, IOAddInput{Direction: model.IOIn, Text: "Nothing here", From: "ops@acme.example"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithWrite(t.Context(), WriteMeta{Why: "From the message.", Sources: []string{f.source}, Input: msg.ID})
	created, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "New from message", DoneWhen: "Done."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TaskEdit.Run(ctx, f.agent, TaskEditInput{ID: toEdit, Effort: model.Effort("s")}); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskDone.Run(ctx, f.agent, TaskDoneInput{ID: toClose}); err != nil {
		t.Fatal(err)
	}

	out, err := IOMade.Run(t.Context(), f.owner, IOMadeInput{ID: msg.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 3 {
		t.Fatalf("rows = %+v", out.Rows)
	}
	for id, want := range map[string]string{created.ID: "created", toEdit: "edited", toClose: "closed"} {
		r := madeRow(t, out, id)
		if !slices.Equal(r.Actions, []string{want}) || r.Title == "" || r.Type != "task" || r.Actor != "claude" || r.Receipt == "" || r.Why == "" {
			t.Fatalf("%s row = %+v, want action %s", id, r, want)
		}
	}
	if out.Counts["created"] != 1 || out.Counts["edited"] != 1 || out.Counts["closed"] != 1 {
		t.Fatalf("counts = %v", out.Counts)
	}

	list, err := IOList.Run(t.Context(), f.owner, IOListInput{})
	if err != nil {
		t.Fatal(err)
	}
	made := map[string]int{}
	for _, it := range list.Items {
		made[it.ID] = it.Made
	}
	if made[msg.ID] != 3 || made[quiet.ID] != 0 {
		t.Fatalf("made = %v", made)
	}
}

// An item with no receipts lists what it became and links to.
func TestIOMadeLinkedOnly(t *testing.T) {
	f := newAgentFix(t)
	msg, err := IOAdd.Run(t.Context(), f.owner, IOAddInput{Direction: model.IOIn, Text: "Renew the domain", From: "ops@acme.example"})
	if err != nil {
		t.Fatal(err)
	}
	filed, err := IOFile.Run(t.Context(), f.owner, IOFileInput{ID: msg.ID, To: TriageTask, Project: f.proj, DoneWhen: "Renewed."})
	if err != nil {
		t.Fatal(err)
	}
	out, err := IOMade.Run(t.Context(), f.owner, IOMadeInput{ID: msg.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 1 || out.Rows[0].Doc != filed.Became || !slices.Equal(out.Rows[0].Actions, []string{"linked"}) || out.Counts["linked"] != 1 {
		t.Fatalf("out = %+v", out)
	}
}
