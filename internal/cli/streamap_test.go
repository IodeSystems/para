package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// streamAPParityCases are the parity rows of stream AP: the path log and the
// next move of a stalled Ask. Ids come from seedStreamAPParity.
func streamAPParityCases() map[string]parityCase {
	return map[string]parityCase{
		"rfx_paths":    {`{"id":"rfx-ap-a"}`, []string{"rfx", "paths", "rfx-ap-a"}, true},
		"rfx_path_add": {`{"id":"rfx-ap-a","to":"person-ap","medium":"email","reason":"no_response","why":"No reply by slack.","nudge":"2026-11-12"}`, []string{"rfx", "path", "add", "rfx-ap-b", "person-ap", "--medium", "email", "--reason", "no_response", "--why", "No reply by slack.", "--nudge", "2026-11-12"}, false},
	}
}

func seedStreamAPParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{{ID: "person-ap", Folder: model.FolderOrg, Title: "Ap Person",
		Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme.example",
			Contacts: []model.Contact{{Method: model.MediumSlack, Value: "@ap", Preferred: true}, {Method: model.MediumEmail, Value: "ap@acme.example"}}}}}
	for _, id := range []string{"rfx-ap-a", "rfx-ap-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderRFX, Title: "Ap ask " + id,
			Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXOut, Status: model.RFXOpen, Route: "domain-seed", SelfServeCheck: "searched",
				Targets: []model.Target{{To: model.LinkTo("person-ap"), Medium: model.MediumSlack, Value: "@ap", State: model.TargetSent, Sent: "2026-11-01"}}}})
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}
