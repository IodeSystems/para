package mcpserver

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

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

// minor, obvious, and dossier are gone (D4), and owner_said is a field of
// io_approve only: elsewhere each is an unknown field, refused before it
// writes, and the schema of a write lists none of them.
func TestRatingsAreRefused(t *testing.T) {
	s, _ := newVault(t)
	seedSamples(t, s)
	c := connect(t, ops.Deps{Store: s})
	for _, f := range []string{`"minor":true`, `"obvious":true`, `"dossier":"d"`, `"owner_said":"save it"`} {
		res := call(t, c, "io_add", `{"direction":"in","text":"Rated capture",`+f+`}`)
		if !res.IsError || !strings.Contains(textOf(t, res), "unknown field") {
			t.Fatalf("io_add with %s: %s", f, textOf(t, res))
		}
	}
	for _, e := range ops.ForAgents() {
		if !ops.NeedsWhy(e) {
			continue
		}
		raw, err := toolSchema(e)
		if err != nil {
			t.Fatal(err)
		}
		var sc struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(raw, &sc); err != nil {
			t.Fatal(err)
		}
		for _, gone := range []string{"minor", "obvious", "dossier", "owner_said"} {
			if _, has := sc.Properties[gone]; has && (gone != "owner_said" || e.Name() != "io_approve") {
				t.Errorf("%s lists %s", e.Name(), gone)
			}
		}
	}
	for name, p := range metaProps {
		if d, _ := p["description"].(string); len(d) > 40 {
			t.Errorf("metaProps %s: description is %d bytes, want at most 40", name, len(d))
		}
	}
}

// io_approve from MCP: refused without the owner's words, applied with them,
// and its receipt is self-reported with the words. Outward stays held: no
// other tool marks a draft ready.
func TestIOApproveFromMCP(t *testing.T) {
	s, _ := newVault(t)
	seedSamples(t, s)
	c := connect(t, ops.Deps{Store: s})
	if res := call(t, c, "io_edit", `{"id":"io-draft-sample","state":"ready","owner_said":"send it"}`); !res.IsError {
		t.Fatalf("io_edit ready from MCP applied: %s", textOf(t, res))
	}
	if res := call(t, c, "io_approve", `{"id":"io-draft-sample"}`); !res.IsError || !strings.Contains(textOf(t, res), "owner_said is empty") {
		t.Fatalf("io_approve without owner_said: %s", textOf(t, res))
	}
	res := call(t, c, "io_approve", `{"id":"io-draft-sample","owner_said":"Yes, send it to Sam."}`)
	if res.IsError || len(res.Content) != 1 {
		t.Fatalf("io_approve: %s", textOf(t, res))
	}
	var out ops.IOApproveOutput
	if err := json.Unmarshal([]byte(textOf(t, res)), &out); err != nil || out.State != model.IOReady || !out.SelfReported {
		t.Fatalf("io_approve = %v %+v", err, out)
	}
	rs, err := ops.ReceiptList.Run(t.Context(), ops.Deps{Store: s}, ops.ReceiptListInput{Doc: "io-draft-sample", Op: "io_approve"})
	if err != nil || len(rs.Items) != 1 || !rs.Items[0].SelfReported || rs.Items[0].OwnerSaid != "Yes, send it to Sam." {
		t.Fatalf("receipt = %v %+v", err, rs.Items)
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
