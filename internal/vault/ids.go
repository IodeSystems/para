package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
)

// Id changes. A document's id is its file name. When an id changes (a
// rename, an upgrade step's re-prefix, a fold), the old id goes to the
// document's former_ids, so a link, event, or receipt that names it still
// resolves (Get), and every reference in other documents is rewritten to the
// new id. Records (model.RecordTypes) keep the text they were written with.
// former_ids lives in the file, so it moves with sync and every index rebuild
// reads it from files; no other document may take a former id.

// IDChangeResult is what ChangeIDs or RelinkFormer did.
type IDChangeResult struct {
	// Renamed maps each old id to the new one.
	Renamed map[string]string `json:"renamed"`
	// Rewritten lists the documents whose references changed, sorted.
	Rewritten []string `json:"rewritten"`
}

// formerOf returns the document that lists id in former_ids.
func (v *Vault) formerOf(id string) (docFile, bool, error) {
	files, err := v.docFiles()
	if err != nil {
		return docFile{}, false, err
	}
	for _, f := range files {
		data, err := os.ReadFile(v.abs(f.rel))
		if err != nil {
			return docFile{}, false, err
		}
		if slices.ContainsFunc(v.listedOf(f.rel, data).former, func(x string) bool { return strings.EqualFold(x, id) }) {
			return f, true, nil
		}
	}
	return docFile{}, false, nil
}

// notReserved refuses an id that a document lists in former_ids.
func (v *Vault) notReserved(id string) error {
	g, ok, err := v.formerOf(id)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("%w: %s is a former id of %s", model.ErrExists, id, g.id)
	}
	return nil
}

// Resolve returns the current id of id: id itself when a file has it, else
// the document that lists it in former_ids.
func (v *Vault) Resolve(_ context.Context, id string) (string, error) {
	if f, err := v.find(id); err == nil {
		return f.id, nil
	} else if !errors.Is(err, model.ErrNotFound) {
		return "", fmt.Errorf("vault: resolve: %w", err)
	}
	g, ok, err := v.formerOf(id)
	if err != nil {
		return "", fmt.Errorf("vault: resolve: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("vault: resolve: %w: %s", model.ErrNotFound, id)
	}
	return g.id, nil
}

// ChangeIDs changes the id of each document in ids (old to new): it renames
// the file, adds the old id to former_ids, and rewrites every reference in
// the other documents (model.Document.Relink), logging one event per
// document. A link key whose target's type asks for another key moves there
// (model.LinkKeyFor). A run that stopped part way can run again: an old id
// that is already a former id of its new id is not renamed twice.
func (v *Vault) ChangeIDs(ctx context.Context, ids map[string]string) (IDChangeResult, error) {
	res := IDChangeResult{Renamed: map[string]string{}, Rewritten: []string{}}
	olds := make([]string, 0, len(ids))
	for old, nw := range ids {
		if old == nw || !paramd.ValidStem(nw) {
			return res, fmt.Errorf("vault: change ids: %s to %q is not a valid change", old, nw)
		}
		if _, chained := ids[nw]; chained {
			return res, fmt.Errorf("vault: change ids: %s is both an old and a new id", nw)
		}
		olds = append(olds, old)
	}
	sort.Strings(olds)
	for _, old := range olds {
		nw := ids[old]
		if _, err := v.find(old); err == nil {
			if _, err := v.Rename(ctx, old, nw); err != nil {
				return res, fmt.Errorf("vault: change ids: %w", err)
			}
		} else if !errors.Is(err, model.ErrNotFound) {
			return res, fmt.Errorf("vault: change ids: %w", err)
		}
		d, err := v.Get(ctx, nw)
		if err != nil {
			return res, fmt.Errorf("vault: change ids: %s: %w", old, err)
		}
		if h := d.Meta.Header(); !h.WasID(old) {
			h.FormerIDs = append(h.FormerIDs, old)
			if _, err := v.Update(ctx, d); err != nil {
				return res, fmt.Errorf("vault: change ids: %w", err)
			}
		}
		res.Renamed[old] = nw
	}
	rewritten, err := v.relink(ctx, ids)
	res.Rewritten = rewritten
	return res, err
}

// RelinkFormer rewrites every reference to a former id to the current id, and
// moves link keys to the key their target's type asks for: for a file that
// came in by sync after an id change.
func (v *Vault) RelinkFormer(ctx context.Context) (IDChangeResult, error) {
	ids := map[string]string{}
	files, err := v.docFiles()
	if err != nil {
		return IDChangeResult{}, fmt.Errorf("vault: relink: %w", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(v.abs(f.rel))
		if err != nil {
			return IDChangeResult{}, fmt.Errorf("vault: relink: %w", err)
		}
		for _, old := range v.listedOf(f.rel, data).former {
			if _, taken := ids[old]; !taken {
				ids[old] = f.id
			}
		}
	}
	rewritten, err := v.relink(ctx, ids)
	return IDChangeResult{Renamed: map[string]string{}, Rewritten: rewritten}, err
}

// relink rewrites the references in every document that is not a record.
func (v *Vault) relink(ctx context.Context, ids map[string]string) ([]string, error) {
	refs, err := v.List(ctx, "", "")
	if err != nil {
		return nil, fmt.Errorf("vault: relink: %w", err)
	}
	types := make(map[string]model.Type, len(refs))
	for _, r := range refs {
		types[r.ID] = r.Type
	}
	typeOf := func(id string) model.Type {
		if to, ok := ids[id]; ok {
			id = to
		}
		return types[id]
	}
	out := []string{}
	for _, r := range refs {
		if model.IsRecord(r.Type) {
			continue
		}
		d, err := v.Get(ctx, r.ID)
		if err != nil {
			continue // lint reports a file that does not decode
		}
		nd, changed, err := d.Relink(ids, typeOf)
		if err != nil {
			return out, fmt.Errorf("vault: relink: %s: %w", r.ID, err)
		}
		if !changed {
			continue
		}
		if _, err := v.Update(ctx, nd); err != nil {
			return out, fmt.Errorf("vault: relink: %w", err)
		}
		out = append(out, r.ID)
	}
	return out, nil
}
