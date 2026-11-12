package ops

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// The context of an item answers "what is this for?": where it came from
// (the io items filed into it or linking it, the agent work that made or
// changed it, the item it is an action item of, the one it recurred from),
// who it is for (requested_by, owed_to, owed_by, a commitment's with, the
// assignee), and why the owner does it (reasons, reason.go). It reads what
// the vault already links and writes nothing.

// Kinds of a came-from row.
const (
	CameFromIO           = "io"
	CameFromAgentWork    = "agent_work"
	CameFromParent       = "parent"
	CameFromPrevious     = "previous"
	CameFromIncident     = "incident"
	CameFromOccurrenceOf = "occurrence_of"
)

// Roles of a for row.
const (
	ForRequestedBy = "requested_by"
	ForOwedTo      = "owed_to"
	ForOwedBy      = "owed_by"
	ForWith        = "with"
	ForAssignee    = "assignee"
)

// ItemContextInput is the input of context.
type ItemContextInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// CameFrom is one thing an item came from. For an io item: its medium (and
// the medium in words), from (a name; FromID when it links a person or
// group), when (RFC 3339, or the day of a logged contact), and the link to
// the original (url). Text says it in words.
type CameFrom struct {
	Kind        string       `json:"kind"`
	ID          string       `json:"id"`
	Title       string       `json:"title,omitempty"`
	Medium      model.Medium `json:"medium,omitempty"`
	MediumWords string       `json:"medium_words,omitempty"`
	From        string       `json:"from,omitempty"`
	FromID      string       `json:"from_id,omitempty"`
	When        string       `json:"when,omitempty"`
	URL         string       `json:"url,omitempty"`
	Text        string       `json:"text"`
}

