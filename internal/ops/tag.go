package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// A topic is a tag: the Obsidian tags of a vault document, and the tags of a
// dossier item (a dossier's own tags apply to all its items). Tag items where
// they are; nothing moves, so ids, approvals, and events stay. A view of a
// topic is derived from the tags in use (tag_list), not stored.

// systemTags are tags para itself reads; they are not topics.
var systemTags = []string{model.CommitmentTag}

// normTag is a tag as para stores it: lower case, trimmed, no leading #,
// spaces as '-'.
func normTag(s string) string {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "#")
	return strings.Join(strings.Fields(s), "-")
}

// normTags normalizes a list and drops empty and repeated tags.
func normTags(in []string) []string {
	var out []string
	for _, t := range in {
		if t = normTag(t); t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// ItemRef names one dossier item.
type ItemRef struct {
	Proposal string `json:"proposal"`
	Item     string `json:"item"`
}

func (r ItemRef) String() string { return normID(r.Proposal) + "#" + r.Item }

// TagInput is the input of tag_add and tag_remove.
type TagInput struct {
	IDs   []string  `json:"ids,omitempty"`   // vault documents; a dossier id tags the dossier, so all its items
	Items []ItemRef `json:"items,omitempty"` // dossier items
	Tags  []string  `json:"tags"`
}

// TagOutput is the output of tag_add and tag_remove. Changed lists ids and
// proposal#item refs; Skipped those left as they were, with the reason.
type TagOutput struct {
	Changed []string `json:"changed"`
	Skipped []string `json:"skipped"`
	Tags    []string `json:"tags"`
}

func tagWrite(ctx context.Context, d Deps, in TagInput, add bool) (TagOutput, error) {
	tags := normTags(in.Tags)
	if len(tags) == 0 {
		return TagOutput{}, errors.New("tags is empty")
	}
	for _, t := range tags {
		if slices.Contains(systemTags, t) {
			return TagOutput{}, fmt.Errorf("tag %q is para's own: set it with its op", t)
		}
	}
	if len(in.IDs) == 0 && len(in.Items) == 0 {
		return TagOutput{}, errors.New("nothing to tag: give ids or items")
	}
	apply := func(have []string) ([]string, bool) {
		out := slices.Clone(have)
		for _, t := range tags {
			if add && !slices.Contains(out, t) {
				out = append(out, t)
			}
			if !add {
				out = slices.DeleteFunc(out, func(x string) bool { return x == t })
			}
		}
		return out, !slices.Equal(out, have)
	}
	out := TagOutput{Changed: []string{}, Skipped: []string{}, Tags: tags}
	seen := map[string]bool{}
	for _, id := range in.IDs {
		if id = normID(id); id == "" || seen[id] {
			continue
		}
		seen[id] = true
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			return out, linkErr("id", id, err)
		}
		h := doc.Meta.Header()
		if h.Type == model.TypeReceipt {
			return out, fmt.Errorf("%s is a receipt: receipts take no tags", id)
		}
		next, changed := apply(h.Tags)
		if !changed {
			out.Skipped = append(out.Skipped, id+": "+tagNoChange(add))
			continue
		}
		h.Tags = next
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return out, err
		}
		out.Changed = append(out.Changed, id)
	}
	byProp := map[string][]string{}
	var props []string
	for _, r := range in.Items {
		p := normID(r.Proposal)
		if p == "" || r.Item == "" {
			return out, errors.New("an item needs proposal and item")
		}
		if seen[r.String()] {
			continue
		}
		seen[r.String()] = true
		if _, ok := byProp[p]; !ok {
			props = append(props, p)
		}
		byProp[p] = append(byProp[p], r.Item)
	}
	for _, p := range props {
		var changed, skipped []string
		_, err := changeProposal(ctx, d, p, func(pp *proposal) error {
			changed, skipped = nil, nil
			if pp.dossier == nil {
				return fmt.Errorf("%s is not a dossier: tag the proposal by its id instead", pp.doc.ID)
			}
			for _, item := range byProp[p] {
				it, err := pp.item(item)
				if err != nil {
					return err
				}
				next, ok := apply(it.Tags)
				ref := ItemRef{Proposal: pp.doc.ID, Item: item}.String()
				if !ok {
					skipped = append(skipped, ref+": "+tagNoChange(add))
					continue
				}
				it.Tags = next
				changed = append(changed, ref)
			}
			if len(changed) == 0 {
				return errNoChange
			}
			return nil
		})
		if err != nil && !errors.Is(err, errNoChange) {
			return out, err
		}
		out.Changed = append(out.Changed, changed...)
		out.Skipped = append(out.Skipped, skipped...)
	}
	return out, nil
}

