package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// directoryParityCases are the directory, target, interaction, and who rows of
// the parity table. Mutating ops use one document for Invoke and one for the
// CLI (suffix a and b). Ids come from seedDirectoryParity.
var directoryParityCases = map[string]parityCase{
	"org_group_add":  {`{"name":"Parity Inv","org":"acme.example","kind":"team"}`, []string{"org", "group", "add", "Parity Cli", "--org", "acme.example", "--kind", "team"}, false},
	"org_group_list": {`{}`, []string{"org", "group", "list"}, true},
	"org_group_show": {`{"id":"group-seed"}`, []string{"org", "group", "show", "group-seed"}, true},
	"org_person_contact_add": {`{"id":"person-seed-a","method":"slack","value":"@a","preferred":true}`,
		[]string{"org", "person", "contact", "add", "person-seed-b", "slack", "@b", "--preferred"}, false},
	"org_person_contact_remove": {`{"id":"person-seed-a","method":"email","value":"seed-a@acme.example"}`,
		[]string{"org", "person", "contact", "remove", "person-seed-b", "email", "seed-b@acme.example"}, false},
	"org_person_group_add": {`{"id":"person-seed-a","group":"group-seed"}`,
		[]string{"org", "person", "group", "add", "person-seed-b", "group-seed"}, false},
	"org_person_group_remove": {`{"id":"person-seed","group":"group-seed"}`,
		[]string{"org", "person", "group", "remove", "person-seed-b", "group-seed"}, false},
	"org_person_edit": {`{"id":"person-seed-a","role":"Night lead","org":"acme.example"}`,
		[]string{"org", "person", "edit", "person-seed-b", "--role", "Night lead", "--org", "acme.example"}, false},
	"org_person_note": {`{"id":"person-seed-a","text":"Prefers email.","date":"2026-11-01"}`,
		[]string{"org", "person", "note", "person-seed-b", "Prefers", "email.", "--date", "2026-11-01"}, false},
	"rfx_target_add": {`{"id":"rfx-tgt-a","to":"person-seed","medium":"email"}`,
		[]string{"rfx", "target", "add", "rfx-tgt-b", "person-seed", "email"}, false},
	"rfx_target_state": {`{"id":"rfx-tgt-a","to":"person-seed","medium":"slack","state":"sent","date":"2026-11-01"}`,
		[]string{"rfx", "target", "state", "rfx-tgt-b", "person-seed", "sent", "--via", "slack", "--date", "2026-11-01"}, false},
	"interaction_add": {`{"summary":"synthetic","with":["person-seed"],"medium":"slack","direction":"in"}`,
		[]string{"interaction", "add", "synthetic", "--with", "person-seed", "--medium", "slack", "--direction", "in"}, false},
	"interaction_list": {`{"with":"person-seed"}`, []string{"interaction", "list", "--with", "person-seed"}, true},
	"interaction_show": {`{"id":"interaction-seed"}`, []string{"interaction", "show", "interaction-seed"}, true},
	"who":              {`{"query":"person-seed"}`, []string{"who", "person-seed"}, true},
}

// seedDirectoryParity runs after seedRFXParity (person-seed exists).
func seedDirectoryParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := ops.OrgGroupAdd.Run(ctx, deps, ops.GroupAddInput{Name: "Seed", Org: "acme.example", Kind: model.GroupChannel})
	must(err)
	for _, name := range []string{"Seed A", "Seed B"} {
		id := "person-" + strings.ToLower(strings.ReplaceAll(name, " ", "-"))
		_, err := ops.OrgPersonAdd.Run(ctx, deps, ops.OrgAddInput{Name: name, Org: "acme.example"})
		must(err)
		_, err = ops.OrgPersonContactAdd.Run(ctx, deps, ops.PersonContactInput{ID: id, Method: model.MediumEmail, Value: strings.ToLower(strings.ReplaceAll(name, " ", "-")) + "@acme.example"})
		must(err)
	}
	for _, id := range []string{"person-seed", "person-seed-b"} {
		_, err := ops.OrgPersonGroupAdd.Run(ctx, deps, ops.PersonGroupInput{ID: id, Group: "group-seed"})
		must(err)
	}
	for _, id := range []string{"rfx-tgt-a", "rfx-tgt-b"} {
		_, err := deps.Store.Create(ctx, model.Document{
			ID: id, Folder: model.FolderRFX, Title: "Target ask",
			Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXOut, Status: model.RFXOpen,
				Route: "domain-seed", SelfServeCheck: "searched",
				Targets: []model.Target{{To: model.LinkTo("person-seed"), Medium: model.MediumSlack, State: model.TargetDrafted}}},
		})
		must(err)
	}
	_, err = deps.Store.Create(ctx, model.Document{
		ID: "interaction-seed", Folder: model.FolderInteractions, Title: "Seed interaction",
		Meta: &model.Interaction{Common: model.Common{Type: model.TypeInteraction}, Date: "2026-11-01", With: []model.Link{model.LinkTo("person-seed")},
			Medium: model.MediumSlack, Direction: model.InteractionIn, Visibility: model.VisibilityPrivate},
	})
	must(err)
}

