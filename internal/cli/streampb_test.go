package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

func seedStreamPBParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	for _, id := range []string{"area-pb-a", "area-pb-b"} {
		d := model.Document{ID: id, Folder: model.FolderAreas, Title: "PB design " + id, Body: "See [[area-pb-a]].",
			Meta: &model.Area{Common: model.Common{Type: model.TypeArea, Publish: []string{"pbdesign"}}, Status: model.AreaActive}}
		if _, err := deps.Store.Create(context.Background(), d); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

func streamPBParityCases() map[string]parityCase {
	return map[string]parityCase{
		"publish_preview": {`{"set":"pbdesign"}`, []string{"publish", "preview", "pbdesign"}, true},
		"publish_set":     {`{"id":"area-pb-a","sets":["pbdesign","runbooks"]}`, []string{"publish", "set", "area-pb-b", "pbdesign", "runbooks"}, false},
	}
}
