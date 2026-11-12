package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func TestPinItemAndUnpin(t *testing.T) {
	f := newAgentFix(t)
	task, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Goal: f.proj, Title: "Watch me", Due: "2026-12-01"})
	if err != nil {
		t.Fatal(err)
	}
	out := must(t, Pin, f.owner, PinInput{ID: task.ID})
	if !out.Changed || out.Pinned == "" || out.ID != task.ID {
		t.Fatalf("pin = %+v", out)
	}
	doc, _ := f.owner.Store.Get(t.Context(), task.ID)
	if doc.Meta.Header().Pinned != out.Pinned {
		t.Fatalf("pinned = %q", doc.Meta.Header().Pinned)
	}
	// Pinning again keeps the first time and changes nothing.
	again := must(t, Pin, f.owner, PinInput{Key: task.ID})
	if again.Changed || again.Pinned != out.Pinned {
		t.Fatalf("again = %+v", again)
	}
	// An agent may not pin.
	if _, err := Pin.Run(t.Context(), f.agent, PinInput{ID: task.ID}); err == nil || !strings.Contains(err.Error(), "only a person") {
		t.Fatalf("agent pin: %v", err)
	}
	if _, err := Unpin.Run(t.Context(), f.agent, PinInput{ID: task.ID}); err == nil {
		t.Fatal("agent unpinned")
	}
	un := must(t, Unpin, f.owner, PinInput{ID: task.ID})
	if !un.Changed || un.Pinned != "" {
		t.Fatalf("unpin = %+v", un)
	}
	if got := must(t, Unpin, f.owner, PinInput{ID: task.ID}); got.Changed {
		t.Fatalf("unpin twice = %+v", got)
	}
}

func TestPinKeyMakesThreadDoc(t *testing.T) {
	f := newAgentFix(t)
	a := must(t, Pin, f.owner, PinInput{Key: "#Big Launch"})
	if a.Type != model.TypeThread || a.Key != "#big-launch" || !strings.HasPrefix(a.ID, "thread-") || !a.Changed {
		t.Fatalf("pin key = %+v", a)
	}
	b := must(t, Pin, f.owner, PinInput{Key: "#big-launch"})
	if b.ID != a.ID || b.Changed {
		t.Fatalf("second pin made another: %+v", b)
	}
	mustLintClean(t, f.owner)
	list := must(t, Pinned, f.owner, PinnedInput{})
	if len(list.Items) != 1 || list.Items[0].Key != "#big-launch" || list.Items[0].Title != "#big-launch" {
		t.Fatalf("pinned = %+v", list.Items)
	}
	un := must(t, Unpin, f.owner, PinInput{Key: "#big-launch"})
	if !un.Changed || un.ID != a.ID {
		t.Fatalf("unpin key = %+v", un)
	}
	if len(must(t, Pinned, f.owner, PinnedInput{}).Items) != 0 {
		t.Fatal("still pinned")
	}
	if _, err := Unpin.Run(t.Context(), f.owner, PinInput{Key: "#never"}); err == nil {
		t.Fatal("unpinned a key that was never pinned")
	}
}

func TestPinRefusals(t *testing.T) {
	f := newAgentFix(t)
	for name, in := range map[string]PinInput{"both": {ID: "x", Key: "#y"}, "none": {}, "missing": {ID: "task-nope"}, "bad key": {Key: "channel:"}} {
		if _, err := Pin.Run(t.Context(), f.owner, in); err == nil {
			t.Errorf("%s: pinned", name)
		}
	}
	task, err := TaskAdd.Run(writeCtx(t), f.agent, TaskAddInput{Goal: f.proj, Title: "Receipted", DoneWhen: "It exists."})
	if err != nil {
		t.Fatal(err)
	}
	list, err := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: task.ID})
	if err != nil || len(list.Items) == 0 {
		t.Fatalf("receipts = %v %v", list.Items, err)
	}
	if _, err := Pin.Run(t.Context(), f.owner, PinInput{ID: list.Items[0].ID}); err == nil || !strings.Contains(err.Error(), "receipt") {
		t.Fatalf("pinned a receipt: %v", err)
	}
}

func TestPinReceiptUndo(t *testing.T) {
	f := newAgentFix(t)
	task, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Goal: f.proj, Title: "Undo me"})
	if err != nil {
		t.Fatal(err)
	}
	must(t, Pin, f.owner, PinInput{ID: task.ID})
	list, err := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	var pin string
	for _, r := range list.Items {
		if r.Op == "pin" {
			pin = r.ID
		}
	}
	if pin == "" {
		t.Fatalf("no pin receipt in %+v", list.Items)
	}
	if _, err := ReceiptUndo.Run(t.Context(), f.owner, ReceiptUndoInput{ID: pin}); err != nil {
		t.Fatal(err)
	}
	doc, _ := f.owner.Store.Get(t.Context(), task.ID)
	if doc.Meta.Header().Pinned != "" {
		t.Fatal("undo left the pin")
	}
}

