package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/recur"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/upgrade"
)

// Area kinds. An area of kind project is an ongoing body of work that goals
// call home (a goal's area). An area of kind concept is a discipline that spans
// work; goals and project areas draw on it through concepts. Conventions and
// constraints may refine a more general one: the rules in effect for an item
// are found along its scope chain, and a refined rule gives way to the rule
// that refines it.

// conceptLinks turns area ids into links and checks each is a concept area.
func conceptLinks(ctx context.Context, s store.Store, ids []string) ([]model.Link, error) {
	var out []model.Link
	for _, raw := range splitList(ids) {
		doc, a, err := getArea(ctx, s, raw)
		if err != nil {
			return nil, fmt.Errorf("concepts: %w", err)
		}
		if a.Kind.Or() != model.AreaKindConcept {
			return nil, fmt.Errorf("concepts: %s is an area of kind %s: want kind concept", doc.ID, a.Kind.Or())
		}
		if !slices.Contains(out, model.LinkTo(doc.ID)) {
			out = append(out, model.LinkTo(doc.ID))
		}
	}
	return out, nil
}

// checkKindLinks checks what lint checks across the vault, for one document
// about to be written: a goal's home is a project area, concepts name concept
// areas, and refines names a document of the same type with no cycle.
func checkKindLinks(ctx context.Context, s store.Store, doc model.Document) error {
	area := func(field string, l model.Link, want model.AreaKind) error {
		ad, a, err := getArea(ctx, s, l.Target())
		if err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
		if a.Kind.Or() != want {
			return fmt.Errorf("%s: %s is an area of kind %s: want kind %s", field, ad.ID, a.Kind.Or(), want)
		}
		return nil
	}
	concepts := func(ls []model.Link) error {
		for _, l := range ls {
			if err := area("concepts", l, model.AreaKindConcept); err != nil {
				return err
			}
		}
		return nil
	}
	switch m := doc.Meta.(type) {
	case *model.Project:
		if m.Area != "" {
			if err := area("area", m.Area, model.AreaKindProject); err != nil {
				return err
			}
		}
		return concepts(m.Concepts)
	case *model.Area:
		if m.Kind.Or() == model.AreaKindConcept && len(m.Concepts) > 0 {
			return errors.New("concepts: a concept area draws on no concepts; only a project area has them")
		}
		if m.Cadence != "" {
			if _, err := recur.Parse(m.Cadence); err != nil {
				return fmt.Errorf("cadence: %w", err)
			}
		}
		if err := concepts(m.Concepts); err != nil {
			return err
		}
		return checkAreaUsers(ctx, s, doc.ID, m.Kind.Or())
	case *model.Convention:
		return checkRefines(ctx, s, doc.ID, model.TypeConvention, m.Refines)
	case *model.Constraint:
		return checkRefines(ctx, s, doc.ID, model.TypeConstraint, m.Refines)
	}
	return nil
}

// checkAreaUsers refuses a kind that the documents linking to the area do not
// allow: a goal's home must stay a project area, and a concepts entry must stay
// a concept area.
func checkAreaUsers(ctx context.Context, s store.Store, id string, kind model.AreaKind) error {
	for _, typ := range []model.Type{model.TypeProject, model.TypeArea} {
		refs, err := s.List(ctx, store.Filter{Type: typ})
		if err != nil {
			return err
		}
		for _, r := range refs {
			if r.ID == id {
				continue
			}
			d, err := s.Get(ctx, r.ID)
			if err != nil {
				return err
			}
			var home model.Link
			var cs []model.Link
			switch m := d.Meta.(type) {
			case *model.Project:
				home, cs = m.Area, m.Concepts
			case *model.Area:
				cs = m.Concepts
			}
			if kind == model.AreaKindConcept && home.Target() == id {
				return fmt.Errorf("kind: goal %s calls %s home, and a home is a project area", d.ID, id)
			}
			if kind == model.AreaKindProject && slices.Contains(targets(cs), id) {
				return fmt.Errorf("kind: %s lists %s in concepts, and a concept is a concept area", d.ID, id)
			}
		}
	}
	return nil
}

