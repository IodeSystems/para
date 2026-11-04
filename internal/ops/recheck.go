package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Recheck. recheck collects the might-still-be-trues of a scope (assumptions
// not checked lately, constraints and exceptions whose review date has come)
// and queues one agent work item that lists them. The agent checks each with
// its own tools and records one result per item with recheck_record: holds,
// changed (a new fact), or unknown (a question).

// Kinds of RecheckItem.
const (
	RecheckAssumption = "assumption"
	RecheckConstraint = "constraint"
	RecheckException  = "exception"
)

// recheckKind is the request kind of recheck work.
const recheckKind = "recheck"

// recheckDefaultNext is the days to the next review of a constraint or an
// exception that holds, when the vault sets no recheck.days.
const recheckDefaultNext = 90

// RecheckItem is one thing to check again.
type RecheckItem struct {
	Kind     string `json:"kind"` // assumption, constraint, or exception
	ID       string `json:"id"`   // the goal, task, constraint, or exception
	Title    string `json:"title"`
	Text     string `json:"text,omitempty"` // the assumption; empty for a constraint or an exception
	Source   string `json:"source,omitempty"`
	Checked  string `json:"checked,omitempty"` // last check; empty for never
	ReviewBy string `json:"review_by,omitempty"`
	Verify   string `json:"verify"` // what to check
}

// RecheckInput is the input of recheck.
type RecheckInput struct {
	Scope     string `json:"scope,omitempty"`      // goal, area, or task ids, separated by commas or spaces; empty is the whole vault
	OlderThan int    `json:"older_than,omitempty"` // days; an assumption checked fewer days ago is left out. Default recheck.days; with neither, every one
	Tag       string `json:"tag,omitempty"`        // only items on documents with this tag
	DryRun    bool   `json:"dry_run,omitempty"`    // list the items, queue nothing
}

// RecheckOutput is the output of recheck. Request is empty when nothing was
// queued: a dry run, or no items.
type RecheckOutput struct {
	Items     []RecheckItem       `json:"items"`
	OlderThan int                 `json:"older_than,omitempty"`
	Request   string              `json:"request,omitempty"`
	Status    model.RequestStatus `json:"status,omitempty"`
}

// Recheck collects the items and queues one agent work item for them.
var Recheck = defineSafe("recheck", "Collect what might no longer be true in a scope (goal, area, or task ids; empty is the vault): assumptions unchecked or checked more than older_than days ago (default recheck.days), and constraints and policy exceptions whose review date has come. Queues one agent work item listing each, to check with sources and record with recheck_record. dry_run lists only", func(ctx context.Context, d Deps, in RecheckInput) (RecheckOutput, error) {
	if in.OlderThan < 0 {
		return RecheckOutput{}, errors.New("older_than is negative")
	}
	days := in.OlderThan
	if days == 0 {
		var err error
		if days, err = recheckDays(d.Store); err != nil {
			return RecheckOutput{}, err
		}
	}
	today := d.Store.Now().Format(dateLayout)
	items, names, links, err := recheckItems(ctx, d.Store, in.Scope, strings.TrimSpace(in.Tag), daysBefore(today, days), today)
	if err != nil {
		return RecheckOutput{}, err
	}
	out := RecheckOutput{Items: items, OlderThan: days}
	if in.DryRun || len(items) == 0 {
		return out, nil
	}
	title := fmt.Sprintf("Recheck %d items in %s", len(items), names)
	if len([]rune(title)) > 120 {
		title = fmt.Sprintf("Recheck %d items", len(items))
	}
	r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: title, Ask: recheckAsk(items), Links: links, Kind: recheckKind})
	if err != nil {
		return RecheckOutput{}, err
	}
	out.Request, out.Status = r.ID, r.Status
	return out, nil
})

