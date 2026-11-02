package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/ops"
)

// streamPParityCases are the parity rows of stream P: inbox triage, RFX reply
// and draft, person aliases. Mutating ops use one document for Invoke (a) and
// one for the CLI (b). Ids come from seedStreamPParity, which runs after the
// other seeds (person-seed, project-a, domain-seed exist). The second result
// is the stdin of each CLI run.
func streamPParityCases() (map[string]parityCase, map[string]string) {
	return map[string]parityCase{
		"inbox_list": {`{}`, []string{"inbox", "list"}, true},
		"inbox_show": {`{"id":"inbox-p-show"}`, []string{"inbox", "show", "inbox-p-show"}, true},
		"inbox_triage": {`{"id":"inbox-p-a","to":"task","project":"project-a"}`,
			[]string{"inbox", "triage", "inbox-p-b", "--to", "task", "--project", "project-a"}, false},
	}, map[string]string{}
}

// seedStreamPParity creates the fixed-id documents the rows above refer to.
func seedStreamPParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []string{"inbox-p-show", "inbox-p-a", "inbox-p-b"} {
		if _, err := deps.Store.Create(ctx, model.Document{
			ID: id, Folder: model.FolderInbox, Title: "Capture " + id,
			Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}},
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

func TestInboxTriageCommand(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var proj ops.ProjectAddOutput
	e.json(&proj, "project", "add", "Portal", "--owner", "Jordan Example")
	var add ops.InboxAddOutput
	e.json(&add, "inbox", "add", "write the parser")
	if out := e.ok("inbox", "list"); !strings.Contains(out, add.ID) {
		t.Fatalf("list:\n%s", out)
	}
	if _, _, err := e.run("inbox", "triage", add.ID, "--to", "task"); err == nil || !strings.Contains(err.Error(), "needs project") {
		t.Fatalf("err = %v", err)
	}
	var out ops.InboxTriageOutput
	e.json(&out, "inbox", "triage", add.ID, "--to", "task", "--project", proj.ID)
	if out.Folder != model.FolderArchive || !strings.HasPrefix(out.Created, "task-") {
		t.Fatalf("out = %+v", out)
	}
	if list := e.ok("inbox", "list"); strings.Contains(list, add.ID) {
		t.Errorf("triaged item still listed:\n%s", list)
	}
	if all := e.ok("inbox", "list", "--all"); !strings.Contains(all, add.ID) {
		t.Errorf("--all lacks the item:\n%s", all)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Errorf("lint = %+v", lint)
	}
	if ev := e.ok("events", "tail", "-n", "50"); strings.Contains(ev, "write the parser") {
		t.Errorf("content in events:\n%s", ev)
	}
}
