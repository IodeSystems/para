package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// rfxParityCases are the rfx, org, and route rows of the parity table. Names
// differ between input and args so the add rows do not collide. Ids come from
// seedRFXParity.
var rfxParityCases = map[string]parityCase{
	"org_person_add":  {`{"name":"Parity Inv","org":"acme.example"}`, []string{"org", "person", "add", "Parity Cli", "--org", "acme.example"}, false},
	"org_team_add":    {`{"name":"Parity Inv","org":"acme.example"}`, []string{"org", "team", "add", "Parity Cli", "--org", "acme.example"}, false},
	"org_role_add":    {`{"name":"Parity Inv","org":"acme.example"}`, []string{"org", "role", "add", "Parity Cli", "--org", "acme.example"}, false},
	"org_domain_add":  {`{"name":"Parity Inv","owner":"role-seed"}`, []string{"org", "domain", "add", "Parity Cli", "--owner", "role-seed"}, false},
	"org_person_list": {`{}`, []string{"org", "person", "list"}, true},
	"org_team_list":   {`{}`, []string{"org", "team", "list"}, true},
	"org_role_list":   {`{}`, []string{"org", "role", "list"}, true},
	"org_domain_list": {`{}`, []string{"org", "domain", "list"}, true},
	"org_person_show": {`{"id":"person-seed"}`, []string{"org", "person", "show", "person-seed"}, true},
	"org_team_show":   {`{"id":"team-seed"}`, []string{"org", "team", "show", "team-seed"}, true},
	"org_role_show":   {`{"id":"role-seed"}`, []string{"org", "role", "show", "role-seed"}, true},
	"org_domain_show": {`{"id":"domain-seed"}`, []string{"org", "domain", "show", "domain-seed"}, true},
	"rfx_add": {`{"direction":"out","ask":"synthetic","route":"domain-seed"}`,
		[]string{"rfx", "add", "synthetic", "--direction", "out", "--route", "domain-seed"}, false},
	"rfx_close": {`{"id":"rfx-seed-in","playbook":"role-seed"}`,
		[]string{"rfx", "close", "rfx-seed-in", "--playbook", "role-seed"}, false},
	"rfx_list":    {`{}`, []string{"rfx", "list"}, true},
	"rfx_show":    {`{"id":"rfx-seed"}`, []string{"rfx", "show", "rfx-seed"}, true},
	"rfx_status":  {`{"id":"rfx-seed","status":"answered"}`, []string{"rfx", "status", "rfx-seed", "answered"}, false},
	"rfx_waiting": {`{"today":"2026-11-01"}`, []string{"rfx", "waiting", "--today", "2026-11-01"}, true},
	"route":       {`{"query":"seed"}`, []string{"route", "seed"}, true},
}

// seedRFXParity creates the fixed-id documents the rows above refer to.
func seedRFXParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, in := range []struct {
		op   func() error
		name string
	}{
		{func() error {
			_, err := ops.OrgRoleAdd.Run(ctx, deps, ops.OrgAddInput{Name: "Seed", Org: "acme.example"})
			return err
		}, "role"},
		{func() error {
			_, err := ops.OrgTeamAdd.Run(ctx, deps, ops.OrgAddInput{Name: "Seed", Org: "acme.example"})
			return err
		}, "team"},
		{func() error {
			_, err := ops.OrgPersonAdd.Run(ctx, deps, ops.OrgAddInput{Name: "Seed", Org: "acme.example", Role: "role-seed"})
			return err
		}, "person"},
		{func() error {
			_, err := ops.OrgDomainAdd.Run(ctx, deps, ops.OrgAddInput{Name: "Seed", Owner: "role-seed"})
			return err
		}, "domain"},
	} {
		if err := in.op(); err != nil {
			t.Fatalf("seed %s: %v", in.name, err)
		}
	}
	for id, dir := range map[string]model.RFXDirection{"rfx-seed": model.RFXOut, "rfx-seed-in": model.RFXIn} {
		_, err := deps.Store.Create(ctx, model.Document{
			ID: id, Folder: model.FolderRFX, Title: "Seed ask",
			Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: dir, Status: model.RFXOpen,
				Route: "domain-seed", SelfServeCheck: "searched", NextNudge: "2026-11-05"},
		})
		if err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

func TestRFXCommands(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.ok("org", "role", "add", "Platform Lead", "--org", "acme.example")
	e.ok("org", "person", "add", "Ada Example", "--org", "acme.example", "--role", "role-platform-lead",
		"--said", "approvals take two days", "--source", "chat, 2026-10-30")
	e.ok("org", "domain", "add", "Logging", "--owner", "role-platform-lead")

	var noCheck ops.RFXAddOutput
	e.json(&noCheck, "rfx", "add", "need log access", "--route", "domain-logging", "--to", "person-ada-example", "--via", "slack")
	if len(noCheck.Warnings) != 1 || noCheck.Warnings[0] != ops.WarnNoSelfServe {
		t.Fatalf("warnings = %v", noCheck.Warnings)
	}
	var checked ops.RFXAddOutput
	e.json(&checked, "rfx", "add", "second ask", "--route", "domain-logging", "--self-serve", "read policy-logging",
		"--depends-on", noCheck.ID, "--next-nudge", "2026-11-09")
	if len(checked.Warnings) != 0 {
		t.Fatalf("warnings = %v", checked.Warnings)
	}
	if out, _, _ := e.run("rfx", "add", "text mode ask", "--route", "domain-logging"); !strings.Contains(out, "warning:") {
		t.Errorf("text mode hides the warning: %q", out)
	}

	var in ops.RFXAddOutput
	e.json(&in, "rfx", "add", "please approve", "--direction", "in", "--route", "domain-logging")
	if _, _, err := e.run("rfx", "close", in.ID); err == nil || !strings.Contains(err.Error(), "playbook") {
		t.Fatalf("err = %v", err)
	}
	e.ok("rfx", "close", in.ID, "--playbook", "role-platform-lead")

	var wait ops.RFXWaitingOutput
	e.json(&wait, "rfx", "waiting", "--today", "2026-11-01")
	if len(wait.Items) != 2 {
		t.Fatalf("a drafted target is not waiting: %+v", wait.Items)
	}
	e.ok("rfx", "target", "state", noCheck.ID, "person-ada-example", "sent")
	e.json(&wait, "rfx", "waiting", "--today", "2026-11-01")
	if len(wait.Items) != 3 || wait.Items[0].NextNudge != "2026-11-09" {
		t.Fatalf("waiting = %+v", wait.Items)
	}

	var route ops.RouteOutput
	e.json(&route, "route", "who", "owns", "logging?")
	if route.Domain == nil || route.Domain.ID != "domain-logging" || len(route.People) != 1 || len(route.SelfServe.PriorRFX) != 4 {
		t.Fatalf("route = %+v", route)
	}
	text := e.ok("route", "logging")
	if strings.Index(text, "# Check first") > strings.Index(text, "# Then ask") || !strings.Contains(text, "person\tperson-ada-example") {
		t.Fatalf("text:\n%s", text)
	}

	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Fatalf("lint = %+v", lint)
	}
	raw := e.ok("rfx", "list", "--json")
	if !json.Valid([]byte(raw)) || strings.Contains(raw, "approvals take two days") {
		t.Fatalf("raw = %s", raw)
	}
	if ev := e.ok("events", "tail", "-n", "50"); strings.Contains(ev, "need log access") || strings.Contains(ev, "approvals") {
		t.Fatalf("content in events:\n%s", ev)
	}
}
