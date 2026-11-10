package upgrade

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
)

// PolicyDocsFormat is the format at which policies are areas that follow a
// source, and exceptions, conventions, and constraints are annotations in
// area bodies: below it ops write the older types.
const PolicyDocsFormat = 9

// FoldedSub is the folder under archive/ that step policy-docs moves the
// files it folds to. Each keeps its keys and text, plus folded_into.
const FoldedSub = "folded"

// FromField is the callout field that names the document an annotation was
// folded from: the file in archive/folded/.
const FromField = "from"

// AnnotationAtTop is the health kind of a folded annotation still at the top
// of its area: the judgement step annotation-placement selects these. The
// owner dismisses it on the area to settle it.
const AnnotationAtTop = "annotation_at_top"

// Fold is an annotation made of a document: where it goes and what it says.
type Fold struct {
	Area    string // area id
	Anchor  paramd.Anchor
	Callout paramd.Callout
	// Orphan: no area of the document's was found, so Area is the area of
	// its kind that the step makes (exceptions, conventions, constraints),
	// with Title.
	Orphan bool
	Title  string
}

// orphanArea names the area of documents of a kind with no area.
var orphanArea = map[model.Type]struct{ id, title string }{
	model.TypePolicyException: {"exceptions", "Exceptions"},
	model.TypeConvention:      {"conventions", "Conventions"},
	model.TypeConstraint:      {"constraints", "Constraints"},
}

// IsOrphanArea reports whether id names an area the step makes for documents
// of a kind with no area of their own (exceptions, or exceptions-folded).
func IsOrphanArea(id string) bool {
	for _, o := range orphanArea {
		if id == o.id || id == o.id+"-folded" {
			return true
		}
	}
	return false
}

// AnnotationID is the id of the annotation made of the document id: ann- and
// the first hex digits of its hash, longer while taken says the id is used.
func AnnotationID(docID string, taken func(string) bool) string {
	sum := sha256.Sum256([]byte(docID))
	h := hex.EncodeToString(sum[:])
	for n := 4; ; n += 2 {
		if id := "ann-" + h[:n]; n >= len(h) || !taken(id) {
			return id
		}
	}
}

// oneValue makes v a callout field value: one line, without the field
// separator. The archived file keeps the original.
func oneValue(v string) string {
	v = strings.Join(strings.Fields(v), " ")
	return strings.TrimSpace(strings.ReplaceAll(v, strings.TrimSpace(paramd.FieldSep), "-"))
}

// annotationText is the title, then the body after a blank line.
func annotationText(title, body string) string {
	title, body = strings.TrimSpace(title), strings.TrimSpace(body)
	if body == "" {
		return title
	}
	if title == "" {
		return body
	}
	return title + "\n\n" + body
}

// Areas tells a fold what the vault holds.
type Areas interface {
	// Area is the id of the area or policy named by id, or "".
	Area(id string) string
	// HasBlock reports whether the area or policy body has the block id.
	HasBlock(area, block string) bool
}

// AnnotationOf is the annotation an exception, a convention, or a constraint
// becomes, and where. An exception goes in the area its policy became, under
// its rule when the rule is still there; a convention or a constraint in the
// first area its applies_in names. With none, the area is its kind's orphan
// area. ok is false for any other type.
func AnnotationOf(d model.Document, areas Areas) (Fold, bool) {
	var f Fold
	set := func(k, v string) {
		if v = oneValue(v); v != "" {
			f.Callout = f.Callout.Set(k, v)
		}
	}
	join := func(vs []string) string { return strings.Join(vs, ", ") }
	firstArea := func(ids []string) string {
		for _, id := range ids {
			if a := areas.Area(strings.Trim(strings.TrimSpace(id), "[]")); a != "" {
				return a
			}
		}
		return ""
	}
	switch m := d.Meta.(type) {
	case *model.PolicyException:
		f.Callout.Kind = "exception"
		set("kind", string(m.Kind))
		if m.Status != model.ExceptionOpen {
			set("status", string(m.Status))
		}
		set("by", m.By)
		set("with", string(m.With))
		set("review_by", string(m.ReviewBy))
		set("policy_text", m.PolicyText)
		f.Area = areas.Area(m.Policy.Target())
		if _, rule, ok := strings.Cut(strings.TrimSuffix(string(m.Rule), "]]"), "#^"); ok && f.Area != "" && areas.HasBlock(f.Area, rule) {
			f.Anchor = paramd.Anchor{Block: rule}
			set("anchor", "^"+rule)
		}
	case *model.Convention:
		f.Callout.Kind = "convention"
		set("status", string(m.Status))
		set("applies_in", join(m.AppliesIn))
		set("refines", string(m.Refines))
		f.Area = firstArea(m.AppliesIn)
	case *model.Constraint:
		f.Callout.Kind = "constraint"
		set("why", m.Why)
		set("by", m.By)
		set("source", cmp.Or(m.Source.URL, m.Source.Verify))
		set("applies_in", join(m.AppliesIn))
		set("not_in", join(m.NotIn))
		set("review_by", string(m.ReviewBy))
		set("refines", string(m.Refines))
		f.Area = firstArea(m.AppliesIn)
	default:
		return f, false
	}
	set("tags", join(d.Meta.Header().Tags))
	if f.Area == "" {
		o := orphanArea[d.Meta.Header().Type]
		f.Area, f.Title, f.Orphan = o.id, o.title, true
	}
	f.Callout.Text = annotationText(d.Title, d.Body)
	return f, true
}

