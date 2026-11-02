package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// ReceiptView is a receipt without its text. DocName is the document's title.
type ReceiptView struct {
	ID           string             `json:"id"`
	Title        string             `json:"title"`
	Actor        string             `json:"actor"`
	Op           string             `json:"op"`
	Doc          string             `json:"doc"`
	DocName      string             `json:"doc_name,omitempty" nameof:"Doc"`
	Request      string             `json:"request,omitempty"`
	Why          string             `json:"why,omitempty"`
	Sources      []string           `json:"sources,omitempty"`
	Verify       string             `json:"verify,omitempty"`
	Confidence   model.Confidence   `json:"confidence,omitempty"`
	Check        model.ReceiptCheck `json:"check"`
	CheckedBy    string             `json:"checked_by,omitempty"`
	CheckNote    string             `json:"check_note,omitempty"`
	SelfReported bool               `json:"self_reported,omitempty"`
	NotDuplicate string             `json:"not_duplicate,omitempty"`
	Created      model.Date         `json:"created"`
}

func receiptView(d model.Document, r *model.Receipt) ReceiptView {
	return ReceiptView{ID: d.ID, Title: d.Title, Actor: r.Actor, Op: r.Op, Doc: r.Doc, Request: r.Request, Why: r.Why, Sources: r.Sources,
		Verify: r.Verify, Confidence: r.Confidence, Check: r.Check, CheckedBy: r.CheckedBy, CheckNote: r.CheckNote,
		SelfReported: r.SelfReported, NotDuplicate: r.NotDuplicate, Created: r.Created}
}

func getReceipt(ctx context.Context, s store.Store, id string) (model.Document, *model.Receipt, error) {
	doc, err := s.Get(ctx, normID(id))
	if err != nil {
		return doc, nil, err
	}
	r, ok := doc.Meta.(*model.Receipt)
	if !ok {
		return doc, nil, fmt.Errorf("%s is %s, not receipt", doc.ID, doc.Meta.Header().Type)
	}
	return doc, r, nil
}

// ReceiptListInput is the input of receipt_list. Empty fields match all.
type ReceiptListInput struct {
	Doc     string             `json:"doc,omitempty" query:"doc"`
	Request string             `json:"request,omitempty" query:"request"`
	Actor   string             `json:"actor,omitempty" query:"actor"`
	Check   model.ReceiptCheck `json:"check,omitempty" query:"check"`
	Limit   int                `json:"limit,omitempty" query:"limit"` // 0 means all
}

// ReceiptListOutput is the output of receipt_list.
type ReceiptListOutput struct {
	Items []ReceiptView `json:"items"`
}

// listReceipts reads every receipt, newest first.
func listReceipts(ctx context.Context, s store.Store) ([]ReceiptView, error) {
	refs, err := s.List(ctx, store.Filter{Folder: model.FolderReceipts, Type: model.TypeReceipt})
	if err != nil {
		return nil, err
	}
	out := make([]ReceiptView, 0, len(refs))
	for _, ref := range refs {
		doc, r, err := getReceipt(ctx, s, ref.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, receiptView(doc, r))
	}
	slices.SortStableFunc(out, func(a, b ReceiptView) int {
		if c := strings.Compare(string(b.Created), string(a.Created)); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out, nil
}

// ReceiptList lists receipts, newest first.
var ReceiptList = defineRead("receipt_list", "List receipts of agent writes, newest first, by document, request, actor, or check", func(ctx context.Context, d Deps, in ReceiptListInput) (ReceiptListOutput, error) {
	all, err := listReceipts(ctx, d.Store)
	if err != nil {
		return ReceiptListOutput{}, err
	}
	out := ReceiptListOutput{Items: []ReceiptView{}}
	for _, r := range all {
		if (in.Doc != "" && r.Doc != normID(in.Doc)) || (in.Request != "" && r.Request != normID(in.Request)) ||
			(in.Actor != "" && r.Actor != in.Actor) || (in.Check != "" && r.Check != in.Check) {
			continue
		}
		out.Items = append(out.Items, r)
		if in.Limit > 0 && len(out.Items) == in.Limit {
			break
		}
	}
	return out, nil
})

// ReceiptIDInput names one receipt.
type ReceiptIDInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// ReceiptDetail is a receipt with what changed. Reverted says whether the
// document still holds the text the write left: a revert needs that.
type ReceiptDetail struct {
	Receipt    ReceiptView `json:"receipt"`
	Lines      []string    `json:"lines"` // "- old", "+ new", and "  same" lines
	Revertible bool        `json:"revertible"`
	Rev        string      `json:"rev"`
}

// ReceiptShow shows one receipt with its diff.
var ReceiptShow = defineRead("receipt_show", "Show a receipt: what changed, why, sources, and how to verify", func(ctx context.Context, d Deps, in ReceiptIDInput) (ReceiptDetail, error) {
	doc, r, err := getReceipt(ctx, d.Store, in.ID)
	if err != nil {
		return ReceiptDetail{}, err
	}
	c, err := model.ParseReceiptBody(doc.Body)
	if err != nil {
		return ReceiptDetail{}, fmt.Errorf("%s: %w", doc.ID, err)
	}
	out := ReceiptDetail{Receipt: receiptView(doc, r), Lines: trimContext(diffLines(c.Before, c.After), 2), Rev: doc.Rev}
	for i, l := range out.Lines {
		out.Lines[i] = plainTitle(ctx, d.Store, l)
	}
	if cur, err := d.Store.Get(ctx, r.Doc); err == nil {
		out.Revertible = renderText(cur) == c.After
	}
	return out, nil
})

// ReceiptCheckInput is the input of receipt_check.
type ReceiptCheckInput struct {
	ID      string             `json:"id"`
	Verdict model.ReceiptCheck `json:"verdict"`        // verified or wrong
	Note    string             `json:"note,omitempty"` // required when wrong: what is wrong
}

// ReceiptCheckOutput is the output of receipt_check. Request is the agent work
// that a wrong opened.
type ReceiptCheckOutput struct {
	ID      string             `json:"id"`
	Check   model.ReceiptCheck `json:"check"`
	Request string             `json:"request,omitempty"`
	Rev     string             `json:"rev"`
}

// ReceiptCheck records the owner's word on a receipt. Wrong opens a response on
// the work item, so an agent picks it up.
var ReceiptCheck = definePerson("receipt_check", "Mark a receipt verified or wrong. Wrong needs a note and opens a response on the agent work. People only", func(ctx context.Context, d Deps, in ReceiptCheckInput) (ReceiptCheckOutput, error) {
	note := strings.TrimSpace(in.Note)
	if in.Verdict != model.ReceiptVerified && in.Verdict != model.ReceiptWrong {
		return ReceiptCheckOutput{}, fmt.Errorf("verdict %q: want verified or wrong", in.Verdict)
	}
	if in.Verdict == model.ReceiptWrong && note == "" {
		return ReceiptCheckOutput{}, errors.New("note is empty: say what is wrong")
	}
	doc, r, err := getReceipt(ctx, d.Store, in.ID)
	if err != nil {
		return ReceiptCheckOutput{}, err
	}
	r.Check, r.CheckedBy, r.CheckNote = in.Verdict, d.Store.Info().Actor, note
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return ReceiptCheckOutput{}, err
	}
	out := ReceiptCheckOutput{ID: saved.ID, Check: in.Verdict, Rev: saved.Rev}
	if in.Verdict == model.ReceiptWrong {
		if out.Request, err = respondToWrong(ctx, d, saved.ID, r, note); err != nil {
			return out, err
		}
	}
	return out, nil
})

// respondToWrong opens a response on the work item, or queues agent work when
// the write has no finished item to reopen.
func respondToWrong(ctx context.Context, d Deps, id string, r *model.Receipt, note string) (string, error) {
	text := fmt.Sprintf("Receipt %s is marked wrong: %s", id, note)
	if r.Request != "" {
		if doc, _, err := respondTo(ctx, d, RequestRespondInput{ID: r.Request, Kind: RespondChange, Text: text}); err == nil {
			return doc.ID, nil
		}
	}
	ask := fmt.Sprintf("Receipt %s is marked wrong: %s\n\nRead it with receipt_show. Fix the document %s, or revert the write with receipt_revert.", id, note, r.Doc)
	out, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: "Fix a write marked wrong", Ask: ask})
	return out.ID, err
}

