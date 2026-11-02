package tui

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/docs"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/registry"
	"github.com/iodesystems/parasolve/internal/store"
	"github.com/iodesystems/parasolve/internal/store/local"
	"github.com/iodesystems/parasolve/internal/vault"
)

// testVault is a synthetic vault with a TUI env and an agent store on it.
type testVault struct {
	t     *testing.T
	env   Env
	agent ops.Deps
	ids   map[string]string
}

func newTestVault(t *testing.T) *testVault {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "acme")
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "acme", tpl, vault.InitOptions{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	in := registry.Install{Home: home}
	if err := in.Add("acme", root); err != nil {
		t.Fatal(err)
	}
	env, err := OpenEnv(in, registry.Vault{ID: "acme", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	env.Getenv = func(k string) string {
		if k == "PARA_TUI_POLL" {
			return "20ms"
		}
		return ""
	}
	install, _ := in.ID()
	as, err := local.Open(root, vault.Options{ID: "acme", Actor: "claude-agent", Surface: "mcp", Install: install, StateDir: filepath.Join(home, "vaults", "acme-agent")})
	if err != nil {
		t.Fatal(err)
	}
	return &testVault{t: t, env: env, agent: ops.Deps{Store: as}, ids: map[string]string{}}
}

const policyBody = `Logging rules for services. See [the wiki](https://wiki.acme.example/logging).

| Service | Owner | Log level | Retention | Notes |
|---|---|---|---|---|
| portal.acme.example | [[person-ada-example]] | info | 90 days | Authentication failures are logged with a request id |
| billing.acme.example | [[person-ada-example]] | warn | 365 days | Payment events are retained for audit |

` + "```" + `bash
curl -s https://wiki.acme.example/logging | head
` + "```" + `
`

func (v *testVault) seed() {
	v.t.Helper()
	ctx := context.Background()
	d := v.env.Deps
	must := func(err error) {
		v.t.Helper()
		if err != nil {
			v.t.Fatal(err)
		}
	}
	_, err := ops.OrgRoleAdd.Run(ctx, d, ops.OrgAddInput{Name: "Lead", Org: "acme.example"})
	must(err)
	_, err = ops.OrgDomainAdd.Run(ctx, d, ops.OrgAddInput{Name: "Logging", Owner: "role-lead"})
	must(err)
	_, err = ops.OrgPersonAdd.Run(ctx, d, ops.OrgAddInput{Name: "Ada Example", Org: "acme.example", Aliases: []string{"Ada E"}})
	must(err)
	_, err = ops.OrgPersonContactAdd.Run(ctx, d, ops.PersonContactInput{ID: "person-ada-example", Method: model.MediumEmail, Value: "ada@acme.example", Preferred: true})
	must(err)
	_, err = ops.OrgPersonAdd.Run(ctx, d, ops.OrgAddInput{Name: "Adam Example", Org: "acme.example"})
	must(err)
	pr, err := ops.ProjectAdd.Run(ctx, d, ops.ProjectAddInput{Title: "Portal launch", Owner: "Jordan Example", Body: "See [[person-ada-example]] and https://wiki.acme.example/portal for the plan."})
	must(err)
	v.ids["project"] = pr.ID
	t1, err := ops.TaskAdd.Run(ctx, d, ops.TaskAddInput{Project: pr.ID, Title: "Write the parser"})
	must(err)
	t2, err := ops.TaskAdd.Run(ctx, d, ops.TaskAddInput{Project: pr.ID, Title: "Ship the linter", DependsOn: []string{t1.ID}})
	must(err)
	v.ids["t1"], v.ids["t2"] = t1.ID, t2.ID
	rf, err := ops.RFXAdd.Run(ctx, d, ops.RFXAddInput{Direction: model.RFXOut, Ask: "Can we launch the portal?", Route: "domain-logging",
		SelfServe: "read the policies", Targets: []ops.TargetInput{{To: "person-ada-example", Medium: model.MediumEmail}}, NextNudge: "2026-10-01"})
	must(err)
	v.ids["rfx"] = rf.ID
	_, err = ops.RFXTargetState.Run(ctx, d, ops.RFXTargetStateInput{ID: rf.ID, To: "person-ada-example", State: model.TargetSent, Date: "2026-09-20"})
	must(err)
	_, err = ops.InboxAdd.Run(ctx, d, ops.InboxAddInput{Text: "Look at the retention question"})
	must(err)
	ing, err := ops.Ingest.Run(ctx, d, ops.IngestInput{URL: "https://wiki.acme.example/policies/logging", Title: "Logging policy", Text: "# Logging policy\n\nText.", ID: "policy-logging"})
	must(err)
	pol, err := d.Store.Get(ctx, ing.ID)
	must(err)
	pol.Body = policyBody
	_, err = d.Store.Update(ctx, pol)
	must(err)
	_, err = ops.RuleAdd.Run(ctx, d, ops.RuleAddInput{Policy: "policy-logging", Text: "Services log every authentication failure with a request id."})
	must(err)
}

// agentWrites makes events by an agent, on the mcp surface.
func (v *testVault) agentWrites() {
	v.t.Helper()
	ctx := context.Background()
	if _, err := ops.InboxAdd.Run(ctx, v.agent, ops.InboxAddInput{Text: "Agent found a stale link"}); err != nil {
		v.t.Fatal(err)
	}
	if _, err := ops.RFXDraft.Run(ctx, v.agent, ops.RFXDraftInput{ID: v.ids["rfx"], Text: "Hi Ada, any news on the portal?"}); err != nil {
		v.t.Fatal(err)
	}
	if _, err := ops.RequestAdd.Run(ctx, v.agent, ops.RequestAddInput{Ask: "Summarize the retention policy"}); err != nil {
		v.t.Fatal(err)
	}
}

// drv drives a Model without a terminal: it runs commands inline and feeds
// their messages back.
type drv struct {
	t *testing.T
	m *Model
}

func newDrv(t *testing.T, v *testVault, w, h int) *drv {
	d := &drv{t: t, m: New(v.env)}
	d.run(d.m.Init())
	d.send(tea.WindowSizeMsg{Width: w, Height: h})
	return d
}

func (d *drv) send(msg tea.Msg) {
	d.t.Helper()
	_, cmd := d.m.Update(msg)
	d.run(cmd)
}

// run executes cmd and feeds results back, except the poll tick, which would
// wait for its timer.
func (d *drv) run(cmd tea.Cmd) {
	d.t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(5 * time.Second):
		return // a long timer command: not needed headless. Disk writes can take seconds.
	}
	switch msg := msg.(type) {
	case nil, pollMsg:
	case tea.BatchMsg:
		for _, c := range msg {
			d.run(c)
		}
	default:
		if _, ok := msg.(tea.QuitMsg); ok {
			return
		}
		d.send(msg)
	}
}

func (d *drv) key(s string) {
	d.t.Helper()
	d.send(keyMsg(s))
}

func (d *drv) keys(ss ...string) {
	for _, s := range ss {
		d.key(s)
	}
}

func (d *drv) typeText(s string) {
	for _, r := range s {
		d.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

// plainFrame is the frame without SGR codes.
func (d *drv) plainFrame() string { return stripSGR(d.m.frame) }

func (d *drv) has(sub string) bool { return strings.Contains(d.plainFrame(), sub) }

// keyFor returns the code of the key whose target is id.
func (d *drv) keyFor(id string) string {
	d.t.Helper()
	for code, t := range d.m.keymap {
		if t == id {
			return code
		}
	}
	d.t.Fatalf("no key for %s in\n%s", id, d.plainFrame())
	return ""
}

func storeFilter(t model.Type) store.Filter { return store.Filter{Type: t} }
