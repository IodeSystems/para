package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/model"
)

// Ops that return ids also return the names, so no surface shows an id.
func TestNamesBesideIds(t *testing.T) {
	d := targetDeps(t) // person-ada-example (Ada Example), contact ada@acme.example
	proj := must(t, ProjectAdd, d, ProjectAddInput{Title: "Portal rebuild"})
	t1 := must(t, TaskAdd, d, TaskAddInput{Project: proj.ID, Title: "Draft the plan", Assignee: "person-ada-example"})
	t2 := must(t, TaskAdd, d, TaskAddInput{Project: proj.ID, Title: "Review the plan", DependsOn: []string{t1.ID}})

	list := must(t, TaskList, d, TaskListInput{})
	var got TaskView
	for _, v := range list.Tasks {
		if v.ID == t2.ID {
			got = v
		}
	}
	if got.Project != proj.ID || got.ProjectName != "Portal rebuild" {
		t.Errorf("project %q name %q", got.Project, got.ProjectName)
	}
	if len(got.DependsOnNames) != 1 || got.DependsOnNames[0] != "Draft the plan" {
		t.Errorf("depends_on_names = %v", got.DependsOnNames)
	}
	for _, v := range list.Tasks {
		if v.ID == t1.ID && v.AssigneeName != "Ada Example" {
			t.Errorf("assignee name %q", v.AssigneeName)
		}
		if v.ID == t1.ID && v.DependsOnNames != nil {
			t.Errorf("a task with no dependencies has names %v", v.DependsOnNames)
		}
	}

	r := addRFX(t, d, RFXAddInput{Targets: []TargetInput{{To: "person-ada-example", Medium: model.MediumEmail}}})
	rl := must(t, RFXList, d, RFXListInput{})
	found := false
	for _, s := range rl.Items {
		if s.ID == r {
			found = true
			if len(s.TargetNames) != 1 || s.TargetNames[0] != "Ada Example" {
				t.Errorf("target_names = %v", s.TargetNames)
			}
		}
	}
	if !found {
		t.Fatal("rfx not listed")
	}
	w := must(t, RFXWaiting, d, RFXWaitingInput{})
	for _, it := range w.Items {
		if it.ID == r && (len(it.TargetNames) != 1 || it.TargetNames[0] != "Ada Example") {
			t.Errorf("waiting target_names = %v", it.TargetNames)
		}
	}
}

