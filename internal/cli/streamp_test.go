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
		"rfx_reply": {`{"id":"rfx-p-a","from":"person-seed","outcome":"no","text":"Not this quarter.","why":"Audit freeze","url":"https://chat.acme.example/t/1"}`,
			[]string{"rfx", "reply", "rfx-p-b", "--from", "person-seed", "--outcome", "no", "--why", "Audit freeze", "--url", "https://chat.acme.example/t/2"}, false},
	}, map[string]string{"rfx_reply": "Not this quarter."}
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
	for _, id := range []string{"rfx-p-a", "rfx-p-b"} {
		if _, err := deps.Store.Create(ctx, model.Document{
			ID: id, Folder: model.FolderRFX, Title: "Stream P ask",
			Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXOut, Status: model.RFXOpen,
				Route: "domain-seed", SelfServeCheck: "searched",
				Targets: []model.Target{{To: model.LinkTo("person-seed"), Medium: model.MediumSlack, State: model.TargetSent, Sent: "2026-11-01"}}},
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

func TestRFXReplyCommand(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.ok("org", "role", "add", "Lead", "--org", "acme.example")
	e.ok("org", "domain", "add", "Logging", "--owner", "role-lead")
	var bo ops.OrgAddOutput
	e.json(&bo, "org", "person", "add", "Bo Example", "--org", "acme.example")
	e.ok("org", "person", "contact", "add", bo.ID, "email", "bo@acme.example")
	var rfx ops.RFXAddOutput
	e.json(&rfx, "rfx", "add", "Can we launch X?", "--route", "domain-logging", "--self-serve", "read policies", "--to", bo.ID, "--via", "email")
	e.ok("rfx", "target", "state", rfx.ID, bo.ID, "sent")

	// A no needs its reason.
	e.stdin = "Not this quarter."
	if _, _, err := e.run("rfx", "reply", rfx.ID, "--from", bo.ID, "--outcome", "no", "--url", "https://chat.acme.example/t/1"); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("err = %v", err)
	}
	e.stdin = "Not this quarter.\nAudit freeze."
	var out ops.RFXReplyOutput
	e.json(&out, "rfx", "reply", rfx.ID, "--from", bo.ID, "--outcome", "no", "--why", "Audit freeze", "--url", "https://chat.acme.example/t/1", "--review-by", "2027-01-15")
	if out.Constraint == "" || out.Interaction == "" || out.State != model.TargetReplied {
		t.Fatalf("out = %+v", out)
	}
	var con ops.ConstraintShowOutput
	e.json(&con, "constraint", "show", out.Constraint)
	if con.Why != "Audit freeze" || con.By != "Bo Example" || con.SourceURL != "https://chat.acme.example/t/1" {
		t.Fatalf("constraint = %+v", con.ConstraintView)
	}
	var ia ops.InteractionShowOutput
	e.json(&ia, "interaction", "show", out.Interaction)
	if !strings.Contains(ia.Doc.Body, "Audit freeze.") {
		t.Errorf("stdin text lost: %q", ia.Doc.Body)
	}
	var show ops.RFXShowOutput
	e.json(&show, "rfx", "show", rfx.ID)
	if m := show.Doc.Meta.(*model.RFX); m.Targets[0].State != model.TargetReplied {
		t.Errorf("target = %+v", m.Targets[0])
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Errorf("lint = %+v", lint)
	}
	if ev := e.ok("events", "tail", "-n", "50"); strings.Contains(ev, "Audit freeze") || strings.Contains(ev, "Not this quarter") {
		t.Errorf("content in events:\n%s", ev)
	}
}
