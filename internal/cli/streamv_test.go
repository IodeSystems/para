package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// streamVParityCases are the parity rows of stream V: meetings, agenda. A
// mutating op uses one document for Invoke (a) and one for the CLI (b). Ids
// come from seedStreamVParity.
func streamVParityCases() map[string]parityCase {
	return map[string]parityCase{
		"meeting_add":      {`{"title":"Launch sync","with":["person-v-seed"],"start":"2026-11-04T10:00:00-05:00","end":"2026-11-04T10:30:00-05:00"}`, []string{"meeting", "add", "Launch", "sync", "--with", "person-v-seed", "--start", "2026-11-04T10:00:00-05:00", "--end", "2026-11-04T10:30:00-05:00"}, false},
		"meeting_list":     {`{"all":true}`, []string{"meeting", "list", "--all"}, false},
		"meeting_show":     {`{"id":"meeting-v-show"}`, []string{"meeting", "show", "meeting-v-show"}, true},
		"meeting_schedule": {`{"id":"meeting-v-a","start":"2026-11-05T09:00:00+01:00","end":"2026-11-05T09:30:00+01:00"}`, []string{"meeting", "schedule", "meeting-v-b", "--start", "2026-11-05T09:00:00+01:00", "--end", "2026-11-05T09:30:00+01:00"}, false},
		"meeting_cancel":   {`{"id":"meeting-v-c","reason":"Not needed."}`, []string{"meeting", "cancel", "meeting-v-d", "--reason", "Not needed."}, false},
	}
}

// seedStreamVParity creates the fixed-id documents the rows above refer to.
func seedStreamVParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{
		{ID: "person-v-seed", Folder: model.FolderOrg, Title: "Vera Example",
			Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme.example"}},
	}
	for _, id := range []string{"meeting-v-show", "meeting-v-a", "meeting-v-b", "meeting-v-c", "meeting-v-d"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderMeetings, Title: "V meeting " + id,
			Body: "## Questions\n\nNone yet.\n\n## Notes\n",
			Meta: &model.Meeting{Common: model.Common{Type: model.TypeMeeting}, Status: model.MeetingScheduled,
				Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00", With: []model.Link{model.LinkTo("person-v-seed")}}})
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}

func TestMeetingCLI(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var person ops.OrgAddOutput
	e.json(&person, "org", "person", "add", "Vera Example", "--org", "acme.example")
	var ask ops.MeetingAddOutput
	e.json(&ask, "meeting", "add", "Launch", "sync", "--with", person.ID)
	if ask.Status != model.MeetingRequested {
		t.Fatalf("add = %+v", ask)
	}
	if out := e.ok("meeting", "list"); !strings.Contains(out, ask.ID) || !strings.Contains(out, "requested") || !strings.Contains(out, "Vera Example") {
		t.Fatalf("list = %q", out)
	}
	if out := e.ok("meeting", "schedule", ask.ID, "--start", "2026-11-04T23:30:00-05:00", "--end", "2026-11-05T00:30:00-05:00"); !strings.Contains(out, "scheduled") {
		t.Fatalf("schedule = %q", out)
	}
	if out := e.ok("meeting", "show", ask.ID); !strings.Contains(out, "## Questions") || !strings.Contains(out, "## Notes") {
		t.Fatalf("show = %q", out)
	}
	e.ok("meeting", "cancel", ask.ID, "--reason", "moved")
	if out := e.ok("meeting", "list"); strings.Contains(out, ask.ID) {
		t.Fatalf("a cancelled meeting is listed by default: %q", out)
	}
	if out := e.ok("meeting", "list", "--all"); !strings.Contains(out, "cancelled") {
		t.Fatalf("list --all = %q", out)
	}
	e.ok("lint")
}
