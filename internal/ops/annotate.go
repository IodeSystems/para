package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/upgrade"
)

// Annotations are notes, exceptions, conventions, and constraints written
// inline in an area body, under the section they apply to, as Obsidian
// callouts with a para header (paramd/callout.go). An area that follows a
// remote source (follows) is what a policy was: its body is the summary,
// and an annotation may quote the source. Upgrade step policy-docs made the
// older exception, convention, and constraint files annotations.

// policyDocsOn reports whether the vault is at the policy docs format.
func policyDocsOn(d Deps) bool {
	v, err := upgrade.Current(d.Store)
	return err == nil && v >= upgrade.PolicyDocsFormat
}

// annotate routes to itself as an alias target (opalias.go).
var annotateRouted = func() bool {
	verbRoutes["annotate"] = func(_ context.Context, _ Deps, raw json.RawMessage) (verbRoute, error) {
		return verbRoute{op: Annotate, input: raw}, nil
	}
	return true
}()

// annotationKeys are the fields each kind takes; required lists those it must have.
var (
	annotationKeys = map[string][]string{
		"note":       {"by", "review_by"},
		"exception":  {"kind", "status", "by", "with", "review_by", "policy_text"},
		"convention": {"status", "applies_in", "refines"},
		"constraint": {"why", "by", "source", "applies_in", "not_in", "review_by", "refines"},
	}
	annotationRequired = map[string][]string{"exception": {"kind", "by"}, "constraint": {"why", "by"}}
)

// AnnotationField is one header field of an annotation.
type AnnotationField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// AnnotationView is one annotation of an area.
type AnnotationView struct {
	Area      string            `json:"area"`
	AreaTitle string            `json:"area_title,omitempty"`
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Heading   string            `json:"heading"`          // the section it sits under; empty at the top
	Anchor    string            `json:"anchor,omitempty"` // ^R4 when it is about a rule or block
	Fields    []AnnotationField `json:"fields"`
	Text      string            `json:"text"`
	ReviewBy  string            `json:"review_by,omitempty"`
	Quote     string            `json:"quote,omitempty"` // words of the followed source it is about
	// Lost: its block is gone from the body, or its quote is not in the
	// newest version of the source.
	Lost bool `json:"lost,omitempty"`
	// From is the file it was folded from (archive/folded), if any.
	From string `json:"from,omitempty"`
}

func annotationView(area model.Document, c paramd.Callout) AnnotationView {
	v := AnnotationView{Area: area.ID, AreaTitle: area.Title, ID: c.ID, Kind: c.Kind, Heading: c.Heading, Text: c.Text,
		Anchor: c.Field("anchor"), ReviewBy: c.Field("review_by"), Quote: c.Field("quote"),
		From: model.Link(c.Field(upgrade.FromField)).Target(), Fields: []AnnotationField{}}
	for _, f := range c.Fields {
		v.Fields = append(v.Fields, AnnotationField{Key: f.Key, Value: f.Value})
	}
	return v
}

// followedText is the newest text of an area's source, or "" with none.
func followedText(ctx context.Context, d Deps, doc model.Document, f *model.Followed) (string, int, error) {
	if f == nil || f.Source.Fidelity == model.FidelityNone {
		return "", 0, nil
	}
	tdoc, _, err := getTranscript(ctx, d, doc.ID, f)
	if errors.Is(err, model.ErrNotFound) {
		return "", 0, nil
	}
	if err != nil || tdoc == nil {
		return "", 0, err
	}
	n, err := sourceVersion(ctx, d, doc.ID)
	return transcriptText(tdoc.Body), n, err
}

// sourceVersion is the version the transcript <id>.source holds: one more
// than the versions kept before it, <id>-v<n>.source.
func sourceVersion(ctx context.Context, d Deps, id string) (int, error) {
	n := 1
	for {
		_, err := d.Store.Get(ctx, fmt.Sprintf("%s-v%d.source", id, n))
		if errors.Is(err, model.ErrNotFound) {
			return n, nil
		}
		if err != nil {
			return 0, err
		}
		n++
	}
}

