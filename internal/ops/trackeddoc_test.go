package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

const runbookV1 = "# Portal runbook\n\nStep one: drain the node.\n\nStep two: restart the pods.\n\nStep three: check the dashboard."

func trackFix(t *testing.T) (*agentFix, string) {
	t.Helper()
	f := newAgentFix(t)
	mustRun(t, OrgPersonAdd, f.owner, OrgAddInput{Name: "Ada Example", Org: "acme.example"})
	out := mustRun(t, DocTrack, f.owner, DocTrackInput{URL: "https://wiki.acme.example/runbooks/portal", Text: runbookV1, System: model.SystemConfluence,
		Project: f.proj, Owner: "person-ada-example", Via: &model.IOVia{Source: "confluence", Ref: "12345"}})
	return f, out.ID
}

func TestDocTrackAndVersionAdd(t *testing.T) {
	f, id := trackFix(t)
	show := mustRun(t, DocTrackedShow, f.owner, DocTrackedShowInput{ID: id})
	if show.Doc.Title != "Portal runbook" || show.Doc.Latest != 1 || show.Doc.Unseen != 1 || len(show.Versions) != 1 || show.Versions[0].ID != id+"-v1.source" {
		t.Fatalf("show = %+v", show)
	}
	if !strings.HasPrefix(show.Text, "## Portal runbook") || show.Versions[0].Hash != hashText(show.Text) {
		t.Fatalf("text = %q", show.Text)
	}
	// The same text, with other line endings and trailing blanks, is the same version.
	same := mustRun(t, DocVersionAdd, f.owner, DocVersionAddInput{ID: id, Text: strings.ReplaceAll(runbookV1, "\n", "\r\n") + "  \n"})
	if same.Result != VersionUnchanged || same.Version != 1 {
		t.Fatalf("same = %+v", same)
	}
	changed := strings.Replace(runbookV1, "restart the pods", "restart the pods one at a time", 1)
	add := mustRun(t, DocVersionAdd, f.agent, DocVersionAddInput{ID: id, Text: changed})
	if add.Result != VersionAdded || add.Version != 2 || add.Unseen != 2 || add.Added != 1 || add.Removed != 1 {
		t.Fatalf("add = %+v", add)
	}
	again := mustRun(t, DocVersionAdd, f.agent, DocVersionAddInput{ID: id, Text: changed})
	if again.Result != VersionUnchanged || again.Version != 2 {
		t.Fatalf("again = %+v", again)
	}
	// Events hold hashes, never the text.
	evs, err := f.owner.Store.Events(t.Context(), 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if strings.Contains(e.Hash+e.From+e.To+e.Doc, "restart") {
			t.Fatalf("event holds text: %+v", e)
		}
	}
	if !slices.ContainsFunc(evs, func(e model.Event) bool { return e.Op == "doc_version_add" && e.To == "2" && e.Hash == add.Hash }) {
		t.Fatal("no doc_version_add event with the hash")
	}
	mustLintClean(t, f.owner)
}

func TestDocDiff(t *testing.T) {
	f, id := trackFix(t)
	mustRun(t, DocVersionAdd, f.owner, DocVersionAddInput{ID: id, Text: strings.Replace(runbookV1, "Step two: restart the pods.", "Step two: restart the pods.\nWait a minute.", 1)})
	d := mustRun(t, DocDiff, f.owner, DocDiffInput{ID: id})
	if d.From != 0 || d.To != 2 {
		t.Fatalf("nothing seen: from %d to %d", d.From, d.To)
	}
	d = mustRun(t, DocDiff, f.owner, DocDiffInput{ID: id, From: 1, Context: 1})
	if d.Added != 1 || d.Removed != 0 || !strings.Contains(d.Text, "+ Wait a minute.") || strings.Contains(d.Text, "Portal runbook") {
		t.Fatalf("diff = %+v\n%s", d, d.Text)
	}
	if d.Parts[0].Op != DiffGap {
		t.Fatalf("parts = %+v", d.Parts)
	}
	p := mustRun(t, DocDiff, f.owner, DocDiffInput{ID: id, From: 1, Unit: UnitParagraph})
	if p.Added != 1 || p.Removed != 1 || !strings.Contains(p.Text, "- Step two: restart the pods.\n+ Step two: restart the pods.\n+ Wait a minute.") {
		t.Fatalf("paragraph diff:\n%s", p.Text)
	}
	if _, err := DocDiff.Run(t.Context(), f.owner, DocDiffInput{ID: id, From: 3}); err == nil {
		t.Fatal("from after to")
	}
}