func recheckAsk(items []RecheckItem) string {
	var b strings.Builder
	b.WriteString("Check each item below again. Para fetches nothing: check with your own tools, stay read-only outside para, and cite a source for each result. The owner's words in a chat count as a source; a guess does not.\n\n")
	b.WriteString("For each item call `recheck_record` with its id, the assumption text when it has one, and one result:\n\n")
	b.WriteString("- holds: still true. Give the source.\n")
	b.WriteString("- changed: no longer true. Give the fact that is true now and the source. If a decision follows, give the question too.\n")
	b.WriteString("- unknown: you could not tell. Give the question to ask.\n\n")
	b.WriteString("## Items\n")
	for _, it := range items {
		b.WriteString("\n- [[" + it.ID + "]] " + it.Kind)
		if it.Text != "" {
			fmt.Fprintf(&b, ": %q", it.Text)
		}
		b.WriteString(": " + it.Verify)
	}
	return b.String()
}

// recheckHolders returns the goals and tasks whose assumptions a scope covers,
// and the ids rules are looked up for. An empty scope is every active goal and
// every open task.
func recheckHolders(ctx context.Context, s store.Store, scope string) (holders []model.Document, chains []string, names string, err error) {
	ids := strings.FieldsFunc(scope, func(r rune) bool { return r == ',' || r == ' ' })
	all, err := s.List(ctx, store.Filter{})
	if err != nil {
		return nil, nil, "", err
	}
	docs := map[string]model.Document{}
	var goalsAndTasks []model.Document
	for _, r := range all {
		if r.Type != model.TypeProject && r.Type != model.TypeTask {
			continue
		}
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, nil, "", err
		}
		docs[doc.ID] = doc
		goalsAndTasks = append(goalsAndTasks, doc)
	}
	live := func(doc model.Document) bool {
		switch m := doc.Meta.(type) {
		case *model.Project:
			return m.Status == model.ProjectActive
		case *model.Task:
			return !taskTerminal(m.Status)
		}
		return false
	}
	seen := map[string]bool{}
	add := func(doc model.Document) {
		if !seen[doc.ID] && live(doc) {
			seen[doc.ID] = true
			holders = append(holders, doc)
		}
	}
	if len(ids) == 0 {
		for _, doc := range goalsAndTasks {
			add(doc)
		}
		return holders, nil, "the vault", nil
	}
	var titles []string
	for _, raw := range ids {
		id := normID(raw)
		doc, err := s.Get(ctx, id)
		if err != nil {
			return nil, nil, "", fmt.Errorf("scope %s: %w", id, err)
		}
		titles = append(titles, doc.Title)
		chains = append(chains, doc.ID)
		switch doc.Meta.(type) {
		case *model.Task:
			add(doc)
		case *model.Project:
			add(doc)
			for _, x := range goalsAndTasks {
				if t, ok := x.Meta.(*model.Task); ok && t.Project.Target() == doc.ID {
					add(x)
				}
			}
		case *model.Area:
			goals := map[string]bool{}
			for _, x := range goalsAndTasks {
				if p, ok := x.Meta.(*model.Project); ok && p.Area.Target() == doc.ID {
					goals[x.ID] = true
					add(x)
					chains = append(chains, x.ID)
				}
			}
			for _, x := range goalsAndTasks {
				if t, ok := x.Meta.(*model.Task); ok && (t.Area.Target() == doc.ID || goals[t.Project.Target()]) {
					add(x)
				}
			}
		default:
			return nil, nil, "", fmt.Errorf("scope %s is a %s: want a goal, an area, or a task", doc.ID, doc.Meta.Header().Type)
		}
	}
	return holders, chains, strings.Join(titles, ", "), nil
}