// foldsToAnnotation selects what step policy-docs folds: a policy, and an
// exception, a convention, or a constraint, outside archive/.
func foldsToAnnotation(d model.Document) bool {
	if d.Folder == model.FolderArchive {
		return false
	}
	switch d.Meta.(type) {
	case *model.Policy, *model.PolicyException, *model.Convention, *model.Constraint:
		return true
	}
	return false
}

// FoldsToAnnotation reports whether step policy-docs folds the document.
func FoldsToAnnotation(d model.Document) bool { return foldsToAnnotation(d) }

// AsFollowedArea is a policy as the area step policy-docs makes of it: kind
// concept, active, follows with the policy's source block verbatim, the body
// unchanged. Every other key stays in the file.
func AsFollowedArea(d model.Document) (model.Document, error) {
	p, ok := d.Meta.(*model.Policy)
	if !ok {
		return d, fmt.Errorf("upgrade: %s is not a policy", d.ID)
	}
	return model.Retype(d, model.TypeArea, map[string]any{
		"status": string(model.AreaActive), "kind": string(model.AreaKindConcept), "follows": p.Followed,
		"folded_from": string(model.TypePolicy), "source": nil, "verified": nil, "reviewed_by": nil,
	})
}

// vaultAreas is Areas over documents read once.
type vaultAreas map[string]model.Document

func (v vaultAreas) Area(id string) string {
	d, ok := v[id]
	if !ok {
		return ""
	}
	switch d.Meta.(type) {
	case *model.Area, *model.Policy:
		if d.Folder != model.FolderArchive {
			return d.ID
		}
	}
	return ""
}

func (v vaultAreas) HasBlock(area, block string) bool {
	d, ok := v[area]
	if !ok {
		return false
	}
	_, err := paramd.InsertCallout(d.Body, paramd.Anchor{Block: block}, paramd.Callout{Kind: "note", ID: "ann-probe"})
	return err == nil
}

// AreasOf reads the areas and policies of a store, for AnnotationOf.
func AreasOf(ctx context.Context, r Reader) (Areas, error) {
	v := vaultAreas{}
	for _, t := range []model.Type{model.TypeArea, model.TypePolicy} {
		refs, err := r.List(ctx, store.Filter{Type: t})
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			d, err := r.Get(ctx, ref.ID)
			if err != nil {
				return nil, err
			}
			v[d.ID] = d
		}
	}
	return v, nil
}

