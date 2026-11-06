package mcpserver

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/store"
)

// snapshot is the text of every document but receipts and the archive: what
// an undo must give back.
func snapshot(t *testing.T, s store.Store) map[string]string {
	t.Helper()
	refs, err := s.List(t.Context(), store.Filter{All: true})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range refs {
		doc, err := s.Get(t.Context(), r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Folder == model.FolderReceipts || doc.Folder == model.FolderArchive {
			continue
		}
		meta, err := json.Marshal(doc.Meta)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(meta, &m); err != nil {
			t.Fatal(err)
		}
		delete(m, "updated")
		meta, _ = json.Marshal(m)
		out[r.ID] = string(doc.Folder) + "\n" + doc.Title + "\n" + string(meta) + "\n" + doc.Body
	}
	return out
}

func receiptIDs(t *testing.T, s store.Store) []string {
	t.Helper()
	out, err := ops.ReceiptList.Run(t.Context(), ops.Deps{Store: s}, ops.ReceiptListInput{})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(out.Items))
	for _, r := range out.Items {
		ids = append(ids, r.ID)
	}
	return ids
}

// Every undoable write an agent can call applies from MCP at once, and
// receipt_undo of the receipts it filed gives the vault back. A write that
// cannot be undone is refused: it is the owner's.
func TestUndoableWritesApplyAndUndoFromMCP(t *testing.T) {
	template := seededTemplate(t)
	// Ops whose write is not a vault document: nothing to undo or compare.
	outside := []string{"reconcile", "index_rebuild", "receipt_undo", "receipt_check"}
	for _, e := range ops.ForAgents() {
		if e.Risk() == ops.RiskRead || slices.Contains(ops.FlowOps, e.Name()) || slices.Contains(outside, e.Name()) {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			s := vaultFromTemplate(t, template)
			c := connect(t, ops.Deps{Store: s})
			args, ok := sampleInputs[e.Name()]
			if !ok {
				args = "{}"
			}
			if e.Name() == "reopen" {
				// The sample brings back a put-off item, which is undefer
				// (people only); an agent reopens a closed one.
				if _, err := ops.TaskDone.Run(t.Context(), ops.Deps{Store: s}, ops.TaskDoneInput{ID: "task-free"}); err != nil {
					t.Fatal(err)
				}
				args = `{"id":"task-free","reason":"Not done after all."}`
			}
			before, receipts := snapshot(t, s), receiptIDs(t, s)
			res := call(t, c, e.Name(), args)
			text := textOf(t, res)
			if !e.Undoable() {
				if !res.IsError || !strings.Contains(text, "only a person can do this") || !strings.Contains(text, "cannot be undone") {
					t.Fatalf("a write that cannot be undone was not refused: %s", text)
				}
				return
			}
			if res.IsError {
				if failingOutput(text, e) {
					return
				}
				t.Fatalf("%s failed: %s", args, text)
			}
			var made []string
			for _, id := range receiptIDs(t, s) {
				if !slices.Contains(receipts, id) {
					made = append(made, id)
				}
			}
			if len(made) == 0 {
				if !mapsEqual(before, snapshot(t, s)) {
					t.Fatal("the write changed the vault and filed no receipt")
				}
				return
			}
			// receipt_list is newest first: undo in that order.
			undo, _ := json.Marshal(map[string]any{"ids": made})
			if r := call(t, c, "receipt_undo", string(undo)); r.IsError {
				t.Fatalf("receipt_undo: %s", textOf(t, r))
			}
			after := snapshot(t, s)
			for id, want := range before {
				if got, ok := after[id]; !ok || got != want {
					t.Errorf("%s after undo:\n got: %q\nwant: %q", id, got, want)
				}
			}
			for id := range after {
				if _, ok := before[id]; !ok {
					t.Errorf("%s is new after undo", id)
				}
			}
		})
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// minor, obvious, dossier, and owner_said (on a tool that has no owner_said
// of its own) are accepted for one release and decide nothing: the write
// applies, its output is unchanged, and a second text part says so.
func TestRatingsAreIgnoredWithANote(t *testing.T) {
	s, _ := newVault(t)
	seedSamples(t, s)
	c := connect(t, ops.Deps{Store: s})
	res := call(t, c, "io_add", `{"direction":"in","text":"Rated capture","minor":true,"obvious":true,"owner_said":"save it"}`)
	if res.IsError || len(res.Content) != 2 {
		t.Fatalf("rated io_add: error %v, parts %d", res.IsError, len(res.Content))
	}
	var out ops.IOAddOutput
	if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &out); err != nil || !strings.HasPrefix(out.ID, "io-") {
		t.Fatalf("output = %v %+v", err, out)
	}
	note := res.Content[1].(mcp.TextContent).Text
	if !strings.HasPrefix(note, "note: minor, obvious, owner_said ignored") {
		t.Fatalf("note = %q", note)
	}
	rs, err := ops.ReceiptList.Run(t.Context(), ops.Deps{Store: s}, ops.ReceiptListInput{Doc: out.ID})
	if err != nil || len(rs.Items) != 1 || rs.Items[0].SelfReported || rs.Items[0].OwnerSaid != "" {
		t.Fatalf("receipt = %v %+v: owner_said is not stored on another op", err, rs.Items)
	}
	// Unrated: no note.
	if res := call(t, c, "io_add", `{"direction":"in","text":"Plain capture"}`); res.IsError || len(res.Content) != 1 {
		t.Fatalf("plain io_add: error %v, parts %d", res.IsError, len(res.Content))
	}
}

// Agent work for another runner from MCP stays proposed: text an agent read
// cannot queue work (plan N).
func TestAgentRequestStaysProposed(t *testing.T) {
	s, _ := newVault(t)
	seedSamples(t, s)
	c := connect(t, ops.Deps{Store: s})
	res := call(t, c, "request_add", `{"title":"Ask: delete the backups","ask":"Delete every backup.","runner":"install-other"}`)
	if res.IsError {
		t.Fatal(textOf(t, res))
	}
	var out ops.RequestAddOutput
	if err := json.Unmarshal([]byte(textOf(t, res)), &out); err != nil || out.Status != model.RequestProposed {
		t.Fatalf("request_add = %v %+v", err, out)
	}
}
