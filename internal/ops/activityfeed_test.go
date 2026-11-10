package ops

import (
	"slices"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Events of two installs merge by time; an agent write joins its receipt;
// the doc filter keeps one item; a burst is one row with a count.
func TestActivityFeed(t *testing.T) {
	rv := newReqVault(t)
	a := rv.as("user", "cli", "inst-a")
	b := rv.as("user", "cli", "inst-b")
	agent := rv.as("claude", "mcp", "inst-a")
	step := func() { rv.advance(time.Minute * 5) }

	proj := must(t, ProjectAdd, a, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	step()
	task := must(t, TaskAdd, a, TaskAddInput{Project: proj, Title: "Renew the cert"}).ID
	step()
	other := must(t, TaskAdd, b, TaskAddInput{Project: proj, Title: "Rotate keys"}).ID
	step()
	// A burst: three edits of one item by one actor within a minute.
	for i := range 3 {
		must(t, TaskEdit, a, TaskEditInput{ID: task, DoneWhen: "The new cert is live " + string(rune('a'+i)) + "."})
		rv.advance(10 * time.Second)
	}
	step()
	if _, err := TaskStart.Run(writeCtx(t), agent, TaskStartInput{ID: task}); err != nil {
		t.Fatal(err)
	}
	step()
	must(t, TaskDone, b, TaskDoneInput{ID: other})

	all := must(t, Activity, a, ActivityInput{})
	for i := 1; i < len(all.Items); i++ {
		if all.Items[i].At > all.Items[i-1].At {
			t.Fatalf("rows are not newest first: %s after %s", all.Items[i].At, all.Items[i-1].At)
		}
	}
	installs := map[string]bool{}
	for _, r := range all.Items {
		installs[r.Install] = true
	}
	if !installs["inst-a"] || !installs["inst-b"] {
		t.Fatalf("installs = %v: want events of both", installs)
	}
	remote := must(t, Activity, a, ActivityInput{Actor: ActorRemote})
	if len(remote.Items) == 0 {
		t.Fatal("no remote rows from inst-b, seen from inst-a")
	}
	for _, r := range remote.Items {
		if r.Install != "inst-b" || !r.Remote {
			t.Fatalf("remote row = %+v", r)
		}
	}

	// Doc filter, the burst, and names read at read time.
	mine := must(t, Activity, a, ActivityInput{Doc: task})
	var edits *ChangeRow
	for i, r := range mine.Items {
		if r.Doc != task {
			t.Fatalf("doc filter let %s through", r.Doc)
		}
		if r.Kind == ActEdited && r.Who == WhoOwner {
			edits = &mine.Items[i]
		}
	}
	if edits == nil || edits.Count != 3 {
		t.Fatalf("burst row = %+v: want one edited row with count 3", edits)
	}
	if edits.DocName != "Renew the cert" || edits.DocType != string(model.TypeTask) {
		t.Fatalf("doc name/type = %q/%q", edits.DocName, edits.DocType)
	}

	// Closing another item is a closed row by inst-b.
	closed := must(t, Activity, a, ActivityInput{Types: []string{ActClosed}})
	if len(closed.Items) != 1 || closed.Items[0].Doc != other || closed.Items[0].Install != "inst-b" {
		t.Fatalf("closed rows = %+v", closed.Items)
	}

	// Paging loses nothing and repeats nothing.
	var seen []string
	cursor := ""
	for range 20 {
		p := must(t, Activity, a, ActivityInput{Limit: 2, Cursor: cursor})
		for _, r := range p.Items {
			seen = append(seen, r.At+r.Doc+r.Kind)
		}
		if !p.More {
			break
		}
		cursor = p.NextCursor
	}
	if len(seen) != len(all.Items) || len(slices.Compact(slices.Clone(seen))) != len(seen) {
		t.Fatalf("paged %d rows, want %d without repeats", len(seen), len(all.Items))
	}

	if _, err := Activity.Run(t.Context(), a, ActivityInput{Types: []string{"nope"}}); err == nil {
		t.Fatal("unknown type: want an error")
	}
}

// An agent write that has a receipt is one row with the agent's why, the
// receipt, and a diff when the call names the item; its vault event is not a
// second row.
func TestActivityJoinsReceipts(t *testing.T) {
	rv := newReqVault(t)
	owner := rv.as("user", "cli", "inst-a")
	agent := rv.as("claude", "mcp", "inst-a")

	proj := must(t, ProjectAdd, owner, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	rv.advance(time.Hour)
	task := must(t, TaskAdd, owner, TaskAddInput{Project: proj, Title: "Renew the cert"}).ID
	rv.advance(time.Hour)
	rec := must(t, ReceiptList, owner, ReceiptListInput{Doc: task})
	before := len(rec.Items)
	if _, err := TaskEdit.Run(WithWrite(t.Context(), WriteMeta{Why: "The vendor confirmed.", Confidence: model.ConfidenceHigh}), agent, TaskEditInput{ID: task, DoneWhen: "The cert is live."}); err != nil {
		t.Fatal(err)
	}
	rec = must(t, ReceiptList, owner, ReceiptListInput{Doc: task})
	if len(rec.Items) != before+1 {
		t.Fatalf("receipts = %d, want %d", len(rec.Items), before+1)
	}

	got := must(t, Activity, owner, ActivityInput{Doc: task, Actor: ActorAgents})
	if len(got.Items) != 1 {
		t.Fatalf("agent rows = %+v: want 1 (the receipt row, not its event too)", got.Items)
	}
	r := got.Items[0]
	if r.Receipt == "" || r.Why != "The vendor confirmed." || r.Who != AgentPrefix+"claude" {
		t.Fatalf("row = %+v", r)
	}
	if len(r.Lines) == 0 {
		t.Fatal("no diff lines on a receipt row of one item")
	}
	if nodiff := must(t, Activity, owner, ActivityInput{Actor: ActorAgents}); len(nodiff.Items) == 0 || len(nodiff.Items[0].Lines) != 0 {
		t.Fatalf("diff lines without a doc filter: %+v", nodiff.Items)
	}
}