// recheckItems collects the items of a scope. cutoff is the date before which
// a check is old; empty takes every assumption. links are the goals and tasks
// that hold an item.
func recheckItems(ctx context.Context, s store.Store, scope, tag, cutoff, today string) (items []RecheckItem, names string, links []string, err error) {
	holders, chains, names, err := recheckHolders(ctx, s, scope)
	if err != nil {
		return nil, "", nil, err
	}
	tagged := func(m model.Meta) bool { return tag == "" || slices.Contains(m.Header().Tags, tag) }
	items = []RecheckItem{}
	for _, doc := range holders {
		if !tagged(doc.Meta) {
			continue
		}
		n := len(items)
		for _, a := range assumptionsOf(doc, "") {
			if a.Checked != "" && cutoff != "" && string(a.Checked) >= cutoff {
				continue
			}
			verify := "never checked: does it still hold?"
			if a.Checked != "" {
				verify = fmt.Sprintf("last checked %s (%s): does it still hold?", a.Checked, a.Status)
			}
			items = append(items, RecheckItem{Kind: RecheckAssumption, ID: doc.ID, Title: doc.Title, Text: a.Text, Source: a.Source, Checked: string(a.Checked), Verify: verify})
		}
		if len(items) > n {
			links = append(links, doc.ID)
		}
	}
	// Rules: those in effect along the scope's chains, or every one for the vault.
	var ruleIDs []string
	if scope == "" || len(chains) == 0 {
		for _, typ := range []model.Type{model.TypeConstraint, model.TypePolicyException} {
			refs, err := s.List(ctx, store.Filter{Type: typ})
			if err != nil {
				return nil, "", nil, err
			}
			for _, r := range refs {
				ruleIDs = append(ruleIDs, r.ID)
			}
		}
	} else {
		for _, id := range chains {
			sc, err := scopeChain(ctx, s, id)
			if err != nil {
				return nil, "", nil, err
			}
			rules, err := rulesFor(ctx, s, sc)
			if err != nil {
				return nil, "", nil, err
			}
			for _, r := range rules {
				if r.Type == model.TypeConstraint && !slices.Contains(ruleIDs, r.ID) {
					ruleIDs = append(ruleIDs, r.ID)
				}
			}
		}
	}
	slices.Sort(ruleIDs)
	for _, id := range ruleIDs {
		doc, err := s.Get(ctx, id)
		if err != nil {
			return nil, "", nil, err
		}
		if !tagged(doc.Meta) {
			continue
		}
		switch m := doc.Meta.(type) {
		case *model.Constraint:
			if m.ReviewBy != "" && string(m.ReviewBy) <= today {
				items = append(items, RecheckItem{Kind: RecheckConstraint, ID: doc.ID, Title: doc.Title, Source: m.Source.URL, ReviewBy: string(m.ReviewBy),
					Verify: fmt.Sprintf("review date %s has come: does it still hold (why: %s; by: %s)?", m.ReviewBy, m.Why, m.By)})
			}
		case *model.PolicyException:
			if m.Status == model.ExceptionOpen && m.ReviewBy != "" && string(m.ReviewBy) <= today {
				items = append(items, RecheckItem{Kind: RecheckException, ID: doc.ID, Title: doc.Title, ReviewBy: string(m.ReviewBy),
					Verify: fmt.Sprintf("review date %s has come: does the exception (%s, by %s) still hold?", m.ReviewBy, m.Kind, m.By)})
			}
		}
	}
	slices.Sort(links)
	return items, names, links, nil
}

// RecheckRecordInput is the input of recheck_record.
type RecheckRecordInput struct {
	ID         string                 `json:"id"`                    // the goal, task, constraint, or exception
	Assumption string                 `json:"assumption,omitempty"`  // the assumption's text, for a goal or a task
	Result     model.AssumptionStatus `json:"result"`                // holds, changed, or unknown
	Source     string                 `json:"source,omitempty"`      // required for holds and changed; the owner's chat words count
	Fact       string                 `json:"fact,omitempty"`        // what is true now; required for changed
	Question   string                 `json:"question,omitempty"`    // the question to ask; unknown gets one by default, changed only when given
	Scope      string                 `json:"scope,omitempty"`       // where the question of a constraint or an exception goes: a goal, task, or area id; default root
	NextReview string                 `json:"next_review,omitempty"` // a constraint or an exception that holds: the new review_by; default today plus recheck.days, else 90 days
}

