package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamATParity: tasks to attach to and send about, an attachment to
// read, and queued work to take back.
func seedStreamATParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []string{"task-at-a", "task-at-b", "task-at-c", "task-at-d"} {
		if _, err := deps.Store.Create(ctx, model.Document{ID: id, Folder: model.FolderProjects, Title: "AT " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-a")}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := deps.Store.AttachmentAdd(ctx, "task-at-a", "20261101-090000-at.txt", []byte("Synthetic notes.")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"request-at-a", "request-at-b"} {
		if _, err := deps.Store.Create(ctx, model.Document{ID: id, Folder: model.FolderRequests, Title: "AT " + id, Body: "Synthetic.",
			Meta: &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: model.RequestQueued, Runner: "x", Queued: "2026-11-01T09:00:00Z"}}); err != nil {
			t.Fatal(err)
		}
	}
}

func streamATParityCases(t *testing.T, e *env) map[string]parityCase {
	file := filepath.Join(e.dir, "board photo.txt")
	if err := os.WriteFile(file, []byte("Synthetic board."), 0o600); err != nil {
		t.Fatal(err)
	}
	const att = "attachments/task-at-a/20261101-090000-at.txt"
	return map[string]parityCase{
		"attachment_add":  {`{"id":"task-at-a","name":"board photo.txt","data":"U3ludGhldGljIGJvYXJkLg=="}`, []string{"attachment", "add", "task-at-b", file}, false},
		"attachment_read": {`{"file":"` + att + `"}`, []string{"attachment", "read", att}, true},
		"agent_send":      {`{"id":"task-at-a","text":"Met Sam.","attachments":["` + att + `"],"done":true}`, []string{"agent", "send", "task-at-b", "Met", "Sam.", "--attach", att, "--done"}, false},
		"item_close":      {`{"id":"task-at-c"}`, []string{"item", "close", "task-at-d"}, false},
	}
}

func TestAttachmentCLI(t *testing.T) {
	e := newEnv(t)
	root := e.init("acme")
	file := filepath.Join(e.dir, "evil name.md")
	if err := os.WriteFile(file, []byte("# Synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var project, task struct {
		ID string `json:"id"`
	}
	e.json(&project, "project", "add", "--done-when", "Done.", "Board", "--owner", "Jordan Example")
	e.json(&task, "task", "add", "Photograph the board", "--project", project.ID)
	id := task.ID
	out := e.ok("attachment", "add", id, file)
	path := strings.Fields(out)[0]
	if !strings.HasPrefix(path, "attachments/"+id+"/") || !strings.HasSuffix(path, "-evil-name.md") {
		t.Fatalf("path = %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatal(err)
	}
	if got := e.ok("attachment", "read", path); !strings.Contains(got, "# Synthetic") {
		t.Fatalf("read = %q", got)
	}
	if got := e.ok("lint"); strings.Contains(got, "attachments") {
		t.Fatalf("lint names the attachment: %q", got)
	}
	if _, _, err := e.run("attachment", "read", "attachments/"+id+"/../../.para/vault.properties"); err == nil {
		t.Fatal("a path out of attachments/ was read")
	}
}
