package ops

import (
	"context"
	"fmt"
	"slices"

	"github.com/iodesystems/para/internal/model"
)

// A dossier message block may name an outgoing io draft (io: "[[io-x]]")
// instead of holding the text. The draft is the one copy of the message:
// para fills the item's people, subject, and body from it when it reads the
// dossier, so the hash an approval binds to covers the draft's text, and an
// io_edit after approval stops the item from being reported done. An owner's
// edit in the block was written to the draft. Since vault format 8 a dossier
// is read-only: hydrateIO only shows the draft's text in an old dossier.

// hydrateIO fills each message item that names an io draft. strict (an agent
// writing the dossier) refuses a draft that is missing, incoming, or dealt
// with; otherwise such an item keeps no text.
func hydrateIO(ctx context.Context, d Deps, items []model.ProposalItem, strict bool) error {
	for i := range items {
		it := &items[i]
		if it.IO == "" || it.Kind != model.ItemMessage {
			continue
		}
		doc, m, err := getIO(ctx, d, it.IO)
		if err == nil && strict {
			switch {
			case m.Direction != model.IOOut:
				err = fmt.Errorf("%s is incoming: a message block names an outgoing draft", doc.ID)
			case !m.State.Open():
				err = fmt.Errorf("%s is already %s", doc.ID, m.State)
			}
		}
		if err != nil {
			if strict {
				return fmt.Errorf("item %s: io: %w", it.ID, err)
			}
			continue
		}
		it.People, it.Subject, it.Body = append([]string(nil), m.To...), m.Subject, doc.Body
	}
	return nil
}

// syncIO writes the text of each message item that names an open io draft back
// to the draft, and makes the draft ready once the item is approved with that
// text. The stored text is read back into the item; an approval bound to the
// text before the write is bound to the stored text.
func (p *proposal) syncIO(ctx context.Context, d Deps) error {
	for i := range p.content.Items {
		it := &p.content.Items[i]
		if it.IO == "" || it.Kind != model.ItemMessage {
			continue
		}
		doc, m, err := getIO(ctx, d, it.IO)
		if err != nil || m.Direction != model.IOOut || !m.State.Open() {
			continue
		}
		before, err := it.Hash()
		if err != nil {
			return err
		}
		changed := false
		if body := cleanText(it.Body); body != "" && body != doc.Body {
			doc.Body, changed = body, true
		}
		if s := ioLine(it.Subject); s != m.Subject {
			m.Subject, changed = s, true
		}
		if len(it.People) > 0 && !slices.Equal(it.People, m.To) {
			to, err := ioParties(ctx, d, it.People)
			if err != nil {
				return fmt.Errorf("item %s: io %s: to: %w", it.ID, doc.ID, err)
			}
			if len(ioLinks(to)) == 0 {
				return fmt.Errorf("item %s: io %s: to needs a person or group", it.ID, doc.ID)
			}
			if !slices.Equal(to, m.To) {
				m.To, changed = to, true
			}
		}
		bound := it.ApprovedHash != "" && it.ApprovedHash == before
		approved := it.State == model.ItemApproved || it.State == model.ItemClaimed
		if bound && approved && m.State == model.IODraft {
			m.State, changed = model.IOReady, true
		}
		if !changed {
			continue
		}
		doc.Title = ioTitle(m.Subject, doc.Body)
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return fmt.Errorf("item %s: io %s: %w", it.ID, doc.ID, err)
		}
		if err := hydrateIO(ctx, d, p.content.Items[i:i+1], false); err != nil {
			return err
		}
		if bound {
			if it.ApprovedHash, err = it.Hash(); err != nil {
				return err
			}
		}
	}
	return nil
}
