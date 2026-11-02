package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		"meeting_hold":     {`{"id":"meeting-v-e","notes":"Agreed to ship in January.","actions":[{"title":"Send the plan","assignee":"person-v-seed","project":"project-v-seed"}]}`, []string{"meeting", "hold", "meeting-v-f", "--notes", "Agreed to ship in January.", "--action", "Send the plan | person-v-seed", "--project", "project-v-seed"}, false},
		"agenda":           {`{"days":14}`, []string{"agenda", "--days", "14"}, false},
		"calendar_export":  {`{"days":30}`, []string{"calendar", "export", "--days", "30"}, false},
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
		{ID: "project-v-seed", Folder: model.FolderProjects, Title: "V project",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Vera Example"}},
	}
	for _, id := range []string{"meeting-v-show", "meeting-v-a", "meeting-v-b", "meeting-v-c", "meeting-v-d", "meeting-v-e", "meeting-v-f"} {
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

func TestMeetingHoldCLI(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var person ops.OrgAddOutput
	e.json(&person, "org", "person", "add", "Vera Example", "--org", "acme.example")
	var project ops.ProjectAddOutput
	e.json(&project, "project", "add", "Portal", "--owner", "Jordan Example")
	var q ops.QuestionAddOutput
	e.json(&q, "question", "add", "Which window?", "--project", project.ID)
	var m ops.MeetingAddOutput
	e.json(&m, "meeting", "add", "Sync", "--with", person.ID, "--scope", project.ID,
		"--start", "2026-11-04T10:00:00-05:00", "--end", "2026-11-04T10:30:00-05:00")
	if out := e.ok("meeting", "show", m.ID); !strings.Contains(out, q.ID) {
		t.Fatalf("the open question in scope is not on the agenda: %q", out)
	}
	var h ops.MeetingHoldOutput
	e.json(&h, "meeting", "hold", m.ID, "--notes", "Window is December.",
		"--answer", q.ID+"=December", "--action", "Send the plan | "+person.ID+" | 2026-11-10", "--project", project.ID)
	if h.Status != model.MeetingHeld || len(h.Answered) != 1 || len(h.Tasks) != 1 {
		t.Fatalf("hold = %+v", h)
	}
	var task ops.TaskShowOutput
	e.json(&task, "task", "show", h.Tasks[0])
	if task.Task.Assignee != person.ID || string(task.Task.Due) != "2026-11-10" {
		t.Fatalf("task = %+v", task.Task)
	}
	e.ok("lint")

	// The whole input as JSON on stdin; mixing it with flags is refused.
	var m2 ops.MeetingAddOutput
	e.json(&m2, "meeting", "add", "Second", "--with", person.ID, "--start", "2026-11-05T10:00:00Z", "--end", "2026-11-05T10:30:00Z")
	e.stdin = `{"notes":"Done.","actions":[{"title":"Follow up","project":"` + project.ID + `"}]}`
	var h2 ops.MeetingHoldOutput
	e.json(&h2, "meeting", "hold", m2.ID, "--stdin-json")
	if len(h2.Tasks) != 1 {
		t.Fatalf("stdin hold = %+v", h2)
	}
	var m3 ops.MeetingAddOutput
	e.json(&m3, "meeting", "add", "Third", "--with", person.ID, "--start", "2026-11-06T10:00:00Z", "--end", "2026-11-06T10:30:00Z")
	e.stdin = `{"notes":"x"}`
	if _, _, err := e.run("meeting", "hold", m3.ID, "--stdin-json", "--notes", "y"); err == nil || !strings.Contains(err.Error(), "leave out") {
		t.Fatalf("err = %v", err)
	}
}

func TestAgendaAndCalendarCLI(t *testing.T) {
	e := newEnv(t)
	vaultPath := e.init("acme")
	var person ops.OrgAddOutput
	e.json(&person, "org", "person", "add", "Vera Example", "--org", "acme.example")
	var project ops.ProjectAddOutput
	e.json(&project, "project", "add", "Portal", "--owner", "Jordan Example")
	at := time.Now().Add(3 * time.Hour).Truncate(time.Minute)
	e.ok("meeting", "add", "Launch", "sync", "--with", person.ID, "--start", at.Format(time.RFC3339), "--end", at.Add(30*time.Minute).Format(time.RFC3339))
	e.ok("task", "add", "File the form", "-p", project.ID, "--due", time.Now().Format("2006-01-02"))

	out := e.ok("agenda", "--days", "3", "--zone", "UTC")
	for _, want := range []string{"agenda ", "(UTC)", "Launch sync", "with Vera Example", "File the form", "due"} {
		if !strings.Contains(out, want) {
			t.Errorf("agenda lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, at.UTC().Format("15:04")+"-"+at.Add(30*time.Minute).UTC().Format("15:04")) {
		t.Errorf("agenda shows no UTC hours %s:\n%s", at.UTC().Format("15:04"), out)
	}
	if _, _, err := e.run("agenda", "--zone", "Nowhere/Land"); err == nil {
		t.Error("a bad zone must fail")
	}

	ics := e.ok("calendar", "export", "--days", "3")
	if !strings.HasPrefix(ics, "BEGIN:VCALENDAR\r\n") || !strings.Contains(ics, "SUMMARY:Launch sync") || !strings.Contains(ics, "SUMMARY:Due: File the form") {
		t.Errorf("ics = %q", ics)
	}
	file := filepath.Join(e.dir, "agenda.ics")
	e.ok("calendar", "export", "--to", "file:"+file)
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
	if _, _, err := e.run("calendar", "export", "--to", "file:"+file, "--json"); err == nil {
		t.Error("--to with --json must fail")
	}
	// An export is an egress event; with an allowlist that lacks the transport it is refused.
	var tail ops.EventsTailOutput
	e.json(&tail, "events", "tail", "-n", "50")
	n := 0
	for _, ev := range tail.Events {
		if ev.Op == "egress" && ev.Doc == "calendar" {
			n++
		}
	}
	if n < 2 {
		t.Errorf("egress events for the calendar = %d, want at least 2", n)
	}
	props := filepath.Join(vaultPath, ".para", "vault.properties")
	cur, err := os.ReadFile(props)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(props, append(cur, []byte("egress.export=stdout\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(e.dir, "other.ics")
	if _, _, err := e.run("calendar", "export", "--to", "file:"+other); err == nil || !strings.Contains(err.Error(), "egress.export") {
		t.Errorf("err = %v, want a refusal that names egress.export", err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Error("a refused export wrote a file")
	}
	e.ok("calendar", "export")
}
