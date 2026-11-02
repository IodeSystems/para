package importer

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/events"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
	"github.com/iodesystems/parasolve/internal/vault"
)

func get(t *testing.T, s store.Store, id string) model.Document {
	t.Helper()
	d, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return d
}

func mustLintClean(t *testing.T, s store.Store) {
	t.Helper()
	fs, err := s.Lint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		t.Errorf("lint: %s", f)
	}
}

func TestLegacyScanFoldCounts(t *testing.T) {
	rep, _, err := Scan(legacyFixture(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Adapter != AdapterLegacy {
		t.Fatalf("adapter = %s", rep.Adapter)
	}
	f := rep.Fold
	want := map[string]map[string]int{
		"tasks":     {"inflight": 1, "waiting": 2, "todo": 2, "done": 1},
		"deadlines": {"todo": 1},
		"questions": {"answered": 1, "open": 1, "waiting": 1},
	}
	for name, got := range map[string]map[string]int{"tasks": f.TasksByStatus, "deadlines": f.DeadlinesByStatus, "questions": f.QuestionsByStatus} {
		if fmt.Sprint(got) != fmt.Sprint(want[name]) {
			t.Errorf("%s by status = %v, want %v", name, got, want[name])
		}
	}
	if f.OpenQuestions != 2 || f.Contacts != 4 || f.DependsEdges != 4 || f.Checks != 1 || f.Risks != 1 || f.Items != 15 {
		t.Errorf("fold = %+v", f)
	}
	if len(rep.Unmapped) != 3 {
		t.Errorf("unmapped = %+v", rep.Unmapped)
	}
	if len(rep.Refused) != 0 {
		t.Errorf("refused = %+v", rep.Refused)
	}
}

func TestLegacyMappings(t *testing.T) {
	s, root, _, res := importAll(t, legacyFixture(t))
	if res.Refused != 0 || res.Created != 41 {
		t.Fatalf("result = %+v", res)
	}
	mustLintClean(t, s)

	// T: inflight -> doing; due kept; risk in the body.
	t1 := get(t, s, "t-250301-01")
	task := t1.Meta.(*model.Task)
	if task.Status != model.TaskDoing || task.Due != "2025-03-10" || task.Project != "[[atlas]]" {
		t.Errorf("T-01 = %+v", task)
	}
	if !strings.Contains(t1.Body, "The disk may arrive late.") || !strings.Contains(t1.Body, "Imported id: T-250301-01") {
		t.Errorf("T-01 body:\n%s", t1.Body)
	}
	// A task with a sent contact is waiting: blocked, with the RFX it waits on.
	t2 := get(t, s, "t-250301-02").Meta.(*model.Task)
	if t2.Status != model.TaskBlocked || len(t2.BlockedBy) != 1 || t2.BlockedBy[0] != "waiting on Ada Example" ||
		len(t2.DependsOn) != 1 || t2.DependsOn[0] != "[[rfx-t-250301-02-ada-example]]" {
		t.Errorf("T-02 = %+v", t2)
	}
	// The contact becomes an RFX: via -> medium, state, reply_by -> next_nudge.
	r := get(t, s, "rfx-t-250301-02-ada-example").Meta.(*model.RFX)
	if r.Direction != model.RFXOut || r.Status != model.RFXOpen || len(r.Targets) != 1 || r.Targets[0].To != "[[person-ada-example]]" ||
		r.Targets[0].Medium != model.MediumEmail || r.Targets[0].State != model.TargetSent || r.Targets[0].Ref != "thread-17" ||
		r.NextNudge != "2025-03-08" || len(r.Blocks) != 1 || r.Blocks[0] != "[[t-250301-02]]" {
		t.Errorf("rfx = %+v", r)
	}
	// depends accumulate; a note target and an unknown id are kept as text, not links.
	t3 := get(t, s, "t-250301-03")
	d3 := t3.Meta.(*model.Task).DependsOn
	if len(d3) != 2 || d3[0] != "[[t-250301-01]]" || d3[1] != "[[t-250301-02]]" {
		t.Errorf("T-03 depends_on = %v", d3)
	}
	if !strings.Contains(t3.Body, "Depends on: N-250301-01") || !strings.Contains(t3.Body, "Depends on (not imported): T-999999-99") {
		t.Errorf("T-03 body:\n%s", t3.Body)
	}
	// check: latest wins, recorded in the body as text.
	if !strings.Contains(t3.Body, "test -e /mnt/new-disk") || strings.Contains(t3.Body, "PWNED") {
		t.Errorf("T-03 check:\n%s", t3.Body)
	}
	// waiting keeps its text in blocked_by; no project file -> the unsorted area.
	t4 := get(t, s, "t-250301-04").Meta.(*model.Task)
	if t4.Status != model.TaskBlocked || len(t4.BlockedBy) != 1 || t4.BlockedBy[0] != "wait for the paint order" ||
		t4.Area != "[[area-imported-unsorted]]" || t4.Project != "" {
		t.Errorf("T-04 = %+v", t4)
	}
	if t5 := get(t, s, "t-250301-05").Meta.(*model.Task); t5.Status != model.TaskDone {
		t.Errorf("T-05 = %+v", t5)
	}
	// Every contact replied or no-reply: back to todo.
	if t6 := get(t, s, "t-250301-06").Meta.(*model.Task); t6.Status != model.TaskTodo || len(t6.BlockedBy) != 0 {
		t.Errorf("T-06 = %+v", t6)
	}
	if rr := get(t, s, "rfx-t-250301-06-ada-example").Meta.(*model.RFX); rr.Status != model.RFXAnswered || rr.Targets[0].State != model.TargetReplied || rr.Targets[0].Medium != model.MediumSlack {
		t.Errorf("replied rfx = %+v", rr)
	}
	if rr := get(t, s, "rfx-t-250301-06-bo-example").Meta.(*model.RFX); rr.Status != model.RFXClosed || rr.Targets[0].State != model.TargetNoReply || rr.Targets[0].Medium != model.MediumPhone {
		t.Errorf("no-reply rfx = %+v", rr)
	}
	// Q: scoped to its project, answered with its answer; root when no project.
	q1 := get(t, s, "q-250301-01")
	if q := q1.Meta.(*model.Question); q.Status != model.QuestionAnswered || q.Scope != "[[atlas]]" || !strings.Contains(q1.Body, "## Answer\n\n/etc/atlas") {
		t.Errorf("Q-01 = %+v\n%s", q, q1.Body)
	}
	if q := get(t, s, "q-250301-02").Meta.(*model.Question); q.Status != model.QuestionOpen || q.Scope != model.ScopeRoot || q.RFX != "[[rfx-q-250301-02-bo-example]]" {
		t.Errorf("Q-02 = %+v", q)
	}
	// D: a task with due.
	if d := get(t, s, "d-250301-01").Meta.(*model.Task); d.Due != "2025-04-01" || d.Status != model.TaskTodo || d.Project != "[[beacon]]" {
		t.Errorf("D-01 = %+v", d)
	}
	// N: a note with its notes; X: a note tagged decision.
	n := get(t, s, "n-250301-01")
	if n.Meta.Header().Type != model.TypeNote || n.Title != "Port 3 is the uplink" || !strings.Contains(n.Body, "- 2025-03-01: Label confirmed.") {
		t.Errorf("N-01 = %+v\n%s", n.Meta, n.Body)
	}
	if x := get(t, s, "x-250301-01").Meta.(*model.Note); len(x.Tags) != 1 || x.Tags[0] != "decision" {
		t.Errorf("X-01 = %+v", x)
	}
	// I: an interaction; the opinion is kept only when the source has one.
	i1 := get(t, s, "i-250301-01")
	ia := i1.Meta.(*model.Interaction)
	if ia.Date != "2025-03-02" || ia.Medium != model.MediumSlack || ia.Direction != model.InteractionIn || ia.Visibility != model.VisibilityPrivate ||
		len(ia.With) != 1 || ia.With[0] != "[[person-ada-example]]" || !strings.Contains(i1.Body, "Opinion (recorded in the source): sounded unsure") {
		t.Errorf("I-01 = %+v\n%s", ia, i1.Body)
	}
	i2 := get(t, s, "i-250301-02")
	if strings.Contains(i2.Body, "pinion") || i2.Meta.(*model.Interaction).Medium != model.MediumOther {
		t.Errorf("I-02 invented something:\n%s", i2.Body)
	}
	if i3 := get(t, s, "i-250301-03"); i3.Meta.Header().Type != model.TypeNote {
		t.Errorf("I-03 with no one named should be a note, is %s", i3.Meta.Header().Type)
	}
	// Files: projects, areas, people, notes by type.
	if p := get(t, s, "atlas").Meta.(*model.Project); p.Status != model.ProjectActive || p.Owner != "Jordan Example" || p.Area != "[[home-lab]]" {
		t.Errorf("atlas = %+v", p)
	}
	if p := get(t, s, "beacon").Meta.(*model.Project); p.Status != model.ProjectPaused || p.Area != "" {
		t.Errorf("beacon = %+v", p)
	}
	// A project with no area gets a root question, not a guess.
	if q := get(t, s, "question-area-beacon").Meta.(*model.Question); q.Scope != model.ScopeRoot || q.Status != model.QuestionOpen {
		t.Errorf("area question = %+v", q)
	}
	if _, err := s.Get(ctx, "question-area-atlas"); err == nil {
		t.Error("atlas has an area; it needs no question")
	}
	if p := get(t, s, "old"); p.Folder != model.FolderArchive || p.Meta.(*model.Project).Status != model.ProjectDone {
		t.Errorf("archived project = %s %+v", p.Folder, p.Meta)
	}
	if p := get(t, s, "jordan-example").Meta.(*model.Person); p.Org != "acme.example" || len(p.Aliases) != 1 {
		t.Errorf("person = %+v", p)
	}
	if pe := get(t, s, "person-ada-example"); pe.Meta.(*model.Person).Org != "unknown" {
		t.Errorf("event person = %+v", pe.Meta)
	}
	if j := get(t, s, "2025-w10").Meta.(*model.Journal); j.Scope != model.JournalWeek || j.Date != "2025-03-03" {
		t.Errorf("weekly = %+v", j)
	}
	if j := get(t, s, "2025-03-01").Meta.(*model.Journal); j.Scope != model.JournalDay {
		t.Errorf("daily = %+v", j)
	}
	// Clarification: policy_exception, with the kind map; a bad kind stays a note.
	ex := get(t, s, "c-001")
	if e := ex.Meta.(*model.PolicyException); e.Kind != model.ExceptionOverlyBroad || e.Policy != "[[logging-policy]]" || e.By != "Jordan Example" {
		t.Errorf("C-001 = %+v", e)
	}
	if c2 := get(t, s, "c-002"); c2.Meta.Header().Type != model.TypeNote || c2.Meta.Header().Tags[0] != "clarification" {
		t.Errorf("C-002 = %+v", c2.Meta)
	}
	for id, tag := range map[string]string{"evidence": "evidence", "cc-7": "change-case", "logging-policy": "policy", "outage-two": "incident"} {
		d := get(t, s, id)
		if d.Meta.Header().Type != model.TypeNote || len(d.Meta.Header().Tags) == 0 || d.Meta.Header().Tags[0] != tag {
			t.Errorf("%s = %+v", id, d.Meta)
		}
	}
	if inc := get(t, s, "outage").Meta.(*model.Incident); inc.Severity != model.Sev2 || inc.Status != model.IncidentResolved {
		t.Errorf("incident = %+v", inc)
	}
	// An outbox draft is a note tagged draft, and nothing says it was sent.
	dr := get(t, s, "draft-to-ada")
	if dr.Meta.Header().Type != model.TypeNote || dr.Meta.Header().Tags[0] != "draft" {
		t.Errorf("draft = %+v", dr.Meta)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "resources", "draft-to-ada.md"))
	if strings.Contains(string(raw), "status: sent") || !strings.Contains(string(raw), "status: draft-not-sent") {
		t.Errorf("draft file:\n%s", raw)
	}
	// Links: wikilinks rewritten, embed turned into a link, unresolved kept as text, code left alone.
	dn := get(t, s, "design-notes")
	if !strings.Contains(dn.Body, "See [[atlas]] and [[atlas]] and Missing Page and [[atlas#Plan|the plan]].") || !strings.Contains(dn.Body, "`[[not-a-link]]`") {
		t.Errorf("design notes body:\n%s", dn.Body)
	}
	// Unknown frontmatter is kept.
	if !strings.Contains(string(mustRead(t, filepath.Join(root, "resources", "design-notes.md"))), "colour: teal") {
		t.Error("unknown frontmatter dropped")
	}
	// Old ids are searchable, in the body and in imported_id.
	hits, err := s.Search(ctx, searchQuery("T-250301-03"))
	if err != nil || len(hits) == 0 {
		t.Errorf("imported id not searchable: %v %v", hits, err)
	}
	// Provenance: file:// url and hash.
	if src := string(mustRead(t, filepath.Join(root, "projects", "atlas.md"))); !strings.Contains(src, "url: file://") || !strings.Contains(src, "hash: sha256:") {
		t.Errorf("provenance missing:\n%s", src)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestIdempotentRerun(t *testing.T) {
	dir := legacyFixture(t)
	s, root, _, first := importAll(t, dir)
	count := func() int {
		refs, _ := s.List(ctx, store.Filter{})
		return len(refs)
	}
	before, evBefore := count(), len(mustEvents(t, s))
	_, plan, err := Scan(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Run(ctx, s, plan, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Created != 0 || again.Already != first.Created || again.Refused != 0 || again.SourceChanged != 0 {
		t.Errorf("rerun = %+v, first created %d", again, first.Created)
	}
	if count() != before || len(mustEvents(t, s)) != evBefore {
		t.Errorf("rerun wrote: docs %d -> %d, events %d -> %d", before, count(), evBefore, len(mustEvents(t, s)))
	}
	mustLintClean(t, s)
	_ = root
}

func mustEvents(t *testing.T, s store.Store) []model.Event {
	t.Helper()
	evs, err := s.Events(ctx, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func TestRerunWithChangedSourceDoesNotOverwrite(t *testing.T) {
	dir := legacyFixture(t)
	s, _, _, _ := importAll(t, dir)
	put(t, dir, "02-Areas/Home Lab.md", "---\ntype: area\n---\n\n# Home Lab\n\nEdited at the source.\n")
	_, plan, err := Scan(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(ctx, s, plan, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceChanged != 1 || res.Created != 0 {
		t.Errorf("rerun = %+v", res)
	}
	if strings.Contains(get(t, s, "home-lab").Body, "Edited") {
		t.Error("an imported document was overwritten")
	}
	// A source that changes between scan and run is refused.
	_, plan, _ = Scan(dir, "")
	put(t, dir, "00-Inbox/call-the-plumber.md", "Changed after scan.\n")
	s2, _ := newStore(t)
	res, err = Run(ctx, s2, plan, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Refused != 1 || !strings.Contains(res.Issues[0].Reason, "source changed since scan") {
		t.Errorf("run = %+v", res)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := legacyFixture(t)
	s, root := newStore(t)
	_, plan, _ := Scan(dir, "")
	res, err := Run(ctx, s, plan, RunOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 41 || !res.DryRun {
		t.Errorf("dry run = %+v", res)
	}
	refs, _ := s.List(ctx, store.Filter{})
	if len(refs) != 0 || len(mustEvents(t, s)) != 0 {
		t.Errorf("dry run wrote %d docs, %d events", len(refs), len(mustEvents(t, s)))
	}
	_ = root
}

func TestUndo(t *testing.T) {
	dir := legacyFixture(t)
	s, _, _, res := importAll(t, dir)
	// A document edited after the import blocks the undo; nothing is removed.
	d := get(t, s, "home-lab")
	d.Body += "\n\nEdited by hand."
	if _, err := s.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	out, err := Undo(ctx, s, res.Batch)
	if err == nil || len(out.Changed) != 1 || out.Changed[0] != "home-lab" || len(out.Removed) != 0 {
		t.Fatalf("undo = %+v, %v", out, err)
	}
	refs, _ := s.List(ctx, store.Filter{})
	if len(refs) != res.Created {
		t.Errorf("a refused undo removed documents: %d left of %d", len(refs), res.Created)
	}
	// Put it back; the batch then undoes whole.
	d = get(t, s, "home-lab")
	d.Body = strings.TrimSuffix(d.Body, "\n\nEdited by hand.")
	if _, err := s.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	// An update changes the hash even when the text returns, so still blocked: the rule is "changed since".
	if _, err := Undo(ctx, s, res.Batch); err == nil {
		t.Fatal("a document written after the import must block undo")
	}
	// A clean batch.
	s2, _, _, res2 := importAll(t, dir)
	out, err = Undo(ctx, s2, res2.Batch)
	if err != nil || len(out.Removed) != res2.Created {
		t.Fatalf("undo = %+v, %v", out, err)
	}
	refs, _ = s2.List(ctx, store.Filter{})
	if len(refs) != 0 {
		t.Errorf("%d documents left", len(refs))
	}
	mustLintClean(t, s2)
	var undone int
	for _, e := range mustEvents(t, s2) {
		if e.Op == events.OpImportUndo && e.From == res2.Batch {
			undone++
		}
	}
	if undone != res2.Created {
		t.Errorf("import_undo events = %d, want %d", undone, res2.Created)
	}
	// Undo twice removes nothing more; an unknown batch is an error.
	if out, err := Undo(ctx, s2, res2.Batch); err != nil || len(out.Removed) != 0 {
		t.Errorf("second undo = %+v, %v", out, err)
	}
	if _, err := Undo(ctx, s2, "imp-nosuchbatch"); err == nil {
		t.Error("unknown batch must fail")
	}
	// After the undo the same plan imports again.
	_, plan, _ := Scan(dir, "")
	again, err := Run(ctx, s2, plan, RunOptions{})
	if err != nil || again.Created != res2.Created {
		t.Errorf("reimport = %+v, %v", again, err)
	}
	_ = vault.ErrBatchChanged
}

func TestNoTextInEvents(t *testing.T) {
	dir := legacyFixture(t)
	s, root, _, res := importAll(t, dir)
	evs := mustEvents(t, s)
	if len(evs) != res.Created {
		t.Fatalf("events = %d, want one per document (%d)", len(evs), res.Created)
	}
	for _, e := range evs {
		if e.Op != events.OpImport || e.From != res.Batch || !strings.HasPrefix(e.Hash, "sha256:") {
			t.Errorf("event = %+v", e)
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, "events", "*.jsonl"))
	var all string
	for _, f := range files {
		all += string(mustRead(t, f))
	}
	for _, text := range []string{"Order the disk", "Mount the disk", "sounded unsure", "/etc/atlas", "Port 3", "Ada Example", "Jordan Example", "test -e", "wait for the paint", "uplink", "Atlas", "Call the plumber"} {
		if strings.Contains(all, text) {
			t.Errorf("event log holds source text %q", text)
		}
	}
}

// A check is text. The import records it and never runs it, and the package
// has no way to start a process.
func TestCheckIsNeverExecuted(t *testing.T) {
	dir := legacyFixture(t)
	marker := filepath.Join(t.TempDir(), "executed")
	put(t, dir, "events.jsonl", string(mustRead(t, filepath.Join(dir, "events.jsonl")))+
		`{"ts":"2025-03-02T09:00:00Z","actor":"owner","action":"add","id":"T-250302-01","type":"task","text":"Run a check","project":"Atlas"}`+"\n"+
		`{"ts":"2025-03-02T09:01:00Z","actor":"owner","action":"check","id":"T-250302-01","check":"touch `+marker+`; $(touch `+marker+`)"}`+"\n")
	s, _, _, res := importAll(t, dir)
	if res.Refused != 0 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the check was executed")
	}
	if body := get(t, s, "t-250302-01").Body; !strings.Contains(body, "touch "+marker) || !strings.Contains(body, "not run") {
		t.Errorf("check not recorded as text:\n%s", body)
	}
	// No file of the package imports os/exec or a process starter.
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range af.Imports {
			switch p := strings.Trim(im.Path.Value, `"`); p {
			case "os/exec", "syscall", "plugin", "net/http", "net":
				t.Errorf("%s imports %s", f, p)
			}
		}
	}
}

func TestDuplicatesAndPlanEdits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "md")
	put(t, dir, "a/one.md", "# One\n\nSame bytes.\n")
	put(t, dir, "b/one-copy.md", "# One\n\nSame bytes.\n")
	put(t, dir, "two.md", "Links to [[one-copy]] and [[One]].\n")
	put(t, dir, "three.md", "# Three\n\nSkip me, see [[two]].\n")
	rep, plan, err := Scan(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Adapter != AdapterMarkdown || len(rep.Duplicates) != 1 || len(rep.Duplicates[0]) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	byPath := map[string]*Entry{}
	for i := range plan.Entries {
		byPath[plan.Entries[i].Path] = &plan.Entries[i]
	}
	if byPath["b/one-copy.md"].DuplicateOf != "a/one.md" {
		t.Errorf("duplicate_of = %q", byPath["b/one-copy.md"].DuplicateOf)
	}
	// Edit the plan: skip three, rename two, put one in the archive.
	byPath["three.md"].Skip = "not needed"
	byPath["two.md"].ID = "second-note"
	byPath["a/one.md"].Folder = "archive"
	byPath["a/one.md"].Tags = []string{"kept"}
	s, root := newStore(t)
	res, err := Run(ctx, s, plan, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 || res.Skipped != 1 || res.Duplicates != 1 || res.Refused != 0 {
		t.Fatalf("result = %+v", res)
	}
	two := get(t, s, "second-note")
	// Both links go to the single imported copy, and [[One]] resolves case-insensitively.
	if !strings.Contains(two.Body, "[[one]] and [[one]]") {
		t.Errorf("two body: %q", two.Body)
	}
	if one := get(t, s, "one"); one.Folder != model.FolderArchive || one.Meta.Header().Tags[0] != "kept" {
		t.Errorf("one = %+v", one)
	}
	mustLintClean(t, s)
	if _, err := os.Stat(filepath.Join(root, "resources", "three.md")); err == nil {
		t.Error("skipped entry was imported")
	}
	// A skipped target leaves plain text for links to it.
	byPath["two.md"].Skip = "later"
	byPath["three.md"].Skip = ""
	s2, _ := newStore(t)
	if _, err := Run(ctx, s2, plan, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if body := get(t, s2, "three").Body; !strings.Contains(body, "see two.") {
		t.Errorf("three body: %q", body)
	}
	// Bad decisions are refused with a reason, not guessed at.
	byPath["two.md"].Skip = ""
	byPath["two.md"].Type = "task"
	byPath["three.md"].ID = "Bad Id"
	byPath["a/one.md"].Folder = "nowhere"
	s3, _ := newStore(t)
	res, err = Run(ctx, s3, plan, RunOptions{})
	if err != nil || res.Refused != 3 || res.Created != 0 {
		t.Errorf("result = %+v, %v", res, err)
	}
}

func TestPlanFileRoundTripAndRefusesOverwrite(t *testing.T) {
	_, plan, err := Scan(legacyFixture(t), "legacy")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "plan.yaml")
	if err := WritePlan(p, plan, false); err != nil {
		t.Fatal(err)
	}
	if err := WritePlan(p, plan, false); err == nil {
		t.Error("a plan someone edited must not be overwritten without --force")
	}
	back, err := LoadPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Entries) != len(plan.Entries) || back.Entries[3].ID != plan.Entries[3].ID || back.Source != plan.Source {
		t.Error("plan did not round-trip")
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(bad, []byte("version: 1\nadapter: markdown\nsource: /x\nentries: []\nunknown_field: 1\n"), 0o600)
	if _, err := LoadPlan(bad); err == nil {
		t.Error("an unknown plan field must be an error")
	}
}

func TestMarkdownAdapterNormalizesAndRefuses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notes")
	put(t, dir, "plain note.md", "Just text, no title.\r\nSecond line with a [[linked-page]] and a [[Ghost]].\r\n")
	put(t, dir, "linked-page.md", "\xef\xbb\xbf---\ntags: [a, \"#b\"]\nrating: 5\ndate: 2026-01-02\nmood: calm\n---\n# Linked Page\n\nBody ![[plain note]].\n\n# A second heading\n\nText.\n\n```\n# code comment\n```\n")
	put(t, dir, "html.md", "# Has HTML\n\n<div>raw</div>\n")
	put(t, dir, "table.md", "# Table\n\n| a | b |\n|---|---|\n| 1 |\n")
	put(t, dir, "badyaml.md", "---\nkey: [unclosed\n---\n\n# Bad\n")
	put(t, dir, "image.md", "# Image\n\n![](pic.png)\n")
	put(t, dir, "sub/nested.md", "# Nested\n\nLink to [[sub/nested]] and [[linked-page#Section|alias]].\n")
	put(t, dir, "notes.txt", "not markdown")
	put(t, dir, "latin1.md", "# Caf\xe9\n")
	put(t, dir, ".obsidian/workspace.md", "# hidden\n")
	rep, plan, err := Scan(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Adapter != AdapterMarkdown || rep.Documents != 8 {
		t.Fatalf("report = %+v", rep)
	}
	refused := map[string]string{}
	for _, r := range rep.Refused {
		refused[r.Path] = r.Reason
	}
	for _, name := range []string{"html.md", "table.md", "badyaml.md", "image.md", "latin1.md"} {
		if refused[name] == "" {
			t.Errorf("%s was not refused: %v", name, refused)
		}
	}
	if !strings.Contains(refused["html.md"], "raw-html") || !strings.Contains(refused["table.md"], "table") {
		t.Errorf("reasons = %v", refused)
	}
	if len(refused) != 5 {
		t.Errorf("refused = %v", refused)
	}
	s, root := newStore(t)
	res, err := Run(ctx, s, plan, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing is dropped silently: every document is created or reported.
	if res.Created+res.Refused != rep.Documents || res.Created != 3 || res.Refused != 5 {
		t.Errorf("result = %+v", res)
	}
	pn := get(t, s, "plain-note")
	if pn.Title != "plain note" || !strings.Contains(pn.Body, "[[linked-page]] and a Ghost.") || strings.Contains(pn.Body, "\r") {
		t.Errorf("plain note = %q\n%s", pn.Title, pn.Body)
	}
	lp := mustRead(t, filepath.Join(root, "resources", "linked-page.md"))
	for _, want := range []string{"rating: 5", "date: 2026-01-02", "mood: calm", "[[plain-note]]", "## A second heading", "# code comment", "- a", "- b"} {
		if !strings.Contains(string(lp), want) {
			t.Errorf("linked-page lacks %q:\n%s", want, lp)
		}
	}
	if !strings.Contains(string(mustRead(t, filepath.Join(root, "resources", "nested.md"))), "[[nested]] and [[linked-page#Section|alias]]") {
		t.Error("nested links not rewritten")
	}
	for _, k := range []string{"title", "line-ending", "bom", "link", "embed", "heading"} {
		if rep.Normalized[k] == 0 {
			t.Errorf("no %q normalization reported: %v", k, rep.Normalized)
		}
	}
	// The hidden folder and the non-markdown file are reported, not read.
	got := map[string]string{}
	for _, u := range rep.Unmapped {
		got[u.Path] = u.Reason
	}
	if got[".obsidian/"] == "" || got["notes.txt"] == "" {
		t.Errorf("unmapped = %v", got)
	}
	mustLintClean(t, s)
}

func TestHostileNames(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "hostile")
	secret := filepath.Join(base, "secret.txt")
	os.WriteFile(secret, []byte("TOP-SECRET-CONTENT"), 0o600)
	names := []string{
		"line\nbreak.md", "esc\x1b[31mred.md", "..md", "-rf.md", "[[x]].md", "<img src=x onerror=alert(1)>.md", "日本語.md",
		strings.Repeat("a", 200) + ".md", ".md.md", "a b c.md", "sp ace/..hidden.md", "CON.md", "x%2Fy.md", "back\\slash.md",
	}
	for i, n := range names {
		put(t, dir, n, fmt.Sprintf("Some text %d.\n", i))
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadDir(base)
	rep, plan, err := Scan(dir, "markdown")
	if err != nil {
		t.Fatal(err)
	}
	s, root := newStore(t)
	res, err := Run(ctx, s, plan, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created+res.Refused != rep.Documents {
		t.Errorf("a document vanished: %+v of %d", res, rep.Documents)
	}
	// The script name is refused (raw HTML in the title); the rest are created.
	if res.Refused != 1 {
		t.Errorf("refused = %+v", res.Issues)
	}
	// Symbolic links are never followed.
	links := 0
	for _, u := range rep.Unmapped {
		if strings.Contains(u.Reason, "symbolic link") {
			links++
		}
	}
	if links != 2 {
		t.Errorf("symlinks reported = %d, want 2", links)
	}
	if hits, _ := s.Search(ctx, searchQuery("TOP-SECRET-CONTENT")); len(hits) != 0 {
		t.Error("a symlink target was read into the vault")
	}
	// Every id is a legal file stem; every file is inside the vault; the report has no control characters.
	refs, _ := s.List(ctx, store.Filter{})
	seen := map[string]bool{}
	for _, r := range refs {
		if seen[strings.ToLower(r.ID)] || strings.ContainsAny(r.ID, "/\\.%\n\x1b") {
			t.Errorf("bad id %q", r.ID)
		}
		seen[strings.ToLower(r.ID)] = true
	}
	for _, line := range append(issueTexts(rep.Unmapped), issueTexts(rep.Refused)...) {
		for _, r := range line {
			if r < 0x20 || r == 0x7f {
				t.Errorf("control character in report line %q", line)
			}
		}
	}
	for _, i := range res.Issues {
		for _, r := range i.Path + i.Reason {
			if r < 0x20 || r == 0x7f {
				t.Errorf("control character in run line %+v", i)
			}
		}
	}
	after, _ := os.ReadDir(base)
	if len(after) != len(before) { // nothing was added beside the source
		t.Errorf("files appeared outside the vault: %d -> %d", len(before), len(after))
	}
	mustLintClean(t, s)
	_ = root
}

func issueTexts(is []Issue) []string {
	var out []string
	for _, i := range is {
		out = append(out, i.Path+" "+i.Reason)
	}
	return out
}

func TestScanWritesNothingAndNeedsNoVault(t *testing.T) {
	dir := legacyFixture(t)
	before := snapshot(t, dir)
	if _, _, err := Scan(dir, ""); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, dir); after != before {
		t.Error("scan changed the source")
	}
	if _, _, err := Scan(t.TempDir(), ""); err == nil {
		t.Error("an empty folder has nothing to import")
	}
	if _, _, err := Scan(filepath.Join(dir, "nope"), ""); err == nil {
		t.Error("a missing folder must fail")
	}
}

func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			info, _ := d.Info()
			fmt.Fprintf(&b, "%s %d %d\n", p, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return b.String()
}

// TestTiming2000 reports how long a batch import of 2,000 files takes, and how
// long the same documents take one Create at a time (200, scaled).
func TestTiming2000(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	dir := filepath.Join(t.TempDir(), "big")
	for i := range 2000 {
		next := (i + 1) % 2000
		put(t, dir, fmt.Sprintf("folder-%02d/note-%04d.md", i%20, i),
			fmt.Sprintf("---\ntags: [bulk]\n---\n\n# Synthetic note %d\n\nBody text %d with a [[note-%04d]] link.\n", i, i, next))
	}
	t0 := time.Now()
	_, plan, err := Scan(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	scanT := time.Since(t0)
	s, _ := newStore(t)
	t0 = time.Now()
	res, err := Run(ctx, s, plan, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runT := time.Since(t0)
	if res.Created != 2000 || res.Refused != 0 {
		t.Fatalf("result = %+v", res)
	}
	mustLintClean(t, s)
	// Baseline: the same documents through Create, one lock and two fsyncs each.
	s2, _ := newStore(t)
	t0 = time.Now()
	for i := range 200 {
		d := model.Document{ID: fmt.Sprintf("note-%04d", i), Folder: model.FolderResources, Title: fmt.Sprintf("Synthetic note %d", i), Body: "Body.", Meta: &model.Note{Common: model.Common{Type: model.TypeNote}}}
		if _, err := s2.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	oneT := time.Since(t0)
	t.Logf("2000 files: scan %v, batch run %v (%.2f ms/doc); 200 single Creates %v (%.2f ms/doc, would be ~%v for 2000, growing with vault size)",
		scanT.Round(time.Millisecond), runT.Round(time.Millisecond), float64(runT.Microseconds())/2000/1000,
		oneT.Round(time.Millisecond), float64(oneT.Microseconds())/200/1000, (oneT * 10).Round(time.Millisecond))
	if runT > 60*time.Second {
		t.Errorf("batch run of 2000 files took %v", runT)
	}
	// Undo of the whole batch.
	t0 = time.Now()
	if out, err := Undo(ctx, s, res.Batch); err != nil || len(out.Removed) != 2000 {
		t.Fatalf("undo = %+v, %v", out, err)
	}
	t.Logf("undo of 2000: %v", time.Since(t0).Round(time.Millisecond))
}
