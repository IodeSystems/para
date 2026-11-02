package tui

import (
	"strings"
	"testing"
)

// An opened question has Reject answer on e and Reword on a free key; Reword
// changes the title through question_edit.
func TestOpenedQuestionCanBeReworded(t *testing.T) {
	v, _, q, _ := azVault(t)
	d := newDrv(t, v, 120, 36)
	d.key("3")
	d.onRow("question", q)
	d.key("enter")
	if d.m.docPage() == nil {
		t.Fatalf("no document page:\n%s", d.plainFrame())
	}
	if k := d.keyFor("act:reject"); k != "e" {
		t.Errorf("Reject answer is on %q, want e", k)
	}
	k := d.keyFor("act:reword")
	if k == "" || k == "e" {
		t.Fatalf("Reword key = %q", k)
	}
	d.key(k)
	d.typeText(" in 2026")
	d.key("enter")
	d.key("enter")
	got := status(t, v, q)
	if !strings.HasSuffix(got.Question.Title, " in 2026") {
		t.Fatalf("title = %q\n%s", got.Question.Title, d.plainFrame())
	}
}
