package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
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
	Undone       string             `json:"undone,omitempty"` // when the write was undone, RFC 3339
	SelfReported bool               `json:"self_reported,omitempty"`
	NotDuplicate string             `json:"not_duplicate,omitempty"`
	OwnerSaid    string             `json:"owner_said,omitempty"`
	Surface      string             `json:"surface,omitempty"`
	At           string             `json:"at,omitempty"`
	Background   bool               `json:"background,omitempty"`
	Align        *model.AlignResult `json:"align,omitempty"`
	Created      model.Date         `json:"created"`
}

func receiptView(d model.Document, r *model.Receipt) ReceiptView {
	return ReceiptView{ID: d.ID, Title: d.Title, Actor: r.Actor, Op: r.Op, Doc: r.Doc, Request: r.Request, Why: r.Why, Sources: r.Sources,
		Verify: r.Verify, Confidence: r.Confidence, Check: r.Check, CheckedBy: r.CheckedBy, CheckNote: r.CheckNote, Undone: r.Undone,
		SelfReported: r.SelfReported, NotDuplicate: r.NotDuplicate, OwnerSaid: r.OwnerSaid, Surface: r.Surface, At: r.At, Background: r.Background, Align: r.Align, Created: r.Created}
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
	Op      string             `json:"op,omitempty" query:"op"`
	Since   string             `json:"since,omitempty" query:"since"` // YYYY-MM-DD: made on or after
	Until   string             `json:"until,omitempty" query:"until"` // YYYY-MM-DD: made on or before
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
		if c := strings.Compare(b.At, a.At); c != 0 && a.At != "" && b.At != "" {
			return c
		}
		if c := strings.Compare(string(b.Created), string(a.Created)); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out, nil
}

