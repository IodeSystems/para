package ops

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// One synthetic item with every source of history: made, edited by the
// owner, written by an agent with a receipt, two notes entries, a hand edit,
// an investigate that an agent claims and ends, and a message in with the
// owner's reply out.
func TestHistoryJoinsEverySource(t *testing.T) {
	rv := newReqVault(t)
	owner := rv.as("user", "cli", "inst-a")
	agent := rv.as("claude", "mcp", "inst-a")
	ctx := t.Context()
	step := func() { rv.advance(time.Minute) }
	day := func() string { return time.Unix(0, rv.now.Load()).In(time.Local).Format(dateLayout) }

	proj := must(t, GoalAdd, owner, GoalAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	sam := must(t, OrgPersonAdd, owner, OrgAddInput{Name: "Sam Example", Org: "acme.example"}).ID
	step()
	task := must(t, TaskAdd, owner, TaskAddInput{Goal: proj, Title: "Renew the cert"}).ID
	step()
	must(t, TaskEdit, owner, TaskEditInput{ID: task, DoneWhen: "The new cert is live."})
	step()
	if _, err := TaskStart.Run(writeCtx(t), agent, TaskStartInput{ID: task}); err != nil {
		t.Fatal(err)
	}
	step()
	first := "**" + day() + "**\n\nCalled the vendor: renewal is due Friday."
	n := must(t, NotesEdit, owner, NotesEditInput{ID: task, MD: first})
	step()
	must(t, NotesEdit, owner, NotesEditInput{ID: task, Hash: n.Hash, MD: first + "\n\n**" + day() + "**\n\nSam has the account login."})
	step()
	// A hand edit, which reconcile logs.
	var path string
	_ = filepath.WalkDir(rv.root, func(p string, de os.DirEntry, err error) error {
		if err == nil && de.Name() == task+".md" {
			path = p
		}
		return nil
	})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\nEdited in the editor.\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	// Reconcile holds a change for a grace time before it logs it.
	for range 2 {
		if _, err := owner.Store.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		rv.advance(time.Hour)
	}
	step()
	inv := must(t, Investigate, owner, InvestigateInput{ID: task, Hint: "Is the cert renewed?"})
	step()
	if next := nextReq(t, agent); next.Request == nil || next.Request.ID != inv.Request {
		t.Fatalf("next = %+v", next)
	}
	step()
	must(t, RequestDone, agent, RequestDoneInput{ID: inv.Request, Resolution: model.ResolutionDone, Result: "Not yet: the vendor portal shows it pending."})
	step()
	in := must(t, IOAdd, owner, IOAddInput{Direction: model.IOIn, Text: "Cert question\n\nCan you send the CSR?", From: sam}).ID
	must(t, IOFile, owner, IOFileInput{ID: in, Existing: task})
	step()
	out := must(t, IOAdd, owner, IOAddInput{Direction: model.IOOut, Text: "Sent the CSR.", Subject: "Re: Cert question", To: []string{sam}, Expects: model.ExpectsNone, Thread: in}).ID
	step()
	must(t, IODone, owner, IODoneInput{ID: out, URL: "https://mail.example/sent/1"})

	all := must(t, History, owner, HistoryInput{ID: task})
	var got []string
	for _, r := range slices.Backward(all.Items) {
		got = append(got, r.Kind+"/"+r.Who)
	}
	want := []string{
		"created/owner", "edited/owner", "receipt/agent:claude", "note/owner", "note/owner", "edited/hand edit",
		"asked_agent/owner", "agent_claimed/agent:claude", "agent_result/agent:claude", "message_in/person", "message_out/owner",
	}
	// Filing the message into the task writes nothing on the task; the
	// timeline has no more rows than these.
	if !slices.Equal(got, want) {
		t.Fatalf("history oldest first:\n got %v\nwant %v", got, want)
	}
	byKind := func(rows []HistoryRow, k string) []HistoryRow {
		return slices.DeleteFunc(slices.Clone(rows), func(r HistoryRow) bool { return r.Kind != k })
	}
	rc := byKind(all.Items, HistReceipt)[0]
	// Later writes changed the task: this write no longer undoes.
	if rc.Undo || rc.Ref == "" || !strings.Contains(rc.Summary, "status") || rc.Text != "The owner asked." {
		t.Errorf("receipt row = %+v", rc)
	}
	notes := byKind(all.Items, HistNote)
	if notes[0].Summary != "Sam has the account login." || notes[1].Summary != "Called the vendor: renewal is due Friday." || notes[0].DateOnly {
		t.Errorf("notes = %+v", notes)
	}
	res := byKind(all.Items, HistAgentResult)[0]
	if res.Ref != inv.Request || !strings.Contains(res.Text, "pending") {
		t.Errorf("result = %+v", res)
	}
	if ask := byKind(all.Items, HistAskedAgent)[0]; ask.Text != "Is the cert renewed?" {
		t.Errorf("ask = %+v", ask)
	}
	if m := byKind(all.Items, HistMessageIn)[0]; m.Actor != "Sam Example" || m.Ref != in {
		t.Errorf("message in = %+v", m)
	}
	if m := byKind(all.Items, HistMessageOut)[0]; m.URL != "https://mail.example/sent/1" || m.Ref != out {
		t.Errorf("message out = %+v", m)
	}

	// Conversation: notes, messages, agent results, and the owner's asks.
	conv := must(t, History, owner, HistoryInput{ID: task, Filter: HistoryConversation})
	var kinds []string
	for _, r := range slices.Backward(conv.Items) {
		kinds = append(kinds, r.Kind)
	}
	if want := []string{"note", "note", "asked_agent", "agent_result", "message_in", "message_out"}; !slices.Equal(kinds, want) {
		t.Errorf("conversation = %v", kinds)
	}

	// Pages of four: no row lost or repeated.
	var paged []HistoryRow
	before := ""
	for range 10 {
		p := must(t, History, owner, HistoryInput{ID: task, Limit: 4, Before: before})
		paged = append(paged, p.Items...)
		if !p.More {
			break
		}
		if len(p.Items) != 4 || p.NextBefore != p.Items[3].At {
			t.Fatalf("page = %+v", p)
		}
		before = p.NextBefore
	}
	if len(paged) != len(all.Items) {
		t.Fatalf("paged %d rows, want %d", len(paged), len(all.Items))
	}
	for i := range paged {
		if paged[i].Kind != all.Items[i].Kind || paged[i].At != all.Items[i].At {
			t.Fatalf("page row %d = %+v, want %+v", i, paged[i], all.Items[i])
		}
	}

	if _, err := History.Run(ctx, owner, HistoryInput{ID: task, Filter: "chat"}); err == nil {
		t.Error("filter chat: no error")
	}

	// The events still hold ids and metadata: no text of the notes, the
	// messages, or the agent's result.
	evs, err := owner.Store.Events(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		line := strings.Join([]string{e.Op, e.Doc, e.From, e.To, e.Hash, e.Actor, e.Surface}, " ")
		for _, text := range []string{"vendor", "account login", "CSR", "pending", "Edited in the editor"} {
			if strings.Contains(line, text) {
				t.Errorf("event %+v holds %q", e, text)
			}
		}
	}
}

// The newest write, an agent's, can be undone from its row; once undone the
// row says so.
func TestHistoryUndo(t *testing.T) {
	f := newAgentFix(t)
	task := must(t, TaskAdd, f.owner, TaskAddInput{Goal: f.proj, Title: "Undo me"}).ID
	if _, err := TaskStart.Run(writeCtx(t), f.agent, TaskStartInput{ID: task}); err != nil {
		t.Fatal(err)
	}
	row := func() HistoryRow {
		h := must(t, History, f.owner, HistoryInput{ID: task})
		if len(h.Items) == 0 || h.Items[0].Kind != HistReceipt {
			t.Fatalf("history = %+v", h.Items)
		}
		return h.Items[0]
	}
	r := row()
	if !r.Undo || r.Who != "agent:claude" {
		t.Fatalf("row = %+v", r)
	}
	must(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: r.Ref})
	h := must(t, History, f.owner, HistoryInput{ID: task})
	var undone bool
	for _, x := range h.Items {
		if x.Kind == HistReceipt {
			undone = !x.Undo && strings.HasSuffix(x.Summary, "(undone)")
		}
	}
	if !undone || h.Items[0].Summary != "Undid a write" {
		t.Fatalf("after undo = %+v", h.Items)
	}
}

