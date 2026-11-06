package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamPUParity creates the fixed-id documents the PU rows refer to.
func seedStreamPUParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []string{"group-pu-a", "group-pu-b"} {
		doc := model.Document{ID: id, Folder: model.FolderOrg, Title: "PU group",
			Meta: &model.Group{Common: model.Common{Type: model.TypeGroup}, Org: "acme.example", Kind: model.GroupTeam}}
		if _, err := deps.Store.Create(ctx, doc); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

func streamPUParityCases() map[string]parityCase {
	return map[string]parityCase{
		"priority_set": {`{"id":"group-pu-a","priority":"high"}`, []string{"priority", "set", "group-pu-b", "high"}, false},
	}
}
