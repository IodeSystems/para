package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/index"
	"github.com/iodesystems/para/internal/model"
)

func taskIDs(o TaskListOutput) []string {
	out := []string{}
	for _, x := range o.Tasks {
		out = append(out, x.ID)
	}
	return out
}

func TestDocMergeMovesLinksListsAndNotesAndHidesAlias(t *testing.T) {
	d, proj := laterVault(t)
	ada := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"}).ID
	bo := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Bo Example", Org: "acme.example"}).ID
	keep := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Weekly sync", Kind: model.KindMeeting, With: []string{ada}}).ID
	alias := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Weekly sync (moved)", Kind: model.KindMeeting, With: []string{ada, bo}}).ID
	pre := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Prep slides"}).ID
	must(t, TaskDependsAdd, d, TaskDependsInput{ID: alias, DependsOn: []string{pre}})
	after := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Send minutes", DependsOn: []string{alias, keep}}).ID
	note := must(t, NoteAdd, d, NoteAddInput{Title: "Agenda", Body: "See [[" + alias + "]] for the room."}).ID
	must(t, NotesEdit, d, NotesEditInput{ID: alias, MD: "Room 4 is booked."})
	must(t, NotesEdit, d, NotesEditInput{ID: keep, MD: "Bring the roadmap."})

	// Refusals.
	for name, in := range map[string]DocMergeInput{
		"itself":     {Keep: keep, Alias: keep},
		"other type": {Keep: note, Alias: alias},
		"no keep":    {Keep: "task-nope", Alias: alias},
	} {
		if _, err := DocMerge.Run(ctx, d, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	ag := agentDeps(d, "scribe")
	out, err := DocMerge.Run(WithWrite(ctx, WriteMeta{Why: "Same meeting, moved."}), ag, DocMergeInput{Keep: keep, Alias: alias})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Closed || !out.Notes || !slices.Contains(out.Rewritten, note) {
		t.Fatalf("merge = %+v", out)
	}
	kv, kbody := taskOf(t, d, keep)
	if !slices.Contains(kv.With, bo) || !slices.Contains(kv.DependsOn, pre) || !strings.Contains(kbody, "Room 4 is booked.") || !strings.Contains(kbody, "Bring the roadmap.") {
		t.Fatalf("keep = %+v\n%s", kv, kbody)
	}
	av, _ := taskOf(t, d, after)
	if !slices.Equal(av.DependsOn, []string{keep}) {
		t.Fatalf("dependent waits on %v, want only %s", av.DependsOn, keep)
	}
	nd, err := d.Store.Get(ctx, note)
	if err != nil || !strings.Contains(nd.Body, "[["+keep+"]]") || strings.Contains(nd.Body, "[["+alias+"]]") {
		t.Fatalf("note body = %q, %v", nd.Body, err)
	}
	// The alias resolves, names its primary, and leaves every list.
	adoc, err := d.Store.Get(ctx, alias)
	if err != nil || adoc.Meta.Header().DuplicateOf != model.LinkTo(keep) {
		t.Fatalf("alias = %+v, %v", adoc.Meta, err)
	}
	if slices.Contains(taskIDs(must(t, TaskList, d, TaskListInput{})), alias) {
		t.Fatal("task_list lists the merged alias")
	}
	if hits := must(t, Search, d, SearchInput{Query: "moved"}); slices.ContainsFunc(hits.Hits, func(h index.Hit) bool { return h.ID == alias }) {
		t.Fatalf("search finds the merged alias: %+v", hits.Hits)
	}
	if m := must(t, DocMerges, d, DocMergesInput{ID: keep}); len(m.MergedFrom) != 1 || m.MergedFrom[0].ID != alias {
		t.Fatalf("merged from = %+v", m)
	}

	// One receipt; undo restores every document.
	recs := must(t, ReceiptList, d, ReceiptListInput{Op: "doc_merge"})
	if len(recs.Items) != 1 {
		t.Fatalf("receipts = %+v", recs.Items)
	}
	if det := must(t, ReceiptShow, d, ReceiptIDInput{ID: recs.Items[0].ID}); !det.Revertible || len(det.Also) < 3 {
		t.Fatalf("receipt detail = %+v", det)
	}
	must(t, ReceiptUndo, d, ReceiptUndoInput{ID: recs.Items[0].ID})
	if adoc, _ := d.Store.Get(ctx, alias); adoc.Meta.Header().DuplicateOf != "" || adoc.Meta.(*model.Task).Status != model.TaskTodo {
		t.Fatalf("undo left alias = %+v", adoc.Meta)
	}
	if nd, _ := d.Store.Get(ctx, note); !strings.Contains(nd.Body, "[["+alias+"]]") {
		t.Fatalf("undo left the note = %q", nd.Body)
	}
	if _, kbody := taskOf(t, d, keep); strings.Contains(kbody, "Room 4") {
		t.Fatalf("undo left keep's notes:\n%s", kbody)
	}
	if !slices.Contains(taskIDs(must(t, TaskList, d, TaskListInput{})), alias) {
		t.Fatal("undo did not list the alias again")
	}
}

func TestTaskResolveDuplicateMerges(t *testing.T) {
	d, proj := laterVault(t)
	keep := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Renew cert"}).ID
	alias := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Renew the TLS cert"}).ID
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which CA?", Task: alias}).ID
	out := must(t, TaskResolve, d, TaskResolveInput{ID: alias, Outcome: model.OutcomeDuplicate, Link: keep})
	if out.Merge == nil || out.Status != model.TaskDropped {
		t.Fatalf("resolve = %+v", out)
	}
	qd, err := d.Store.Get(ctx, q)
	if err != nil || !strings.Contains(renderText(qd), "[["+keep+"]]") {
		t.Fatalf("question still points at the alias: %s", renderText(qd))
	}
	if slices.Contains(taskIDs(must(t, TaskList, d, TaskListInput{Status: model.TaskDropped})), alias) {
		t.Fatal("closed duplicate still listed")
	}
	// A person's merge leaves a receipt too, so it can be undone.
	if recs := must(t, ReceiptList, d, ReceiptListInput{Doc: alias}); len(recs.Items) != 1 {
		t.Fatalf("receipts = %+v", recs.Items)
	}
}

func TestDocArchiveHidesAndUndoRestores(t *testing.T) {
	d, proj := laterVault(t)
	ada := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"}).ID
	m := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Old standup", Kind: model.KindMeeting, With: []string{ada}}).ID
	out := must(t, DocArchive, d, DocArchiveInput{ID: m, Reason: "It moved to Tuesday."})
	if out.Folder != string(model.FolderArchive) {
		t.Fatalf("archive = %+v", out)
	}
	if slices.Contains(taskIDs(must(t, TaskList, d, TaskListInput{})), m) {
		t.Fatal("task_list lists the archived meeting")
	}
	if _, err := d.Store.Get(ctx, m); err != nil {
		t.Fatalf("archived id does not resolve: %v", err)
	}
	if _, err := DocArchive.Run(ctx, d, DocArchiveInput{ID: m}); err == nil {
		t.Fatal("archived twice")
	}
	recs := must(t, ReceiptList, d, ReceiptListInput{Op: "doc_archive"})
	if len(recs.Items) != 1 {
		t.Fatalf("receipts = %+v", recs.Items)
	}
	must(t, ReceiptUndo, d, ReceiptUndoInput{ID: recs.Items[0].ID})
	if doc, _ := d.Store.Get(ctx, m); doc.Folder == model.FolderArchive || strings.Contains(doc.Body, "Tuesday") {
		t.Fatalf("undo left %s in %s:\n%s", m, doc.Folder, doc.Body)
	}
}

func TestNotesReadEdit(t *testing.T) {
	d, proj := laterVault(t)
	id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Plan offsite", Body: "Context line.\n\n## Updates\n\n### 2026-11-01\n\nBooked."}).ID
	r := must(t, NotesRead, d, NotesReadInput{ID: id})
	if r.MD != "" || r.Hash != "" {
		t.Fatalf("empty notes = %+v", r)
	}
	h1 := must(t, NotesEdit, d, NotesEditInput{ID: id, MD: "First."}).Hash
	if h1 == "" {
		t.Fatal("no hash")
	}
	// No hash once there are notes: refused with the current hash.
	_, err := NotesEdit.Run(ctx, d, NotesEditInput{ID: id, MD: "Other."})
	if err == nil || !strings.Contains(err.Error(), h1) {
		t.Fatalf("edit without hash = %v", err)
	}
	if _, err := NotesEdit.Run(ctx, d, NotesEditInput{ID: id, Hash: "sha256:stale", MD: "Other."}); err == nil {
		t.Fatal("stale hash accepted")
	}
	if _, err := NotesEdit.Run(ctx, d, NotesEditInput{ID: id, Hash: h1, MD: "## Split"}); err == nil {
		t.Fatal("a ## line was accepted")
	}
	h2 := must(t, NotesEdit, d, NotesEditInput{ID: id, Hash: h1, MD: "Second.\n\n### Sub\n\nMore."}).Hash
	r = must(t, NotesRead, d, NotesReadInput{ID: id})
	if r.Hash != h2 || r.MD != "Second.\n\n### Sub\n\nMore." {
		t.Fatalf("read = %+v", r)
	}
	_, body := taskOf(t, d, id)
	if !strings.Contains(body, "Context line.") || !strings.Contains(body, "### 2026-11-01\n\nBooked.") || strings.Contains(body, "First.") {
		t.Fatalf("body = %s", body)
	}
	must(t, NotesEdit, d, NotesEditInput{ID: id, Hash: h2, MD: ""})
	if _, body := taskOf(t, d, id); strings.Contains(body, "## Notes") {
		t.Fatalf("empty md left the section:\n%s", body)
	}
	// notes_save is the owner's: an agent is refused.
	if _, err := NotesSave.Run(ctx, agentDeps(d, "scribe"), NotesEditInput{ID: id, MD: "x"}); err == nil {
		t.Fatal("an agent ran notes_save")
	}
}
