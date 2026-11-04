package ops

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

var ctx = context.Background()

// newDeps opens a real vault in a temp dir. Data in tests is synthetic.
func newDeps(t *testing.T) Deps {
	t.Helper()
	root := filepath.Join(t.TempDir(), "v")
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "acme", tpl, vault.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	s, err := local.Open(root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "m1", StateDir: filepath.Join(filepath.Dir(root), "state")})
	if err != nil {
		t.Fatal(err)
	}
	return Deps{Store: s}
}

func mustLintClean(t *testing.T, d Deps) {
	t.Helper()
	got, err := d.Store.Lint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("lint: %v", got)
	}
}

func addRFX(t *testing.T, d Deps, in RFXAddInput) string {
	t.Helper()
	if in.Direction == "" {
		in.Direction = model.RFXOut
	}
	if in.Route == "" {
		in.Route = "domain-logging"
	}
	if in.Ask == "" {
		in.Ask = "synthetic ask"
	}
	out, err := RFXAdd.Run(ctx, d, in)
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func TestRFXAdd(t *testing.T) {
	d := newDeps(t)
	must(t, OrgDomainAdd, d, OrgAddInput{Name: "R", Owner: "self"})
	dep := addRFX(t, d, RFXAddInput{SelfServe: "searched the wiki"})
	cases := []struct {
		name     string
		in       RFXAddInput
		wantErr  string
		wantWarn bool
		check    string // self_serve_check stored
	}{
		{"out with self-serve", RFXAddInput{Direction: "out", Ask: "a", Route: "r", SelfServe: " wiki "}, "", false, "wiki"},
		{"out without self-serve warns", RFXAddInput{Direction: "out", Ask: "a", Route: "r"}, "", true, SelfServeNone},
		{"in without self-serve", RFXAddInput{Direction: "in", Ask: "a", Route: "r"}, "", false, SelfServeInbound},
		{"depends and blocks", RFXAddInput{Direction: "out", Ask: "a", Route: "r", SelfServe: "x", DependsOn: []string{dep}, Blocks: []string{"[[" + dep + "]]"}, NextNudge: "2026-12-01"}, "", false, "x"},
		{"bad direction", RFXAddInput{Direction: "sideways", Ask: "a", Route: "r"}, "direction", false, ""},
		{"no direction", RFXAddInput{Ask: "a", Route: "r"}, "direction", false, ""},
		{"no route", RFXAddInput{Direction: "out", Ask: "a"}, "route", false, ""},
		{"no ask", RFXAddInput{Direction: "out", Route: "r"}, "ask", false, ""},
		{"bad nudge", RFXAddInput{Direction: "out", Ask: "a", Route: "r", NextNudge: "tomorrow"}, "next_nudge", false, ""},
		{"missing dependency", RFXAddInput{Direction: "out", Ask: "a", Route: "r", DependsOn: []string{"rfx-nope"}}, "depends_on", false, ""},
		{"missing project", RFXAddInput{Direction: "out", Ask: "a", Route: "r", Project: "nope"}, "project", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := RFXAdd.Run(ctx, d, c.in)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := len(out.Warnings) > 0; got != c.wantWarn {
				t.Fatalf("warnings = %v", out.Warnings)
			}
			show, err := RFXShow.Run(ctx, d, RFXShowInput{ID: out.ID})
			if err != nil {
				t.Fatal(err)
			}
			m := show.Doc.Meta.(*model.RFX)
			if m.SelfServeCheck != c.check || m.Status != model.RFXOpen {
				t.Fatalf("meta = %+v", m)
			}
		})
	}
	mustLintClean(t, d)
}

func TestRFXChain(t *testing.T) {
	d := newDeps(t)
	a := addRFX(t, d, RFXAddInput{})
	b := addRFX(t, d, RFXAddInput{DependsOn: []string{a}})
	c := addRFX(t, d, RFXAddInput{DependsOn: []string{a}})
	dd := addRFX(t, d, RFXAddInput{DependsOn: []string{b, c}}) // diamond
	e := addRFX(t, d, RFXAddInput{DependsOn: []string{dd}})

	show, err := RFXShow.Run(ctx, d, RFXShowInput{ID: e})
	if err != nil {
		t.Fatal(err)
	}
	depth := map[string]int{}
	for _, n := range show.Chain {
		depth[n.ID] = n.Depth
	}
	if want := map[string]int{dd: 1, b: 2, c: 2, a: 3}; !reflect.DeepEqual(depth, want) || len(show.Chain) != 4 {
		t.Fatalf("chain = %+v", show.Chain)
	}
	up, err := RFXShow.Run(ctx, d, RFXShowInput{ID: a})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(up.DependedOnBy, " "); got != strings.Join(sorted(b, c), " ") {
		t.Fatalf("depended_on_by = %v", up.DependedOnBy)
	}
}

func sorted(s ...string) []string {
	if len(s) == 2 && s[0] > s[1] {
		s[0], s[1] = s[1], s[0]
	}
	return s
}

func TestRFXShowSurvivesCycleAndMissing(t *testing.T) {
	d := newDeps(t)
	a := addRFX(t, d, RFXAddInput{})
	b := addRFX(t, d, RFXAddInput{DependsOn: []string{a}})
	// Hand-edit a cycle and a dangling link; show must terminate.
	doc, _ := d.Store.Get(ctx, a)
	m := doc.Meta.(*model.RFX)
	m.DependsOn = []model.Link{model.LinkTo(b), model.LinkTo("rfx-gone")}
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	show, err := RFXShow.Run(ctx, d, RFXShowInput{ID: a})
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, n := range show.Chain {
		status[n.ID] = n.Status
	}
	if status["rfx-gone"] != "missing" || status[b] != "open" || len(show.Chain) != 2 {
		t.Fatalf("chain = %+v", show.Chain)
	}
}