func TestDirectoryCommands(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.ok("org", "role", "add", "Platform Lead", "--org", "acme.example")
	e.ok("org", "group", "add", "Atlas", "--org", "acme.example", "--kind", "team")
	e.ok("org", "group", "add", "Ops Chat", "--org", "acme.example", "--kind", "channel")
	e.ok("org", "person", "add", "Ada Example", "--org", "acme.example", "--role", "role-platform-lead", "--alias", "Ada E")
	e.ok("org", "person", "add", "Adam Example", "--org", "acme.example")
	e.ok("org", "person", "contact", "add", "person-ada-example", "slack", "@ada", "--preferred", "--label", "day to day")
	e.ok("org", "person", "contact", "add", "person-ada-example", "email", "ada@acme.example")
	e.ok("org", "person", "group", "add", "person-ada-example", "group-atlas")
	e.ok("org", "person", "note", "person-ada-example", "Prefers", "Slack", "for", "quick", "questions.", "--date", "2026-11-01")

	if _, _, err := e.run("rfx", "add", "ask", "--route", "domain-x", "--to", "person-ada-example"); err == nil || !strings.Contains(err.Error(), "--via") {
		t.Errorf("--to without --via: err = %v", err)
	}
	var rfx ops.RFXAddOutput
	e.json(&rfx, "rfx", "add", "need access", "--route", "domain-x", "--self-serve", "read",
		"--to", "person-ada-example", "--via", "slack", "--to", "group-atlas", "--via", "email")
	e.ok("rfx", "target", "state", rfx.ID, "person-ada-example", "sent", "--date", "2026-11-01", "--ref", "https://acme.example/t/1")
	e.ok("interaction", "add", "Agreed to review Friday", "--with", "person-ada-example", "--medium", "slack", "--direction", "in", "--date", "2026-11-01")

	var who ops.WhoOutput
	e.json(&who, "who", "SLACK-less", "nobody")
	if who.Person != nil || len(who.Candidates) != 0 {
		t.Errorf("no match: %+v", who)
	}
	e.json(&who, "who", "ada e")
	p := who.Person
	if p == nil || p.ID != "person-ada-example" || len(p.Groups) != 1 || len(p.Notes) != 1 || len(p.Interactions) != 1 || len(p.OpenRFX) != 1 {
		t.Fatalf("who = %+v", who)
	}
	if !p.Contacts[0].Preferred || p.Contacts[0].Value != "@ada" || p.OpenRFX[0].Targets[0].Value != "@ada" || p.OpenRFX[0].Targets[0].State != model.TargetSent {
		t.Errorf("contacts/targets = %+v / %+v", p.Contacts, p.OpenRFX)
	}
	e.json(&who, "who", "ADA@ACME.EXAMPLE")
	if who.Person == nil || who.Person.ID != "person-ada-example" {
		t.Errorf("by contact value: %+v", who)
	}
	e.json(&who, "who", "example")
	if who.Person != nil || len(who.Candidates) != 2 {
		t.Errorf("ambiguous: %+v", who)
	}
	text := e.ok("who", "ada e")
	for _, want := range []string{"contact\t*slack\t@ada", "note\t- 2026-11-01: Prefers Slack", "group\tgroup-atlas", "target\tperson-ada-example\tslack\tsent"} {
		if !strings.Contains(text, want) {
			t.Errorf("who text lacks %q:\n%s", want, text)
		}
	}
	if text := e.ok("who", "example"); !strings.Contains(text, "candidate\tperson-adam-example") || strings.Contains(text, "contact\t") {
		t.Errorf("ambiguous text:\n%s", text)
	}

	if out := e.ok("route", "need", "access"); !strings.Contains(out, "\ttarget\tperson-ada-example\tslack\tsent\t@ada") {
		t.Errorf("route text lacks targets:\n%s", out)
	}
	var wait ops.RFXWaitingOutput
	e.json(&wait, "rfx", "waiting", "--today", "2026-11-01")
	if len(wait.Items) != 1 || !wait.Items[0].Waiting || len(wait.Items[0].Targets) != 2 {
		t.Errorf("waiting = %+v", wait.Items)
	}
	if out := e.ok("org", "group", "show", "group-atlas"); !strings.Contains(out, "member\tperson-ada-example") {
		t.Errorf("group show:\n%s", out)
	}
	var list ops.InteractionListOutput
	e.json(&list, "interaction", "list", "--with", "person-adam-example")
	if len(list.Items) != 0 {
		t.Errorf("filter: %+v", list.Items)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Fatalf("lint = %+v", lint)
	}
}
