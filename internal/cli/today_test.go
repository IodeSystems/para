package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/iodesystems/paradize/internal/ops"
	"github.com/iodesystems/paradize/internal/today"
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