// refinesOf is the refines link of a convention or a constraint.
func refinesOf(m model.Meta) model.Link {
	switch x := m.(type) {
	case *model.Convention:
		return x.Refines
	case *model.Constraint:
		return x.Refines
	}
	return ""
}

func checkRefines(ctx context.Context, s store.Store, id string, typ model.Type, l model.Link) error {
	seen := map[string]bool{id: true}
	for cur := l; cur != ""; {
		dst, err := s.Get(ctx, cur.Target())
		if err != nil {
			return fmt.Errorf("refines: %w", err)
		}
		if t := dst.Meta.Header().Type; t != typ {
			return fmt.Errorf("refines: %s is a %s: want a %s", dst.ID, t, typ)
		}
		if seen[dst.ID] {
			return fmt.Errorf("refines: %s: the refines chain is a cycle", l.Target())
		}
		seen[dst.ID] = true
		cur = refinesOf(dst.Meta)
	}
	return nil
}

// refinesLink returns the link to the convention or constraint a new one
// refines; an empty id gives no link.
func refinesLink(ctx context.Context, s store.Store, raw string, typ model.Type) (model.Link, error) {
	if normID(raw) == "" {
		return "", nil
	}
	// An annotation of the same kind in an area, by its id (no link: it is
	// not a file).
	if id := normID(raw); paramd.ValidAnnotationID(id) {
		refs, err := s.List(ctx, store.Filter{Type: model.TypeArea})
		if err != nil {
			return "", err
		}
		for _, r := range refs {
			doc, err := s.Get(ctx, r.ID)
			if err != nil {
				return "", err
			}
			for _, c := range paramd.Callouts(doc.Body) {
				if c.ID == id {
					if c.Kind != string(typ) {
						return "", fmt.Errorf("refines: %s is a %s: want a %s", id, c.Kind, typ)
					}
					return model.Link(id), nil
				}
			}
		}
		return "", fmt.Errorf("refines: no annotation %s in an area", id)
	}
	doc, err := s.Get(ctx, normID(raw))
	if err != nil {
		return "", fmt.Errorf("refines: %w", err)
	}
	if t := doc.Meta.Header().Type; t != typ {
		return "", fmt.Errorf("refines: %s is a %s: want a %s", doc.ID, t, typ)
	}
	return model.LinkTo(doc.ID), nil
}

// AreaReviewInput is the input of area_review.
type AreaReviewInput struct {
	ID   string `json:"id"`
	Note string `json:"note,omitempty"` // one line, added under ## Reviews
}

// AreaReviewOutput is the output of area_review.
type AreaReviewOutput struct {
	ID           string     `json:"id"`
	LastReviewed model.Date `json:"last_reviewed"`
	// NextReview is the date one cadence later, empty when the area has none.
	NextReview string `json:"next_review,omitempty"`
	Rev        string `json:"rev"`
}

// AreaReview records that the owner reviewed an area today.
var AreaReview = defineSafe("area_review", "Record that the owner reviewed an area today: sets last_reviewed, with an optional one-line note", func(ctx context.Context, d Deps, in AreaReviewInput) (AreaReviewOutput, error) {
	doc, a, err := getArea(ctx, d.Store, in.ID)
	if err != nil {
		return AreaReviewOutput{}, err
	}
	today := d.Store.Now().Format(dateLayout)
	a.LastReviewed = model.Date(today)
	if note := strings.Join(strings.Fields(in.Note), " "); note != "" {
		if err := lintText("note", note); err != nil {
			return AreaReviewOutput{}, err
		}
		line := "- " + today + ": " + note
		if strings.Contains("\n"+doc.Body, "\n## Reviews\n") {
			doc.Body = strings.TrimRight(doc.Body, "\n") + "\n" + line
		} else {
			doc.Body = appendSection(doc.Body, "Reviews", line)
		}
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return AreaReviewOutput{}, err
	}
	return AreaReviewOutput{ID: saved.ID, LastReviewed: a.LastReviewed, NextReview: nextReview(a), Rev: saved.Rev}, nil
})

