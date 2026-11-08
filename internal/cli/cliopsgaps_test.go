package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// TestIOFileTitleDueAndLinkOnly: io file --to task takes --title and --due;
// --existing links an obligation or an area, which io_file never makes.
func TestIOFileTitleDueAndLinkOnly(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Acme LLC")
	var in ops.IOAddOutput
	e.json(&in, "io", "add", "--direction", "in", "Please send the signed form by Friday")
	var out ops.IOFileOutput
	e.json(&out, "io", "file", in.ID, "--to", "task", "--area", area.ID, "--title", "Send the signed form", "--due", "2999-01-08")
	var sh ops.TaskShowOutput
	e.json(&sh, "task", "show", out.Became)
	if sh.Task.Title != "Send the signed form" || sh.Task.Due != "2999-01-08" {
		t.Fatalf("task = %+v", sh.Task)
	}

	var o ops.ObligationAddOutput
	e.json(&o, "obligation", "add", "Send the form", "--kind", "commitment", "--due", "2999-01-01", "--area", area.ID)
	var in2, in3 ops.IOAddOutput
	e.json(&in2, "io", "add", "--direction", "in", "Reminder about the form")
	e.json(&in3, "io", "add", "--direction", "in", "Note about the area")
	e.json(&out, "io", "file", in2.ID, "--existing", o.ID)
	if out.To != ops.TriageObligation || out.Became != o.ID || out.Created {
		t.Fatalf("obligation link = %+v", out)
	}
	e.json(&out, "io", "file", in3.ID, "--existing", area.ID, "--to", "area")
	if out.To != ops.TriageArea || out.Became != area.ID {
		t.Fatalf("area link = %+v", out)
	}
	var in4 ops.IOAddOutput
	e.json(&in4, "io", "add", "--direction", "in", "Another")
	if _, _, err := e.run("io", "file", in4.ID, "--to", "obligation"); err == nil || !strings.Contains(err.Error(), "give existing") {
		t.Fatalf("to obligation without existing: err = %v", err)
	}
}

// TestOrgPersonEmailAndEdit: org person add takes --email; org person edit
// sets a role (a role document or free text), a team, and an org.
func TestOrgPersonEmailAndEdit(t *testing.T) {
	e := newEnv(t)
	root := e.init("acme")
	var p ops.OrgAddOutput
	e.json(&p, "org", "person", "add", "Riley Example", "--email", "riley@acme.example")
	var role ops.OrgAddOutput
	e.json(&role, "org", "role", "add", "Auditor", "--org", "acme")

	var out ops.OrgPersonEditOutput
	e.json(&out, "org", "person", "edit", p.ID, "--role", "Night shift lead", "--org", "acme")
	if out.Role != "" || out.RoleText != "Night shift lead" || out.Org != "acme" || len(out.Notes) != 1 {
		t.Fatalf("free-text role = %+v", out)
	}
	e.json(&out, "org", "person", "edit", p.ID, "--role", role.ID)
	if out.Role != role.ID || out.RoleText != "" || out.Org != "acme" {
		t.Fatalf("role doc = %+v", out)
	}
	e.json(&out, "org", "person", "edit", p.ID, "--clear-role")
	if out.Role != "" || out.RoleText != "" {
		t.Fatalf("cleared = %+v", out)
	}
	if _, _, err := e.run("org", "person", "edit", p.ID); err == nil || !strings.Contains(err.Error(), "nothing to edit") {
		t.Fatalf("empty edit: err = %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, string(model.FolderOrg), p.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "riley@acme.example") {
		t.Fatalf("person file has no email:\n%s", b)
	}
	if _, _, err := e.run("org", "team", "add", "Platform", "--org", "acme", "--email", "x@acme.example"); err == nil {
		t.Fatal("team add took --email")
	}
}

// TestDocEditAppendAndSection: doc edit appends a dated entry, or replaces
// one section, without the whole body.
func TestDocEditAppendAndSection(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Acme LLC", "--body", "Intro line.\n\n## Status\n\nOld status.\n\n## Log\n\n- 2026-01-01: started")
	body := func() string {
		var sh ops.AreaShowOutput
		e.json(&sh, "area", "show", area.ID)
		return sh.Body
	}
	e.ok("doc", "edit", area.ID, "--append", "Closing line.")
	if b := body(); !strings.HasSuffix(b, "Closing line.") || !strings.Contains(b, "Old status.") {
		t.Fatalf("append: %q", b)
	}
	e.ok("doc", "edit", area.ID, "--append", "checked the books", "--dated", "--section", "Log")
	b := body()
	i, j := strings.Index(b, "## Log"), strings.Index(b, ": checked the books")
	if i < 0 || j < i || !strings.Contains(b, "- 2026-01-01: started") {
		t.Fatalf("dated section append: %q", b)
	}
	e.ok("doc", "edit", area.ID, "--section", "Status", "--section-body", "New status.")
	b = body()
	if strings.Contains(b, "Old status.") || !strings.Contains(b, "## Status\n\nNew status.") || !strings.Contains(b, "Intro line.") || !strings.Contains(b, "checked the books") {
		t.Fatalf("section replace: %q", b)
	}
	e.stdin = "From stdin."
	e.ok("doc", "edit", area.ID, "--section", "Status")
	if b = body(); !strings.Contains(b, "## Status\n\nFrom stdin.") {
		t.Fatalf("section from stdin: %q", b)
	}
	if _, _, err := e.run("doc", "edit", area.ID, "--body", "x", "--append", "y"); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("body and append: err = %v", err)
	}
	if _, _, err := e.run("doc", "edit", area.ID, "--dated", "--title", "Acme"); err == nil || !strings.Contains(err.Error(), "dated needs append") {
		t.Fatalf("dated alone: err = %v", err)
	}
}
