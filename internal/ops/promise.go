package ops

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Keeping score, gently. An obligation owed to people records how it went
// (promise): kept, closed done on or before its date; renegotiated, pushed,
// redirected, or cancelled with a notice approved or sent before its date;
// broken, its date passed with no close and no notice. Close and
// obligation_change set it; the tick keeps it in step (a notice approved
// later, a date that passed). Health names a promise past its date that
// nobody was told of. No score is kept per person.

// tickPromises sets promise on each obligation whose standing changed: open
// ones, and closed ones of the last 30 days.
func tickPromises(ctx context.Context, d Deps, out *TickOutput) error {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeObligation})
	if err != nil {
		return err
	}
	now := d.Store.Now()
	today := now.Format(dateLayout)
	since := now.AddDate(0, 0, -scoreDays).Format(dateLayout)
	get := storeGet(ctx, d.Store)
	wctx := WithWrite(ctx, WriteMeta{Why: "Tick: how a promise to people stands (kept, renegotiated, broken) changed: a notice was approved or sent, or its date passed.", Confidence: model.ConfidenceHigh})
	for _, ref := range refs {
		doc, err := d.Store.Get(ctx, ref.ID)
		if err != nil {
			return err
		}
		o, ok := doc.Meta.(*model.Obligation)
		if !ok || !promised(o) || !o.Open() && string(o.Closed) < since {
			continue
		}
		p := promiseNow(get, o, today)
		if p == o.Promise {
			continue
		}
		o.Promise = p
		if _, err := d.Store.Update(wctx, doc); err != nil {
			out.Warnings = append(out.Warnings, doc.ID+": could not record how the promise stands: "+err.Error())
			continue
		}
		out.Promises = append(out.Promises, doc.ID+": "+string(p))
	}
	slices.Sort(out.Promises)
	return nil
}

// promiseFindings adds a finding for each open promise past its date that
// nobody was told of: "tell them now".
func (h *healther) promiseFindings(ctx context.Context, s store.Store) error {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeObligation})
	if err != nil {
		return err
	}
	get := storeGet(ctx, s)
	for _, ref := range refs {
		doc, err := s.Get(ctx, ref.ID)
		if err != nil {
			return err
		}
		o, ok := doc.Meta.(*model.Obligation)
		if !ok || !o.Open() || !promised(o) || len(o.OwedBy) > 0 || attendQuiet(o.Kind, o.Optional, o.Attend) || h.deferred(o) {
			continue
		}
		date := obligationDate(o)
		if date == "" || date >= h.today || noticeBy(get, o, "") {
			continue
		}
		goal, skip := h.goalOf(h.docs[doc.ID])
		if skip {
			goal = ""
		}
		next := h.today
		if t, err := time.Parse(dateLayout, h.today); err == nil {
			next = t.AddDate(0, 0, 2).Format(dateLayout)
		}
		push := fmt.Sprintf("obligation_change {id: %s, action: push, due: %s, reason: ...}", doc.ID, next)
		if o.Kind == model.KindMeeting {
			push = fmt.Sprintf("obligation_change {id: %s, action: push, start: <new start>, reason: ...}", doc.ID)
		}
		h.add(HealthPromiseUntold, doc.ID, goal, fmt.Sprintf("was due %s and is still open, and nobody was told: tell them now", ShortDate(date)),
			push+", or {id: "+doc.ID+", action: cancel, reason: ...}: each writes the notice for the owner to approve; close it if it is done")
	}
	return nil
}
