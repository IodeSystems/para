package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// streamPParityCases are the parity rows of stream P: io, RFX reply
// and draft, person aliases. Mutating ops use one document for Invoke (a) and
// one for the CLI (b). Ids come from seedStreamPParity, which runs after the
// other seeds (person-seed, project-a, domain-seed exist). The second result
// is the stdin of each CLI run.
func streamPParityCases() (map[string]parityCase, map[string]string) {
	return map[string]parityCase{
		"io_list":      {`{}`, []string{"io", "list"}, true},
		"io_show":      {`{"id":"io-p-show"}`, []string{"io", "show", "io-p-show"}, true},
		"io_revisions": {`{"id":"io-p-out-dl"}`, []string{"io", "revisions", "io-p-out-dl"}, true},
		"io_diff":      {`{"id":"io-p-out-dl"}`, []string{"io", "diff", "io-p-out-dl"}, true},
		"io_made":      {`{"id":"io-p-show"}`, []string{"io", "made", "io-p-show"}, true},
		"io_proposals": {`{}`, []string{"io", "proposals"}, true},
		"io_sent":      {`{}`, []string{"io", "sent"}, true},
		"io_file": {`{"id":"io-p-a","to":"task","project":"project-a"}`,
			[]string{"io", "file", "io-p-b", "--to", "task", "--project", "project-a"}, false},
		"io_edit": {`{"id":"io-p-c","state":"read"}`, []string{"io", "edit", "io-p-d", "--state", "read"}, false},
		"io_drop": {`{"id":"io-p-e","reason":"Spam."}`, []string{"io", "drop", "io-p-f", "--reason", "Spam."}, false},
		"io_send": {`{"id":"io-p-out-a","url":"https://mail.acme.example/1"}`, []string{"io", "send", "io-p-out-b", "--url", "https://mail.acme.example/2"}, false},
		"io_attempt": {`{"id":"io-p-out-c","result":"failed","error":"Refused."}`,
			[]string{"io", "attempt", "io-p-out-d", "--result", "failed", "--error", "Refused."}, false},
		"io_review":      {`{"ids":["io-p-rv"]}`, []string{"io", "review", "io-p-rv"}, false},
		"delegation_run": {`{"id":"io-p-out-dl","agent":"worker-q"}`, []string{"delegation", "run", "io-p-out-dl", "--agent", "worker-q"}, true},
		"rfx_reply": {`{"id":"rfx-p-a","from":"person-seed","outcome":"no","text":"Not this quarter.","why":"Audit freeze","url":"https://chat.acme.example/t/1"}`,
			[]string{"rfx", "reply", "rfx-p-b", "--from", "person-seed", "--outcome", "no", "--why", "Audit freeze", "--url", "https://chat.acme.example/t/2"}, false},
		"rfx_draft": {`{"id":"rfx-p-a","text":"Hi, can we launch X?"}`, []string{"rfx", "draft", "rfx-p-b"}, false},
		"org_person_alias_add": {`{"id":"person-seed-a","alias":"Seedy A"}`,
			[]string{"org", "person", "alias", "add", "person-seed-b", "Seedy", "B"}, false},
		"org_person_alias_remove": {`{"id":"person-p-alias-a","alias":"Old Name"}`,
			[]string{"org", "person", "alias", "remove", "person-p-alias-b", "old", "name"}, false},
	}, map[string]string{"rfx_reply": "Not this quarter.", "rfx_draft": "Hi, can we launch X?"}
}

