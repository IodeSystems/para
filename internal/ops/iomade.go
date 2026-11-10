package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// receiptsByInput indexes every receipt that cites an io item, by that item,
// oldest first. One pass over the receipts, whatever the number of items.
func receiptsByInput(ctx context.Context, s store.Store) (map[string][]ReceiptView, error) {
	refs, err := s.List(ctx, store.Filter{Folder: model.FolderReceipts, Type: model.TypeReceipt})
	if err != nil {
		return nil, fmt.Errorf("ops: receiptsByInput: %w", err)
	}
	out := map[string][]ReceiptView{}
	for _, ref := range refs {
		doc, r, err := getReceipt(ctx, s, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("ops: receiptsByInput: %w", err)
		}
		if r.Input == "" || r.Doc == r.Input {
			continue
		}
		out[r.Input] = append(out[r.Input], receiptView(doc, r))
	}
	for _, rs := range out {
		slices.SortStableFunc(rs, func(a, b ReceiptView) int {
			if c := strings.Compare(a.At, b.At); c != 0 && a.At != "" && b.At != "" {
				return c
			}
			if c := strings.Compare(string(a.Created), string(b.Created)); c != 0 {
				return c
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
	return out, nil
}

// madeCount is how many documents the writes that cite an io item touched.
func madeCount(rs []ReceiptView) int {
	seen := map[string]bool{}
	for _, r := range rs {
		seen[r.Doc] = true
	}
	return len(seen)
}

// receiptAction names what a write did: created, edited, closed, or the op.
func receiptAction(op string) string {
	switch {
	case strings.HasSuffix(op, "_add"):
		return "created"
	case strings.HasSuffix(op, "_edit"), strings.HasSuffix(op, "_set"), strings.HasSuffix(op, "_update"):
		return "edited"
	case strings.HasSuffix(op, "_done"), strings.HasSuffix(op, "_close"), strings.HasSuffix(op, "_drop"),
		strings.HasSuffix(op, "_cancel"), strings.HasSuffix(op, "_resolve"), strings.HasSuffix(op, "_fail"),
		strings.HasSuffix(op, "_answer"):
		return "closed"
	}
	return op
}

// IOMadeInput is the input of io_made.
type IOMadeInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// IOMadeRow is one document an io item changed in para. Actions keeps every
// action taken on it, in order; At, Actor, Receipt, and Why are the last
// write's. Receipts lists all of them. A row with action linked has no
// receipt: the item points at it (Became or Links).
type IOMadeRow struct {
	Doc      string   `json:"doc"`
	Title    string   `json:"title,omitempty"`
	Type     string   `json:"type,omitempty"`
	Actions  []string `json:"actions"`
	At       string   `json:"at,omitempty"`
	Actor    string   `json:"actor,omitempty"`
	Receipt  string   `json:"receipt,omitempty"`
	Receipts []string `json:"receipts"`
	Why      string   `json:"why,omitempty"`
}

// IOMadeOutput is the output of io_made. Counts is the number of documents
// per action (a document with two actions counts under both).
type IOMadeOutput struct {
	ID     string         `json:"id"`
	Rows   []IOMadeRow    `json:"rows"`
	Counts map[string]int `json:"counts"`
}

// IOMade lists what an incoming io item changed in para.
var IOMade = defineRead("io_made", "List what one io item changed in para: every write an agent made while ingesting it (receipts that cite it), per document in time order with its actions (created, edited, closed, or the op name), plus the items it became or links to (linked), and a count per action", func(ctx context.Context, d Deps, in IOMadeInput) (IOMadeOutput, error) {
	doc, m, err := getIO(ctx, d, in.ID)
	if err != nil {
		return IOMadeOutput{}, err
	}
	by, err := receiptsByInput(ctx, d.Store)
	if err != nil {
		return IOMadeOutput{}, err
	}
	out := IOMadeOutput{ID: doc.ID, Rows: []IOMadeRow{}, Counts: map[string]int{}}
	at := map[string]int{}
	row := func(id string) *IOMadeRow {
		if i, ok := at[id]; ok {
			return &out.Rows[i]
		}
		at[id] = len(out.Rows)
		r := IOMadeRow{Doc: id, Actions: []string{}, Receipts: []string{}}
		if td, err := d.Store.Get(ctx, id); err == nil {
			r.Title, r.Type = td.Title, string(td.Meta.Header().Type)
		}
		out.Rows = append(out.Rows, r)
		return &out.Rows[len(out.Rows)-1]
	}
	for _, rv := range by[doc.ID] {
		r := row(rv.Doc)
		if a := receiptAction(rv.Op); !slices.Contains(r.Actions, a) {
			r.Actions = append(r.Actions, a)
		}
		r.At, r.Actor, r.Receipt, r.Why = rv.At, rv.Actor, rv.ID, rv.Why
		if r.At == "" {
			r.At = string(rv.Created)
		}
		r.Receipts = append(r.Receipts, rv.ID)
	}
	for _, id := range append([]string{m.Became.Target()}, targets(m.Links)...) {
		if id == "" || id == doc.ID {
			continue
		}
		if _, ok := at[id]; ok {
			continue
		}
		r := row(id)
		r.Actions = append(r.Actions, "linked")
	}
	for _, r := range out.Rows {
		for _, a := range r.Actions {
			out.Counts[a]++
		}
	}
	return out, nil
})