// RecheckRecordOutput is the output of recheck_record.
type RecheckRecordOutput struct {
	ID       string                 `json:"id"`
	Result   model.AssumptionStatus `json:"result"`
	Checked  string                 `json:"checked"`
	ReviewBy string                 `json:"review_by,omitempty"`
	Question string                 `json:"question,omitempty"` // the question made, if any
	Rev      string                 `json:"rev"`
}

// rechecksHeading is the body section of a constraint or an exception that
// holds one line per recheck.
const rechecksHeading = "## Rechecks"

// RecheckRecord records the result of checking one item again.
var RecheckRecord = define("recheck_record", "Record the result of checking one item again: an assumption of a goal or a task (give its text), a constraint, or a policy exception. holds (with source) marks it checked today, and moves a constraint's or exception's review date; changed (with the fact that is true now and its source) records the change; unknown records it and opens a question. A changed item opens a question when you give one", func(ctx context.Context, d Deps, in RecheckRecordInput) (RecheckRecordOutput, error) {
	if !slices.Contains(model.AssumptionStatuses, in.Result) {
		return RecheckRecordOutput{}, fmt.Errorf("result %q: want holds, changed, or unknown", in.Result)
	}
	source, fact, question := fold(in.Source), fold(in.Fact), fold(in.Question)
	if in.Result != model.AssumptionUnknown && source == "" {
		return RecheckRecordOutput{}, fmt.Errorf("result %s needs a source: a URL, a document id, or the owner's words in chat", in.Result)
	}
	if in.Result == model.AssumptionChanged && fact == "" {
		return RecheckRecordOutput{}, errors.New("result changed needs fact: what is true now")
	}
	for _, f := range [][2]string{{"source", source}, {"fact", fact}, {"question", question}} {
		if err := lintText(f[0], f[1]); err != nil {
			return RecheckRecordOutput{}, err
		}
	}
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return RecheckRecordOutput{}, err
	}
	today := d.Store.Now().Format(dateLayout)
	out := RecheckRecordOutput{ID: doc.ID, Result: in.Result, Checked: today}
	what := ""
	switch m := doc.Meta.(type) {
	case *model.Project, *model.Task:
		text := fold(in.Assumption)
		if text == "" {
			return RecheckRecordOutput{}, fmt.Errorf("%s is a %s: give the assumption's text", doc.ID, doc.Meta.Header().Type)
		}
		list, _ := metaAssumptions(m)
		i := slices.IndexFunc(*list, func(a model.Assumption) bool { return model.SameText(a.Text, text) })
		if i < 0 {
			j := slices.IndexFunc(bodyAssumptions(doc.Body), func(t string) bool { return model.SameText(t, text) })
			if j < 0 {
				var known []string
				for _, a := range assumptionsOf(doc, "") {
					known = append(known, fmt.Sprintf("%q", a.Text))
				}
				return RecheckRecordOutput{}, fmt.Errorf("%s has no assumption %q (it has: %s)", doc.ID, text, strings.Join(known, "; "))
			}
			// A body line moves to the frontmatter, so its check is kept; the body stays as written.
			*list = append(*list, model.Assumption{Text: bodyAssumptions(doc.Body)[j]})
			i = len(*list) - 1
		}
		a := &(*list)[i]
		a.Status, a.Checked, a.By = in.Result, model.Date(today), d.Store.Info().Actor
		if source != "" {
			a.Source = source
		}
		a.Fact = ""
		if in.Result == model.AssumptionChanged {
			a.Fact = fact
		}
		what = a.Text
	case *model.Constraint, *model.PolicyException:
		if fold(in.Assumption) != "" {
			return RecheckRecordOutput{}, fmt.Errorf("%s is a %s: it has no assumptions; leave assumption out", doc.ID, doc.Meta.Header().Type)
		}
		var review *model.Date
		switch x := m.(type) {
		case *model.Constraint:
			review = &x.ReviewBy
		case *model.PolicyException:
			review = &x.ReviewBy
		}
		if in.Result == model.AssumptionHolds {
			next, err := nextReviewDate(d.Store, today, in.NextReview)
			if err != nil {
				return RecheckRecordOutput{}, err
			}
			*review = model.Date(next)
			out.ReviewBy = next
		} else if in.NextReview != "" {
			return RecheckRecordOutput{}, errors.New("next_review is for a result of holds")
		}
		line := "- " + today + ": " + string(in.Result)
		if fact != "" {
			line += ": " + fact
		}
		if source != "" {
			line += " (source: " + source + ")"
		}
		line += " (" + d.Store.Info().Actor + ")"
		doc.Body = appendToSection(doc.Body, rechecksHeading, line)
		what = doc.Title
	default:
		return RecheckRecordOutput{}, fmt.Errorf("%s is a %s: want a goal, a task, a constraint, or a policy exception", doc.ID, doc.Meta.Header().Type)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return RecheckRecordOutput{}, err
	}
	out.Rev = saved.Rev
	if question == "" && in.Result == model.AssumptionUnknown {
		question = fmt.Sprintf("Does this still hold: %s?", what)
	}
	if question == "" {
		return out, nil
	}
	qin := QuestionAddInput{Text: question, Context: recheckContext(doc.ID, what, in.Result, fact, source)}
	if err := questionScope(ctx, d.Store, doc, in.Scope, &qin); err != nil {
		return out, err
	}
	q, err := QuestionAdd.Run(ctx, d, qin)
	if err != nil {
		return out, fmt.Errorf("the result is recorded, but the question failed: %w", err)
	}
	out.Question = q.ID
	return out, nil
})