// policyDocsBatch reads the vault once and returns the rewrite of step
// policy-docs. The orphan areas it makes are made in the batch by the first
// document that folds into each. Folds go in id order, so a second run of
// the same vault gives the same files.
func policyDocsBatch(ctx context.Context, r Reader) (model.ExpandFunc, error) {
	all := map[string]model.Document{}
	var folding []model.Document
	for _, t := range []model.Type{model.TypeArea, model.TypePolicy, model.TypePolicyException, model.TypeConvention, model.TypeConstraint} {
		refs, err := r.List(ctx, store.Filter{Type: t, All: true})
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			d, err := r.Get(ctx, ref.ID)
			if err != nil {
				continue
			}
			all[d.ID] = d
			if foldsToAnnotation(d) {
				folding = append(folding, d)
			}
		}
	}
	slices.SortFunc(folding, func(a, b model.Document) int { return cmp.Compare(a.ID, b.ID) })
	areas := vaultAreas(all)
	taken := map[string]bool{}
	for _, d := range all {
		for _, c := range paramd.Callouts(d.Body) {
			taken[c.ID] = true
		}
	}
	bodies := map[string]string{} // area id -> new body
	into := map[string]string{}   // folded doc id -> area id
	var made []model.Document
	madeIdx := map[string]int{}
	maker := map[string][]string{} // folded doc id -> the orphan areas it makes
	for _, d := range folding {
		if _, ok := d.Meta.(*model.Policy); ok {
			continue
		}
		f, _ := AnnotationOf(d, areas)
		f.Callout.ID = AnnotationID(d.ID, func(id string) bool { return taken[id] })
		taken[f.Callout.ID] = true
		f.Callout = f.Callout.Set(FromField, string(model.LinkTo(d.ID)))
		if f.Orphan {
			o := orphanArea[d.Meta.Header().Type]
			if _, ok := all[o.id]; ok && areas.Area(o.id) == "" {
				f.Area = o.id + "-folded" // the word is the id of something else
			}
			if areas.Area(f.Area) == "" {
				if _, ok := madeIdx[f.Area]; !ok {
					madeIdx[f.Area] = len(made)
					maker[d.ID] = append(maker[d.ID], f.Area)
					made = append(made, model.Document{ID: f.Area, Folder: model.FolderAreas, Title: o.title,
						Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive, Kind: model.AreaKindConcept}})
				}
			}
		}
		body, ok := bodies[f.Area]
		if !ok {
			if i, isNew := madeIdx[f.Area]; isNew {
				body = made[i].Body
			} else {
				body = all[f.Area].Body
			}
		}
		nb, err := paramd.InsertCallout(body, f.Anchor, f.Callout)
		if err != nil && f.Anchor != (paramd.Anchor{}) {
			f.Callout = f.Callout.Set("anchor", "")
			nb, err = paramd.InsertCallout(body, paramd.Anchor{}, f.Callout)
		}
		if err != nil {
			return nil, fmt.Errorf("%s into %s: %w", d.ID, f.Area, err)
		}
		bodies[f.Area] = nb
		if i, isNew := madeIdx[f.Area]; isNew {
			made[i].Body = nb
		}
		into[d.ID] = f.Area
	}
	rewrite := func(d model.Document) (model.Document, []model.Document, bool, error) {
		if _, ok := bodies[d.ID]; !ok && !foldsToAnnotation(d) {
			return d, nil, false, nil
		}
		if area, ok := into[d.ID]; ok {
			nd, err := d.WithFields(map[string]any{"folded_into": string(model.LinkTo(area))})
			if err != nil {
				return d, nil, false, err
			}
			nd.Folder, nd.Sub = model.FolderArchive, FoldedSub
			var news []model.Document
			for _, id := range maker[d.ID] {
				news = append(news, made[madeIdx[id]])
			}
			return nd, news, true, nil
		}
		if _, ok := d.Meta.(*model.Policy); ok {
			nd, err := AsFollowedArea(d)
			if err != nil {
				return d, nil, false, err
			}
			d = nd
		}
		if b, ok := bodies[d.ID]; ok {
			d.Body = b
		}
		return d, nil, true, nil
	}
	return rewrite, nil
}

// placementPending selects an area with an annotation the step folded that
// is still at the top of its body, unless the owner dismissed that.
func placementPending(d model.Document) bool {
	if _, ok := d.Meta.(*model.Area); !ok {
		return false
	}
	for _, x := range d.Meta.Header().HealthDismissed {
		if x.Kind == AnnotationAtTop {
			return false
		}
	}
	return len(FoldedAtTop(d.Body)) > 0
}

// FoldedAtTop lists the annotations of a body that step policy-docs folded
// and that are at the top, above the first heading.
func FoldedAtTop(body string) []paramd.Callout {
	var out []paramd.Callout
	for _, c := range paramd.Callouts(body) {
		if c.HeadingLine == 0 && c.Broken == "" && c.Field(FromField) != "" {
			out = append(out, c)
		}
	}
	return out
}