// seedStreamPParity creates the fixed-id documents the rows above refer to.
func seedStreamPParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []string{"io-p-show", "io-p-rv", "io-p-a", "io-p-b", "io-p-c", "io-p-d", "io-p-e", "io-p-f"} {
		if _, err := deps.Store.Create(ctx, model.Document{
			ID: id, Folder: model.FolderIO, Title: "Capture " + id, Body: "Capture " + id,
			Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOIn, Medium: model.MediumOther, State: model.IOUnread},
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	for _, id := range []string{"io-p-out-a", "io-p-out-b", "io-p-out-c", "io-p-out-d", "io-p-out-dl"} {
		if _, err := deps.Store.Create(ctx, model.Document{
			ID: id, Folder: model.FolderIO, Title: "Draft " + id, Body: "Draft " + id,
			Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOOut, Medium: model.MediumEmail, State: model.IODraft,
				To: []string{"[[person-seed]]"}, Expects: model.ExpectsNone},
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	for _, id := range []string{"person-p-alias-a", "person-p-alias-b"} {
		if _, err := deps.Store.Create(ctx, model.Document{
			ID: id, Folder: model.FolderOrg, Title: "Alias " + id,
			Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme.example", Aliases: []string{"Old Name"}},
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

func TestIOFileCommand(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var proj ops.ProjectAddOutput
	e.json(&proj, "project", "add", "--done-when", "Done.", "Portal", "--owner", "Jordan Example")
	var add ops.IOAddOutput
	e.json(&add, "inbox", "add", "write the parser") // the old name, kept for one release
	if out := e.ok("io", "inbox"); !strings.Contains(out, add.ID) || !strings.HasPrefix(add.ID, "io-") {
		t.Fatalf("list:\n%s", out)
	}
	if _, _, err := e.run("io", "file", add.ID, "--to", "task"); err == nil || !strings.Contains(err.Error(), "needs project") {
		t.Fatalf("err = %v", err)
	}
	var out ops.IOFileOutput
	e.json(&out, "io", "file", add.ID, "--to", "task", "--project", proj.ID, "--done-when", "Parsed.")
	if out.State != model.IOFiled || !strings.HasPrefix(out.Became, "task-") {
		t.Fatalf("out = %+v", out)
	}
	if list := e.ok("io", "inbox"); strings.Contains(list, add.ID) {
		t.Errorf("filed item still listed:\n%s", list)
	}
	if all := e.ok("io", "list", "--all"); !strings.Contains(all, add.ID) {
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
	e.initAt("acme", 8)
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

func TestRFXDraftCommand(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.ok("org", "role", "add", "Lead", "--org", "acme.example")
	e.ok("org", "domain", "add", "Logging", "--owner", "role-lead")
	var rfx ops.RFXAddOutput
	e.json(&rfx, "rfx", "add", "Can we launch X?", "--route", "domain-logging", "--self-serve", "read policies")

	e.stdin = "Hi Bo,\n\nCan we launch X?"
	e.ok("rfx", "draft", rfx.ID)
	var show ops.RFXShowOutput
	e.json(&show, "rfx", "show", rfx.ID)
	if show.Draft != "Hi Bo,\n\nCan we launch X?" {
		t.Fatalf("draft = %q", show.Draft)
	}
	e.ok("rfx", "draft", rfx.ID, "--text", "Second draft")
	if out := e.ok("rfx", "show", rfx.ID); !strings.Contains(out, "draft\nSecond draft") || strings.Contains(out, "Hi Bo") {
		t.Errorf("text:\n%s", out)
	}
	if _, _, err := e.run("rfx", "draft", rfx.ID); err == nil || !strings.Contains(err.Error(), "text is empty") {
		t.Errorf("err = %v", err)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Errorf("lint = %+v", lint)
	}
}

func TestPersonAliasCommand(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var bo ops.OrgAddOutput
	e.json(&bo, "org", "person", "add", "Bo Example", "--org", "acme.example")
	e.ok("org", "person", "alias", "add", bo.ID, "Bobby", "Ex")
	if out := e.ok("who", "bobby ex"); !strings.Contains(out, bo.ID) {
		t.Fatalf("who:\n%s", out)
	}
	if _, _, err := e.run("org", "person", "alias", "add", bo.ID, "BOBBY EX"); err == nil {
		t.Error("duplicate alias accepted")
	}
	e.ok("org", "person", "alias", "remove", bo.ID, "bobby", "ex")
	if out, _, _ := e.run("who", "bobby ex"); strings.Contains(out, bo.ID) {
		t.Errorf("alias still found:\n%s", out)
	}
}