// errNoChange stops changeProposal when nothing changed: it writes nothing.
var errNoChange = errors.New("no change")

func tagNoChange(add bool) string {
	if add {
		return "already tagged"
	}
	return "not tagged"
}

const tagDesc = "ids are vault documents. Tags are lower case, without #. Undo with receipt_undo"

// TagAdd tags vault documents and dossier items with topics.
var TagAdd = withProposalReceipts(withOneReceipt(defineSafe("tag_add", "Tag items with a topic, such as socials, where they are: nothing moves. Tag at filing instead (tags on io_add, task_add, question_add) when you can; tag existing items rather than withdraw and refile them. "+tagDesc, func(ctx context.Context, d Deps, in TagInput) (TagOutput, error) {
	return tagWrite(ctx, d, in, true)
})))

// TagRemove removes topics from vault documents and dossier items.
var TagRemove = withProposalReceipts(withOneReceipt(defineSafe("tag_remove", "Remove a topic tag from items. "+tagDesc, func(ctx context.Context, d Deps, in TagInput) (TagOutput, error) {
	return tagWrite(ctx, d, in, false)
})))

// TagListInput is the input of tag_list.
type TagListInput struct{}

// TagCount is one topic: open vault items, items waiting in the review stack,
// and items put off (under Later).
type TagCount struct {
	Tag   string `json:"tag"`
	Open  int    `json:"open"`
	Stack int    `json:"stack"`
	Later int    `json:"later"`
}

// TagListOutput is the output of tag_list, the most used tag first.
type TagListOutput struct {
	Tags []TagCount `json:"tags"`
}

// TagList lists the topics in use.
var TagList = defineRead("tag_list", "List the topics (tags) in use, the most used first, each with counts: open vault items, items that need the owner, and items put off (later). Para's own tags are left out", func(ctx context.Context, d Deps, _ TagListInput) (TagListOutput, error) {
	counts := map[string]*TagCount{}
	at := func(t string) *TagCount {
		if slices.Contains(systemTags, t) {
			return nil
		}
		c, ok := counts[t]
		if !ok {
			c = &TagCount{Tag: t}
			counts[t] = c
		}
		return c
	}
	today := d.Store.Now().Format(dateLayout)
	deferred, err := deferredSet(ctx, d.Store, today)
	if err != nil {
		return TagListOutput{}, err
	}
	refs, err := d.Store.List(ctx, store.Filter{})
	if err != nil {
		return TagListOutput{}, err
	}
	for _, r := range refs {
		if r.Type == model.TypeProposal || r.Type == model.TypeReceipt {
			continue
		}
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			continue
		}
		tags := doc.Meta.Header().Tags
		if len(tags) == 0 || !openDoc(doc) {
			continue
		}
		later := deferred[doc.ID].any()
		for _, t := range normTags(tags) {
			if c := at(t); c != nil {
				if later {
					c.Later++
				} else {
					c.Open++
				}
			}
		}
	}
	all, err := allProposals(ctx, d)
	if err != nil {
		return TagListOutput{}, err
	}
	for _, p := range all {
		if p.meta.Status == model.ProposalSuperseded || p.broken != nil {
			continue
		}
		for _, it := range p.content.Items {
			st := stackState(it, today)
			if st != StackProposed && st != StackLater {
				continue
			}
			for _, t := range normTags(model.ItemTags(p.meta.Tags, it)) {
				if c := at(t); c != nil {
					if st == StackLater {
						c.Later++
					} else {
						c.Stack++
					}
				}
			}
		}
	}
	out := TagListOutput{Tags: []TagCount{}}
	for _, c := range counts {
		out.Tags = append(out.Tags, *c)
	}
	sort.Slice(out.Tags, func(i, j int) bool {
		a, b := out.Tags[i], out.Tags[j]
		if na, nb := a.Open+a.Stack+a.Later, b.Open+b.Stack+b.Later; na != nb {
			return na > nb
		}
		return a.Tag < b.Tag
	})
	return out, nil
})

// openDoc is false for a finished task, question, goal, Ask, request, or io
// item; any other document counts as open.
func openDoc(doc model.Document) bool {
	if m, ok := doc.Meta.(*model.IO); ok {
		return m.State.Open()
	}
	return !resolved(doc)
}
