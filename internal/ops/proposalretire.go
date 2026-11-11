package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/upgrade"
)

// Retiring an old proposal. Since 0.3.0 nothing runs a dossier: a proposal
// whose staged writes were made directly, or that no one will finish, stays
// open, listed and linted, and nothing closes it. proposal_retire closes it
// (status closed) with a dated section that says why and what each item that
// was still open became. No item block changes, and the vault format stays.

// ProposalRetireInput is the input of proposal_retire. Give ids, or
// all_applied: every open proposal whose open items are all staged changes
// already made by a direct write.
type ProposalRetireInput struct {
	IDs        []string `json:"ids,omitempty"`
	AllApplied bool     `json:"all_applied,omitempty"`
	Note       string   `json:"note,omitempty"` // why, one line; default "pre-0.3.0 proposal: nothing runs it"
	DryRun     bool     `json:"dry_run,omitempty"`
}

// RetireItem is an item of a proposal that was still open.
type RetireItem struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Op   string `json:"op,omitempty"`
	// Applied is the receipt of the direct write that made a staged change,
	// or "".
	Applied string `json:"applied,omitempty"`
}

// RetireRow is one proposal: retired (or, in a dry run, would be), or
// skipped with why.
type RetireRow struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Status  string       `json:"status"`
	Open    []RetireItem `json:"open"`
	Retired bool         `json:"retired"`
	Skipped string       `json:"skipped,omitempty"`
}

// ProposalRetireOutput is the output of proposal_retire.
type ProposalRetireOutput struct {
	Rows   []RetireRow `json:"rows"`
	DryRun bool        `json:"dry_run,omitempty"`
}

// retiredHeading opens the section a retire writes.
const retiredHeading = "## Retired"

