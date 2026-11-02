package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/today"
)

func TestTodayAcrossVaults(t *testing.T) {
	e := newEnv(t)
	a, b := e.init("acme"), e.init("beta")
	task := func(status, blockedBy string) string {
		return "---\ntype: task\ncreated: 2026-11-01\nstatus: " + status + "\nproject: \"[[p]]\"\nblocked_by: " + blockedBy + "\n---\n\n# Task " + status + blockedBy + "\n"
	}
	constraint := func(review string) string {
		return strings.ReplaceAll(`---
type: constraint
created: 2026-11-01
why: freeze
by: Sam Example
source:
  url: https://wiki.acme.example/pages/1
  retrieved: 2026-11-01
  fidelity: none
applies_in: [x]
not_in: [y]
review_by: REVIEW
---

# Constraint REVIEW
`, "REVIEW", review)
	}
	write(t, a, "projects/task-blocked.md", task("blocked", "[]"))
	write(t, a, "projects/task-waiting.md", task("todo", "[task-blocked]"))
	write(t, a, "projects/task-done.md", task("done", "[task-blocked]"))
	write(t, a, "projects/task-plain.md", task("todo", "[]"))
	write(t, a, "constraints/constraint-old.md", constraint("2020-01-01"))
	write(t, a, "constraints/constraint-future.md", constraint("2999-01-01"))
	write(t, b, "constraints/constraint-old.md", constraint("2020-01-01"))

	var rep today.Report
	e.json(&rep, "today")
	if len(rep.Vaults) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	acme, beta := rep.Vaults[0], rep.Vaults[1]
	if acme.Vault != "acme" || acme.Due.Count != 1 || acme.Blocked.Count != 2 ||
		acme.Blocked.Items[0].ID != "task-blocked" || acme.Blocked.Items[1].ID != "task-waiting" {
		t.Errorf("acme = %+v", acme)
	}
	if beta.Vault != "beta" || beta.Due.Count != 1 || beta.Blocked.Count != 0 || len(beta.Blocked.Items) != 0 {
		t.Errorf("beta = %+v", beta)
	}

	// Reading is live: a change shows on the next call.
	if err := os.Remove(filepath.Join(a, "projects", "task-blocked.md")); err != nil {
		t.Fatal(err)
	}
	e.json(&rep, "today")
	if rep.Vaults[0].Blocked.Count != 1 {
		t.Errorf("not live: %+v", rep.Vaults[0])
	}

	// One broken vault is reported; the other still reads, and the exit is non-zero.
	if err := os.RemoveAll(filepath.Join(b, ".para")); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run("today", "--json")
	if err == nil {
		t.Error("broken vault exited zero")
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Vaults[0].Error != "" || rep.Vaults[1].Error == "" {
		t.Errorf("errors = %+v", rep.Vaults)
	}
}

// today spans vaults, so no agent surface may reach it.
func TestTodayIsNotAnOp(t *testing.T) {
	for _, e := range ops.All() {
		if strings.Contains(e.Name(), "today") {
			t.Errorf("op %s exposes today", e.Name())
		}
	}
	found := false
	walk(newRoot(&loader{}), func(c *cobra.Command) {
		if c.Name() == "today" {
			found = true
			if c.Annotations[opAnnotation] != "" {
				t.Error("today is bound to an op")
			}
		}
	})
	if !found {
		t.Error("no today command")
	}
}

func TestTodayCountsNudgesExceptionsUnverifiedInbox(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.ok("org", "role", "add", "Lead", "--org", "acme.example")
	e.ok("org", "domain", "add", "Logging", "--owner", "role-lead")
	var late, future, closed ops.RFXAddOutput
	e.json(&late, "rfx", "add", "late ask", "--route", "domain-logging", "--self-serve", "x", "--next-nudge", "2020-01-01")
	e.json(&future, "rfx", "add", "future ask", "--route", "domain-logging", "--self-serve", "x", "--next-nudge", "2999-01-01")
	e.json(&closed, "rfx", "add", "closed ask", "--direction", "in", "--route", "domain-logging", "--next-nudge", "2020-01-01")
	e.ok("rfx", "close", closed.ID, "--playbook", "role-lead")

	pol := e.ingest(govText)
	e.ok("exception", "add", "--policy", pol.ID, "--kind", "bug", "--by", "Jordan Example", "--review-by", "2020-01-01")
	e.ok("exception", "add", "--policy", pol.ID, "--kind", "bug", "--by", "Jordan Example", "--review-by", "2999-01-01")
	e.ok("exception", "add", "--policy", pol.ID, "--kind", "bug", "--by", "Jordan Example")
	e.ok("inbox", "add", "one")
	e.ok("inbox", "add", "two")

	var rep today.Report
	e.json(&rep, "today")
	v := rep.Vaults[0]
	if v.Nudges.Count != 1 || v.Nudges.Items[0].ID != late.ID {
		t.Errorf("nudges = %+v", v.Nudges)
	}
	if v.Expiring.Count != 1 {
		t.Errorf("expiring = %+v", v.Expiring)
	}
	if v.Unverified != 1 || v.Inbox != 2 {
		t.Errorf("unverified %d, inbox %d", v.Unverified, v.Inbox)
	}
	e.ok("policy", "review", pol.ID, "--by", "Jordan Example")
	e.json(&rep, "today")
	if rep.Vaults[0].Unverified != 0 {
		t.Errorf("reviewed policy still unverified: %+v", rep.Vaults[0])
	}
	if out := e.ok("today"); !strings.Contains(out, "nudges 1") || !strings.Contains(out, "inbox 2") {
		t.Errorf("text:\n%s", out)
	}
}

func TestTodayReportsBrokenEntry(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	// A vault whose directory is gone is reported too.
	gone := e.init("gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.home, "vaults", "broken")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault.ref"), []byte("nonsense\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run("today", "--json")
	if err == nil || !strings.Contains(err.Error(), "2 vaults could not be read") {
		t.Fatalf("err = %v", err)
	}
	var rep today.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Vaults) != 3 {
		t.Fatalf("report = %+v", rep)
	}
	by := map[string]today.VaultToday{}
	for _, v := range rep.Vaults {
		by[v.Vault] = v
	}
	if by["acme"].Error != "" || by["broken"].Error == "" || by["gone"].Error == "" {
		t.Errorf("errors: acme %q broken %q gone %q", by["acme"].Error, by["broken"].Error, by["gone"].Error)
	}
}

func TestVaultPrune(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	gone := e.init("gone")
	keys := filepath.Join(e.home, "vaults", "gone", "keys.properties")
	if err := os.WriteFile(keys, []byte("k=v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run("vault", "--dry-run"); err == nil {
		t.Error("--dry-run without --prune must fail")
	}
	out := e.ok("vault", "--prune", "--dry-run")
	if !strings.Contains(out, "would prune\tgone\t"+gone) {
		t.Errorf("dry run output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(e.home, "vaults", "gone", "vault.ref")); err != nil {
		t.Fatalf("dry run changed the registry: %v", err)
	}
	out = e.ok("vault", "--prune")
	if !strings.Contains(out, "pruned\tgone\t"+gone) || strings.Contains(out, "acme") {
		t.Errorf("output = %q", out)
	}
	var list struct {
		Vaults []struct{ ID string } `json:"vaults"`
	}
	e.json(&list, "vault", "list")
	if len(list.Vaults) != 1 || list.Vaults[0].ID != "acme" {
		t.Errorf("list = %+v", list)
	}
	if _, err := os.Stat(keys); err != nil {
		t.Errorf("state removed: %v", err)
	}
	if out = e.ok("vault", "--prune"); out != "" {
		t.Errorf("second prune = %q", out)
	}
}

func TestVaultListShowsBrokenAndMissing(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	gone := e.init("gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.home, "vaults", "broken")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault.ref"), []byte("nonsense\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := e.run("vault", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acme\t", "gone\t" + gone + "\tmissing\n", "broken\tbroken: "} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "acme\t"+filepath.Join(e.dir, "acme")+"\tmissing") {
		t.Errorf("a healthy vault is marked missing:\n%s", out)
	}
	if !strings.Contains(errOut, "--prune --dry-run") {
		t.Errorf("no hint on stderr: %q", errOut)
	}
	var js struct {
		Vaults []struct {
			ID      string `json:"id"`
			Error   string `json:"error"`
			Missing bool   `json:"missing"`
		} `json:"vaults"`
	}
	e.json(&js, "vault", "list")
	if len(js.Vaults) != 3 || js.Vaults[0].ID != "acme" || js.Vaults[1].Error == "" || !js.Vaults[2].Missing {
		t.Errorf("json = %+v", js)
	}
	// Prune drops the missing one; the broken one stays and still shows.
	e.ok("vault", "--prune")
	if out = e.ok("vault", "list"); strings.Contains(out, "gone") || !strings.Contains(out, "broken\tbroken: ") {
		t.Errorf("after prune:\n%s", out)
	}
}

// Today names what its items point at, so a surface shows titles, not ids.
func TestTodayNamesProjectAndScope(t *testing.T) {
	e := newEnv(t)
	a := e.init("acme")
	write(t, a, "projects/p.md", "---\ntype: project\ncreated: 2026-11-01\nstatus: active\nowner: self\n---\n\n# Portal rebuild\n")
	write(t, a, "projects/t.md", "---\ntype: task\ncreated: 2026-11-01\nstatus: todo\nproject: \"[[p]]\"\ndue: 2020-01-01\n---\n\n# Order the pool\n")
	write(t, a, "projects/q.md", "---\ntype: question\ncreated: 2026-11-01\nstatus: open\nkind: input\nscope: \"[[p]]\"\n---\n\n# Which window?\n")
	var rep today.Report
	e.json(&rep, "today")
	v := rep.Vaults[0]
	if len(v.Overdue.Items) != 1 || v.Overdue.Items[0].Project != "p" || v.Overdue.Items[0].ProjectName != "Portal rebuild" {
		t.Errorf("overdue = %+v", v.Overdue.Items)
	}
	if len(v.Questions.Items) != 1 || v.Questions.Items[0].Scope != "p" || v.Questions.Items[0].ScopeName != "Portal rebuild" {
		t.Errorf("questions = %+v", v.Questions.Items)
	}
}