func TestPinnedCardsAndOrder(t *testing.T) {
	f := newAgentFix(t)
	mk := func(title, due, assignee string) string {
		out, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Goal: f.proj, Title: title, Due: due, Assignee: assignee})
		if err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	late := mk("Late owner task", "2026-12-30", "")
	soon := mk("Soon owner task", "2026-12-01", "")
	agentTask := mk("Agent task", "2026-11-01", "agent:scribe")
	undated := mk("Undated owner task", "", "")
	for _, id := range []string{late, agentTask, undated, soon} {
		must(t, Pin, f.owner, PinInput{ID: id})
	}
	got := must(t, Pinned, f.owner, PinnedInput{}).Items
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	// Needs you first, by next date, undated last; then what an agent holds.
	want := []string{soon, late, undated, agentTask}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		for _, c := range got {
			t.Logf("%s needs=%v next=%q reason=%q", c.ID, c.NeedsYou, c.NextDate, c.Reason)
		}
		t.Fatalf("order = %v, want %v", ids, want)
	}
	c := got[0]
	if c.Type != model.TypeTask || c.Title != "Soon owner task" || c.State != "todo" || c.Life != LifeOpen || c.NextDate != "2026-12-01" || c.NextKind != "due" ||
		c.Matters != f.proj || c.Pinned == "" || c.WaitingOn == nil || !c.NeedsYou {
		t.Fatalf("card = %+v", c)
	}
	if got[3].NeedsYou || got[3].Reason == "" {
		t.Fatalf("agent card = %+v", got[3])
	}
}

func TestPinnedThreadTurns(t *testing.T) {
	f := newAgentFix(t)
	task, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Goal: f.proj, Title: "Ask about it"})
	if err != nil {
		t.Fatal(err)
	}
	must(t, Pin, f.owner, PinInput{ID: task.ID})
	if _, err := ThreadPost.Run(t.Context(), f.owner, ThreadPostInput{ID: task.ID, Text: "Why is this late?", Kind: model.RequestTurnAsk}); err != nil {
		t.Fatal(err)
	}
	c := must(t, Pinned, f.owner, PinnedInput{}).Items[0]
	if c.Turns != c.Queued+c.Claimed || c.Turns > 1 {
		t.Fatalf("turns = %+v", c)
	}
}

func TestPinnedWaitingAndStale(t *testing.T) {
	f := newAgentFix(t)
	dep, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Goal: f.proj, Title: "First"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Goal: f.proj, Title: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	must(t, DependsSet, f.owner, DependsSetInput{ID: next.ID, On: dep.ID})
	must(t, Pin, f.owner, PinInput{ID: next.ID})
	c := must(t, Pinned, f.owner, PinnedInput{}).Items[0]
	if len(c.WaitingOn) != 1 || c.WaitingOn[0] != dep.ID || c.Stale != "" {
		t.Fatalf("card = %+v", c)
	}
}

func TestPinnedLastSkipsThePin(t *testing.T) {
	rv := newReqVault(t)
	owner := rv.as("user", "cli", "install-a")
	proj := must(t, GoalAdd, owner, GoalAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	task := must(t, TaskAdd, owner, TaskAddInput{Goal: proj, Title: "Watch me", Due: "2026-12-01"})
	rv.advance(time.Hour)
	must(t, Pin, owner, PinInput{ID: task.ID})
	// Only the pin happened after the task was made: the last change is the task being made.
	got := must(t, Pinned, owner, PinnedInput{}).Items
	if len(got) != 1 || got[0].Last == nil || got[0].Last.Kind != ActCreated {
		t.Fatalf("card shows the pin: %+v", got[0].Last)
	}
	// A real change after the pin shows; an unpin and a pin after it do not replace it.
	rv.advance(time.Hour)
	must(t, TaskEdit, owner, TaskEditInput{ID: task.ID, DoneWhen: "shipped"})
	rv.advance(time.Hour)
	must(t, Unpin, owner, PinInput{ID: task.ID})
	rv.advance(time.Hour)
	must(t, Pin, owner, PinInput{ID: task.ID})
	got = must(t, Pinned, owner, PinnedInput{}).Items
	if got[0].Last == nil || got[0].Last.Kind != ActEdited || strings.Contains(strings.ToLower(got[0].Last.Summary), "pin") {
		t.Fatalf("last = %+v, want the edit", got[0].Last)
	}
	// The item's own activity keeps the pin.
	var pins int
	for _, r := range must(t, Activity, owner, ActivityInput{Doc: task.ID}).Items {
		for _, id := range r.Receipts {
			if _, rc, err := getReceipt(t.Context(), owner.Store, id); err == nil && (rc.Op == "pin" || rc.Op == "unpin") {
				pins++
			}
		}
	}
	if pins == 0 {
		t.Fatal("activity lost the pin")
	}
}
