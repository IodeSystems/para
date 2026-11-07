package ops

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// An open item with no reason (reason.go) becomes agent work: on each tick
// the item gets one request of kind investigate, linked to it, that asks the
// agent to find why the owner does it. It shares gap work's switch
// (gap.enabled) and daily cap (gap.daily_cap: fill-gap and reason work
// together), and is made only when tick can queue work. An item with open
// agent work of any kind gets none, and an item is asked about once: when the
// work ends and the item still has no reason, the tick does not ask again.
// needs_you and the value order do not change.

// reasonMarker is the line that marks a request as a reason check.
const reasonMarker = "Reason check: none"

var reasonMarkerRe = regexp.MustCompile(`(?m)^` + reasonMarker + `$`)

// NoReasonHint is the Ask of a reason check.
const NoReasonHint = "This item has no reason recorded. Find why the owner is doing it: the goal it leads to, who it is owed to (set requested_by), or the area it keeps up; set the link with evidence. If none fits, say so and propose dropping it; do not drop it yourself."

// needsReason reports whether an open item should have a reason: the items
// health checks for one (no_path), not put off.
func needsReason(d model.Document, today string) bool {
	if df := deferralOf(d.Meta); df != nil && df.IsDeferred(today) {
		return false
	}
	switch m := d.Meta.(type) {
	case *model.Task:
		return !taskTerminal(m.Status) && !IsOther(m.Assignee) && !meetingQuiet(m) && !needsNoPath(m.EffectiveKind()) && m.Kind != model.KindIncident
	case *model.RFX:
		return m.Status == model.RFXOpen && m.Direction == model.RFXOut
	case *model.Question:
		return m.Status == model.QuestionOpen
	}
	return false
}

// tickReasons makes the reason checks of this pass. It runs after tickGaps:
// the fill-gap work made today counts toward the cap.
func tickReasons(ctx context.Context, d Deps, out *TickOutput) error {
	on, err := d.Store.Config(GapEnabledKey)
	if err != nil {
		return err
	}
	if strings.TrimSpace(on) == "false" {
		return nil
	}
	limit, err := configInt(d.Store, GapCapKey, DefaultGapCap)
	if err != nil || limit == 0 {
		return err
	}
	if ok, err := canQueue(d, TickSurface); err != nil || !ok {
		if err == nil {
			out.notQueued()
		}
		return err
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return err
	}
	today := workOf(sn, FillGapKind).today
	open, asked := map[string]bool{}, map[string]bool{}
	day := sn.Now.Format(dateLayout)
	docs := map[string]model.Document{}
	for _, doc := range sn.Docs() {
		docs[doc.ID] = doc
		r, ok := doc.Meta.(*model.Request)
		if !ok {
			continue
		}
		check := r.Kind == InvestigateKind && reasonMarkerRe.MatchString(doc.Body)
		if t, err := time.Parse(time.RFC3339Nano, r.Queued); check && err == nil && t.In(sn.Now.Location()).Format(dateLayout) == day {
			today++
		}
		for _, l := range r.Links {
			if requestOpen(r) {
				open[l.Target()] = true
			}
			if check {
				asked[l.Target()] = true
			}
		}
	}
	g := newReasonGraph(docs)
	for _, doc := range sn.Docs() {
		if today >= limit {
			break
		}
		id := doc.ID
		if !needsReason(doc, sn.Today) || open[id] || asked[id] || len(g.reasons(id)) > 0 {
			continue
		}
		_, question := doc.Meta.(*model.Question)
		body := findInstruction(id, question, true) + "\n\n" + reasonMarker
		body = appendSection(body, "Ask", NoReasonHint)
		req, err := addRequest(ctx, d, RequestAddInput{Title: investigatePrefix + shorten(doc.Title), Ask: body, Kind: InvestigateKind, Links: []string{id}}, "")
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("reason %s: %v", id, err))
			continue
		}
		open[id], asked[id] = true, true
		today++
		out.Reasons = append(out.Reasons, req.ID)
		out.noteRequest(ctx, d, req.ID)
	}
	return nil
}
