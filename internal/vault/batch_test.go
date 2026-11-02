package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/events"
	"github.com/iodesystems/parasolve/internal/model"
)

func noteDoc(id, title string) model.Document {
	return model.Document{ID: id, Folder: model.FolderResources, Title: title, Body: "Body of " + id,
		Meta: &model.Note{Common: model.Common{Type: model.TypeNote, Created: "2026-11-01"}}}
}

func TestCreateBatch(t *testing.T) {
	v, root := newVault(t)
	if _, err := v.Create(ctx, noteDoc("taken", "Taken")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	docs := []model.Document{
		noteDoc("one", "One"),
		noteDoc("taken", "Clashes with an existing document"),
		noteDoc("bad id", "Bad"),
		noteDoc("two", "Two <b>raw html</b>"), // lint refuses it
		noteDoc("three", "Three"),
		noteDoc("three", "Repeated in the batch"),
	}
	res, err := v.CreateBatch(ctx, "imp-test1", docs)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range res {
		got = append(got, fmt.Sprint(r.Err == nil))
	}
	if strings.Join(got, ",") != "true,false,false,false,true,false" {
		t.Fatalf("results = %v: %+v", got, res)
	}
	if !errors.Is(res[1].Err, model.ErrExists) {
		t.Errorf("clash = %v", res[1].Err)
	}
	var lintErr *model.LintError
	if !errors.As(res[3].Err, &lintErr) {
		t.Errorf("html = %v", res[3].Err)
	}
	for _, id := range []string{"one", "three"} {
		d, err := v.Get(ctx, id)
		if err != nil || d.Rev != res[map[string]int{"one": 0, "three": 4}[id]].Rev {
			t.Errorf("get %s = %v %v", id, d.Rev, err)
		}
	}
	// One import event per written document, carrying the batch id and a hash.
	var imports []model.Event
	for _, e := range eventLog(t, v) {
		if e.Op == events.OpImport {
			imports = append(imports, e)
		}
	}
	if len(imports) != 2 || imports[0].From != "imp-test1" || imports[0].To != "resources" || !strings.HasPrefix(imports[0].Hash, "sha256:") {
		t.Errorf("events = %+v", imports)
	}
	// The manifest knows the files: a reconcile logs nothing.
	rr, err := v.Reconcile(ctx)
	if err != nil || len(rr.Changes) != 0 {
		t.Errorf("reconcile after a batch = %+v, %v", rr, err)
	}
	if f, err := v.Check(ctx); err != nil || len(f) != 0 {
		t.Errorf("check = %v, %v", f, err)
	}
	if left, _ := filepath.Glob(filepath.Join(root, "resources", ".*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	// Provenance listing.
	docs2 := []model.Document{noteDoc("from-file", "From file")}
	if err := docs2[0].SetExtra("source", map[string]any{"url": "file:///src/a.md", "hash": "sha256:" + strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if res, err := v.CreateBatch(ctx, "imp-test2", docs2); err != nil || res[0].Err != nil {
		t.Fatalf("batch 2 = %+v, %v", res, err)
	}
	imported, ids, err := v.ImportedDocs(ctx)
	if err != nil || len(imported) != 1 || imported[0].ID != "from-file" || imported[0].SourceURL != "file:///src/a.md" || !ids["one"] || !ids["taken"] {
		t.Errorf("imported = %+v, %d ids, %v", imported, len(ids), err)
	}
}

func TestRemoveBatch(t *testing.T) {
	v, root := newVault(t)
	docs := []model.Document{noteDoc("a", "A"), noteDoc("b", "B"), noteDoc("c", "C")}
	if _, err := v.CreateBatch(ctx, "imp-rm", docs); err != nil {
		t.Fatal(err)
	}
	// A hand edit blocks the undo.
	p := filepath.Join(root, "resources", "b.md")
	data, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append(data, []byte("\nedited\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := v.RemoveBatch(ctx, "imp-rm")
	if !errors.Is(err, ErrBatchChanged) || len(res.Changed) != 1 || res.Changed[0] != "b" {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(root, "resources", "a.md")); err != nil {
		t.Error("a refused undo removed a file")
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err = v.RemoveBatch(ctx, "imp-rm")
	if err != nil || len(res.Removed) != 3 {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if rr, err := v.Reconcile(ctx); err != nil || len(rr.Changes) != 0 {
		t.Errorf("reconcile after undo = %+v, %v", rr, err)
	}
	if _, err := v.RemoveBatch(ctx, "imp-none"); err == nil {
		t.Error("an unknown batch must fail")
	}
}
