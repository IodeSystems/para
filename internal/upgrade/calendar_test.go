package upgrade

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// The calendar-uids step reads a meeting's single calendar_uid as one entry
// of calendar_uids, role primary. Undo puts the old field back.
func TestCalendarUIDsStep(t *testing.T) {
	f := newFixture(t)
	s := f.open("one")
	ctx := context.Background()
	if _, err := s.Create(ctx, model.Document{ID: "task-sync", Folder: model.FolderProjects, Title: "Sync",
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Kind: model.KindMeeting,
			With: []model.Link{model.LinkTo("person-ada")}, CalendarUID: "sync@acme.example"}}); err != nil {
		t.Fatal(err)
	}
	before := raw(t, f, "projects/task-sync.md")
	res, err := Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	batch := ""
	for _, so := range res.Steps {
		if so.ID == "calendar-uids" {
			batch = so.Batch
			if so.Count != 1 {
				t.Fatalf("calendar-uids = %+v", so)
			}
		}
	}
	d, err := s.Get(ctx, "task-sync")
	if err != nil {
		t.Fatal(err)
	}
	tk := d.Meta.(*model.Task)
	if tk.CalendarUID != "" || len(tk.CalendarUIDs) != 1 || tk.CalendarUIDs[0] != (model.CalendarEvent{UID: "sync@acme.example", Role: model.RolePrimary}) {
		t.Fatalf("task = %+v", tk)
	}
	if _, err := Undo(ctx, s, batch); err != nil {
		t.Fatal(err)
	}
	if got := raw(t, f, "projects/task-sync.md"); got != before {
		t.Fatalf("undo left:\n%s", got)
	}
}
