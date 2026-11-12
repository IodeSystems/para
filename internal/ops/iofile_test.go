package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestIOFile(t *testing.T) {
	d := newDeps(t)
	seedOrg(t, d)
	proj := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"})
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Home"})
	add := func(text string) string {
		return must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: text, From: "person-bo-example"}).ID
	}

	cases := []struct {
		name    string
		text    string
		in      IOFileInput
		wantErr string
		check   func(t *testing.T, created string)
	}{
		{"task", "write parser\nfor the portal", IOFileInput{To: TriageTask, Goal: proj.ID}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			if err != nil || doc.Title != "write parser" || !strings.Contains(doc.Body, "for the portal") || !strings.Contains(doc.Body, "From [[io-") {
				t.Fatalf("task = %+v, %v", doc, err)
			}
		}},
		{"project", "new thing", IOFileInput{To: TriageGoal, Owner: "Jordan Example", DoneWhen: "Done."}, "", nil},
		{"rfx", "who owns logs?", IOFileInput{To: TriageRFX, Route: "domain-logging", SelfServe: "searched policies",
			Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}}}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			m, _ := doc.Meta.(*model.RFX)
			if err != nil || m == nil || m.Direction != model.RFXIn || len(m.Targets) != 1 || !strings.Contains(doc.Body, "From [[io-") {
				t.Fatalf("rfx = %+v, %v", doc, err)
			}
		}},
		{"note", "prefers email for records", IOFileInput{To: TriageNote, Person: "person-bo-example", Date: "2026-11-01"}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			if err != nil || id != "person-bo-example" || !strings.Contains(doc.Body, "- 2026-11-01: prefers email for records (From [[io-") {
				t.Fatalf("person = %+v, %v", doc, err)
			}
		}},
		{"interaction", "agreed on Friday", IOFileInput{To: TriageInteraction}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			m, _ := doc.Meta.(*model.Interaction)
			if err != nil || m == nil || m.Direction != model.InteractionIn || len(m.With) != 1 || m.With[0] != model.LinkTo("person-bo-example") {
				t.Fatalf("interaction = %+v, %v", doc, err)
			}
		}},
		{"existing", "already a task", IOFileInput{Existing: proj.ID}, "", nil},
		{"existing type mismatch", "x", IOFileInput{To: TriageTask, Existing: proj.ID}, "not a task", nil},
		{"existing takes no fields", "x", IOFileInput{Existing: proj.ID, Area: area.ID}, "leave out", nil},
		{"task needs project or area", "x", IOFileInput{To: TriageTask}, "needs goal or area", nil},
		{"project owner defaults", "x", IOFileInput{To: TriageGoal, DoneWhen: "Done."}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			if p, _ := doc.Meta.(*model.Goal); err != nil || p == nil || p.Owner != "self" {
				t.Fatalf("project = %+v, %v", doc, err)
			}
		}},
		{"task with title and due", "write parser\nfor the portal", IOFileInput{To: TriageTask, Goal: proj.ID, Title: "Parser for the portal", Due: "2026-12-01"}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			if m, _ := doc.Meta.(*model.Task); err != nil || m == nil || doc.Title != "Parser for the portal" || string(m.Due) != "2026-12-01" {
				t.Fatalf("task = %+v, %v", doc, err)
			}
		}},
		{"title applies to a task", "x", IOFileInput{To: TriageNote, Title: "T"}, "does not apply", nil},
		{"task in an area", "water plants", IOFileInput{To: TriageTask, Area: area.ID}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			if m, _ := doc.Meta.(*model.Task); err != nil || m == nil || m.Area != model.LinkTo(area.ID) || m.Goal != "" {
				t.Fatalf("task = %+v, %v", doc, err)
			}
		}},
		{"rfx needs route", "x", IOFileInput{To: TriageRFX}, "needs route", nil},
		{"note document", "keep this recipe", IOFileInput{To: TriageNote, Area: area.ID, Goal: proj.ID}, "", func(t *testing.T, id string) {
			doc, err := d.Store.Get(ctx, id)
			n, _ := doc.Meta.(*model.Note)
			if err != nil || n == nil || doc.Folder != model.FolderResources || n.Area != model.LinkTo(area.ID) ||
				n.Goal != model.LinkTo(proj.ID) || !strings.Contains(doc.Body, "From [[io-") {
				t.Fatalf("note = %+v, %v", doc, err)
			}
		}},
		{"bare note document", "no links", IOFileInput{To: TriageNote}, "", nil},
		{"person note refuses area", "x", IOFileInput{To: TriageNote, Person: "person-bo-example", Area: area.ID}, "area and goal apply", nil},
		{"note date needs person", "x", IOFileInput{To: TriageNote, Date: "2026-11-01"}, "date applies", nil},
		{"note unknown area", "x", IOFileInput{To: TriageNote, Area: "area-none"}, "area-none", nil},
		{"stray field", "x", IOFileInput{To: TriageInteraction, Goal: proj.ID}, "does not apply", nil},
		{"bad target", "x", IOFileInput{To: "file"}, "want one of", nil},
		{"unknown project", "x", IOFileInput{To: TriageTask, Goal: "project-none"}, "project-none", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := add(c.text)
			c.in.ID = id
			out, err := IOFile.Run(ctx, d, c.in)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				if _, m, gerr := getIO(ctx, d, id); gerr != nil || m.State != model.IOUnread {
					t.Fatalf("failed filing changed the item: %+v, %v", m, gerr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			doc, m, err := getIO(ctx, d, id)
			if err != nil || doc.Folder != model.FolderIO || m.State != model.IOFiled || out.State != model.IOFiled {
				t.Fatalf("io item not filed in place: %+v %+v, %v", doc, m, err)
			}
			if m.Became != model.LinkTo(out.Became) || out.Became == "" {
				t.Errorf("became = %q, out %q", m.Became, out.Became)
			}
			if c.check != nil {
				c.check(t, out.Became)
			}
		})
	}

	t.Run("twice", func(t *testing.T) {
		id := add("once only")
		must(t, IODrop, d, IODropInput{ID: id, Reason: "not needed"})
		if _, err := IOFile.Run(ctx, d, IOFileInput{ID: id, To: TriageTask, Area: area.ID}); err == nil || !strings.Contains(err.Error(), "already dropped") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("not an io item", func(t *testing.T) {
		if _, err := IOFile.Run(ctx, d, IOFileInput{ID: proj.ID, To: TriageTask, Area: area.ID}); err == nil {
			t.Fatal("filed a project")
		}
	})
	t.Run("long capture keeps its text", func(t *testing.T) {
		long := strings.Repeat("word ", 40)
		id := add(long)
		out := must(t, IOFile, d, IOFileInput{ID: id, To: TriageRFX, Route: "domain-logging"})
		doc, _ := d.Store.Get(ctx, out.Became)
		if !strings.Contains(doc.Body, "word word word") || strings.Count(doc.Title+doc.Body, strings.TrimSpace(long)) < 1 {
			t.Fatalf("rfx = %q / %q", doc.Title, doc.Body)
		}
	})
	t.Run("list and show", func(t *testing.T) {
		before := must(t, IOList, d, IOListInput{Direction: model.IOIn})
		id := add("listed")
		after := must(t, IOList, d, IOListInput{Direction: model.IOIn})
		if len(after.Items) != len(before.Items)+1 || after.Counts.Unread != before.Counts.Unread+1 {
			t.Fatalf("list %d -> %d", len(before.Items), len(after.Items))
		}
		if got := must(t, IOShow, d, IOShowInput{ID: id}); got.Doc.Title != "listed" || got.Item.State != model.IOUnread {
			t.Fatalf("show = %+v", got)
		}
		must(t, IOEdit, d, IOEditInput{ID: id, State: model.IORead})
		if got := must(t, IOList, d, IOListInput{State: model.IORead}); len(got.Items) != 1 || got.Items[0].ID != id {
			t.Fatalf("read = %+v", got.Items)
		}
		if _, err := IOEdit.Run(ctx, d, IOEditInput{ID: id, Text: "changed"}); err == nil {
			t.Fatal("edited the text of an incoming message")
		}
		if _, err := IOEdit.Run(ctx, d, IOEditInput{ID: id, State: model.IOReady}); err == nil {
			t.Fatal("an incoming message took state ready")
		}
		must(t, IODrop, d, IODropInput{ID: id, Reason: "spam"})
		if again := must(t, IOList, d, IOListInput{Direction: model.IOIn}); len(again.Items) != len(before.Items) {
			t.Fatalf("dropped item still listed")
		}
		if all := must(t, IOList, d, IOListInput{Direction: model.IOIn, All: true}); len(all.Items) <= len(before.Items) {
			t.Fatalf("all = %d", len(all.Items))
		}
		if _, err := IOShow.Run(ctx, d, IOShowInput{ID: proj.ID}); err == nil {
			t.Fatal("showed a project as io")
		}
		if _, err := IODrop.Run(ctx, d, IODropInput{ID: id}); err == nil || !strings.Contains(err.Error(), "reason") {
			t.Fatalf("drop without reason: %v", err)
		}
	})
	mustLintClean(t, d)
}