// ForParty is one person, group, or free text the item is for. ID is set
// when it links a document.
type ForParty struct {
	Role string `json:"role"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// ItemContextOutput is the context of an item. Reasons is never empty: an
// item with none has one reason of kind none. Looking is the open agent work
// that looks into the item (investigate), or "".
type ItemContextOutput struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Type     model.Type `json:"type"`
	CameFrom []CameFrom `json:"came_from"`
	For      []ForParty `json:"for"`
	Reasons  []Reason   `json:"reasons"`
	Looking  string     `json:"looking,omitempty"`
	// WaitsOn is what the item waits on, Blocks what waits on it (dependsRows).
	WaitsOn []DepRow `json:"waits_on"`
	Blocks  []DepRow `json:"blocks"`
	// BlockedBy is the open items it waits on (depends_on), labelled for the
	// owner: each with type, state, and title.
	BlockedBy []Blocker `json:"blocked_by"`
	// Later is "until <title> starts (<date>)" or "until <title> ends
	// (<date>)" while the item is put off until an obligation starts or
	// ends; LaterRef is that obligation's id.
	Later    string `json:"later,omitempty"`
	LaterRef string `json:"later_ref,omitempty"`
	// PartOf is what the item is part of, nearest first: its parent, the
	// obligations and questions it waits on or serves, its goal, its tags.
	PartOf []PartOfRef `json:"part_of"`
}

// Where a dependency row is recorded.
const (
	DepHard      = "depends_on"     // the waiter's depends_on
	DepOptional  = "optional_after" // the waiter's optional_after: never holds it
	DepReason    = "blocked_by"     // a text reason of a task: no id
	DepRFXBlocks = "rfx_blocks"     // an rfx's blocks: change it on the rfx
)

// DepRow is one dependency of an item, seen from either end. ID is empty for
// a text reason; State is "missing" for an id with no document. Optional is
// true when it does not hold the waiter (optional_after).
type DepRow struct {
	ID       string     `json:"id,omitempty"`
	Title    string     `json:"title"`
	Type     model.Type `json:"type,omitempty"`
	State    string     `json:"state,omitempty"`
	Open     bool       `json:"open"`
	Optional bool       `json:"optional"`
	Reason   string     `json:"reason,omitempty"`
	Via      string     `json:"via"`
}

// ItemContext shows what an item is for.
var ItemContext = defineRead("context", "Show what an item is for and what it waits on: later (\"until <obligation> starts (<date>)\" while put off until an obligation starts or ends, with later_ref its id), waits_on (each item it waits on with type, state, and optional: an optional one never holds it; a text reason has reason and no id), blocks (the open items that wait on it, optional too), blocked_by (the open items it waits on, each with a type label such as output or input for an io item, a state label such as unapproved or waiting for reply, and title), came_from (the io items filed into it or linking it, with medium, from, when, and url; the agent work that made or changed it; its parent, previous, incident), for (requested_by, owed_to, owed_by, a commitment's with, assignee), and reasons: goal (it leads to a goal, directly or through parent, what it unblocks, or a question's scope), owed (someone waits for it), upkeep (an area, a recurring item, an incident), or none. Each reason has via (the items on the way) and text. part_of is what it belongs to, nearest first: parent, the obligations and questions it waits on, is put off until, or serves, goal, tags. Read it before you decide whether an item is still needed", func(ctx context.Context, d Deps, in ItemContextInput) (ItemContextOutput, error) {
	id := normID(in.ID)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return ItemContextOutput{}, linkErr("id", id, err)
	}
	docs, err := allDocs(ctx, d.Store)
	if err != nil {
		return ItemContextOutput{}, fmt.Errorf("ops: context: %w", err)
	}
	g := newReasonGraph(docs)
	out := ItemContextOutput{ID: doc.ID, Title: doc.Title, Type: doc.Meta.Header().Type, CameFrom: []CameFrom{}, For: []ForParty{}}
	c := &ctxBuild{g: g, out: &out}
	ios, err := ioAbout(ctx, d.Store, []string{doc.ID})
	if err != nil {
		return out, fmt.Errorf("ops: context: %w", err)
	}
	for _, x := range ios {
		if x.direct {
			c.io(x.doc, x.m)
		}
	}
	c.agentWork(doc.ID)
	c.links(doc)
	c.parties(doc)
	out.Reasons = g.reasonsOrNone(doc.ID)
	out.WaitsOn, out.Blocks = dependsRows(docs, doc.ID)
	out.BlockedBy = blockersOf(func(id string) (model.Document, bool) { x, ok := docs[id]; return x, ok }, targets(model.HardDeps(doc.Meta)))
	looking, err := investigating(ctx, d.Store)
	if err != nil {
		return out, fmt.Errorf("ops: context: %w", err)
	}
	out.Looking = looking[doc.ID]
	if df := deferralOf(doc.Meta); df != nil && df.IsDeferred(d.Store.Now().Format(dateLayout)) {
		if out.Later = followText(df, func(id string) (model.Document, bool) { x, ok := docs[id]; return x, ok }); out.Later != "" {
			out.LaterRef, _ = df.Follows()
		}
	}
	out.PartOf = newPartIndex(ctx, d.Store).partOf(doc)
	return out, nil
})

// allDocs reads every document of the vault by id.
func allDocs(ctx context.Context, s store.Store) (map[string]model.Document, error) {
	refs, err := s.List(ctx, store.Filter{})
	if err != nil {
		return nil, err
	}
	docs := map[string]model.Document{}
	for _, r := range refs {
		d, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		docs[r.ID] = d
	}
	return docs, nil
}

type ctxBuild struct {
	g   *reasonGraph
	out *ItemContextOutput
}

// party is the name of a person or group link, or free text as it is, and
// the id when it links one.
func (c *ctxBuild) party(s string) (name, id string) {
	s = strings.TrimSpace(s)
	if t := model.Link(s).Target(); t != s || strings.HasPrefix(s, "[[") {
		return c.g.title(t), t
	}
	if _, ok := c.g.docs[s]; ok {
		return c.g.title(s), s
	}
	return s, ""
}

// io adds an io item the item was filed from or that links it.
func (c *ctxBuild) io(doc model.Document, m *model.IO) {
	row := CameFrom{Kind: CameFromIO, ID: doc.ID, Title: firstOf(m.Subject, doc.Title), Medium: m.Medium,
		MediumWords: firstOf(mediumWords[m.Medium], string(m.Medium)), When: firstOf(string(m.Date), m.At), URL: firstOf(m.Ref, m.URL)}
	if m.From != "" {
		row.From, row.FromID = c.party(m.From)
	} else if len(m.With) > 0 {
		var names []string
		for _, w := range m.With {
			n, _ := c.party(string(w))
			names = append(names, n)
		}
		row.From = strings.Join(names, ", ")
		if len(m.With) == 1 {
			row.FromID = m.With[0].Target()
		}
	}
	row.Text = cameFromText(row, m)
	c.out.CameFrom = append(c.out.CameFrom, row)
}

// cameFromText says an io row in words: "Conversation with Dana, 2026-11-06",
// "Carried over by hand from the tracker", "Email from Dana, 2026-11-06".
func cameFromText(r CameFrom, m *model.IO) string {
	s := r.MediumWords
	switch {
	case r.From == "":
	case m.Medium == model.MediumVerbal || m.Logged() && m.From == "":
		s += " with " + r.From
	case m.Direction == model.IOOut:
		s += " to " + r.From
	default:
		s += " from " + r.From
	}
	if day := r.When; len(day) >= 10 {
		s += ", " + day[:10]
	}
	return s
}

// agentWork adds the agent work that made or changed the item (result_links).
func (c *ctxBuild) agentWork(id string) {
	var ids []string
	for rid, d := range c.g.docs {
		if r, ok := d.Meta.(*model.Request); ok && slices.ContainsFunc(r.Results, func(l model.Link) bool { return l.Target() == id }) {
			ids = append(ids, rid)
		}
	}
	slices.Sort(ids)
	for _, rid := range ids {
		t := c.g.title(rid)
		c.out.CameFrom = append(c.out.CameFrom, CameFrom{Kind: CameFromAgentWork, ID: rid, Title: t, Text: "Agent work that made or changed it: " + t})
	}
}

// links adds the items the item names as its origin.
func (c *ctxBuild) links(doc model.Document) {
	add := func(kind string, l model.Link, words string) {
		t := l.Target()
		if t == "" {
			return
		}
		c.out.CameFrom = append(c.out.CameFrom, CameFrom{Kind: kind, ID: t, Title: c.g.title(t), Text: words + " " + c.g.title(t)})
	}
	switch m := doc.Meta.(type) {
	case *model.Task:
		add(CameFromParent, m.Parent, "Action item of")
		add(CameFromPrevious, m.Previous, "Recurred from")
		add(CameFromIncident, m.Incident, "Follows up the incident")
	case *model.Obligation:
		add(CameFromParent, m.Parent, "Action item of")
		add(CameFromOccurrenceOf, m.OccurrenceOf, "Occurrence of")
		add(CameFromPrevious, m.Previous, "After")
	}
}

// parties adds who the item is for.
func (c *ctxBuild) parties(doc model.Document) {
	add := func(role, s string) {
		if strings.TrimSpace(s) == "" {
			return
		}
		name, id := c.party(s)
		c.out.For = append(c.out.For, ForParty{Role: role, ID: id, Name: name})
	}
	switch m := doc.Meta.(type) {
	case *model.Task:
		add(ForRequestedBy, m.RequestedBy)
		if m.EffectiveKind() == model.KindCommitment {
			for _, w := range m.With {
				add(ForWith, string(w))
			}
		}
		if IsOther(m.Assignee) {
			add(ForAssignee, m.Assignee)
		}
	case *model.Obligation:
		for _, w := range m.OwedTo {
			add(ForOwedTo, string(w))
		}
		for _, w := range m.OwedBy {
			add(ForOwedBy, string(w))
		}
		if m.Kind == model.KindCommitment && len(m.OwedTo) == 0 {
			for _, w := range m.With {
				add(ForWith, string(w))
			}
		}
	case *model.RFX:
		if m.Direction == model.RFXIn {
			for _, t := range m.Targets {
				add(ForRequestedBy, string(t.To))
			}
		}
	}
}

// dependsRows is what id waits on and the open items that wait on it, from
// depends_on (hard), optional_after (optional), a task's blocked_by (text),
// and an rfx's blocks. It is the one place both directions are read.
func dependsRows(docs map[string]model.Document, id string) (waits, blocks []DepRow) {
	row := func(dep, via string) DepRow {
		r := DepRow{ID: dep, Title: dep, State: "missing", Via: via, Optional: via == DepOptional}
		if d, ok := docs[dep]; ok {
			r.Title, r.Type, r.State, r.Open = cmp.Or(d.Title, dep), d.Meta.Header().Type, statusOf(d.Meta), !resolved(d)
		}
		return r
	}
	waits, blocks = []DepRow{}, []DepRow{}
	if d, ok := docs[id]; ok {
		for _, l := range model.HardDeps(d.Meta) {
			waits = append(waits, row(l.Target(), DepHard))
		}
		for _, l := range model.OptionalDeps(d.Meta) {
			waits = append(waits, row(l.Target(), DepOptional))
		}
		if t, ok := d.Meta.(*model.Task); ok {
			for _, b := range t.BlockedBy {
				waits = append(waits, DepRow{Title: b, Reason: b, State: string(t.Status), Open: true, Via: DepReason})
			}
		}
		if r, ok := d.Meta.(*model.RFX); ok && r.Status == model.RFXOpen {
			for _, l := range r.Blocks {
				if w, ok := docs[l.Target()]; ok && !resolved(w) {
					blocks = append(blocks, row(l.Target(), DepRFXBlocks))
				}
			}
		}
	}
	names := func(l model.Link) bool { return l.Target() == id }
	for _, wid := range slices.Sorted(maps.Keys(docs)) {
		w := docs[wid]
		if wid == id {
			continue
		}
		if r, ok := w.Meta.(*model.RFX); ok && r.Status == model.RFXOpen && slices.ContainsFunc(r.Blocks, names) {
			waits = append(waits, row(wid, DepRFXBlocks))
		}
		if resolved(w) {
			continue
		}
		switch {
		case slices.ContainsFunc(model.HardDeps(w.Meta), names):
			blocks = append(blocks, row(wid, DepHard))
		case slices.ContainsFunc(model.OptionalDeps(w.Meta), names):
			blocks = append(blocks, row(wid, DepOptional))
		}
	}
	return waits, blocks
}
