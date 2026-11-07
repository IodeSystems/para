package egress

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/events"
)

func TestCheck(t *testing.T) {
	cases := []struct {
		name    string
		cfg     map[string]string
		dest    string
		def     string
		allowed bool
		marking string
	}{
		{"unset denies", nil, "git:push", "deny", false, UnmarkedMarking},
		{"deny blocks other paths", map[string]string{KeyDefault: "deny"}, "mcp:llm", "deny", false, UnmarkedMarking},
		{"deny still allows copy", map[string]string{KeyDefault: "deny"}, "copy:plain", "deny", true, UnmarkedMarking},
		{"allow", map[string]string{KeyDefault: " Allow "}, "mcp:llm", "allow", true, UnmarkedMarking},
		{"junk means deny", map[string]string{KeyDefault: "maybe"}, "mcp:llm", "deny", false, UnmarkedMarking},
		{"export under deny", map[string]string{KeyDefault: "deny"}, "export:ssh", "deny", true, UnmarkedMarking},
		{"export unset list", nil, "export:wormhole", "deny", true, UnmarkedMarking},
		{"export in list", map[string]string{KeyExport: "stdout, SSH"}, "export:ssh", "deny", true, UnmarkedMarking},
		{"export not in list", map[string]string{KeyExport: "stdout,ssh", KeyDefault: "allow"}, "export:wormhole", "allow", false, UnmarkedMarking},
		{"publish under deny", map[string]string{KeyDefault: "deny", KeyExport: "stdout"}, "publish:design", "deny", false, UnmarkedMarking},
		{"publish under deny, opted in", map[string]string{KeyDefault: "deny", KeyPublish: "allow"}, "publish:design", "deny", true, UnmarkedMarking},
		{"publish under allow", map[string]string{KeyDefault: "allow"}, "publish:design", "allow", true, UnmarkedMarking},
		{"marking", map[string]string{KeyMarking: " ACME CONFIDENTIAL "}, "copy:slack", "deny", true, "ACME CONFIDENTIAL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Check(c.cfg, c.dest)
			if got.Default != c.def || got.Allowed != c.allowed || got.Marking != c.marking {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestLogHoldsOnlyIDAndDestination(t *testing.T) {
	dir := t.TempDir()
	l := events.NewLog(dir, events.Source{Vault: "acme", Install: "m1", Actor: "user", Surface: "cli"})
	if err := Log(l, "policy-abc", "copy:email", Decision{Default: "deny", Allowed: true, Marking: "SECRET WORDS"}); err != nil {
		t.Fatal(err)
	}
	evs, err := l.Tail(5)
	if err != nil || len(evs) != 1 {
		t.Fatalf("events %v err %v", evs, err)
	}
	e := evs[0]
	if e.Op != "egress" || e.Doc != "policy-abc" || e.To != "copy:email" || e.From != "deny" {
		t.Fatalf("event = %+v", e)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	raw, _ := os.ReadFile(files[0])
	if strings.Contains(string(raw), "SECRET") {
		t.Fatalf("marking leaked into the log: %s", raw)
	}
	if err := Log(l, "x", "bad dest with spaces", Decision{Default: "deny"}); err == nil {
		t.Fatal("event guard accepted free text")
	}
}
