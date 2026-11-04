package ops

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Search before you create. An agent's create is refused when a likely match
// exists, unless the call says why this one is different. A person's create is
// never refused: the surfaces show a quiet hint. All reads are local.

// nearTypes maps a type to the types where the same thing may already live.
var nearTypes = map[model.Type][]model.Type{
	model.TypeTask:     {model.TypeTask, model.TypeQuestion},
	model.TypeQuestion: {model.TypeQuestion, model.TypeTask},
	model.TypeProject:  {model.TypeProject, model.TypeArea},
	model.TypeArea:     {model.TypeArea, model.TypeProject},
	model.TypeNote:     {model.TypeNote},
}

// wordOverlap is how much of the shorter title's words must match.
const wordOverlap = 0.7

// crossTypeOverlap is the bar when the match is another type (a question
// against a task): the same subject is not the same thing.
const crossTypeOverlap = 0.9

// minSharedWords is how many words titles must share. One shared word is a
// topic, not a duplicate.
const minSharedWords = 2

// Similar is one existing document that may be the same thing. Match says how it
// matched: title or words. Action is a suggestion: link, merge, or new.
type Similar struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Type   model.Type `json:"type"`
	Match  string     `json:"match"`
	Action string     `json:"action"`
}

// titleWords returns the distinct content words of a title.
func titleWords(s string) map[string]bool {
	stop := map[string]bool{"the": true, "and": true, "for": true, "with": true, "from": true, "that": true, "this": true, "into": true, "our": true, "your": true}
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) > 2 && !stop[w] {
			out[w] = true
		}
	}
	return out
}

func overlap(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return float64(n) / float64(min(len(a), len(b)))
}

// sameWords reports whether two titles share enough words to be one thing:
// at least minSharedWords, the bar for the type pair on the shorter title,
// and half of the longer title.
func sameWords(a, b map[string]bool, cross bool) bool {
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	if n < minSharedWords || float64(n)/float64(max(len(a), len(b))) < 0.5 {
		return false
	}
	bar := wordOverlap
	if cross {
		bar = crossTypeOverlap
	}
	return overlap(a, b) >= bar
}

// FindSimilar lists open documents of the type and its near types that may be
// the same thing: exact title first, then word overlap of titles, then the
// search index on the text. A closed document is not a match: it is done.
func FindSimilar(ctx context.Context, s store.Store, typ model.Type, title, text string) ([]Similar, error) {
	types, ok := nearTypes[typ]
	if !ok {
		types = []model.Type{typ}
	}
	want := strings.Join(strings.Fields(strings.ToLower(title)), " ")
	words := titleWords(title)
	found := map[string]Similar{}
	var refs []model.Ref
	for _, t := range types {
		list, err := s.List(ctx, store.Filter{Type: t})
		if err != nil {
			return nil, err
		}
		refs = append(refs, list...)
	}
	for _, r := range refs {
		if r.Folder == model.FolderArchive {
			continue
		}
		have := strings.Join(strings.Fields(strings.ToLower(r.Title)), " ")
		switch {
		case have == want:
			found[r.ID] = Similar{ID: r.ID, Title: r.Title, Type: r.Type, Match: "title", Action: "link"}
		case len(words) >= 2 && sameWords(words, titleWords(r.Title), r.Type != typ):
			found[r.ID] = Similar{ID: r.ID, Title: r.Title, Type: r.Type, Match: "words", Action: "merge"}
		}
	}
	if q := strings.TrimSpace(text); q != "" && len(found) == 0 && len(words) >= 2 {
		if hits, err := s.Search(ctx, index.Query{Text: title, Limit: 5}); err == nil {
			for _, h := range hits {
				if slices.Contains(types, model.Type(h.Kind)) && sameWords(words, titleWords(h.Title), model.Type(h.Kind) != typ) {
					found[h.ID] = Similar{ID: h.ID, Title: h.Title, Type: model.Type(h.Kind), Match: "search", Action: "merge"}
				}
			}
		}
	}
	out := make([]Similar, 0, len(found))
	for _, id := range slices.Sorted(maps.Keys(found)) {
		f := found[id]
		if closedRef(ctx, s, id) {
			continue
		}
		out = append(out, f)
	}
	slices.SortStableFunc(out, func(a, b Similar) int {
		if a.Match == b.Match {
			return 0
		}
		if a.Match == "title" {
			return -1
		}
		return 1
	})
	return out, nil
}

