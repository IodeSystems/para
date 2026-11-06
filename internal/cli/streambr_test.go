package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamBRParity: a meeting with a person who has an open ask, and an
// attendee kept as text who is not in the directory, for brief.
func seedStreamBRParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{
		{ID: "person-br-sam", Folder: model.FolderOrg, Title: "Sam Example",
			Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme.example"}},
		{ID: "project-br", Folder: model.FolderProjects, Title: "BR project",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}},
		{ID: "rfx-br-ask", Folder: model.FolderRFX, Title: "Can Sam review the plan?",
			Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXOut, Status: model.RFXOpen, Route: "unknown",
				SelfServeCheck: "read the plan", Targets: []model.Target{{To: model.LinkTo("person-br-sam"), Medium: model.MediumEmail, State: model.TargetSent, Sent: "2026-11-02"}}}},
		{ID: "task-br-sync", Folder: model.FolderProjects, Title: "Plan sync",
			Body: "Walk through the plan.\n\n## Questions\n\n- [[rfx-br-ask]] Can Sam review the plan?\n\n## Notes\n",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Kind: model.KindMeeting, Project: model.LinkTo("project-br"),
				Scheduled: "2026-11-10T10:00:00-05:00", End: "2026-11-10T10:30:00-05:00",
				With: []model.Link{model.LinkTo("person-br-sam")}, Attendees: []string{"Pat Nobody <pat@elsewhere.example>"}}},
	}
	for _, doc := range docs {
		if _, err := deps.Store.Create(ctx, doc); err != nil {
			t.Fatalf("seed %s: %v", doc.ID, err)
		}
	}
}

func streamBRParityCases() map[string]parityCase {
	return map[string]parityCase{
		"brief": {`{"id":"task-br-sync"}`, []string{"brief", "task-br-sync"}, true},
	}
}

// The terminal prints the brief's markdown, the same text an agent gets.
func TestBriefCLIPrintsMarkdown(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	l := &loader{env: func(k string) string {
		if k == "PARA_HOME" {
			return e.home
		}
		return ""
	}}
	if err := l.load(); err != nil {
		t.Fatal(err)
	}
	deps, err := l.deps()
	if err != nil {
		t.Fatal(err)
	}
	seedStreamBRParity(t, deps)
	var out ops.BriefOutput
	e.json(&out, "brief", "task-br-sync")
	text := e.ok("brief", "task-br-sync")
	if text != out.Markdown {
		t.Fatalf("text and --json markdown differ:\n%s\n---\n%s", text, out.Markdown)
	}
	for _, want := range []string{"[[rfx-br-ask]]", "you asked them by email: sent, sent 2026-11-02", "Pat Nobody <pat@elsewhere.example> (unresolved)", "Walk through the plan."} {
		if !strings.Contains(text, want) {
			t.Errorf("brief lacks %q:\n%s", want, text)
		}
	}
}
