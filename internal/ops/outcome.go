package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// TaskResolveDescription is task_resolve's tool text. Each outcome has one
// sentence on when it applies, so dropped is not the word for every close.
const TaskResolveDescription = "Close a task with an outcome. " +
	"done (default): it was finished. " +
	"partial: some of it was done; needs follow_ups or a note. " +
	"dropped: nobody needs it any more, and nobody said no; needs a note. " +
	"duplicate: another item is the same work; needs link. " +
	"superseded: another item replaced this way of doing it; needs link. " +
	"declined: someone said no to it; needs reason, declined_by (owner or other, optional declined_person), and source (a document id or URL) or date. " +
	"failed: it was tried and could not be done; needs tried and stopped_because, optional retry_after (health and agenda list it again on that day). " +
	"rerouted: another task, Ask, or an outside system holds the work now; needs to (a vault id or a URL) and reason; dependents move to a vault task or Ask, else they are released with a warning. " +
	"follow_ups are titles of new tasks in the same goal or area; dependents move to the first one. " +
	"On declined, failed, rerouted, and dropped, the output lists the open Asks that hang on this task only: declined and dropped close them, failed and rerouted leave them open for a decision."

// closeFields are the checked outcome fields of a task_resolve.
type closeFields struct {
	outcome        model.TaskOutcome
	reason         string
	declinedBy     model.DeclinedBy
	person         model.Link
	source, date   string // source: "[[id]]" or a URL
	tried, stopped string
	retryAfter     model.Date
	to             string // the id or URL as given, for messages
	toField        string // "[[id]]" or a URL: task.rerouted_to
	moveTo         string // a vault task or rfx id: dependents move to it
}

// checkClose checks the fields an outcome needs, before anything is written.
func checkClose(ctx context.Context, d Deps, id string, outcome model.TaskOutcome, in TaskResolveInput) (closeFields, error) {
	cf := closeFields{outcome: outcome, reason: flatLine(in.Reason)}
	given := map[string]bool{"declined_by": in.DeclinedBy != "", "declined_person": in.DeclinedPerson != "", "source": in.Source != "", "date": in.Date != "",
		"tried": in.Tried != "", "stopped_because": in.StoppedBecause != "", "retry_after": in.RetryAfter != "", "to": in.To != "", "reason": in.Reason != ""}
	own := map[model.TaskOutcome][]string{
		model.OutcomeDeclined: {"reason", "declined_by", "declined_person", "source", "date"},
		model.OutcomeFailed:   {"tried", "stopped_because", "retry_after"},
		model.OutcomeRerouted: {"to", "reason"},
	}
	for _, k := range []string{"declined_by", "declined_person", "source", "date", "tried", "stopped_because", "retry_after", "to", "reason"} {
		if given[k] && !slices.Contains(own[outcome], k) {
			return cf, fmt.Errorf("%s is not a field of outcome %s", k, outcome)
		}
	}
	switch outcome {
	case model.OutcomeDeclined:
		if cf.reason == "" {
			return cf, errors.New("declined needs reason: what was said no to, and why")
		}
		if !slices.Contains(model.DeclinedBys, in.DeclinedBy) {
			return cf, fmt.Errorf("declined needs declined_by: owner or other, not %q", in.DeclinedBy)
		}
		cf.declinedBy = in.DeclinedBy
		if p := normID(in.DeclinedPerson); p != "" {
			doc, err := d.Store.Get(ctx, p)
			if err != nil {
				return cf, linkErr("declined_person", p, err)
			}
			if doc.Meta.Header().Type != model.TypePerson {
				return cf, fmt.Errorf("declined_person %s is a %s, not a person", p, doc.Meta.Header().Type)
			}
			cf.person = model.LinkTo(p)
		}
		if strings.TrimSpace(in.Source) == "" && strings.TrimSpace(in.Date) == "" {
			return cf, errors.New("declined needs source (a document id or URL where the no was said) or date (YYYY-MM-DD)")
		}
		date, err := parseDate("date", in.Date)
		if err != nil {
			return cf, err
		}
		cf.date = string(date)
		if src := strings.TrimSpace(in.Source); src != "" {
			ref, _, err := vaultOrURL(ctx, d, "source", src)
			if err != nil {
				return cf, err
			}
			cf.source = ref
		}
	case model.OutcomeFailed:
		cf.tried, cf.stopped = strings.TrimSpace(in.Tried), strings.TrimSpace(in.StoppedBecause)
		if cf.tried == "" || cf.stopped == "" {
			return cf, errors.New("failed needs tried (what was tried) and stopped_because (why it stopped)")
		}
		retry, err := parseDate("retry_after", in.RetryAfter)
		if err != nil {
			return cf, err
		}
		cf.retryAfter = retry
	case model.OutcomeRerouted:
		to := strings.TrimSpace(in.To)
		if to == "" || cf.reason == "" {
			return cf, errors.New("rerouted needs to (a vault id, or the URL of an outside system) and reason")
		}
		if normID(to) == id {
			return cf, fmt.Errorf("%s cannot be rerouted to itself", id)
		}
		ref, typ, err := vaultOrURL(ctx, d, "to", to)
		if err != nil {
			return cf, err
		}
		cf.to, cf.toField = to, ref
		if typ == model.TypeTask || typ == model.TypeRFX {
			cf.moveTo = normID(to)
		}
	}
	return cf, nil
}