// ReceiptRevertInput is the input of receipt_revert.
type ReceiptRevertInput struct {
	ID string `json:"id"`
}

// ReceiptRevertOutput is the output of receipt_revert.
type ReceiptRevertOutput struct {
	Doc string `json:"doc"`
	Rev string `json:"rev"`
}

// ReceiptRevert puts a document back to the text in a receipt. It needs no git.
// A document the write created moves to the archive: nothing is deleted. It is
// refused when the document changed since, so a revert never loses later work.
// Undoing an op that took review is a review too: an agent proposes it.
var ReceiptRevert = defineSafe("receipt_revert", "Put a document back to the text before a write, from its receipt. Refused if the document changed since. Reverting a review op from an agent needs a proposal", func(ctx context.Context, d Deps, in ReceiptRevertInput) (ReceiptRevertOutput, error) {
	doc, r, err := getReceipt(ctx, d.Store, in.ID)
	if err != nil {
		return ReceiptRevertOutput{}, err
	}
	c, err := model.ParseReceiptBody(doc.Body)
	if err != nil {
		return ReceiptRevertOutput{}, fmt.Errorf("%s: %w", doc.ID, err)
	}
	if e, ok := registry[r.Op]; ok && e.Risk() == RiskReview && agentSurface(d) {
		return ReceiptRevertOutput{}, fmt.Errorf("reverting %s: %w", r.Op, ErrNeedsProposal)
	}
	cur, err := d.Store.Get(ctx, r.Doc)
	if err != nil {
		return ReceiptRevertOutput{}, err
	}
	if renderText(cur) != c.After {
		return ReceiptRevertOutput{}, fmt.Errorf("%s changed since this write: fix it by hand, or revert the later receipt first", r.Doc)
	}
	if c.Before == "" {
		moved, err := d.Store.Move(ctx, r.Doc, model.FolderArchive)
		if err != nil {
			return ReceiptRevertOutput{}, err
		}
		return ReceiptRevertOutput{Doc: moved.ID, Rev: moved.Rev}, nil
	}
	old, err := model.ParseDocument(r.Doc, cur.Folder, c.Before)
	if err != nil {
		return ReceiptRevertOutput{}, fmt.Errorf("%s: %w", doc.ID, err)
	}
	old.Rev = cur.Rev
	saved, err := d.Store.Update(ctx, old)
	if err != nil {
		return ReceiptRevertOutput{}, err
	}
	if c.Folder != "" && c.Folder != saved.Folder {
		if saved, err = d.Store.Move(ctx, r.Doc, c.Folder); err != nil {
			return ReceiptRevertOutput{}, err
		}
	}
	return ReceiptRevertOutput{Doc: saved.ID, Rev: saved.Rev}, nil
})
