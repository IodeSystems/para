package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Query asks a Finder for documents.
type Query struct {
	Types []model.Type // match any of these types
	Terms []string     // lower case; a document matches when it contains any term
}

// Finder is the seam between route and document search. ScanFinder reads the
// vault with store.List and store.Get. The index (stream F) replaces
// DefaultFinder with a faster one that returns the same refs.
type Finder interface {
	Find(ctx context.Context, s store.Store, q Query) ([]model.Ref, error)
}

// DefaultFinder is the Finder route uses.
var DefaultFinder Finder = ScanFinder{}

// searchable is a Meta that offers frontmatter text to a Finder, such as a
// person's aliases and contact values. An index Finder must match it too.
type searchable interface{ SearchText() []string }

// ScanFinder matches terms against id, title, tags, and body of each document,
// and the SearchText of its Meta.
type ScanFinder struct{}

// Find implements Finder.
func (ScanFinder) Find(ctx context.Context, s store.Store, q Query) ([]model.Ref, error) {
	var out []model.Ref
	for _, t := range q.Types {
		refs, err := s.List(ctx, store.Filter{Type: t})
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			doc, err := s.Get(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			parts := append([]string{doc.ID, doc.Title, doc.Body}, doc.Meta.Header().Tags...)
			if st, ok := doc.Meta.(searchable); ok {
				parts = append(parts, st.SearchText()...)
			}
			hay := strings.ToLower(strings.Join(parts, "\n"))
			for _, term := range q.Terms {
				if term != "" && strings.Contains(hay, term) {
					out = append(out, r)
					break
				}
			}
		}
	}
	return out, nil
}

// RouteInput is the input of route.
type RouteInput struct {
	Query string `json:"query"` // a domain id or name, or free text
}

// RouteOutput is the output of route. Read SelfServe before asking Who.
type RouteOutput struct {
	Query string `json:"query"`
	// SelfServe lists what already exists in the domain. Check it first.
	SelfServe SelfServe `json:"self_serve"`
	// Domain is the resolved domain; nil when the query matched none or
	// several (see Candidates).
	Domain     *model.Ref  `json:"domain"`
	Candidates []model.Ref `json:"candidates"`
	Owner      *model.Ref  `json:"owner"` // the domain's owner: role, team, or person
	People     []model.Ref `json:"people"`
	Terms      []string    `json:"terms"` // what self-serve matched on
}

// SelfServe is what exists before anyone is asked.
type SelfServe struct {
	Policies    []model.Ref  `json:"policies"`
	Constraints []model.Ref  `json:"constraints"`
	Conventions []model.Ref  `json:"conventions"`
	PriorRFX    []RFXSummary `json:"prior_rfx"`
}

// Route resolves a domain to a role to a person, and lists the policies,
// constraints, and prior RFX first.
var Route = define("route", "Find who to ask, after listing what already answers it", route)

func route(ctx context.Context, d Deps, in RouteInput) (RouteOutput, error) {
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return RouteOutput{}, fmt.Errorf("query is empty")
	}
	out := RouteOutput{Query: q, Candidates: []model.Ref{}, People: []model.Ref{}}
	domains, err := d.Store.List(ctx, store.Filter{Type: model.TypeDomain})
	if err != nil {
		return RouteOutput{}, err
	}
	picked, cands := pickDomain(q, domains)
	var domainDoc model.Document
	switch {
	case picked != nil:
		out.Domain = picked
		if domainDoc, err = d.Store.Get(ctx, picked.ID); err != nil {
			return RouteOutput{}, err
		}
	default:
		out.Candidates = append(out.Candidates, cands...)
	}

	out.Terms = routeTerms(q, out.Domain)
	ss := SelfServe{Policies: []model.Ref{}, Constraints: []model.Ref{}, Conventions: []model.Ref{}, PriorRFX: []RFXSummary{}}
	find := func(t model.Type) ([]model.Ref, error) {
		refs, err := DefaultFinder.Find(ctx, d.Store, Query{Types: []model.Type{t}, Terms: out.Terms})
		if refs == nil {
			refs = []model.Ref{}
		}
		return refs, err
	}
	if ss.Policies, err = find(model.TypePolicy); err != nil {
		return RouteOutput{}, err
	}
	if ss.Constraints, err = find(model.TypeConstraint); err != nil {
		return RouteOutput{}, err
	}
	if ss.Conventions, err = find(model.TypeConvention); err != nil {
		return RouteOutput{}, err
	}
	prior, err := priorRFX(ctx, d, out.Domain, out.Terms)
	if err != nil {
		return RouteOutput{}, err
	}
	ss.PriorRFX = prior
	out.SelfServe = ss

	if dm, ok := domainDoc.Meta.(*model.Domain); ok {
		if err := resolveOwner(ctx, d, dm.Owner, &out); err != nil {
			return RouteOutput{}, err
		}
	}
	return out, nil
}