// nextReview is the date an area is due for review: one cadence after the last
// review. Empty when the area has no readable cadence or was never reviewed.
func nextReview(a *model.Area) string {
	rule, err := recur.Parse(a.Cadence)
	if a.Cadence == "" || err != nil {
		return ""
	}
	last, err := time.Parse(dateLayout, string(a.LastReviewed))
	if err != nil {
		return ""
	}
	return rule.After(last).Format(dateLayout)
}

// areaNeglected reports whether an active area with a cadence is past its
// review: never reviewed, or reviewed more than one cadence before today.
func areaNeglected(a *model.Area, today string) bool {
	if a.Status != model.AreaActive || a.Cadence == "" {
		return false
	}
	if _, err := recur.Parse(a.Cadence); err != nil {
		return false
	}
	next := nextReview(a)
	return next == "" || next < today
}

// RuleRef is a rule in effect, short: what an item view carries.
type RuleRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Via   string `json:"via"` // the scope it came through
}

// RuleInEffect is one convention or constraint in effect for an item.
type RuleInEffect struct {
	ID    string     `json:"id"`
	Type  model.Type `json:"type"` // convention or constraint
	Title string     `json:"title"`
	// Via is the scope in the chain that its applies_in names, most specific
	// first. Refines is the rule it specializes, if any.
	Via     string `json:"via"`
	Refines string `json:"refines,omitempty"`
}

// RulesInEffectInput is the input of rules_in_effect.
type RulesInEffectInput struct {
	ID string `json:"id" query:"id" required:"true"` // a task, goal, or area
}

// RulesInEffectOutput is the output of rules_in_effect.
type RulesInEffectOutput struct {
	ID string `json:"id"`
	// Scopes is the chain, most specific first: the item, its goal or area,
	// the goal's home area, the goal's concepts, the home area's concepts.
	Scopes []string       `json:"scopes"`
	Rules  []RuleInEffect `json:"rules"`
}

// RulesInEffect lists the adopted conventions and the constraints that apply
// to an item through its scope chain.
var RulesInEffect = defineRead("rules_in_effect", "List the conventions (adopted) and constraints in effect for a task, goal, or area: those whose applies_in names the item, its goal, the goal's area, or their concept areas, and whose not_in names none. A rule refined by another in the list is left out", func(ctx context.Context, d Deps, in RulesInEffectInput) (RulesInEffectOutput, error) {
	id := normID(in.ID)
	scopes, err := scopeChain(ctx, d.Store, id)
	if err != nil {
		return RulesInEffectOutput{}, err
	}
	rules, err := rulesFor(ctx, d.Store, scopes)
	if err != nil {
		return RulesInEffectOutput{}, err
	}
	return RulesInEffectOutput{ID: id, Scopes: scopes, Rules: rules}, nil
})

// scopeChain returns the scopes of an item, most specific first, without
// repeats. A scope that does not exist ends that branch quietly.
func scopeChain(ctx context.Context, s store.Store, id string) ([]string, error) {
	doc, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var chain []string
	add := func(ids ...string) {
		for _, x := range ids {
			if x != "" && !slices.Contains(chain, x) {
				chain = append(chain, x)
			}
		}
	}
	get := func(id string) model.Meta {
		if id == "" {
			return nil
		}
		if d, err := s.Get(ctx, id); err == nil {
			return d.Meta
		}
		return nil
	}
	add(doc.ID)
	var goal *model.Project
	var home string
	switch m := obligationShape(doc).Meta.(type) {
	case *model.Task:
		if p, ok := get(m.Project.Target()).(*model.Project); ok {
			add(m.Project.Target())
			goal, home = p, p.Area.Target()
		} else {
			home = m.Area.Target()
		}
	case *model.Meeting:
	case *model.Project:
		goal, home = m, m.Area.Target()
	case *model.Area:
		home = doc.ID
	default:
		return nil, fmt.Errorf("%s is a %s: want a task, goal, or area", doc.ID, doc.Meta.Header().Type)
	}
	homeArea, _ := get(home).(*model.Area)
	if homeArea != nil {
		add(home)
	}
	if goal != nil {
		add(targets(goal.Concepts)...)
	}
	if homeArea != nil {
		add(targets(homeArea.Concepts)...)
	}
	return chain, nil
}

