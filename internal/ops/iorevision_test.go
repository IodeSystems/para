package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

func revFiles(t *testing.T, f *agentFix, id string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(f.root, "io", id+".r*.md"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestIORevisionsOwnerAndAgentEdits(t *testing.T) {
	f := ioFix(t)
	id := outDraft(t, f, model.ExpectsNone, "")
	first := mustRun(t, IOShow, f.owner, IOShowInput{ID: id}).Doc.Body
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: id, Text: "Hi Ada, the quote please."})
	mustRun(t, IOEdit, f.agent, IOEditInput{ID: id, Text: "Hi Ada, the quote, thanks."})
	// An effort change keeps no revision.
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: id, Effort: model.EffortS})
	if got := revFiles(t, f, id); len(got) != 2 {
		t.Fatalf("version files = %v", got)
	}
	revs := mustRun(t, IORevisions, f.owner, IORevisionsInput{ID: id})
	if revs.Current != 3 || len(revs.Revisions) != 2 || revs.Revisions[0].N != 1 || revs.Revisions[1].N != 2 ||
		revs.Revisions[0].By != "user" || revs.Revisions[1].By != "claude" || revs.Revisions[0].Hash == "" || revs.Revisions[0].At == "" {
		t.Fatalf("revisions = %+v", revs)
	}
	r1 := mustRun(t, IORevisions, f.owner, IORevisionsInput{ID: id, N: 1})
	if !strings.Contains(r1.Text, first) || !strings.Contains(r1.Text, "type: io") {
		t.Fatalf("r1 = %q", r1.Text)
	}
	raw, err := os.ReadFile(filepath.Join(f.root, "io", id+".r2.md"))
	if err != nil || !strings.Contains(string(raw), "the quote please") {
		t.Fatalf("r2 file = %q %v", raw, err)
	}
	if _, err := IORevisions.Run(t.Context(), f.owner, IORevisionsInput{ID: id, N: 9}); err == nil {
		t.Fatal("revision 9 read")
	}

	// Version files are not documents: not listed, not linted, no external edit.
	for _, it := range mustRun(t, IOList, f.owner, IOListInput{}).Items {
		if strings.Contains(it.ID, ".r") {
			t.Fatalf("listed %s", it.ID)
		}
	}
	refs, err := f.owner.Store.List(t.Context(), store.Filter{All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		if strings.Contains(r.ID, ".r") {
			t.Fatalf("store listed %s", r.ID)
		}
	}
	mustLintClean(t, f.owner)
	if res, err := f.owner.Store.Reconcile(t.Context()); err != nil || len(res.Changes) != 0 {
		t.Fatalf("reconcile = %+v %v", res, err)
	}

	d := mustRun(t, IODiff, f.owner, IODiffInput{ID: id, From: 1, To: 2})
	if d.Same || d.Added == 0 || d.Removed == 0 || !strings.Contains(d.Text, "+ Hi Ada, the quote please.") {
		t.Fatalf("diff = %+v", d)
	}
	// To 0 is the current text; from 0 is the one before.
	if d := mustRun(t, IODiff, f.owner, IODiffInput{ID: id}); d.From != 2 || d.To != 3 || !strings.Contains(d.Text, "+ Hi Ada, the quote, thanks.") {
		t.Fatalf("current diff = %+v", d)
	}
	if _, err := IODiff.Run(t.Context(), f.owner, IODiffInput{ID: id, From: 3, To: 1}); err == nil {
		t.Fatal("backward diff")
	}
}

func TestIORevisionsApprovalResetAndIncoming(t *testing.T) {
	f := ioFix(t)
	id := outDraft(t, f, model.ExpectsNone, "")
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: id})
	if o := mustRun(t, IOEdit, f.owner, IOEditInput{ID: id, Text: "Changed text."}); o.State != model.IODraft {
		t.Fatalf("state = %s", o.State)
	}
	if _, m, _ := getIO(t.Context(), f.owner, id); m.ApprovedHash != "" || m.Revision != 2 || len(m.Revisions) != 1 {
		t.Fatalf("io = %+v", m)
	}
	// An approval alone keeps no revision.
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: id})
	if got := revFiles(t, f, id); len(got) != 1 {
		t.Fatalf("version files = %v", got)
	}
	in := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Hello."}).ID
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: in, State: model.IORead})
	if got := revFiles(t, f, in); len(got) != 0 {
		t.Fatalf("incoming version files = %v", got)
	}
	if _, err := IORevisions.Run(t.Context(), f.owner, IORevisionsInput{ID: in}); err == nil {
		t.Fatal("incoming has revisions")
	}
}

// Version files follow their draft when it moves or its id changes.
func TestIORevisionsFollowTheDraft(t *testing.T) {
	f := ioFix(t)
	id := outDraft(t, f, model.ExpectsNone, "")
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: id, Text: "Second text."})
	if _, err := f.owner.Store.Move(t.Context(), id, model.FolderArchive); err != nil {
		t.Fatal(err)
	}
	if got, _ := filepath.Glob(filepath.Join(f.root, "archive", id+".r1.md")); len(got) != 1 {
		t.Fatalf("archive version files = %v", got)
	}
	if got := revFiles(t, f, id); len(got) != 0 {
		t.Fatalf("io version files = %v", got)
	}
	if _, err := f.owner.Store.Rename(t.Context(), id, id+"-b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := filepath.Glob(filepath.Join(f.root, "archive", id+"-b.r1.md")); len(got) != 1 {
		t.Fatalf("renamed version files = %v", got)
	}
	if r := mustRun(t, IORevisions, f.owner, IORevisionsInput{ID: id + "-b", N: 1}); !strings.Contains(r.Text, "can you send the quote") {
		t.Fatalf("r1 = %q", r.Text)
	}
	if res, err := f.owner.Store.Reconcile(t.Context()); err != nil || len(res.Changes) != 0 {
		t.Fatalf("reconcile = %+v %v", res, err)
	}
}