func TestSummaryOfOpItem(t *testing.T) {
	f := newPropFix(t)
	person := must(t, OrgPersonAdd, f.owner, OrgAddInput{Name: "Priya Raman", Org: "acme.example"})
	must(t, OrgPersonContactAdd, f.owner, PersonContactInput{ID: person.ID, Method: "email", Value: "priya@acme.example"})
	id := f.file(
		ItemInput{Kind: model.ItemOp, Op: "task_add", Input: map[string]any{"project": f.proj, "title": "Order extra node pool", "assignee": person.ID, "due": "2026-11-03"}, Evidence: ev},
		ItemInput{Kind: model.ItemOp, Op: "task_start", Input: map[string]any{"id": "x"}, Evidence: ev},
		ItemInput{Kind: model.ItemOp, Op: "task_depends_add", Input: map[string]any{"id": "x"}, Evidence: ev},
		mailItem("Hi."),
		ItemInput{Kind: model.ItemExternal, Medium: model.MediumEmail, Target: "marcus.oyelaran@brightwater.example", Draft: "Hi.", Evidence: ev},
	)
	got := must(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	want := []string{
		"Add task: Order extra node pool, for Priya Raman, due Nov 3, in Portal",
		"Start: x",
		"Task depends add",
		"Email to ada@acme.example",
		"Email to Marcus Oyelaran",
	}
	for i, w := range want {
		if got.Items[i].Summary != w {
			t.Errorf("item %d summary %q, want %q", i+1, got.Items[i].Summary, w)
		}
	}
	for _, it := range got.Items {
		if strings.ContainsAny(it.Summary, "{}") {
			t.Errorf("raw JSON in %q", it.Summary)
		}
	}
	if got.Proposal.SourceName != "Sync notes" {
		t.Errorf("source_name = %q", got.Proposal.SourceName)
	}
}

func TestReadableSnippet(t *testing.T) {
	name := func(id string) string {
		if id == "person-priya-raman" {
			return "Priya Raman"
		}
		return ""
	}
	cases := []struct {
		name, passage, snippet string
		marks                  []index.Span
	}{
		{"plain", "the <<heron>> flew", "the heron flew", []index.Span{{Start: 4, End: 9}}},
		{"two words", "<<a>> and <<b>>", "a and b", []index.Span{{Start: 0, End: 1}, {Start: 6, End: 7}}},
		{"link becomes a title", "ask [[person-priya-raman]] about <<nodes>>", "ask Priya Raman about nodes", []index.Span{{Start: 22, End: 27}}},
		{"match inside a link covers the title", "ask [[person-<<priya>>-raman]] today", "ask Priya Raman today", []index.Span{{Start: 4, End: 15}}},
		{"unknown id stays an id", "see [[note-x]]", "see note-x", nil},
		{"alias wins", "see [[note-x|the note]]", "see the note", nil},
		{"anchor is dropped", "see [[policy-x#^R1]]", "see policy-x", nil},
		{"cut link loses its brackets", "[[person-pri ... <<node>>", "person-pri ... node", nil},
		{"no markers", "just words", "just words", nil},
		{"unicode offsets are runes", "café <<bar>>", "café bar", []index.Span{{Start: 5, End: 8}}},
	}
	for _, c := range cases {
		h := index.Hit{Passage: c.passage}
		readable(&h, name)
		if h.Snippet != c.snippet {
			t.Errorf("%s: snippet %q, want %q", c.name, h.Snippet, c.snippet)
		}
		if strings.Contains(h.Snippet, "<<") || strings.Contains(h.Snippet, ">>") {
			t.Errorf("%s: markers left in %q", c.name, h.Snippet)
		}
		if c.marks != nil && !equalSpans(h.Marks, c.marks) {
			t.Errorf("%s: marks %v, want %v", c.name, h.Marks, c.marks)
		}
	}
}

func equalSpans(a, b []index.Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSearchJournalHitsRankLastAndShowADate(t *testing.T) {
	hits := []index.Hit{
		{ID: "2026-11-01", Kind: "journal", Passage: "- 09:12 task_add <<heron>>"},
		{ID: "note-a", Kind: "note", Passage: "a <<heron>> note"},
		{ID: "proposal-x", Kind: "proposal", Passage: "project: project-1m6 <<heron>>"},
	}
	readableHits(hits, nil)
	if hits[0].ID != "note-a" || hits[1].ID != "proposal-x" || hits[2].ID != "2026-11-01" {
		t.Fatalf("order %s, %s, %s", hits[0].ID, hits[1].ID, hits[2].ID)
	}
	if strings.Contains(hits[1].Snippet, "project-1m6") {
		t.Errorf("raw proposal text in %q", hits[1].Snippet)
	}
	if got := hits[2].Snippet; got != "Activity in the journal of 2026-11-01" {
		t.Errorf("journal snippet %q", got)
	}
}

func TestSearchRanksWorkFirst(t *testing.T) {
	hits := []index.Hit{
		{ID: "2026-11-01", Kind: "journal", Passage: "- 09:12 task_add <<heron>>"},
		{ID: "domain-a", Kind: "domain", Title: "Heron domain", Passage: "<<heron>>"},
		{ID: "note-a", Kind: "note", Title: "Heron note", Passage: "<<heron>>"},
		{ID: "task-a", Kind: "task", Title: "Heron task", Passage: "<<heron>>"},
		{ID: "rfx-a", Kind: "rfx", Title: "Heron ask", Passage: "<<heron>>"},
	}
	readableHits(hits, nil)
	var got []string
	for _, h := range hits {
		got = append(got, h.ID)
	}
	want := []string{"task-a", "rfx-a", "note-a", "2026-11-01", "domain-a"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v", got)
	}
}

func TestSearchPrefixMatchesTheStartOfTheLastWord(t *testing.T) {
	ctx := t.Context()
	d := newDeps(t)
	if _, err := NoteAdd.Run(ctx, d, NoteAddInput{Title: "Replication rehearsal", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	exact, err := Search.Run(ctx, d, SearchInput{Query: "repl"})
	if err != nil || len(exact.Hits) != 0 {
		t.Fatalf("exact repl = %v, %v", exact.Hits, err)
	}
	live, err := Search.Run(ctx, d, SearchInput{Query: "repl", Prefix: true})
	if err != nil || len(live.Hits) != 1 {
		t.Fatalf("prefix repl = %v, %v", live.Hits, err)
	}
	// An invalid or empty query is an error or no hits, never a panic.
	if _, err := Search.Run(ctx, d, SearchInput{Query: `"`, Prefix: true}); err != nil {
		t.Logf("quote: %v", err)
	}
}
