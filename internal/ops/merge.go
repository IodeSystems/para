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

// Merge and archive. A duplicate of any type is merged into its primary:
// links move, lists and notes join, and the alias carries duplicate_of and
// leaves every list. Archive moves a document out of every list. Both leave
// one receipt on every surface; receipt_undo reverses all of it.

// mergeRefused lists types that hold their own record and are not merged.
var mergeRefused = []model.Type{model.TypeProposal, model.TypeReceipt, model.TypeRequest, model.TypeTranscript}

// noRewrite lists types whose text is a record: a merge leaves their links.
var noRewrite = []model.Type{model.TypeReceipt, model.TypeProposal, model.TypeTranscript}

// DocMergeInput is the input of doc_merge.
type DocMergeInput struct {
	Keep  string `json:"keep"`  // the primary: it stays
	Alias string `json:"alias"` // the duplicate: merged into keep, then hidden
	Note  string `json:"note,omitempty"`
}

// DocMergeOutput is the output of doc_merge.
type DocMergeOutput struct {
	Keep  string `json:"keep"`
	Alias string `json:"alias"`
	// Rewritten are the documents whose links to alias now point to keep.
	Rewritten []string `json:"rewritten"`
	// Joined are the list fields of keep that took entries from alias.
	Joined   []string `json:"joined"`
	Notes    bool     `json:"notes"` // alias's notes were added to keep's
	Closed   bool     `json:"closed"`
	Rev      string   `json:"rev"`
	Warnings []string `json:"warnings"`
}

// DocMergeDescription is doc_merge's tool text.
const DocMergeDescription = "Merge a duplicate (alias) into its primary (keep), any type: every wikilink in the vault to alias is rewritten to keep; " +
	"alias's list fields (with, rfx, depends_on, blocked_by, calendar_uids, tags) and ## Notes join keep's; alias gets duplicate_of keep and leaves every list, agenda, order, stack, and search, " +
	"but its id still resolves and keep's page lists it as merged from. An open task alias closes with outcome duplicate. Both must be the same type. One receipt; receipt_undo reverses all of it"

// DocMerge merges any duplicate into its primary.
var DocMerge = withOneReceipt(define("doc_merge", DocMergeDescription, func(ctx context.Context, d Deps, in DocMergeInput) (DocMergeOutput, error) {
	keep, alias := normID(in.Keep), normID(in.Alias)
	if keep == "" || alias == "" {
		return DocMergeOutput{}, errors.New("keep and alias are required: the primary, and the duplicate merged into it")
	}
	adoc, err := d.Store.Get(ctx, alias)
	if err != nil {
		return DocMergeOutput{}, linkErr("alias", alias, err)
	}
	if t, ok := adoc.Meta.(*model.Task); ok && !taskTerminal(t.Status) {
		// An open task closes as a duplicate; task_resolve merges it.
		r, err := TaskResolve.Run(ctx, d, TaskResolveInput{ID: alias, Outcome: model.OutcomeDuplicate, Link: keep, Note: in.Note})
		if err != nil {
			return DocMergeOutput{}, err
		}
		if r.Merge == nil {
			return DocMergeOutput{}, fmt.Errorf("%s closed as a duplicate, but it was not merged", alias)
		}
		out := *r.Merge
		out.Closed = true
		out.Warnings = append(out.Warnings, r.Warnings...)
		return out, nil
	}
	return mergeDocs(ctx, d, keep, alias)
}))

// sameKind reports whether two types may merge: the same type, or a task and
// a legacy meeting.
func sameKind(a, b model.Type) bool {
	meetingish := func(t model.Type) bool { return t == model.TypeTask || t == model.TypeMeeting }
	return a == b || (meetingish(a) && meetingish(b))
}

