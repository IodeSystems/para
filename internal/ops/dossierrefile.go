package ops

import (
	"context"
	"fmt"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// DossierRefileInput is the input of dossier_refile: the staged change, and
// result (what you wrote for it) or dropped (why it is not needed).
type DossierRefileInput struct {
	Proposal string `json:"proposal"`
	Item     string `json:"item"`
	Result   string `json:"result,omitempty"`  // the id the direct write made or changed, or its receipt
	Dropped  string `json:"dropped,omitempty"` // why the change is not needed any more
}

// DossierRefileOutput is the output of dossier_refile. Left counts the staged
// changes still open in the dossier; Status is superseded once none is.
type DossierRefileOutput struct {
	Proposal string               `json:"proposal"`
	Item     string               `json:"item"`
	State    model.ItemState      `json:"state"`
	Status   model.ProposalStatus `json:"status"`
	Left     int                  `json:"left"`
	Rev      string               `json:"rev"`
}

// DossierRefile is the judgement part of vault upgrade step dossiers.
var DossierRefile = withProposalReceipts(defineSafe("dossier_refile", "Vault upgrade step dossiers: record what became of a staged change in an old dossier (a change block para no longer runs). Make the change yourself first with the op it names, then give result (the id it wrote, or its receipt); or give dropped, why it is not needed. The dossier closes as superseded when no staged change is left", func(ctx context.Context, d Deps, in DossierRefileInput) (DossierRefileOutput, error) {
	id := normID(in.Proposal)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return DossierRefileOutput{}, err
	}
	if _, ok := doc.Meta.(*model.Proposal); !ok {
		return DossierRefileOutput{}, fmt.Errorf("%s is a %s, not a dossier", id, doc.Meta.Header().Type)
	}
	if in.Result != "" {
		if _, err := d.Store.Get(ctx, normID(in.Result)); err != nil {
			return DossierRefileOutput{}, fmt.Errorf("result %s: %w", in.Result, err)
		}
	}
	nd, err := upgrade.RefileItem(doc, in.Item, normID(in.Result), in.Dropped)
	if err != nil {
		return DossierRefileOutput{}, err
	}
	saved, err := d.Store.Update(ctx, nd)
	if err != nil {
		return DossierRefileOutput{}, err
	}
	out := DossierRefileOutput{Proposal: saved.ID, Item: in.Item, Status: saved.Meta.(*model.Proposal).Status,
		Left: upgrade.DossierCounts(saved)[upgrade.MoveStaged], Rev: saved.Rev}
	if dz, err := model.ParseDossier(saved.Body); err == nil {
		for _, it := range dz.Items {
			if it.ID == in.Item {
				out.State = it.State
			}
		}
	}
	return out, nil
}))