// pickDomain returns the one domain the query names, or the candidates when
// it names none or several. An exact id or title match wins; otherwise the
// domains with the most title words in the query.
func pickDomain(q string, domains []model.Ref) (*model.Ref, []model.Ref) {
	lq := strings.ToLower(q)
	for i, r := range domains {
		if sameRoute(r.ID, q) || strings.EqualFold(r.Title, q) {
			return &domains[i], nil
		}
	}
	qwords := words(lq)
	best, tied := 0, []model.Ref(nil)
	for _, r := range domains {
		score := 0
		for w := range words(strings.ToLower(r.Title + " " + domainSlug(r.ID))) {
			if qwords[w] {
				score++
			}
		}
		switch {
		case score > best:
			best, tied = score, []model.Ref{r}
		case score == best && score > 0:
			tied = append(tied, r)
		}
	}
	if len(tied) == 1 {
		return &tied[0], nil
	}
	return nil, tied
}

// words returns the distinct words of s with three or more characters.
func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if len(w) >= 3 {
			out[w] = true
		}
	}
	return out
}

// routeTerms: with a resolved domain, its slug and title; otherwise the words
// of the query.
func routeTerms(q string, domain *model.Ref) []string {
	if domain != nil {
		terms := []string{domainSlug(domain.ID)}
		if t := strings.ToLower(domain.Title); t != terms[0] {
			terms = append(terms, t)
		}
		return terms
	}
	var out []string
	for w := range words(strings.ToLower(q)) {
		out = append(out, w)
	}
	slices.Sort(out)
	return out
}

// priorRFX returns RFX routed to the domain, or matching a term.
func priorRFX(ctx context.Context, d Deps, domain *model.Ref, terms []string) ([]RFXSummary, error) {
	hits, err := DefaultFinder.Find(ctx, d.Store, Query{Types: []model.Type{model.TypeRFX}, Terms: terms})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []RFXSummary{}
	add := func(id, title string, m *model.RFX) {
		if !seen[id] {
			seen[id] = true
			out = append(out, summarize(id, title, m))
		}
	}
	for _, h := range hits {
		_, m, err := getRFX(ctx, d, h.ID)
		if err != nil {
			return nil, err
		}
		add(h.ID, h.Title, m)
	}
	if domain != nil {
		all, err := loadRFX(ctx, d)
		if err != nil {
			return nil, err
		}
		for _, s := range all {
			if sameRoute(s.Route, domain.ID) && !seen[s.ID] {
				seen[s.ID] = true
				out = append(out, s)
			}
		}
	}
	slices.SortFunc(out, func(a, b RFXSummary) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// resolveOwner fills Owner and People. A role resolves to the people who hold
// it, a team to its members, a person to themselves.
func resolveOwner(ctx context.Context, d Deps, owner model.Link, out *RouteOutput) error {
	doc, err := d.Store.Get(ctx, owner.Target())
	if err != nil {
		return fmt.Errorf("domain owner %s: %w", owner, err)
	}
	ref := model.Ref{ID: doc.ID, Folder: doc.Folder, Type: doc.Meta.Header().Type, Title: doc.Title, Rev: doc.Rev}
	out.Owner = &ref
	if ref.Type == model.TypePerson {
		out.People = append(out.People, ref)
		return nil
	}
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypePerson})
	if err != nil {
		return err
	}
	for _, r := range refs {
		pd, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return err
		}
		p := pd.Meta.(*model.Person)
		if (ref.Type == model.TypeRole && p.Role.Target() == doc.ID) || (ref.Type == model.TypeTeam && p.Team.Target() == doc.ID) {
			out.People = append(out.People, r)
		}
	}
	return nil
}
