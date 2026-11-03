package ops

import (
	"reflect"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

func testSummarizer() itemSummarizer {
	names := map[string]string{"person-priya": "Priya Raman", "task-1": "Order a node pool"}
	return itemSummarizer{
		name: func(id string) string { return names[id] },
		who:  func(string) string { return "" },
		now:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestChangeIsPlainWords(t *testing.T) {
	z := testSummarizer()
	it := model.ProposalItem{Kind: model.ItemOp, Op: "task_add", Input: map[string]any{
		"title": "Order a node pool", "assignee": "person-priya", "due": "2026-11-03", "empty": "",
	}}
	want := []string{"Given to: Priya Raman", "Due: Nov 3", "Title: Order a node pool"}
	if got := z.change(it); !reflect.DeepEqual(got, want) {
		t.Fatalf("change = %q, want %q", got, want)
	}
}

func TestChangeOfExternalNamesTargetAndMedium(t *testing.T) {
	z := testSummarizer()
	it := model.ProposalItem{Kind: model.ItemAction, Target: "a@b.example", Subtype: "email"}
	want := []string{"Send to: a@b.example", "By: Email"}
	if got := z.change(it); !reflect.DeepEqual(got, want) {
		t.Fatalf("change = %q, want %q", got, want)
	}
}

func TestResultNameIsTheTitleOfWhatWasMade(t *testing.T) {
	z := testSummarizer()
	if got := z.resultName(model.ProposalItem{Result: "task-1"}); got != "Order a node pool" {
		t.Fatalf("resultName = %q", got)
	}
	if got := z.resultName(model.ProposalItem{Result: "nope"}); got != "" {
		t.Fatalf("resultName of an unknown id = %q", got)
	}
}
