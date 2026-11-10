package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/publish"
)

// Publish sets: a document's frontmatter publish lists the sets it belongs
// to, and para publish <set> writes a set to a folder on this machine (CLI
// only; no op writes the folder). The field decides what leaves the vault, so
// only the owner sets it: publish_set is a person's op, and a write by any
// other op that changes the field is refused on an agent surface
// (checkPublishField). A tag would not do: an agent's tag_add applies at once.

// ErrPublishOwnerOnly is an agent's write that changes a document's publish.
var ErrPublishOwnerOnly = errors.New("publish decides what para publish writes out of the vault, so only the owner sets it (para publish set); ask the owner, and do not retry")

// checkPublishField refuses a write that changes a document's publish sets.
// before is nil for a new document.
func checkPublishField(before *model.Document, after model.Document) error {
	var was []string
	if before != nil {
		was = before.Meta.Header().Publish
	}
	return checkPublishSets(after.ID, was, after)
}

// checkPublishSets is checkPublishField with the sets before the write given
// as a copy, for a caller whose write may change the document in place.
func checkPublishSets(id string, was []string, after model.Document) error {
	if !slices.Equal(was, after.Meta.Header().Publish) {
		return fmt.Errorf("%s: %w: %w", id, ErrPersonOnly, ErrPublishOwnerOnly)
	}
	return nil
}

// PublishSetInput is the input of publish_set.
type PublishSetInput struct {
	ID   string   `json:"id"`
	Sets []string `json:"sets"` // the sets the document is in; empty takes it out of every set
}

// PublishSetOutput is the output of publish_set.
type PublishSetOutput struct {
	ID      string   `json:"id"`
	Publish []string `json:"publish"`
	Rev     string   `json:"rev"`
}

// PublishSet sets the publish sets of a document. People only.
var PublishSet = definePerson("publish_set", "Set the publish sets of a document: the sets para publish writes it in, to a folder on this machine; no sets takes it out of every set. People only", func(ctx context.Context, d Deps, in PublishSetInput) (PublishSetOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return PublishSetOutput{}, linkErr("id", normID(in.ID), err)
	}
	sets := []string{}
	for _, s := range in.Sets {
		s = strings.TrimSpace(s)
		if err := publish.CheckSet(s); err != nil {
			return PublishSetOutput{}, err
		}
		if !slices.Contains(sets, s) {
			sets = append(sets, s)
		}
	}
	slices.Sort(sets)
	doc.Meta.Header().Publish = sets
	if len(sets) == 0 {
		doc.Meta.Header().Publish = nil
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return PublishSetOutput{}, err
	}
	return PublishSetOutput{ID: saved.ID, Publish: sets, Rev: saved.Rev}, nil
})

// PublishPreviewInput is the input of publish_preview.
type PublishPreviewInput struct {
	Set string `json:"set" query:"set" required:"true"`
}

// PublishPreviewFile is one file a run would write.
type PublishPreviewFile struct {
	ID    string `json:"id"` // empty for the index
	Title string `json:"title"`
	Name  string `json:"name"`
}

// PublishPreviewOutput is the output of publish_preview.
type PublishPreviewOutput struct {
	Set      string               `json:"set"`
	Marking  string               `json:"marking"`
	Files    []PublishPreviewFile `json:"files"`
	Rewrites []publish.Rewrite    `json:"rewrites"`
	// Review: people, groups, and io items the set's documents link, for the
	// owner to check before sharing.
	Review []publish.ReviewItem `json:"review"`
	// Refused: embeds and block references of documents outside the set. A
	// run with any writes nothing.
	Refused []publish.Refusal `json:"refused"`
}

// PublishPreview reports what para publish <set> would write, without content.
var PublishPreview = defineRead("publish_preview", "Preview a publish set: the files para publish would write (one per document and an index), how each wikilink is rewritten (a relative link in the set, else plain text), the people, groups, and io items the documents link (to review before sharing), and the embeds or block references of documents outside the set that refuse the run. Writes nothing", func(ctx context.Context, d Deps, in PublishPreviewInput) (PublishPreviewOutput, error) {
	p, err := publish.Build(ctx, d.Store, strings.TrimSpace(in.Set))
	if err != nil {
		return PublishPreviewOutput{}, err
	}
	out := PublishPreviewOutput{Set: p.Set, Marking: p.Marking, Files: []PublishPreviewFile{}, Rewrites: p.Rewrites, Review: p.Review, Refused: p.Refused}
	if len(p.Files) == 0 {
		return out, nil // no document is in the set: a run writes nothing
	}
	for _, f := range append(slices.Clone(p.Files), p.Index) {
		out.Files = append(out.Files, PublishPreviewFile{ID: f.ID, Title: f.Title, Name: f.Name})
	}
	return out, nil
})
