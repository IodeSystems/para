package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEgress(t *testing.T) {
	cases := []struct {
		name, extra, dest string
		def               string
		allowed           bool
		marking           string
	}{
		{"init default denies other paths", "", "git:push", "deny", false, "UNMARKED"},
		{"copy stays allowed", "", "copy:plain", "deny", true, "UNMARKED"},
		{"marking", "classification=ACME INTERNAL\n", "copy:slack", "deny", true, "ACME INTERNAL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, root := newVault(t)
			p := filepath.Join(root, ".para", "vault.properties")
			raw, _ := os.ReadFile(p)
			if err := os.WriteFile(p, append(raw, c.extra...), 0o600); err != nil {
				t.Fatal(err)
			}
			d, err := v.Egress(ctx, "policy-x", c.dest)
			if err != nil {
				t.Fatal(err)
			}
			if d.Default != c.def || d.Allowed != c.allowed || d.Marking != c.marking {
				t.Fatalf("decision = %+v", d)
			}
			evs := eventLog(t, v)
			last := evs[len(evs)-1]
			if last.Op != "egress" || last.Doc != "policy-x" || last.To != c.dest || last.From != c.def {
				t.Fatalf("event = %+v", last)
			}
		})
	}
}
