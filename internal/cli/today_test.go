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