// ReceiptList lists receipts, newest first.
var ReceiptList = defineRead("receipt_list", "List receipts of agent writes, newest first, by document, request, actor, op, check, or date (since, until)", func(ctx context.Context, d Deps, in ReceiptListInput) (ReceiptListOutput, error) {
	for name, v := range map[string]string{"since": in.Since, "until": in.Until} {
		if _, err := parseDate(name, v); err != nil {
			return ReceiptListOutput{}, err
		}
	}
	all, err := listReceipts(ctx, d.Store)
	if err != nil {
		return ReceiptListOutput{}, err
	}
	out := ReceiptListOutput{Items: []ReceiptView{}}
	for _, r := range all {
		if (in.Doc != "" && r.Doc != normID(in.Doc)) || (in.Request != "" && r.Request != normID(in.Request)) ||
			(in.Actor != "" && r.Actor != in.Actor) || (in.Check != "" && r.Check != in.Check) || (in.Op != "" && r.Op != in.Op) ||
			(in.Since != "" && receiptDay(r) < in.Since) || (in.Until != "" && receiptDay(r) > in.Until) {
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
		out.Revertible = sameText(renderText(cur), c.After)
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
	ask := fmt.Sprintf("Receipt %s is marked wrong: %s\n\nRead it with receipt_show. Fix the document %s, or undo the write with receipt_undo.", id, note, r.Doc)
	out, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: "Fix a write marked wrong", Ask: ask})
	return out.ID, err
}

// ReceiptUndoInput is the input of receipt_undo: one receipt, or several,
// newest first.
type ReceiptUndoInput struct {
	ID  string   `json:"id,omitempty"`
	IDs []string `json:"ids,omitempty"`
}

// ReceiptUndone is one write that was undone.
type ReceiptUndone struct {
	Receipt string `json:"receipt"`
	Doc     string `json:"doc"`
	Rev     string `json:"rev"`
}

// ReceiptUndoOutput is the output of receipt_undo. A refusal in the middle of
// a list returns an error that names the receipt; the earlier ones stay undone.
type ReceiptUndoOutput struct {
	Undone []ReceiptUndone `json:"undone"`
}

// ReceiptUndo puts a document back to the text before a write, from its
// receipt. It needs no git. A document the write created moves to the archive:
// nothing is deleted. It is refused when the document changed since, so an
// undo never loses later work. A person or an agent may call it.
var ReceiptUndo = defineSafe("receipt_undo", "Undo agent writes from their receipts: put each document back to the text before the write. Refused, with the reason, if the document changed since; undo the later receipt first. Takes id or ids, newest first", func(ctx context.Context, d Deps, in ReceiptUndoInput) (ReceiptUndoOutput, error) {
	ids := slices.Clone(in.IDs)
	if in.ID != "" {
		ids = append([]string{in.ID}, ids...)
	}
	if len(ids) == 0 {
		return ReceiptUndoOutput{}, errors.New("give id or ids: the receipts to undo")
	}
	return undoReceipts(ctx, d, ids)
})

func undoReceipts(ctx context.Context, d Deps, ids []string) (ReceiptUndoOutput, error) {
	out := ReceiptUndoOutput{Undone: []ReceiptUndone{}}
	for _, id := range ids {
		u, err := undoReceipt(ctx, d, id)
		if err != nil {
			return out, fmt.Errorf("receipt %s: %w", normID(id), err)
		}
		out.Undone = append(out.Undone, u)
		if err := d.Store.Record(ctx, opReceiptUndo, u.Receipt, "", u.Doc, ""); err != nil {
			return out, err
		}
		if doc, r, err := getReceipt(ctx, d.Store, u.Receipt); err == nil {
			r.Undone = d.Store.Now().UTC().Format(time.RFC3339)
			if _, err := d.Store.Update(ctx, doc); err != nil {
				return out, fmt.Errorf("receipt %s: the write is undone, but the receipt was not marked: %w", u.Receipt, err)
			}
		}
	}
	return out, nil
}

// ReceiptRevert is receipt_undo for the owner in the web app: a person op,
// so a review:decide session can undo a write it reads on the receipts page.
var ReceiptRevert = definePerson("receipt_revert", "Undo one agent write from its receipt, as the owner (the receipts page). Refused when the document changed since. People only", func(ctx context.Context, d Deps, in ReceiptIDInput) (ReceiptUndoOutput, error) {
	return undoReceipts(ctx, d, []string{in.ID})
})

func undoReceipt(ctx context.Context, d Deps, id string) (ReceiptUndone, error) {
	doc, r, err := getReceipt(ctx, d.Store, id)
	if err != nil {
		return ReceiptUndone{}, err
	}
	if r.Op == opAlignRecord || r.Op == opActionRecord {
		return ReceiptUndone{}, fmt.Errorf("%s records %s: there is nothing in the vault to undo", doc.ID, r.Op)
	}
	c, err := model.ParseReceiptBody(doc.Body)
	if err != nil {
		return ReceiptUndone{}, fmt.Errorf("%s: %w", doc.ID, err)
	}
	cur, err := d.Store.Get(ctx, r.Doc)
	if err != nil {
		return ReceiptUndone{}, err
	}
	if !sameText(renderText(cur), c.After) {
		return ReceiptUndone{}, fmt.Errorf("%s changed since this write: undo the later receipt first, or fix it by hand", r.Doc)
	}
	if c.Before == "" {
		moved, err := d.Store.Move(ctx, r.Doc, model.FolderArchive)
		if err != nil {
			return ReceiptUndone{}, err
		}
		return ReceiptUndone{Receipt: doc.ID, Doc: moved.ID, Rev: moved.Rev}, nil
	}
	old, err := model.ParseDocument(r.Doc, cur.Folder, c.Before)
	if err != nil {
		return ReceiptUndone{}, fmt.Errorf("%s: %w", doc.ID, err)
	}
	old.Rev = cur.Rev
	saved, err := d.Store.Update(ctx, old)
	if err != nil {
		return ReceiptUndone{}, err
	}
	if c.Folder != "" && c.Folder != saved.Folder {
		if saved, err = d.Store.Move(ctx, r.Doc, c.Folder); err != nil {
			return ReceiptUndone{}, err
		}
	}
	return ReceiptUndone{Receipt: doc.ID, Doc: saved.ID, Rev: saved.Rev}, nil
}

// receiptDay is the local day a receipt was made: from at, else created.
func receiptDay(r ReceiptView) string {
	if t, err := time.Parse(time.RFC3339Nano, r.At); err == nil {
		return t.In(time.Local).Format(dateLayout)
	}
	return string(r.Created)
}
