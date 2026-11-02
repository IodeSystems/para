package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/model"
)

func TestInboxTriage(t *testing.T) {
	d := newDeps(t)
	seedOrg(t, d)
	proj := must(t, ProjectAdd, d, ProjectAddInput{Title: "Portal", Owner: "Jordan Example"})
	add := func(text string) string { return must(t, InboxAdd, d, InboxAddInput{Text: text}).ID }

	cases := []struct {
		name    string
		text    string
		in      InboxTriageInput
		wantErr string
		check   func(t *testing.T, created string)
	}{
		{"task", "write parser\nfor the portal", InboxTriageInput{To: TriageTask, Project: proj.ID}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			if err != nil || doc.Title != "write parser" || !strings.Contains(doc.Body, "for the portal") || !strings.Contains(doc.Body, "From [[inbox-") {
				t.Fatalf("task = %+v, %v", doc, err)
			}
		}},
		{"project", "new thing", InboxTriageInput{To: TriageProject, Owner: "Jordan Example"}, "", nil},
		{"rfx", "who owns logs?", InboxTriageInput{To: TriageRFX, Route: "domain-logging", SelfServe: "searched policies",
			Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}}}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			m, _ := doc.Meta.(*model.RFX)
			if err != nil || m == nil || m.Direction != model.RFXOut || len(m.Targets) != 1 || !strings.Contains(doc.Body, "From [[inbox-") {
				t.Fatalf("rfx = %+v, %v", doc, err)
			}
		}},
		{"note", "prefers email for records", InboxTriageInput{To: TriageNote, Person: "person-bo-example", Date: "2026-11-01"}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			if err != nil || id != "person-bo-example" || !strings.Contains(doc.Body, "- 2026-11-01: prefers email for records (From [[inbox-") {
				t.Fatalf("person = %+v, %v", doc, err)
			}
		}},
		{"drop", "never mind", InboxTriageInput{To: TriageDrop}, "", nil},
		{"task needs project", "x", InboxTriageInput{To: TriageTask}, "needs project", nil},
		{"project needs owner", "x", InboxTriageInput{To: TriageProject}, "needs owner", nil},
		{"rfx needs route", "x", InboxTriageInput{To: TriageRFX}, "needs route", nil},
		{"note needs person", "x", InboxTriageInput{To: TriageNote}, "needs person", nil},
		{"stray field", "x", InboxTriageInput{To: TriageDrop, Project: proj.ID}, "does not apply", nil},
		{"bad target", "x", InboxTriageInput{To: "file"}, "want one of", nil},
		{"unknown project", "x", InboxTriageInput{To: TriageTask, Project: "project-none"}, "project-none", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := add(c.text)
			c.in.ID = id
			out, err := InboxTriage.Run(ctx, d, c.in)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				doc, gerr := d.Store.Get(ctx, id)
				if gerr != nil || doc.Folder != model.FolderInbox {
					t.Fatalf("failed triage moved the item: %+v, %v", doc, gerr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			doc, err := d.Store.Get(ctx, id)
			if err != nil || doc.Folder != model.FolderArchive || out.Folder != model.FolderArchive {
				t.Fatalf("inbox item not archived: %+v, %v", doc, err)
			}
			if c.in.To == TriageDrop {
				if out.Created != "" {
					t.Errorf("drop created %s", out.Created)
				}
			} else if !strings.Contains(doc.Body, "[["+out.Created+"]]") {
				t.Errorf("inbox item lacks the forward link: %q", doc.Body)
			}
			if c.check != nil {
				c.check(t, out.Created)
			}
		})
	}

	t.Run("twice", func(t *testing.T) {
		id := add("once only")
		must(t, InboxTriage, d, InboxTriageInput{ID: id, To: TriageDrop})
		if _, err := InboxTriage.Run(ctx, d, InboxTriageInput{ID: id, To: TriageDrop}); err == nil || !strings.Contains(err.Error(), "already triaged") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("not an inbox item", func(t *testing.T) {
		if _, err := InboxTriage.Run(ctx, d, InboxTriageInput{ID: proj.ID, To: TriageDrop}); err == nil {
			t.Fatal("triaged a project")
		}
	})
	t.Run("long capture keeps its text", func(t *testing.T) {
		long := strings.Repeat("word ", 40)
		id := add(long)
		out := must(t, InboxTriage, d, InboxTriageInput{ID: id, To: TriageRFX, Route: "domain-logging"})
		doc, _ := d.Store.Get(ctx, out.Created)
		if !strings.Contains(doc.Body, "word word word") || strings.Count(doc.Title+doc.Body, strings.TrimSpace(long)) < 1 {
			t.Fatalf("rfx = %q / %q", doc.Title, doc.Body)
		}
	})
	t.Run("list and show", func(t *testing.T) {
		before := must(t, InboxList, d, InboxListInput{})
		id := add("listed")
		after := must(t, InboxList, d, InboxListInput{})
		if len(after.Items) != len(before.Items)+1 {
			t.Fatalf("list %d -> %d", len(before.Items), len(after.Items))
		}
		if got := must(t, InboxShow, d, InboxShowInput{ID: id}); got.Doc.Title != "listed" {
			t.Fatalf("show = %+v", got)
		}
		must(t, InboxTriage, d, InboxTriageInput{ID: id, To: TriageDrop})
		if again := must(t, InboxList, d, InboxListInput{}); len(again.Items) != len(before.Items) {
			t.Fatalf("triaged item still listed")
		}
		if all := must(t, InboxList, d, InboxListInput{All: true}); len(all.Items) <= len(before.Items) {
			t.Fatalf("all = %d", len(all.Items))
		}
		if _, err := InboxShow.Run(ctx, d, InboxShowInput{ID: proj.ID}); err == nil {
			t.Fatal("showed a project as inbox")
		}
	})
	mustLintClean(t, d)
}
