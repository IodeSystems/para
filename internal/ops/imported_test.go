package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestImportedFromIsStampedOnCreatesNotReceipts(t *testing.T) {
	f := newAgentFix(t)
	wctx := WithWrite(t.Context(), WriteMeta{Why: "Import of the old vault."})
	src := &model.ImportedFrom{Path: "/old/vault/tasks/cut-release.md", Hash: strings.ToUpper(sha("cut the release"))}
	out, err := TaskAdd.Run(wctx, f.agent, TaskAddInput{Goal: f.proj, Title: "Cut the release", DoneWhen: "Tagged.", ImportedFrom: src})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := f.agent.Store.Get(t.Context(), out.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := doc.Meta.Header().ImportedFrom
	if got == nil || got.Path != src.Path || got.Hash != "sha256:"+sha("cut the release") {
		t.Fatalf("imported_from = %+v", got)
	}
	refs, err := f.agent.Store.List(t.Context(), store.Filter{Type: model.TypeReceipt})
	if err != nil || len(refs) == 0 {
		t.Fatalf("no receipt: %v", err)
	}
	for _, r := range refs {
		rd, _ := f.agent.Store.Get(t.Context(), r.ID)
		if rd.Meta.Header().ImportedFrom != nil {
			t.Errorf("receipt %s carries imported_from", r.ID)
		}
	}
	// A bad hash is refused before anything is written.
	if _, err := NoteAdd.Run(t.Context(), f.owner, NoteAddInput{Title: "Bad", ImportedFrom: &model.ImportedFrom{Path: "/old/a.md", Hash: "abc"}}); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Errorf("bad hash: %v", err)
	}
	if _, err := NoteAdd.Run(t.Context(), f.owner, NoteAddInput{Title: "No path", ImportedFrom: &model.ImportedFrom{Hash: sha("x")}}); err == nil {
		t.Error("empty path was accepted")
	}
}

func TestImportVerifyImportedMissingChangedExtra(t *testing.T) {
	f := newAgentFix(t)
	d := f.owner
	add := func(title, path, body string) string {
		t.Helper()
		return must(t, NoteAdd, d, NoteAddInput{Title: title, ImportedFrom: &model.ImportedFrom{Path: path, Hash: sha(body)}}).ID
	}
	same := add("Runbook", "/old/notes/runbook.md", "runbook v1")
	edited := add("Plan", "/old/notes/plan.md", "plan v1")
	gone := add("Gone file", "/old/notes/gone.md", "gone")
	moved := add("Moved", "/old/notes/moved.md", "moved body")
	outside := add("Elsewhere", "/other/x.md", "x")
	goal := must(t, GoalAdd, d, GoalAddInput{Title: "Imported goal", Owner: "Jordan Example", DoneWhen: "Done.",
		ImportedFrom: &model.ImportedFrom{Path: "/old/goals.md", Hash: sha("goals")}})

	out := must(t, ImportVerify, d, ImportVerifyInput{Root: "/old", Files: []ImportFile{
		{Path: "/old/notes/runbook.md", Hash: sha("runbook v1")},
		{Path: "/old/notes/plan.md", Hash: "sha256:" + sha("plan v2")},
		{Path: "/old/notes/new.md", Hash: sha("never imported")},
		{Path: "/old/archive/moved.md", Hash: sha("moved body")},
		{Path: "/old/goals.md", Hash: sha("goals")},
	}})
	if out.Checked != 5 || out.Imported != 3 || out.Missing != 1 || out.Changed != 1 {
		t.Fatalf("counts = %+v", out)
	}
	if out.Files[0].Status != ImportMissing || out.Files[0].Path != "/old/notes/new.md" {
		t.Errorf("missing does not come first: %+v", out.Files)
	}
	if out.Files[1].Status != ImportChanged || !slices.Equal(out.Files[1].Docs, []string{edited}) || out.Files[1].StoredHash != "sha256:"+sha("plan v1") {
		t.Errorf("changed = %+v", out.Files[1])
	}
	byPath := map[string]ImportFileResult{}
	for _, r := range out.Files {
		byPath[r.Path] = r
	}
	if r := byPath["/old/notes/runbook.md"]; r.Status != ImportImported || !slices.Equal(r.Docs, []string{same}) || r.MatchedBy != "path" {
		t.Errorf("runbook = %+v", r)
	}
	if r := byPath["/old/goals.md"]; r.Status != ImportImported || !slices.Equal(r.Docs, []string{goal.ID}) {
		t.Errorf("goal = %+v", r)
	}
	if r := byPath["/old/archive/moved.md"]; r.Status != ImportImported || r.MatchedBy != "hash" || !slices.Equal(r.Docs, []string{moved}) {
		t.Errorf("moved = %+v", r)
	}
	// gone.md is under root and not listed; moved.md was claimed by hash, and
	// /other is outside root.
	if len(out.Extra) != 1 || out.Extra[0].ID != gone {
		t.Errorf("extra = %+v", out.Extra)
	}

	// Without root, no extra.
	if o := must(t, ImportVerify, d, ImportVerifyInput{Files: []ImportFile{{Path: "/old/notes/runbook.md", Hash: sha("runbook v1")}}}); len(o.Extra) != 0 {
		t.Errorf("extra without root = %+v", o.Extra)
	}
	if _, err := ImportVerify.Run(t.Context(), d, ImportVerifyInput{}); err == nil {
		t.Error("empty input was accepted")
	}
	if _, err := ImportVerify.Run(t.Context(), d, ImportVerifyInput{Files: []ImportFile{{Path: "/a.md", Hash: "nope"}}}); err == nil {
		t.Error("bad hash was accepted")
	}

	list := must(t, ImportList, d, ImportListInput{Root: "/old/notes"})
	if len(list.Docs) != 4 {
		t.Errorf("import_list under /old/notes = %+v", list.Docs)
	}
	if all := must(t, ImportList, d, ImportListInput{}); !slices.ContainsFunc(all.Docs, func(x ImportedDoc) bool { return x.ID == outside }) {
		t.Errorf("import_list without root lacks %s", outside)
	}
}