// mergeDocs merges alias into keep. The alias is written last, so a failure
// before it leaves alias listed and its links can be rewritten again.
func mergeDocs(ctx context.Context, d Deps, keep, alias string) (DocMergeOutput, error) {
	out := DocMergeOutput{Keep: keep, Alias: alias, Rewritten: []string{}, Joined: []string{}, Warnings: []string{}}
	adoc, _, err := mergeCheck(ctx, d, keep, alias)
	if err != nil {
		return out, err
	}
	// Links first: every document that names alias names keep.
	refs, err := d.Store.List(ctx, store.Filter{All: true})
	if err != nil {
		return out, err
	}
	for _, r := range refs {
		if r.ID == alias || slices.Contains(noRewrite, r.Type) {
			continue
		}
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return out, err
		}
		nd, changed, err := doc.RewriteLinks(alias, keep)
		if err != nil {
			return out, fmt.Errorf("%s: %w", r.ID, err)
		}
		if !changed {
			continue
		}
		if nd, _, err = model.DedupeLinkLists(nd); err != nil {
			return out, fmt.Errorf("%s: %w", r.ID, err)
		}
		if _, err := d.Store.Update(ctx, nd); err != nil {
			return out, fmt.Errorf("%s: %w", r.ID, err)
		}
		out.Rewritten = append(out.Rewritten, r.ID)
	}
	// Then keep takes alias's lists and notes.
	kdoc, err := d.Store.Get(ctx, keep)
	if err != nil {
		return out, err
	}
	nd, joined, err := model.MergeLists(kdoc, adoc)
	if err != nil {
		return out, err
	}
	out.Joined = append(out.Joined, joined...)
	if notes := headingText(adoc.Body, notesHeading); notes != "" {
		nd.Body = appendToSection(nd.Body, notesHeading, "From [["+alias+"]]:\n\n"+notes)
		out.Notes = true
	}
	if len(joined) > 0 || out.Notes {
		if err := checkDoc(nd); err != nil {
			return out, fmt.Errorf("merging %s into %s: %w", alias, keep, err)
		}
		saved, err := d.Store.Update(ctx, nd)
		if err != nil {
			return out, err
		}
		out.Rev = saved.Rev
	}
	// Last, the alias names its primary and leaves every list.
	if adoc, err = d.Store.Get(ctx, alias); err != nil {
		return out, err
	}
	adoc.Meta.Header().DuplicateOf = model.LinkTo(keep)
	if _, err := d.Store.Update(ctx, adoc); err != nil {
		return out, err
	}
	if out.Rev == "" {
		if k, err := d.Store.Get(ctx, keep); err == nil {
			out.Rev = k.Rev
		}
	}
	return out, d.Store.Record(ctx, "doc_merge", alias, "", keep, "")
}

// mergeCheck reads alias and keep and refuses a merge that cannot be made.
func mergeCheck(ctx context.Context, d Deps, keep, alias string) (adoc, kdoc model.Document, err error) {
	if keep == alias {
		return adoc, kdoc, fmt.Errorf("%s cannot be merged into itself", keep)
	}
	if adoc, err = d.Store.Get(ctx, alias); err != nil {
		return adoc, kdoc, linkErr("alias", alias, err)
	}
	if kdoc, err = d.Store.Get(ctx, keep); err != nil {
		return adoc, kdoc, linkErr("keep", keep, err)
	}
	at, kt := adoc.Meta.Header().Type, kdoc.Meta.Header().Type
	switch {
	case slices.Contains(mergeRefused, at) || slices.Contains(mergeRefused, kt):
		return adoc, kdoc, fmt.Errorf("%s and %s hold their own record and are not merged", at, kt)
	case !sameKind(at, kt):
		return adoc, kdoc, fmt.Errorf("%s is a %s and %s is a %s: merge two items of the same type", alias, at, keep, kt)
	case kdoc.Meta.Header().DuplicateOf != "":
		p := kdoc.Meta.Header().DuplicateOf.Target()
		return adoc, kdoc, fmt.Errorf("%s was merged into %s: merge into %s", keep, p, p)
	case kdoc.Folder == model.FolderArchive:
		return adoc, kdoc, fmt.Errorf("%s is archived: merge into an item that is not", keep)
	case adoc.Meta.Header().DuplicateOf != "":
		return adoc, kdoc, fmt.Errorf("%s is already merged into %s", alias, adoc.Meta.Header().DuplicateOf.Target())
	}
	return adoc, kdoc, nil
}

