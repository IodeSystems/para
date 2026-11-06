package ops

import (
	"context"
	"slices"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// tickSkipWhy is the why of the receipt of an optional meeting tick skipped.
const tickSkipWhy = "Tick: an optional meeting ended and the owner made no attend choice (attend unset or maybe), so it is marked skipped (attend no). Undo the receipt to put it back; tick does not skip it again."

// tickOptionalMeetings marks each past optional meeting skipped (attend no)
// when the owner made no attend choice (attend unset or maybe) and it is
// still scheduled after its end. Each is one meeting_attend write with its
// own receipt, actor tick, which receipt_undo reverses. A meeting tick
// skipped once (a receipt of tick names it, undone or not) is left alone, so
// an undo stands. A meeting that is not optional, or one the owner answered
// yes or no, is never touched. The rule is fixed: no model decides it.
func tickOptionalMeetings(ctx context.Context, d Deps, out *TickOutput) error {
	var refs []model.Ref
	for _, typ := range []model.Type{model.TypeTask, model.TypeObligation} {
		r, err := d.Store.List(ctx, store.Filter{Type: typ})
		if err != nil {
			return err
		}
		refs = append(refs, r...)
	}
	receipts, err := listReceipts(ctx, d.Store)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, r := range receipts {
		if r.Actor == TickSurface && (r.Op == MeetingAttend.Name() || r.Op == ObligationAttend.Name()) {
			done[r.Doc] = true
		}
	}
	now := d.Store.Now()
	wctx := WithWrite(ctx, WriteMeta{Why: tickSkipWhy, Confidence: model.ConfidenceHigh})
	for _, ref := range refs {
		doc, err := d.Store.Get(ctx, ref.ID)
		if err != nil {
			return err
		}
		mt, ok := readMeeting(doc)
		if !ok || mt.occ == nil || mt.Status != model.MeetingScheduled {
			continue
		}
		if t := mt.occ; !t.Optional || (t.Attend != "" && t.Attend != model.AttendMaybe) || done[doc.ID] {
			continue
		}
		end, err := time.Parse(time.RFC3339, mt.End)
		if err != nil || end.After(now) {
			continue
		}
		if _, err := ObligationAttend.Run(wctx, d, MeetingAttendInput{ID: doc.ID, Attend: string(model.AttendNo)}); err != nil {
			out.Warnings = append(out.Warnings, doc.ID+": could not mark the past optional meeting skipped: "+err.Error())
			continue
		}
		out.MeetingsSkipped = append(out.MeetingsSkipped, doc.ID)
	}
	slices.Sort(out.MeetingsSkipped)
	return nil
}
