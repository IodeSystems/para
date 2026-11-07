package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamIPParity writes a pools file that fired in 2000 and the area its
// ideas go in.
func seedStreamIPParity(t *testing.T, deps ops.Deps, root string) {
	t.Helper()
	text := "pools:\n  - name: ip\n    area: area-ip\n    when: {date: 2000-01-01}\n"
	if err := os.WriteFile(filepath.Join(root, ".para", "pools.yaml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := model.Document{ID: "area-ip", Folder: model.FolderAreas, Title: "IP area",
		Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}}
	if _, err := deps.Store.Create(context.Background(), doc); err != nil {
		t.Fatalf("seed area-ip: %v", err)
	}
}

func streamIPParityCases() map[string]parityCase {
	return map[string]parityCase{
		"idea_add":  {`{"pool":"ip","title":"Idea one","text":"A sentence."}`, []string{"idea", "add", "ip", "Idea two", "--text", "A sentence."}, false},
		"pool_list": {`{}`, []string{"pool", "list"}, false},
		"pool_run":  {`{"dry_run":true}`, []string{"pool", "run", "--dry-run"}, false},
		"pool_pick": {`{"pool":"ip","skip":true}`, []string{"pool", "pick", "ip", "--skip"}, false},
	}
}