// transcriptText is a transcript body without its Source line.
func transcriptText(body string) string {
	if first, rest, ok := strings.Cut(body, "\n\n"); ok && strings.HasPrefix(first, "Source: ") {
		return rest
	}
	return body
}

// annotationsOf lists the annotations of an area, with Lost set.
func annotationsOf(ctx context.Context, d Deps, doc model.Document) ([]AnnotationView, error) {
	cs := paramd.Callouts(doc.Body)
	out := []AnnotationView{}
	var text string
	if a, ok := doc.Meta.(*model.Area); ok && a.Follows != nil && slices.ContainsFunc(cs, func(c paramd.Callout) bool { return c.Field("quote") != "" }) {
		var err error
		if text, _, err = followedText(ctx, d, doc, a.Follows); err != nil {
			return nil, err
		}
	}
	for _, c := range cs {
		if c.Broken != "" {
			continue
		}
		v := annotationView(doc, c)
		if b, ok := strings.CutPrefix(v.Anchor, "^"); ok && !hasBlock(doc.Body, b) {
			v.Lost = true
		}
		if v.Quote != "" && text != "" {
			if _, _, ok := locateQuote(text, v.Quote, 0); !ok {
				v.Lost = true
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func hasBlock(body, block string) bool {
	_, err := paramd.InsertCallout(body, paramd.Anchor{Block: block}, paramd.Callout{Kind: "note", ID: "ann-probe"})
	return err == nil
}

// AnnotateInput is the input of annotate.
type AnnotateInput struct {
	Area string `json:"area"`
	// ID is an annotation of the area to change or move; empty adds one.
	ID string `json:"id,omitempty"`
	// Anchor is where it goes: a section heading, ^R4 (a rule or block id),
	// or top. Empty is the top for a new one, and where it is for a change.
	Anchor string `json:"anchor,omitempty"`
	Kind   string `json:"kind,omitempty"` // note, exception, convention, or constraint; required for a new one
	// Fields: note by, review_by; exception kind*, by*, status, with,
	// review_by, policy_text; convention status, applies_in, refines;
	// constraint why*, by*, source, applies_in, not_in, review_by, refines.
	// An empty value removes a field.
	Fields map[string]string `json:"fields,omitempty"`
	// Quote is words of the source the area follows that the annotation is
	// about, as written there; para finds them again in each new version.
	Quote string `json:"quote,omitempty"`
	Text  string `json:"text,omitempty"` // the reason; required for a new one
}

// AnnotateOutput is the output of annotate.
type AnnotateOutput struct {
	Area    string `json:"area"`
	ID      string `json:"id"`
	Heading string `json:"heading"`
	Anchor  string `json:"anchor,omitempty"`
	Rev     string `json:"rev"`
}

// Annotate adds an annotation to an area body, or changes or moves one.
var Annotate = define("annotate", "Add a note, exception, convention, or constraint to an area, inline under the section it applies to (or a rule ^R4); with id, change or move one", annotate)

func annotate(ctx context.Context, d Deps, in AnnotateInput) (AnnotateOutput, error) {
	doc, a, err := getArea(ctx, d.Store, in.Area)
	if err != nil {
		return AnnotateOutput{}, err
	}
	var c paramd.Callout
	body := doc.Body
	if in.ID != "" {
		cs := paramd.Callouts(body)
		i := slices.IndexFunc(cs, func(x paramd.Callout) bool { return x.ID == in.ID })
		if i < 0 {
			return AnnotateOutput{}, fmt.Errorf("%s has no annotation %s", doc.ID, in.ID)
		}
		c = cs[i]
		if in.Kind != "" && in.Kind != c.Kind {
			return AnnotateOutput{}, fmt.Errorf("annotation %s is a %s: the kind does not change", c.ID, c.Kind)
		}
		if strings.TrimSpace(in.Text) != "" {
			c.Text = strings.TrimSpace(in.Text)
		}
	} else {
		if _, ok := annotationKeys[in.Kind]; !ok {
			return AnnotateOutput{}, fmt.Errorf("kind %q must be one of %s", in.Kind, strings.Join(paramd.AnnotationKinds, ", "))
		}
		c = paramd.Callout{Kind: in.Kind, Text: strings.TrimSpace(in.Text)}
		if c.Text == "" {
			return AnnotateOutput{}, errors.New("text is required: what the annotation says")
		}
		taken := map[string]bool{}
		for _, x := range paramd.Callouts(body) {
			taken[x.ID] = true
		}
		c.ID = upgrade.AnnotationID(doc.ID+"\x00"+c.Text+"\x00"+timestamp(), func(id string) bool { return taken[id] })
	}
	if c, err = annotationFields(ctx, d, c, in.Fields); err != nil {
		return AnnotateOutput{}, err
	}
	if q := strings.TrimSpace(in.Quote); q != "" {
		if a.Follows == nil {
			return AnnotateOutput{}, fmt.Errorf("%s follows no source: quote needs an area with follows", doc.ID)
		}
		text, version, err := followedText(ctx, d, doc, a.Follows)
		if err != nil {
			return AnnotateOutput{}, err
		}
		if text == "" {
			return AnnotateOutput{}, fmt.Errorf("%s keeps no copy of its source: quote needs one", doc.ID)
		}
		if _, _, ok := locateQuote(text, q, 0); !ok {
			return AnnotateOutput{}, fmt.Errorf("quote not found in the newest version of %s's source", doc.ID)
		}
		c = c.Set("quote", q).Set("version", strconv.Itoa(version))
	}
	var anchor paramd.Anchor
	move := in.ID == "" || strings.TrimSpace(in.Anchor) != ""
	if move {
		anchor = paramd.ParseAnchor(in.Anchor)
		c = c.Set("anchor", "")
		if anchor.Block != "" {
			c = c.Set("anchor", "^"+anchor.Block)
		}
	}
	if err := paramd.CheckCallout(c); err != nil {
		return AnnotateOutput{}, err
	}
	switch {
	case in.ID != "" && !move:
		body, err = paramd.ReplaceCallout(body, c)
	case in.ID != "":
		if body, err = paramd.RemoveCallout(body, c.ID); err == nil {
			body, err = paramd.InsertCallout(body, anchor, c)
		}
	default:
		body, err = paramd.InsertCallout(body, anchor, c)
	}
	if err != nil {
		return AnnotateOutput{}, err
	}
	doc.Body = body
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return AnnotateOutput{}, err
	}
	out := AnnotateOutput{Area: doc.ID, ID: c.ID, Anchor: c.Field("anchor"), Rev: doc.Rev}
	for _, x := range paramd.Callouts(doc.Body) {
		if x.ID == c.ID {
			out.Heading = x.Heading
		}
	}
	return out, nil
}

// annotationFields applies set to c's fields, checked for c's kind. Links
// are stored as [[id]] and must resolve.
func annotationFields(ctx context.Context, d Deps, c paramd.Callout, set map[string]string) (paramd.Callout, error) {
	allowed := annotationKeys[c.Kind]
	// In the kind's order, so a new one reads as the upgrade step writes it.
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := slices.Index(allowed, keys[i]), slices.Index(allowed, keys[j])
		if a != b {
			return a < b
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		v := strings.TrimSpace(set[k])
		if !slices.Contains(allowed, k) {
			return c, fmt.Errorf("fields: %s takes %s, not %q", c.Kind, strings.Join(allowed, ", "), k)
		}
		if v != "" {
			var err error
			if v, err = annotationValue(ctx, d, c.Kind, k, v); err != nil {
				return c, fmt.Errorf("fields.%s: %w", k, err)
			}
		}
		c = c.Set(k, v)
	}
	for _, k := range annotationRequired[c.Kind] {
		if c.Field(k) == "" {
			return c, fmt.Errorf("fields.%s is required for %s", k, c.Kind)
		}
	}
	return c, nil
}

func annotationValue(ctx context.Context, d Deps, kind, key, v string) (string, error) {
	if strings.ContainsAny(v, "\r\n") || strings.Contains(v, strings.TrimSpace(paramd.FieldSep)) {
		return "", fmt.Errorf("must be one line without %q", strings.TrimSpace(paramd.FieldSep))
	}
	switch key {
	case "review_by":
		return v, checkDate("review_by", v)
	case "kind":
		if !slices.Contains(model.ExceptionKinds, model.ExceptionKind(v)) {
			return "", fmt.Errorf("%q must be one of %v", v, model.ExceptionKinds)
		}
	case "status":
		ok := kind == "exception" && slices.Contains(model.ExceptionStatuses, model.ExceptionStatus(v)) ||
			kind == "convention" && slices.Contains(model.ConventionStatuses, model.ConventionStatus(v))
		if !ok {
			return "", fmt.Errorf("%q is not a status of a %s", v, kind)
		}
	case "source":
		return checkURL(v)
	case "refines":
		l, err := refinesLink(ctx, d.Store, v, model.Type(kind))
		return string(l), err
	case "with":
		id := normID(v)
		if _, err := d.Store.Get(ctx, id); err != nil {
			return "", err
		}
		return string(model.LinkTo(id)), nil
	}
	return v, nil
}

// insertFold puts a fold of an old add op (exception_add, convention_add,
// constraint_add) into its area as annotate does, for a vault at the
// policy docs format. With no area of its own it goes in the area of its
// kind, made when missing, as upgrade step policy-docs does.
func insertFold(ctx context.Context, d Deps, f upgrade.Fold) (AnnotateOutput, error) {
	if f.Orphan {
		areas := storeAreas{ctx, d.Store}
		if _, err := d.Store.Get(ctx, f.Area); err == nil && areas.Area(f.Area) == "" {
			f.Area += "-folded" // the word is the id of something else
		}
		if areas.Area(f.Area) == "" {
			if _, err := d.Store.Create(ctx, model.Document{ID: f.Area, Folder: model.FolderAreas, Title: f.Title,
				Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive, Kind: model.AreaKindConcept}}); err != nil {
				return AnnotateOutput{}, err
			}
		}
	}
	fields := map[string]string{}
	for _, x := range f.Callout.Fields {
		if x.Key != "anchor" {
			fields[x.Key] = x.Value
		}
	}
	return annotate(ctx, d, AnnotateInput{Area: f.Area, Anchor: f.Anchor.String(), Kind: f.Callout.Kind, Fields: fields, Text: f.Callout.Text})
}

// storeAreas is upgrade.Areas over a store.
type storeAreas struct {
	ctx context.Context
	s   store.Store
}

func (a storeAreas) Area(id string) string {
	if id == "" {
		return ""
	}
	d, err := a.s.Get(a.ctx, id)
	if err != nil || d.Folder == model.FolderArchive {
		return ""
	}
	if _, ok := d.Meta.(*model.Area); ok {
		return d.ID
	}
	return ""
}

func (a storeAreas) HasBlock(area, block string) bool {
	d, err := a.s.Get(a.ctx, area)
	return err == nil && hasBlock(d.Body, block)
}

// AnnotationListInput is the input of annotation_list. Empty filters match all.
type AnnotationListInput struct {
	Area string `json:"area,omitempty" query:"area"`
	Kind string `json:"kind,omitempty" query:"kind"`
	// Expiring lists only exceptions, constraints, and notes not resolved
	// whose review_by is past or within this many days, soonest first.
	Expiring int `json:"expiring,omitempty" query:"expiring"`
}

// AnnotationRow is an annotation in a list, with days to its review date.
type AnnotationRow struct {
	AnnotationView `json:",inline"`
	DaysLeft       *int `json:"days_left,omitempty"` // negative: overdue
}

// AnnotationListOutput is the output of annotation_list.
type AnnotationListOutput struct {
	Annotations []AnnotationRow `json:"annotations"`
}

// AnnotationList lists annotations of every area, or of one.
var AnnotationList = defineRead("annotation_list", "List annotations of areas (notes, exceptions, conventions, constraints); expiring: those due for review within that many days", func(ctx context.Context, d Deps, in AnnotationListInput) (AnnotationListOutput, error) {
	ids := []string{normID(in.Area)}
	if in.Area == "" {
		refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeArea})
		if err != nil {
			return AnnotationListOutput{}, err
		}
		ids = ids[:0]
		for _, r := range refs {
			ids = append(ids, r.ID)
		}
	}
	base, _ := time.Parse(dateLayout, string(today()))
	out := AnnotationListOutput{Annotations: []AnnotationRow{}}
	for _, id := range ids {
		doc, _, err := getArea(ctx, d.Store, id)
		if err != nil {
			return AnnotationListOutput{}, err
		}
		vs, err := annotationsOf(ctx, d, doc)
		if err != nil {
			return AnnotationListOutput{}, err
		}
		for _, v := range vs {
			if in.Kind != "" && v.Kind != in.Kind {
				continue
			}
			row := AnnotationRow{AnnotationView: v}
			if due, err := time.Parse(dateLayout, v.ReviewBy); err == nil {
				left := int(due.Sub(base).Hours() / 24)
				row.DaysLeft = &left
			}
			if in.Expiring > 0 && (row.DaysLeft == nil || *row.DaysLeft > in.Expiring || v.Kind == "convention" ||
				annotationField(v, "status") == string(model.ExceptionResolved)) {
				continue
			}
			out.Annotations = append(out.Annotations, row)
		}
	}
	if in.Expiring > 0 {
		sort.SliceStable(out.Annotations, func(i, j int) bool { return out.Annotations[i].ReviewBy < out.Annotations[j].ReviewBy })
	}
	return out, nil
})