func TestRFXWaiting(t *testing.T) {
	d := newDeps(t)
	late := addRFX(t, d, RFXAddInput{NextNudge: "2026-11-20"})
	soon := addRFX(t, d, RFXAddInput{NextNudge: "2026-11-02"})
	none := addRFX(t, d, RFXAddInput{})
	inbound := addRFX(t, d, RFXAddInput{Direction: "in", NextNudge: "2026-10-01"})
	gated := addRFX(t, d, RFXAddInput{NextNudge: "2026-11-03", DependsOn: []string{soon}})
	closed := addRFX(t, d, RFXAddInput{NextNudge: "2026-01-01"})
	if _, err := RFXStatus.Run(ctx, d, RFXStatusInput{ID: closed, Status: model.RFXAnswered}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   RFXWaitingInput
		want []string
		due  map[string]bool
	}{
		{"both, by nudge", RFXWaitingInput{Today: "2026-11-02"}, []string{inbound, soon, gated, late, none},
			map[string]bool{inbound: true, soon: true}},
		{"out only", RFXWaitingInput{Today: "2026-11-10", Direction: "out"}, []string{soon, gated, late, none},
			map[string]bool{soon: true, gated: true}},
		{"in only", RFXWaitingInput{Today: "2026-11-02", Direction: "in"}, []string{inbound}, map[string]bool{inbound: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := RFXWaiting.Run(ctx, d, c.in)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, it := range out.Items {
				ids = append(ids, it.ID)
				if it.Due != c.due[it.ID] {
					t.Errorf("%s due = %v", it.ID, it.Due)
				}
				if (it.ID == gated) != reflect.DeepEqual(it.BlockedOn, []string{soon}) {
					t.Errorf("%s blocked_on = %v", it.ID, it.BlockedOn)
				}
			}
			if !reflect.DeepEqual(ids, c.want) {
				t.Fatalf("order = %v\nwant    %v", ids, c.want)
			}
		})
	}
	if _, err := RFXWaiting.Run(ctx, d, RFXWaitingInput{Today: "soon"}); err == nil {
		t.Error("bad today accepted")
	}
}

func TestRFXStatusAndClose(t *testing.T) {
	d := newDeps(t)
	out := addRFX(t, d, RFXAddInput{})
	in := addRFX(t, d, RFXAddInput{Direction: "in"})
	pb, err := OrgRoleAdd.Run(ctx, d, OrgAddInput{Name: "Playbook stand-in", Org: "acme.example"})
	if err != nil {
		t.Fatal(err)
	}

	step := func(name string, run func() (RFXStatusOutput, error), wantErr string, wantStatus model.RFXStatus) {
		t.Run(name, func(t *testing.T) {
			got, err := run()
			if wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || got.Status != wantStatus {
				t.Fatalf("got %+v err %v", got, err)
			}
		})
	}
	status := func(id string, s model.RFXStatus) func() (RFXStatusOutput, error) {
		return func() (RFXStatusOutput, error) { return RFXStatus.Run(ctx, d, RFXStatusInput{ID: id, Status: s}) }
	}
	closeIt := func(id, pb string) func() (RFXStatusOutput, error) {
		return func() (RFXStatusOutput, error) { return RFXClose.Run(ctx, d, RFXCloseInput{ID: id, Playbook: pb}) }
	}
	step("answered", status(out, model.RFXAnswered), "", model.RFXAnswered)
	step("bad status", status(out, "maybe"), "status", "")
	step("closed goes through close", status(out, model.RFXClosed), "rfx close", "")
	step("unknown id", status("rfx-nope", model.RFXOpen), "not found", "")
	step("not an rfx", status(pb.ID, model.RFXOpen), "not rfx", "")
	step("inbound needs playbook", closeIt(in, ""), "playbook", "")
	step("playbook must exist", closeIt(in, "nope"), "playbook", "")
	step("inbound closes with playbook", closeIt(in, pb.ID), "", model.RFXClosed)
	step("close again, same playbook", closeIt(in, pb.ID), "", model.RFXClosed)
	step("close again, no playbook", closeIt(in, ""), "already closed", "")
	step("outbound closes alone", closeIt(out, ""), "", model.RFXClosed)

	show, _ := RFXShow.Run(ctx, d, RFXShowInput{ID: in})
	if got := show.Doc.Meta.(*model.RFX).Playbook.Target(); got != pb.ID {
		t.Errorf("playbook = %q", got)
	}
	mustLintClean(t, d)
}

func TestRFXList(t *testing.T) {
	d := newDeps(t)
	a := addRFX(t, d, RFXAddInput{Route: "domain-logging"})
	b := addRFX(t, d, RFXAddInput{Route: "billing", Direction: "in"})
	if _, err := RFXStatus.Run(ctx, d, RFXStatusInput{ID: a, Status: model.RFXDeclined}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		in   RFXListInput
		want []string
	}{
		{"all", RFXListInput{}, sorted(a, b)},
		{"direction", RFXListInput{Direction: "in"}, []string{b}},
		{"status", RFXListInput{Status: model.RFXDeclined}, []string{a}},
		{"route bare slug", RFXListInput{Route: "logging"}, []string{a}},
		{"route none", RFXListInput{Route: "nope"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := RFXList.Run(ctx, d, c.in)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, s := range out.Items {
				ids = append(ids, s.ID)
			}
			if !reflect.DeepEqual(ids, c.want) {
				t.Fatalf("ids = %v want %v", ids, c.want)
			}
		})
	}
}

func TestRFXShowNotFound(t *testing.T) {
	d := newDeps(t)
	if _, err := RFXShow.Run(ctx, d, RFXShowInput{ID: "rfx-nope"}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
