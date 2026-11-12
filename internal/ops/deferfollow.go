package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Later until an obligation starts or ends (defer until_start, until_end).
// The item stores the obligation's id (defer_until_start or defer_until_end)
// and the date in defer_until, so every reader of a deferral works as for a
// date. The date is updated on write: each op that moves or closes an
// obligation (obligation_change, obligation_schedule, meeting_schedule,
// meeting_cancel, close) calls followObligation, and tick calls followAll for
// a change made by hand. When the obligation closes, or is gone, the item
// comes back and a dated line in its notes says why.

// obligationEdge is the date an item following the obligation comes back:
// the start's day (else the due), or the day after the end (else the due,
// else the start). "" when the obligation has no such date.
func obligationEdge(o *model.Obligation, end bool) string {
	day := func(s string) string {
		if len(s) >= 10 {
			return s[:10]
		}
		return ""
	}
	if !end {
		return cmp.Or(day(o.Scheduled), string(o.Due))
	}
	e := cmp.Or(day(o.End), string(o.Due), day(o.Scheduled))
	t, err := time.Parse(dateLayout, e)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, 1).Format(dateLayout)
}

// followWords is "until <title> starts (<date>)" or "until <title> ends
// (<end date>)".
func followWords(title string, o *model.Obligation, end bool) string {
	if !end {
		return fmt.Sprintf("until %s starts (%s)", title, obligationEdge(o, false))
	}
	shown := obligationEdge(o, true)
	if t, err := time.Parse(dateLayout, shown); err == nil {
		shown = t.AddDate(0, 0, -1).Format(dateLayout)
	}
	return fmt.Sprintf("until %s ends (%s)", title, shown)
}

// followTarget checks the obligation an item is put off to follow and returns
// the date it comes back on.
func followTarget(ctx context.Context, d Deps, ids []string, id string, end bool, today string) (string, string, error) {
	doc, o, err := getObligation(ctx, d, id)
	if err != nil {
		return "", "", err
	}
	if !o.Open() {
		return "", "", fmt.Errorf("%s is closed (%s): nothing to wait for", doc.ID, o.Outcome)
	}
	for _, x := range ids {
		if normID(x) == doc.ID {
			return "", "", fmt.Errorf("%s cannot be put off until itself starts or ends", doc.ID)
		}
	}
	date := obligationEdge(o, end)
	if date == "" {
		return "", "", fmt.Errorf("%s has no start, end, or due date: put it off with until or after", doc.ID)
	}
	if date <= today {
		return "", "", fmt.Errorf("%s comes back %s, not after today (%s): put it off with until or after", doc.ID, date, today)
	}
	return date, followWords(doc.Title, o, end), nil
}

// followObligation moves the date of every open item put off until the
// obligation starts or ends, and brings them back when it is closed or gone.
func followObligation(ctx context.Context, d Deps, id string) ([]string, error) {
	refs, err := followers(ctx, d.Store, id)
	if err != nil || len(refs) == 0 {
		return nil, err
	}
	odoc, gerr := d.Store.Get(ctx, id)
	if gerr != nil && !errors.Is(gerr, model.ErrNotFound) {
		return nil, gerr
	}
	o, ok := odoc.Meta.(*model.Obligation)
	if gerr == nil && !ok {
		return nil, nil // an older meeting document: not an obligation to follow
	}
	today := d.Store.Now().Format(dateLayout)
	var changed []string
	for _, doc := range refs {
		df := deferralOf(doc.Meta)
		_, end := df.Follows()
		var why string
		switch {
		case o == nil:
			why = fmt.Sprintf("Back from later: %s is gone.", id)
		case !o.Open():
			why = fmt.Sprintf("Back from later: [[%s]] (%s) was cancelled (%s).", odoc.ID, odoc.Title, cmp.Or(string(o.Outcome), "closed"))
			if o.Outcome == model.OutcomeDone || o.Outcome == "" {
				why = fmt.Sprintf("Back from later: [[%s]] (%s) is done.", odoc.ID, odoc.Title)
			}
		default:
			date := obligationEdge(o, end)
			if date == "" || date == df.DeferUntil {
				continue
			}
			df.DeferUntil = date
		}
		if why != "" {
			held := df.IsDeferred(today)
			dropAdded(doc.Meta, df)
			*df = model.Deferral{}
			if held { // an item already back needs no note
				doc.Body = appendToSection(doc.Body, notesHeading, "- "+today+": "+why)
			}
		}
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return changed, err
		}
		changed = append(changed, doc.ID)
	}
	return changed, nil
}

// followers are the open items put off until the obligation starts or ends.
func followers(ctx context.Context, s store.Store, id string) ([]model.Document, error) {
	var out []model.Document
	for _, t := range deferrable {
		refs, err := s.List(ctx, store.Filter{Type: t})
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			doc, err := s.Get(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			df := deferralOf(doc.Meta)
			if df == nil || !openDoc(doc) {
				continue
			}
			if f, _ := df.Follows(); f == id {
				out = append(out, doc)
			}
		}
	}
	return out, nil
}

// followAll runs followObligation for every obligation an open item follows:
// the tick's pass, for a date or a close made by hand.
func followAll(ctx context.Context, d Deps) ([]string, error) {
	seen := map[string]bool{}
	var changed []string
	refs, err := d.Store.List(ctx, store.Filter{Types: deferrable})
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		df := deferralOf(doc.Meta)
		if df == nil || !openDoc(doc) {
			continue
		}
		if f, _ := df.Follows(); f != "" && !seen[f] {
			seen[f] = true
			ids, err := followObligation(ctx, d, f)
			if err != nil {
				return changed, err
			}
			changed = append(changed, ids...)
		}
	}
	slices.Sort(changed)
	return changed, nil
}

// followText is the words of an item put off until an obligation starts or
// ends, read from docs; "" when it follows none.
func followText(df *model.Deferral, get func(string) (model.Document, bool)) string {
	if df == nil {
		return ""
	}
	id, end := df.Follows()
	if id == "" {
		return ""
	}
	doc, ok := get(id)
	if !ok {
		return ""
	}
	o, ok := doc.Meta.(*model.Obligation)
	if !ok {
		return ""
	}
	return followWords(doc.Title, o, end)
}