// Notes split into dated entries; text before the first is one plain entry.
func TestNoteEntries(t *testing.T) {
	got := noteEntries("Old text.\nMore.\n\n**2026-11-01**\n\nFirst.\n\n- 2026-11-02: Held.\n\n**2026-11-03**\n\nSecond\nline.")
	want := []noteEntry{{Text: "Old text.\nMore."}, {Date: "2026-11-01", Text: "First."}, {Date: "2026-11-02", Text: "Held."}, {Date: "2026-11-03", Text: "Second\nline."}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
	if got := noteEntries(""); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestChangedFields(t *testing.T) {
	before := "---\ntype: task\nstatus: todo\nupdated: 2026-11-01\n---\n\nBody.\n\n## Notes\n\nA."
	after := "---\ntype: task\nstatus: doing\nupdated: 2026-11-02\n---\n\nBody.\n\n## Notes\n\nB."
	if got := changedFields(before, after); !slices.Equal(got, []string{"status", "notes"}) {
		t.Fatalf("%v", got)
	}
	if got := changedFields("", after); !slices.Equal(got, []string{"created"}) {
		t.Fatalf("%v", got)
	}
}

func TestNoteEntriesReadTheTimeInTheHeader(t *testing.T) {
	got := noteEntries("**2026-11-05**\n\nold\n\n**2026-11-06 14:32**\n\nnew")
	if len(got) != 2 || got[0].Clock != "" || got[1].Date != "2026-11-06" || got[1].Clock != "14:32" || got[1].Text != "new" {
		t.Fatalf("entries = %+v", got)
	}
}