func closedRef(ctx context.Context, s store.Store, id string) bool {
	doc, err := s.Get(ctx, id)
	if err != nil {
		return true
	}
	if closesDoc(doc) {
		return true
	}
	if p, ok := doc.Meta.(*model.Project); ok {
		return p.Status == model.ProjectDone || p.Status == model.ProjectDropped
	}
	return false
}

// createOps names the field that titles what each agent create op makes.
var createOps = map[string]struct {
	typ   model.Type
	field string
}{
	"task_add":       {model.TypeTask, "title"},
	"project_add":    {model.TypeProject, "title"},
	"area_add":       {model.TypeArea, "title"},
	"note_add":       {model.TypeNote, "title"},
	"question_add":   {model.TypeQuestion, "text"},
	"rfx_add":        {model.TypeRFX, "ask"},
	"org_person_add": {model.TypePerson, "name"},
	"org_team_add":   {model.TypeTeam, "name"},
	"org_role_add":   {model.TypeRole, "name"},
	"org_domain_add": {model.TypeDomain, "name"},
	"org_group_add":  {model.TypeGroup, "name"},
}

// duplicateOf is the refusal of a create with a likely match. It lists the
// matches by id and title, so the agent can use them.
func duplicateOf(list []Similar) error {
	var parts []string
	for _, s := range list {
		parts = append(parts, s.ID+" ("+s.Title+")")
	}
	return fmt.Errorf("this may already exist: %s. Use it, or call again with not_duplicate and a short reason", strings.Join(parts, ", "))
}

// checkCreate refuses a create that has a likely match. reason overrides it.
func checkCreate(ctx context.Context, s store.Store, op string, input map[string]any, reason string) error {
	c, ok := createOps[op]
	if !ok || strings.TrimSpace(reason) != "" {
		return nil
	}
	text, _ := input[c.field].(string)
	title, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if title == "" {
		return nil
	}
	list, err := FindSimilar(ctx, s, c.typ, title, text)
	if err != nil {
		return err
	}
	if len(list) > 0 {
		return duplicateOf(list)
	}
	return nil
}

// SimilarFor is the quiet hint for a person's create: the likely matches of the
// document in input, or nil. It never fails a create.
func SimilarFor(ctx context.Context, s store.Store, op string, input any) []Similar {
	c, ok := createOps[op]
	if !ok {
		return nil
	}
	text, _ := inputMap(input)[c.field].(string)
	title, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if title == "" {
		return nil
	}
	list, err := FindSimilar(ctx, s, c.typ, title, text)
	if err != nil {
		return nil
	}
	return list
}

// SimilarInput is the input of similar.
type SimilarInput struct {
	Type  model.Type `json:"type" query:"type" required:"true"`
	Title string     `json:"title" query:"title" required:"true"`
}

// SimilarOutput is the output of similar.
type SimilarOutput struct {
	Items []Similar `json:"items"`
}

// SimilarOp finds documents that may be what you are about to create.
var SimilarOp = defineRead("similar", "Find open documents that may already be what you are about to create: exact title, then word overlap, across near types", func(ctx context.Context, d Deps, in SimilarInput) (SimilarOutput, error) {
	if !slices.Contains(model.Types, in.Type) {
		return SimilarOutput{}, fmt.Errorf("type %q: want one of %v", in.Type, model.Types)
	}
	list, err := FindSimilar(ctx, d.Store, in.Type, in.Title, in.Title)
	return SimilarOutput{Items: list}, err
})
