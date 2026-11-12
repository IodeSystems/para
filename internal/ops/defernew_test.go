package ops

import "testing"

// New always makes a new condition task, even when an open one has the same
// condition; without new the open one is reused.
func TestDeferWhenNewAlwaysMakes(t *testing.T) {
	d, proj := laterVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which AI vendor", Goal: proj}).ID
	first := must(t, Defer, d, DeferInput{IDs: []string{q}, When: "AI policy notes updated"}).Created
	q2 := must(t, QuestionAdd, d, QuestionAddInput{Text: "Another AI question", Goal: proj}).ID
	o := must(t, Defer, d, DeferInput{IDs: []string{q2}, When: "AI policy notes updated", New: true})
	if o.Created == "" || o.Created == first || o.After != o.Created || len(o.Changed) != 1 {
		t.Fatalf("new = %+v (first %s)", o, first)
	}
	q3 := must(t, QuestionAdd, d, QuestionAddInput{Text: "A third AI question", Goal: proj}).ID
	if o := must(t, Defer, d, DeferInput{IDs: []string{q3}, When: "AI policy notes updated"}); o.Created != "" || o.After == "" {
		t.Fatalf("without new = %+v", o)
	}
}