func recheckContext(id, what string, result model.AssumptionStatus, fact, source string) string {
	s := fmt.Sprintf("Recheck of [[%s]]: %q: %s.", id, what, result)
	if fact != "" {
		s += " Now: " + fact + "."
	}
	if source != "" {
		s += " Source: " + source + "."
	}
	return s
}

// questionScope sets the scope of a recheck question: the goal or the task the
// assumption is on; for a constraint or an exception, the scope given, else
// root.
func questionScope(ctx context.Context, s store.Store, doc model.Document, scope string, in *QuestionAddInput) error {
	target := doc
	switch doc.Meta.(type) {
	case *model.Project, *model.Task:
	default:
		if normID(scope) == "" {
			in.Root = true
			return nil
		}
		var err error
		if target, err = s.Get(ctx, normID(scope)); err != nil {
			return fmt.Errorf("scope: %w", err)
		}
	}
	switch target.Meta.(type) {
	case *model.Project:
		in.Project = target.ID
	case *model.Task:
		in.Task = target.ID
	case *model.Area:
		in.Area = target.ID
	default:
		return fmt.Errorf("scope %s is a %s: want a goal, a task, or an area", target.ID, target.Meta.Header().Type)
	}
	return nil
}

// nextReviewDate returns the review date after a check that holds: given, or
// today plus recheck.days, else plus 90 days. It must be after today.
func nextReviewDate(s store.Store, today, given string) (string, error) {
	if given != "" {
		if err := checkDate("next_review", given); err != nil {
			return "", err
		}
		if given <= today {
			return "", fmt.Errorf("next_review %s is not after today", given)
		}
		return given, nil
	}
	days, err := recheckDays(s)
	if err != nil {
		return "", err
	}
	t, err := time.Parse(dateLayout, today)
	if err != nil {
		return "", err
	}
	return t.AddDate(0, 0, cmp.Or(days, recheckDefaultNext)).Format(dateLayout), nil
}