// scopeNames returns the ids a list of applies_in or not_in entries names: a
// wikilink names its target, a bare word itself.
func scopeNames(entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if n := strings.ToLower(normID(e)); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// firstScope returns the first scope of the chain that names lists, or "".
func firstScope(chain, names []string) string {
	for _, sc := range chain {
		if slices.Contains(names, strings.ToLower(sc)) {
			return sc
		}
	}
	return ""
}

// rulesFor returns the rules in effect along a scope chain, minus every rule
// refined, directly or not, by another rule in the result. Order: by scope,
// most specific first, then conventions before constraints, then id.
func rulesFor(ctx context.Context, s store.Store, chain []string) ([]RuleInEffect, error) {
	all := map[string]model.Document{}
	var found []RuleInEffect
	for _, typ := range []model.Type{model.TypeConvention, model.TypeConstraint} {
		refs, err := s.List(ctx, store.Filter{Type: typ})
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			doc, err := s.Get(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			all[doc.ID] = doc
			via := ""
			switch m := doc.Meta.(type) {
			case *model.Convention:
				if m.Status == model.ConventionAdopted {
					via = firstScope(chain, scopeNames(m.AppliesIn))
				}
			case *model.Constraint:
				if firstScope(chain, scopeNames(m.NotIn)) == "" {
					via = firstScope(chain, scopeNames(m.AppliesIn))
				}
			}
			if via != "" {
				found = append(found, RuleInEffect{ID: doc.ID, Type: typ, Title: doc.Title, Via: via, Refines: refinesOf(doc.Meta).Target()})
			}
		}
	}
	// Conventions and constraints are annotations in areas at the policy docs
	// format: one holds in its area and in the scopes its applies_in names.
	arefs, err := s.List(ctx, store.Filter{Type: model.TypeArea})
	if err != nil {
		return nil, err
	}
	for _, r := range arefs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range paramd.Callouts(doc.Body) {
			if c.Broken != "" || c.Kind != "convention" && c.Kind != "constraint" {
				continue
			}
			names := scopeNames(append(splitList([]string{c.Field("applies_in")}), doc.ID))
			via := ""
			switch {
			case c.Kind == "convention" && c.Field("status") != "" && c.Field("status") != string(model.ConventionAdopted):
			case c.Kind == "constraint" && firstScope(chain, scopeNames(splitList([]string{c.Field("not_in")}))) != "":
			default:
				via = firstScope(chain, names)
			}
			if via == "" {
				continue
			}
			// A folded one keeps the id of its file, so it reads as before the fold.
			title, _, _ := strings.Cut(c.Text, "\n")
			id := c.ID
			if f := model.Link(c.Field(upgrade.FromField)).Target(); f != "" {
				id = f
			}
			found = append(found, RuleInEffect{ID: id, Type: model.Type(c.Kind), Title: title, Via: via, Refines: model.Link(c.Field("refines")).Target()})
		}
	}
	refined := map[string]bool{}
	for _, r := range found {
		seen := map[string]bool{r.ID: true}
		for cur := r.Refines; cur != "" && !seen[cur]; {
			seen[cur] = true
			refined[cur] = true
			next, ok := all[cur]
			if !ok {
				break
			}
			cur = refinesOf(next.Meta).Target()
		}
	}
	out := []RuleInEffect{}
	for _, r := range found {
		if !refined[r.ID] {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b RuleInEffect) int {
		if c := slices.Index(chain, a.Via) - slices.Index(chain, b.Via); c != 0 {
			return c
		}
		if a.Type != b.Type {
			if a.Type == model.TypeConvention {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// rulesShort is the short list of rules in effect that an item view carries.
func rulesShort(ctx context.Context, s store.Store, id string) ([]RuleRef, error) {
	chain, err := scopeChain(ctx, s, id)
	if err != nil {
		return nil, err
	}
	rules, err := rulesFor(ctx, s, chain)
	if err != nil {
		return nil, err
	}
	out := make([]RuleRef, len(rules))
	for i, r := range rules {
		out[i] = RuleRef{ID: r.ID, Title: r.Title, Via: r.Via}
	}
	return out, nil
}
