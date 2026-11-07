package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamOBParity creates the fixed-id documents the OB rows refer to: a
// person and open obligations owed to them.
func seedStreamOBParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{{ID: "person-ob-seed", Folder: model.FolderOrg, Title: "Olu Example",
		Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme.example",
			Contacts: []model.Contact{{Method: model.MediumEmail, Value: "olu@acme.example", Preferred: true}}}}}
	for _, id := range []string{"obligation-ob-a", "obligation-ob-b", "obligation-ob-show"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderObligations, Title: "OB promise " + id,
			Meta: &model.Obligation{Common: model.Common{Type: model.TypeObligation}, Kind: model.KindCommitment, State: model.StateOpen,
				Schedule: model.ScheduleOnce, OwedTo: []model.Link{model.LinkTo("person-ob-seed")}, Due: "2099-01-10"}})
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}

func streamOBParityCases() map[string]parityCase {
	return map[string]parityCase{
		"obligation_parts":    {`{"id":"obligation-ob-show"}`, []string{"obligation", "parts", "obligation-ob-show"}, true},
	}
}
