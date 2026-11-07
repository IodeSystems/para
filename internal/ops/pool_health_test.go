package ops

import (
	"strings"
	"testing"
)

// A pools file that does not parse is a health finding.
func TestPoolsInvalidIsAHealthFinding(t *testing.T) {
	d, root, _ := poolFixture(t)
	writePools(t, root, "pools:\n  - name: x\n    when: {bogus: 1}\n")
	h, err := Health(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			if f.Kind == HealthPoolsInvalid {
				if !strings.Contains(f.Message, "pools.yaml") {
					t.Errorf("message = %q", f.Message)
				}
				return
			}
		}
	}
	t.Errorf("no pools_invalid finding: %+v", h)
}