func annotationField(v AnnotationView, key string) string {
	for _, f := range v.Fields {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

// AreaPart is a run of the body's text, or one annotation, in body order.
type AreaPart struct {
	Text       string          `json:"text,omitempty"`
	Annotation *AnnotationView `json:"annotation,omitempty"`
}

// AreaSection is one heading of an area body and what is under it, the top first.
type AreaSection struct {
	Heading string     `json:"heading"`
	Level   int        `json:"level"`
	Parts   []AreaPart `json:"parts"`
}

// areaSections splits a body into sections, each text and annotations in order.
func areaSections(body string, anns []AnnotationView) []AreaSection {
	lines := strings.Split(body, "\n")
	byLine := map[int]paramd.Callout{}
	for _, c := range paramd.Callouts(body) {
		byLine[c.Line] = c
	}
	view := map[string]AnnotationView{}
	for _, v := range anns {
		view[v.ID] = v
	}
	var out []AreaSection
	for _, s := range paramd.Sections(body) {
		sec := AreaSection{Heading: s.Heading, Level: s.Level, Parts: []AreaPart{}}
		var text []string
		flush := func() {
			if t := strings.Trim(strings.Join(text, "\n"), "\n"); t != "" {
				sec.Parts = append(sec.Parts, AreaPart{Text: t})
			}
			text = nil
		}
		for i := s.Line + 1; i < s.End; i++ {
			c, ok := byLine[i+1]
			if !ok {
				text = append(text, lines[i])
				continue
			}
			if v, ok := view[c.ID]; ok && c.Broken == "" {
				flush()
				sec.Parts = append(sec.Parts, AreaPart{Annotation: &v})
				i = c.End - 1
				continue
			}
			text = append(text, lines[i]) // a broken callout stays text
		}
		flush()
		if s.Line >= 0 || len(sec.Parts) > 0 {
			out = append(out, sec)
		}
	}
	if out == nil {
		out = []AreaSection{}
	}
	return out
}