// vaultOrURL reads ref as a URL (http or https) or as the id of a document in
// the vault. It returns the stored form, "[[id]]" or the URL, and the type of
// a vault document.
func vaultOrURL(ctx context.Context, d Deps, name, ref string) (string, model.Type, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		u, err := checkURL(ref)
		if err != nil {
			return "", "", fmt.Errorf("%s: %w", name, err)
		}
		return u, "", nil
	}
	id := normID(ref)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return "", "", fmt.Errorf("%s: want a vault document id or an http(s) URL: %w", name, linkErr(name, id, err))
	}
	return string(model.LinkTo(id)), doc.Meta.Header().Type, nil
}

// lines are the ## Outcome lines of the outcome fields.
func (cf closeFields) lines() []string {
	var out []string
	switch cf.outcome {
	case model.OutcomeDeclined:
		who := string(cf.declinedBy)
		if cf.person != "" {
			who += " (" + string(cf.person) + ")"
		}
		line := "Declined by " + who
		if cf.date != "" {
			line += " on " + cf.date
		}
		line += ": " + cf.reason
		if cf.source != "" {
			line += "\nSource: " + cf.source
		}
		out = append(out, line)
	case model.OutcomeFailed:
		line := "Tried: " + cf.tried + "\nStopped because: " + cf.stopped
		if cf.retryAfter != "" {
			line += "\nRetry after: " + string(cf.retryAfter)
		}
		out = append(out, line)
	case model.OutcomeRerouted:
		out = append(out, "Rerouted to "+cf.toField+": "+cf.reason)
	}
	return out
}

func flatLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Ask relations and actions (AskEffect).
const (
	askBlocks        = "blocks"
	askDependsOn     = "depends_on"
	askProject       = "project"
	askClosed        = "closed"
	askNeedsDecision = "needs_decision"
)

// askRef is an open Ask that hangs on one task only.
type askRef struct{ id, title, relation string }

// asksOf finds the open Asks that hang on task id only: one the task waits on
// (depends_on, blocked_by, or the Ask's blocks) that nothing else open waits
// on; one that depends on this task alone; and one whose project names it.
func asksOf(ctx context.Context, s store.Store, id string, t *model.Task) ([]askRef, error) {
	dp, err := loadDependents(ctx, s)
	if err != nil {
		return nil, err
	}
	blockedBy := map[string][]string{} // rfx id -> open tasks that list it in blocked_by
	for _, tv := range dp.task {
		if taskTerminal(tv.Status) {
			continue
		}
		for _, b := range tv.BlockedBy {
			blockedBy[normID(b)] = append(blockedBy[normID(b)], tv.ID)
		}
	}
	refs, err := s.List(ctx, store.Filter{Type: model.TypeRFX})
	if err != nil {
		return nil, err
	}
	var out []askRef
	for _, ref := range refs {
		doc, err := s.Get(ctx, ref.ID)
		if err != nil {
			return nil, err
		}
		r, ok := doc.Meta.(*model.RFX)
		if !ok || r.Status != model.RFXOpen {
			continue
		}
		rel := ""
		blocks := targets(r.Blocks)
		waitedOn := slices.Contains(blocks, id) || slices.ContainsFunc(t.DependsOn, func(l model.Link) bool { return l.Target() == doc.ID }) ||
			slices.ContainsFunc(t.BlockedBy, func(b string) bool { return normID(b) == doc.ID })
		deps := targets(r.DependsOn)
		switch {
		case waitedOn && onlyOpen(id, blocks, dp) && onlyOpen(id, dp.by[doc.ID], dp) && onlyOpen(id, blockedBy[doc.ID], dp):
			rel = askBlocks
		case len(deps) > 0 && !slices.ContainsFunc(deps, func(x string) bool { return x != id }):
			rel = askDependsOn
		case r.Goal.Target() == id:
			rel = askProject
		}
		if rel != "" {
			out = append(out, askRef{id: doc.ID, title: doc.Title, relation: rel})
		}
	}
	return out, nil
}

// onlyOpen reports whether no open item but id is in list.
func onlyOpen(id string, list []string, dp dependents) bool {
	return !slices.ContainsFunc(list, func(x string) bool { return x != id && dp.open[x] })
}

// settleAsks closes the Asks of a declined or dropped task, with the reason,
// and lists those of a failed or rerouted task as needing a decision. Other
// outcomes touch no Ask.
func settleAsks(ctx context.Context, d Deps, id string, outcome model.TaskOutcome, why string, asks []askRef) ([]AskEffect, error) {
	out := []AskEffect{}
	closeThem := outcome == model.OutcomeDeclined || outcome == model.OutcomeDropped
	if !closeThem && outcome != model.OutcomeFailed && outcome != model.OutcomeRerouted {
		return out, nil
	}
	for _, a := range asks {
		e := AskEffect{ID: a.id, Title: a.title, Relation: a.relation, Action: askNeedsDecision}
		if closeThem {
			doc, err := d.Store.Get(ctx, a.id)
			if err != nil {
				return out, err
			}
			r, ok := doc.Meta.(*model.RFX)
			if !ok || r.Status != model.RFXOpen {
				continue
			}
			r.Status = model.RFXClosed
			doc.Body = appendToSection(doc.Body, "## Closed", fmt.Sprintf("Closed: [[%s]] was %s: %s", id, outcome, flatLine(why)))
			if _, err := d.Store.Update(ctx, doc); err != nil {
				return out, fmt.Errorf("task_resolve: %s is closed, but Ask %s is not: %w", id, a.id, err)
			}
			e.Action = askClosed
		}
		out = append(out, e)
	}
	return out, nil
}