// mergedFrom lists the documents merged into id, by id.
func mergedFrom(ctx context.Context, s store.Store, id string) []string {
	out := []string{}
	refs, err := s.List(ctx, store.Filter{All: true})
	if err != nil {
		return out
	}
	for _, r := range refs {
		if !r.Hidden {
			continue
		}
		doc, err := s.Get(ctx, r.ID)
		if err == nil && doc.Meta.Header().DuplicateOf.Target() == id {
			out = append(out, r.ID)
		}
	}
	return out
}

// DocArchiveInput is the input of doc_archive.
type DocArchiveInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

// DocArchiveOutput is the output of doc_archive.
type DocArchiveOutput struct {
	ID       string   `json:"id"`
	Folder   string   `json:"folder"`
	Rev      string   `json:"rev"`
	Warnings []string `json:"warnings"`
}

// DocArchive is para's delete: the document moves to archive/ and leaves
// every list; links to it still resolve; receipt_undo puts it back.
var DocArchive = withOneReceipt(define("doc_archive", "Archive a document of any type (para's delete): it moves to archive/ and leaves every list, agenda, order, stack, and search; links to it still resolve. For a meeting that moved, prefer doc_merge into the new one. reason goes under ## Archived. One receipt; receipt_undo restores it", func(ctx context.Context, d Deps, in DocArchiveInput) (DocArchiveOutput, error) {
	id := normID(in.ID)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return DocArchiveOutput{}, linkErr("id", id, err)
	}
	if t := doc.Meta.Header().Type; slices.Contains(mergeRefused, t) {
		return DocArchiveOutput{}, fmt.Errorf("%s is a %s, which holds its own record: it is not archived", id, t)
	}
	if doc.Folder == model.FolderArchive {
		return DocArchiveOutput{}, fmt.Errorf("%s is already archived", id)
	}
	out := DocArchiveOutput{ID: id, Warnings: []string{}}
	if dp, err := loadDependents(ctx, d.Store); err == nil && len(dp.by[id]) > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%d items still wait on %s: %s; move them or close them", len(dp.by[id]), id, strings.Join(dp.by[id], ", ")))
	}
	if r := flatLine(in.Reason); r != "" {
		doc.Body = appendToSection(doc.Body, "## Archived", "- "+d.Store.Now().Format(dateLayout)+": "+r)
		if doc, err = d.Store.Update(ctx, doc); err != nil {
			return out, err
		}
	}
	moved, err := d.Store.Move(ctx, id, model.FolderArchive)
	if err != nil {
		return out, err
	}
	out.Folder, out.Rev = string(moved.Folder), moved.Rev
	return out, nil
}))

// DocMergesInput names a document.
type DocMergesInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// DocMergesOutput says what was merged into a document, and what it was
// merged into.
type DocMergesOutput struct {
	ID          string    `json:"id"`
	DuplicateOf string    `json:"duplicate_of,omitempty"`
	Archived    bool      `json:"archived"`
	MergedFrom  []PartRef `json:"merged_from"`
}

// DocMerges lists the duplicates merged into a document.
var DocMerges = defineRead("doc_merges", "Show what was merged into a document (merged from), what it was merged into (duplicate_of), and whether it is archived", func(ctx context.Context, d Deps, in DocMergesInput) (DocMergesOutput, error) {
	id := normID(in.ID)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return DocMergesOutput{}, linkErr("id", id, err)
	}
	out := DocMergesOutput{ID: id, DuplicateOf: doc.Meta.Header().DuplicateOf.Target(), Archived: doc.Folder == model.FolderArchive, MergedFrom: []PartRef{}}
	for _, m := range mergedFrom(ctx, d.Store, id) {
		ref := PartRef{ID: m, Title: m}
		if x, err := d.Store.Get(ctx, m); err == nil {
			ref.Title, ref.Kind = x.Title, string(x.Meta.Header().Type)
		}
		out.MergedFrom = append(out.MergedFrom, ref)
	}
	return out, nil
})
