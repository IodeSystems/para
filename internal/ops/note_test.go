package ops

import "testing"

func TestNoteEdit(t *testing.T) {
	f := newAgentFix(t)
	n, err := NoteAdd.Run(t.Context(), f.owner, NoteAddInput{Title: "Old title", Body: "Old."})
	if err != nil {
		t.Fatal(err)
	}
	title := "New title"
	body := "New."
	if _, err := NoteEdit.Run(t.Context(), f.owner, NoteEditInput{ID: n.ID, Title: &title, Body: &body}); err != nil {
		t.Fatal(err)
	}
	got, _ := NoteShow.Run(t.Context(), f.owner, NoteShowInput{ID: n.ID})
	if got.Note.Title != title || got.Body != body {
		t.Fatalf("got %+v", got)
	}
	// An agent's edit applies at once, with a receipt the owner can undo.
	agentBody := "By the agent."
	ctx := WithWrite(t.Context(), WriteMeta{Why: "The owner asked."})
	if _, err := NoteEdit.Run(ctx, f.agent, NoteEditInput{ID: n.ID, Body: &agentBody}); err != nil {
		t.Fatal(err)
	}
	got, _ = NoteShow.Run(t.Context(), f.owner, NoteShowInput{ID: n.ID})
	if got.Body != agentBody {
		t.Fatalf("body = %q", got.Body)
	}
	rs := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Doc: n.ID}).Items
	if len(rs) == 0 {
		t.Fatal("no receipt for the agent's edit")
	}
	mustRun(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: rs[0].ID})
	got, _ = NoteShow.Run(t.Context(), f.owner, NoteShowInput{ID: n.ID})
	if got.Body != body {
		t.Fatalf("after undo body = %q", got.Body)
	}
}
