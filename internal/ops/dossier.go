package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Dossiers: a proposal whose body is markdown with typed blocks (model.Dossier).
// Since vault format 8 they are read-only records (proposal_list,
// proposal_show); evidence is still checked where an op takes it.

// checkEvidence refuses evidence the owner cannot check: the locator must be
// an http(s) URL or a vault document, or via names a private connector item
// with verify.
func checkEvidence(ctx context.Context, d Deps, e model.Evidence) error {
	switch e.Check() {
	case model.CheckURL, model.CheckVia:
		return nil
	case model.CheckVault:
		ref := model.VaultRef(e.Locator)
		var err error
		if strings.Contains(ref, "/") {
			_, err = d.Store.DocAt(ctx, ref)
		} else {
			_, err = d.Store.Get(ctx, ref)
		}
		if err != nil {
			return fmt.Errorf("evidence locator %q: no such vault document: %s", e.Locator, model.EvidenceForms)
		}
		return nil
	}
	if e.Via != nil && strings.TrimSpace(e.Verify) == "" {
		return errors.New("evidence via needs verify: a sentence that tells the owner how to find the item themself")
	}
	return fmt.Errorf("evidence locator %q: the owner cannot open it: %s", e.Locator, model.EvidenceForms)
}