// ProposalRetire closes old proposals.
var ProposalRetire = withProposalReceipts(withOneReceipt(definePerson("proposal_retire", "Retire old proposals (dossiers from before 0.3.0) that nothing runs: status closed, with a dated note under ## Retired that lists each item still open and, for a staged change, the receipt of the direct write that made it. A closed proposal is not listed as open, not linted, and not a pending vault upgrade item. Give ids, or all_applied for every open proposal whose open items are all staged changes already made. dry_run lists what it would do. One receipt undoes it; person only", func(ctx context.Context, d Deps, in ProposalRetireInput) (ProposalRetireOutput, error) {
	if (len(in.IDs) == 0) == !in.AllApplied {
		return ProposalRetireOutput{}, errors.New("give proposal ids, or all_applied, not both")
	}
	note := strings.Join(strings.Fields(in.Note), " ")
	if note == "" {
		note = "pre-0.3.0 proposal: nothing runs it"
	}
	if err := lintText("note", note); err != nil {
		return ProposalRetireOutput{}, err
	}
	var docs []model.Document
	if in.AllApplied {
		refs, err := d.Store.List(ctx, store.Filter{Folder: model.FolderProposals, Type: model.TypeProposal})
		if err != nil {
			return ProposalRetireOutput{}, err
		}
		for _, r := range refs {
			doc, err := d.Store.Get(ctx, r.ID)
			if err != nil {
				return ProposalRetireOutput{}, err
			}
			if m := doc.Meta.(*model.Proposal); m.Status == model.ProposalOpen || m.Status == model.ProposalSentBack {
				docs = append(docs, doc)
			}
		}
	} else {
		for _, id := range in.IDs {
			doc, err := d.Store.Get(ctx, normID(id))
			if err != nil {
				return ProposalRetireOutput{}, err
			}
			if _, ok := doc.Meta.(*model.Proposal); !ok {
				return ProposalRetireOutput{}, fmt.Errorf("%s is a %s, not a proposal", doc.ID, doc.Meta.Header().Type)
			}
			docs = append(docs, doc)
		}
	}
	applied, err := appliedWrites(ctx, d)
	if err != nil {
		return ProposalRetireOutput{}, err
	}
	out := ProposalRetireOutput{Rows: []RetireRow{}, DryRun: in.DryRun}
	today := d.Store.Now().Format(dateLayout)
	for _, doc := range docs {
		m := doc.Meta.(*model.Proposal)
		row := RetireRow{ID: doc.ID, Title: doc.Title, Status: string(m.Status), Open: []RetireItem{}}
		if m.Status == model.ProposalClosed || m.Status == model.ProposalSuperseded {
			row.Skipped = "already " + string(m.Status)
			out.Rows = append(out.Rows, row)
			continue
		}
		_, perr := model.ParseProposalBody(doc.Body)
		for _, it := range upgrade.OpenItems(doc) {
			ri := RetireItem{ID: it.ID, Kind: string(model.BlockKind(it.Kind)), Op: it.Op}
			if upgrade.IsStaged(it) {
				ri.Applied = applied.find(ctx, d, it, m.Created)
			}
			row.Open = append(row.Open, ri)
		}
		if in.AllApplied {
			if perr != nil {
				row.Skipped = "its blocks do not read: retire it by id"
			}
			for _, ri := range row.Open {
				if row.Skipped == "" && ri.Applied == "" {
					row.Skipped = "item " + ri.ID + " is open and not applied: retire it by id if it is not needed"
				}
			}
			if row.Skipped != "" {
				out.Rows = append(out.Rows, row)
				continue
			}
		}
		row.Retired = true
		if !in.DryRun {
			m.Status = model.ProposalClosed
			doc.Body = retiredBody(doc.Body, today, d.Store.Info().Actor, note, row.Open)
			if _, err := d.Store.Update(ctx, doc); err != nil {
				return out, fmt.Errorf("proposal_retire: %s: %w", doc.ID, err)
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
})))

// retiredBody is body with the ## Retired section (a retire again replaces
// it).
func retiredBody(body, today, actor, note string, open []RetireItem) string {
	if i := strings.Index(body, "\n"+retiredHeading+"\n"); i >= 0 {
		body = body[:i]
	}
	lines := []string{retiredHeading, "", fmt.Sprintf("Retired %s by %s: %s.", today, actor, strings.TrimSuffix(note, "."))}
	if len(open) > 0 {
		lines = append(lines, "", "Items that were still open:", "")
		for _, it := range open {
			switch {
			case it.Applied != "":
				lines = append(lines, fmt.Sprintf("- %s %s: applied directly, [[%s]]", it.ID, it.Op, it.Applied))
			case it.Op != "":
				lines = append(lines, fmt.Sprintf("- %s %s: not applied", it.ID, it.Op))
			default:
				lines = append(lines, fmt.Sprintf("- %s %s: not carried out", it.ID, it.Kind))
			}
		}
	}
	return strings.TrimRight(body, "\n") + "\n\n" + strings.Join(lines, "\n") + "\n"
}

// writeReceipts are the receipts of writes not undone, by op.
type writeReceipts map[string][]model.Document

func appliedWrites(ctx context.Context, d Deps) (writeReceipts, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeReceipt})
	if err != nil {
		return nil, err
	}
	out := writeReceipts{}
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if m, ok := doc.Meta.(*model.Receipt); ok && m.Undone == "" {
			out[m.Op] = append(out[m.Op], doc)
		}
	}
	return out, nil
}

// find is the receipt of a direct write, on or after the proposal was made,
// that made the staged change it: the same op on the document its input
// names (id), or for a create, on a document with its title. "" when none.
func (w writeReceipts) find(ctx context.Context, d Deps, it model.ProposalItem, since model.Date) string {
	id, _ := it.Input["id"].(string)
	title, _ := it.Input["title"].(string)
	id, title = normID(id), strings.TrimSpace(title)
	for _, rd := range w[it.Op] {
		r := rd.Meta.(*model.Receipt)
		if r.Created < since {
			continue
		}
		switch {
		case id != "":
			if r.Doc == id {
				return rd.ID
			}
		case title != "":
			if doc, err := d.Store.Get(ctx, r.Doc); err == nil && strings.EqualFold(strings.TrimSpace(doc.Title), title) {
				return rd.ID
			}
		}
	}
	return ""
}