func TestDocSeenIsThePersons(t *testing.T) {
	f, id := trackFix(t)
	if _, err := DocSeen.Run(t.Context(), f.agent, DocSeenInput{ID: id}); err == nil || !strings.Contains(err.Error(), "only a person") {
		t.Fatalf("agent: %v", err)
	}
	s := mustRun(t, DocSeen, f.owner, DocSeenInput{ID: id})
	if s.LastSeen != 1 || s.Unseen != 0 {
		t.Fatalf("seen = %+v", s)
	}
	mustRun(t, DocVersionAdd, f.owner, DocVersionAddInput{ID: id, Text: runbookV1 + "\n\nStep four."})
	d := mustRun(t, DocDiff, f.owner, DocDiffInput{ID: id})
	if d.From != 1 || d.To != 2 || d.Added != 2 {
		t.Fatalf("since seen = %+v", d)
	}
	list := mustRun(t, DocTracked, f.owner, DocTrackedInput{Unseen: true})
	if list.Unseen != 1 || len(list.Docs) != 1 || list.Docs[0].Unseen != 1 {
		t.Fatalf("list = %+v", list)
	}
}

func TestDocCommentAnchors(t *testing.T) {
	f, id := trackFix(t)
	if _, err := DocCommentAdd.Run(t.Context(), f.owner, DocCommentAddInput{ID: id, By: "person-ada-example", Text: "Why?", Quote: "Step nine"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing quote: %v", err)
	}
	c1 := mustRun(t, DocCommentAdd, f.owner, DocCommentAddInput{ID: id, By: "person-ada-example", Text: "Which pods?", Quote: "restart the  pods",
		URL: "https://wiki.acme.example/runbooks/portal?focusedCommentId=1", At: "2026-11-01T09:00:00Z"})
	c2 := mustRun(t, DocCommentAdd, f.owner, DocCommentAddInput{ID: id, By: "Sam Outside", Text: "Link the dashboard.", Quote: "check the dashboard"})
	if c1.Comment != "c1" || c2.Comment != "c2" {
		t.Fatalf("ids = %s %s", c1.Comment, c2.Comment)
	}
	// Version 2 moves c1's quote and drops c2's.
	v2 := "Intro line.\n\n" + strings.Replace(runbookV1, "Step three: check the dashboard.", "Step three: done.", 1)
	add := mustRun(t, DocVersionAdd, f.owner, DocVersionAddInput{ID: id, Text: v2})
	if !slices.Equal(add.AnchorsLost, []string{"c2"}) {
		t.Fatalf("lost = %v", add.AnchorsLost)
	}
	cs := mustRun(t, DocComments, f.owner, DocCommentsInput{ID: id})
	got := cs.Comments[0]
	text := mustRun(t, DocTrackedShow, f.owner, DocTrackedShowInput{ID: id}).Text
	if got.Anchor == nil || got.Anchor.Version != 2 || string([]rune(text)[got.Anchor.Start:got.Anchor.End]) != "restart the pods" {
		t.Fatalf("c1 anchor = %+v", got.Anchor)
	}
	if !cs.Comments[1].Lost || cs.Open != 2 {
		t.Fatalf("c2 = %+v", cs.Comments[1])
	}
	// The quote comes back in version 3: found again.
	mustRun(t, DocVersionAdd, f.owner, DocVersionAddInput{ID: id, Text: runbookV1})
	if cs = mustRun(t, DocComments, f.owner, DocCommentsInput{ID: id}); cs.Comments[1].Lost {
		t.Fatal("c2 still lost")
	}
	// A comment on an older version is placed in the latest.
	old := mustRun(t, DocCommentAdd, f.owner, DocCommentAddInput{ID: id, By: "Sam Outside", Text: "Intro?", Quote: "Intro line.", Version: 2})
	if old.Anchor == nil || !old.Anchor.Lost || old.Anchor.Version != 3 {
		t.Fatalf("old anchor = %+v", old.Anchor)
	}
	mustLintClean(t, f.owner)
}

func TestDocCommentFile(t *testing.T) {
	f, id := trackFix(t)
	for i, q := range []string{"drain the node", "restart the pods", "check the dashboard", "Step one"} {
		mustRun(t, DocCommentAdd, f.owner, DocCommentAddInput{ID: id, By: "person-ada-example", Text: "Comment " + string(rune('A'+i)), Quote: q})
	}
	ask := mustRun(t, DocCommentFile, f.owner, DocCommentFileInput{ID: id, Comment: "c1", To: CommentToAsk})
	q := mustRun(t, DocCommentFile, f.owner, DocCommentFileInput{ID: id, Comment: "c2", To: CommentToQuestion})
	due := time.Now().AddDate(0, 0, 3).Format(dateLayout)
	com := mustRun(t, DocCommentFile, f.owner, DocCommentFileInput{ID: id, Comment: "c3", To: CommentToCommitment, Due: due})
	if ask.Type != model.TypeRFX || q.Type != model.TypeQuestion || com.Type != model.TypeTask {
		t.Fatalf("types = %s %s %s", ask.Type, q.Type, com.Type)
	}
	rfx, err := f.owner.Store.Get(t.Context(), ask.Became)
	if err != nil {
		t.Fatal(err)
	}
	if r := rfx.Meta.(*model.RFX); r.Direction != model.RFXIn || r.Route != "confluence" || r.Project.Target() != f.proj || len(r.Targets) != 1 {
		t.Fatalf("ask = %+v", r)
	}
	if !strings.Contains(rfx.Body, "> drain the node") || !strings.Contains(rfx.Body, "[["+id+"]]") {
		t.Fatalf("ask body = %q", rfx.Body)
	}
	task, _ := f.owner.Store.Get(t.Context(), com.Became)
	if m := task.Meta.(*model.Task); m.EffectiveKind() != model.KindCommitment || string(m.Due) != due || m.Project.Target() != f.proj {
		t.Fatalf("commitment = %+v", m)
	}
	if _, err := DocCommentFile.Run(t.Context(), f.owner, DocCommentFileInput{ID: id, Comment: "c1", To: CommentToAsk}); err == nil || !strings.Contains(err.Error(), "already became") {
		t.Fatalf("twice: %v", err)
	}
	if _, err := DocCommentResolve.Run(t.Context(), f.owner, DocCommentResolveInput{ID: id, Comment: "c4", Resolution: model.CommentDropped}); err == nil {
		t.Fatal("dropped with no note")
	}
	mustRun(t, DocCommentResolve, f.owner, DocCommentResolveInput{ID: id, Comment: "c4", Resolution: model.CommentDropped, Note: "Covered by c1."})
	cs := mustRun(t, DocComments, f.owner, DocCommentsInput{ID: id})
	if cs.Open != 0 || cs.Comments[0].BecameTitle == "" || cs.Comments[3].Note != "Covered by c1." {
		t.Fatalf("comments = %+v", cs)
	}
	mustLintClean(t, f.owner)
}

func TestDocHealth(t *testing.T) {
	f, id := trackFix(t)
	mustRun(t, DocCommentAdd, f.owner, DocCommentAddInput{ID: id, By: "Sam Outside", Text: "Old?", Quote: "Step one", At: "2026-01-01T09:00:00Z"})
	if err := f.owner.Store.SetConfig(t.Context(), "docs.check_days", "3"); err != nil {
		t.Fatal(err)
	}
	later := time.Now().AddDate(0, 0, 10).Format(dateLayout)
	h, err := HealthAt(t.Context(), f.owner.Store, later)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, g := range h.Groups {
		for _, x := range g.Findings {
			if x.ID == id {
				kinds[x.Kind] = true
				if g.Goal != f.proj {
					t.Errorf("%s in group %s", x.Kind, g.Goal)
				}
			}
		}
	}
	for _, k := range []string{HealthDocUnseen, HealthDocStale, HealthDocCommentOpen} {
		if !kinds[k] {
			t.Errorf("no %s: %v", k, kinds)
		}
	}
	mustRun(t, DocTrackedStatus, f.owner, DocTrackedStatusInput{ID: id, Status: model.DocPaused})
	if h, _ = HealthAt(t.Context(), f.owner.Store, later); strings.Contains(strings.Join(docHealthKinds(h), ","), "doc_") {
		t.Fatalf("a paused document has findings: %v", docHealthKinds(h))
	}
}

func docHealthKinds(h HealthOutput) []string {
	var out []string
	for _, g := range h.Groups {
		for _, x := range g.Findings {
			out = append(out, x.Kind)
		}
	}
	return out
}

func TestLocateQuote(t *testing.T) {
	text := "alpha beta\ngamma  beta delta beta"
	if s, e, ok := locateQuote(text, "beta", 27); !ok || s != 29 || e != 33 {
		t.Fatalf("nearest = %d %d %v", s, e, ok)
	}
	if s, e, ok := locateQuote(text, "beta gamma", 0); !ok || string([]rune(text)[s:e]) != "beta\ngamma" {
		t.Fatalf("folded = %d %d %v", s, e, ok)
	}
	if _, _, ok := locateQuote("café au lait", "au lait", 0); !ok {
		t.Fatal("runes")
	}
	if s, _, _ := locateQuote("café au lait", "au", 0); s != 5 {
		t.Fatalf("code points: %d", s)
	}
	if _, _, ok := locateQuote(text, "omega", 0); ok {
		t.Fatal("found omega")
	}
}
