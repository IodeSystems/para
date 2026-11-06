package ops

import (
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// RefLink is one source string of a receipt or an item, resolved for a reader.
// The stored string stays as the agent wrote it. URL is empty when nothing
// opens it; Internal marks a path inside this app. Para fetches nothing: a
// link comes from the string itself, a vault id, or a source's ref_url
// template.
type RefLink struct {
	Text     string `json:"text"`
	URL      string `json:"url,omitempty"`
	Internal bool   `json:"internal,omitempty"`
}

// refResolver resolves refs against the sources and documents of one vault.
// Lookups are memoised: a list of receipts reads each document once.
type refResolver struct {
	ctx     context.Context
	s       store.Store
	sources sourceResolver
	loaded  bool
	docs    map[string]*model.Document
}

func newRefResolver(ctx context.Context, s store.Store) *refResolver {
	return &refResolver{ctx: ctx, s: s, docs: map[string]*model.Document{}}
}

func (r *refResolver) source(name string) (model.Document, *model.SourceDoc, bool) {
	if !r.loaded {
		r.loaded = true
		docs, err := loadSources(r.ctx, r.s)
		if err != nil {
			docs = nil
		}
		r.sources = newSourceResolver(docs)
	}
	id := r.sources.resolve(name)
	if id == "" {
		return model.Document{}, nil, false
	}
	d := r.sources.byID[strings.ToLower(id)]
	return d, d.Meta.(*model.SourceDoc), true
}

func (r *refResolver) doc(id string) *model.Document {
	if d, ok := r.docs[id]; ok {
		return d
	}
	var out *model.Document
	if id != "" {
		if d, err := r.s.Get(r.ctx, id); err == nil {
			out = &d
		}
	}
	r.docs[id] = out
	return out
}

func isWebURL(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

// fillRefURL puts ref into a template. It returns "" for a template that is
// not an http(s) address with {ref} in it.
func fillRefURL(tmpl, ref string) string {
	if !isWebURL(tmpl) || !strings.Contains(tmpl, "{ref}") || ref == "" {
		return ""
	}
	return strings.ReplaceAll(tmpl, "{ref}", url.PathEscape(ref))
}

// resolve turns one stored string into a RefLink.
func (r *refResolver) resolve(raw string) RefLink {
	s := strings.TrimSpace(raw)
	if isWebURL(s) {
		return RefLink{Text: s, URL: s}
	}
	if name, ref, ok := strings.Cut(s, ":"); ok && name != "" && ref != "" && !strings.ContainsAny(name, "/# ") {
		if doc, m, found := r.source(name); found {
			if u := fillRefURL(m.RefURL, ref); u != "" {
				return RefLink{Text: s, URL: u}
			}
			if isWebURL(ref) {
				return RefLink{Text: s, URL: ref}
			}
			return RefLink{Text: s + " (" + doc.Title + ")"}
		}
		return RefLink{Text: s}
	}
	// A vault id, <id>#<item>, a wikilink, or a path.
	id, _, _ := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(s, "[["), "]]"), "#")
	id = strings.ToLower(strings.TrimSuffix(path.Base(id), ".md"))
	if d := r.doc(id); d != nil {
		return RefLink{Text: s, URL: "/doc/" + url.PathEscape(d.ID), Internal: true}
	}
	return RefLink{Text: s}
}

// resolveRefs resolves refs in order, dropping a repeat of the same string or
// the same address.
func (r *refResolver) resolveRefs(refs []string) []RefLink {
	out := []RefLink{}
	seen := map[string]bool{}
	for _, raw := range refs {
		l := r.resolve(raw)
		if l.Text == "" {
			continue
		}
		key := l.URL
		if key == "" {
			key = l.Text
		}
		if seen[key] || seen[l.Text] {
			continue
		}
		seen[key], seen[l.Text] = true, true
		out = append(out, l)
	}
	return out
}

// ResolveRefs resolves source strings against the vault.
func ResolveRefs(ctx context.Context, s store.Store, refs []string) []RefLink {
	return newRefResolver(ctx, s).resolveRefs(refs)
}

// Line prints a RefLink for text output: the text, then the address when it
// has one and the text is not already it.
func (l RefLink) Line() string {
	if l.URL == "" || l.URL == l.Text || l.Internal {
		return l.Text
	}
	return l.Text + " " + l.URL
}
