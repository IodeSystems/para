package server

import (
	"encoding/json"
	"testing"

	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/opsample"
)

// TestShowDetailIsTyped: over GraphQL, show's detail is the Output type of the
// show op that made it, for each sample item a show op reads.
func TestShowDetailIsTyped(t *testing.T) {
	f := newFixture(t)
	tok := f.sampleToken()
	seen := 0
	for _, e := range ops.ShowOps() {
		var in struct {
			ID string `json:"id"`
		}
		if s, ok := opsample.Inputs[e.Name()]; !ok || json.Unmarshal([]byte(s), &in) != nil || in.ID == "" {
			continue
		}
		_, res := f.graphql(tok, `query($id: String!) { para { v1 { show(id: $id) { op detail { __typename } } } } }`, map[string]any{"id": in.ID})
		if res["errors"] != nil {
			t.Errorf("%s %s: %v", e.Name(), in.ID, res["errors"])
			continue
		}
		show, _ := res["data"].(map[string]any)["para"].(map[string]any)["v1"].(map[string]any)["show"].(map[string]any)
		op, _ := show["op"].(string)
		by, ok := ops.Lookup(op)
		if !ok {
			t.Errorf("%s %s: show op %q", e.Name(), in.ID, op)
			continue
		}
		detail, _ := show["detail"].(map[string]any)
		if got, want := detail["__typename"], "para_"+by.OutputType().Name(); got != want {
			t.Errorf("%s %s: detail is %v, want %s", e.Name(), in.ID, got, want)
		}
		seen++
	}
	if seen < 11 {
		t.Errorf("checked %d show ops, want at least 11", seen)
	}
}
