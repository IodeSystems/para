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

// A theme is what items belong to: a goal, an obligation (a trip, an event, a
// meeting), a question or decision, a parent task, or a tag. "Part of" lists
// the themes of one item, nearest first. When a theme ends, the items that
// hang on it are the open items whose Part of names it: nothing that only
// mentions it in text. theme_items lists them; theme_close closes the theme
// and drops or moves them as one bulk batch, so one undo takes it all back.

// Kinds of a Part of row, nearest first.
const (
	PartParent     = "parent"
	PartObligation = "obligation"
	PartQuestion   = "question"
	PartGoal       = "goal"
	PartTag        = "tag"
)

// How an item belongs to a theme.
const (
	ViaParent   = "parent"         // its parent task or obligation
	ViaGoal     = "goal"           // its goal (project, or a question's scope)
	ViaDepends  = "depends_on"     // it waits on the theme
	ViaOptional = "optional_after" // better after the theme; never holds it
	ViaPutOff   = "put_off_until"  // put off until the theme resolves
	ViaServes   = "serves"         // the theme (an obligation) waits on it
	ViaTag      = "tag"            // it has the tag
)

// PartOfRef is one thing an item is part of. ID is a document id, or the tag
// itself for kind tag; Title is the document's title, or the tag.
type PartOfRef struct {
	Kind  string `json:"kind"`
	Via   string `json:"via"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// partIndex computes Part of with a document getter. serves maps an item to
// the obligations that wait on it; it is read once, on first use.
type partIndex struct {
	get    func(id string) (model.Document, bool)
	refs   func() []model.Ref
	serves map[string][]string
}

func newPartIndex(ctx context.Context, s store.Store) *partIndex {
	cache := map[string]*model.Document{}
	var refs []model.Ref
	listed := false
	return &partIndex{
		get: func(id string) (model.Document, bool) {
			if d, ok := cache[id]; ok {
				if d == nil {
					return model.Document{}, false
				}
				return *d, true
			}
			d, err := s.Get(ctx, id)
			if err != nil {
				cache[id] = nil
				return model.Document{}, false
			}
			cache[id] = &d
			return d, true
		},
		refs: func() []model.Ref {
			if !listed {
				listed = true
				refs, _ = s.List(ctx, store.Filter{})
			}
			return refs
		},
	}
}

// snapPartIndex is a partIndex over a snapshot.
func snapPartIndex(sn *Snapshot) *partIndex {
	return &partIndex{
		get: func(id string) (model.Document, bool) {
			d, err := sn.Get(id)
			return d, err == nil
		},
		refs: func() []model.Ref { return sn.refs },
	}
}

// eventLike reports whether a document is an obligation or a meeting: below
// the obligation format, a task of kind meeting or commitment.
func eventLike(m model.Meta) bool {
	switch x := m.(type) {
	case *model.Obligation, *model.Meeting:
		return true
	case *model.Task:
		k := x.EffectiveKind()
		return k == model.KindMeeting || k == model.KindCommitment
	}
	return false
}

func (p *partIndex) servesOf(id string) []string {
	if p.serves == nil {
		p.serves = map[string][]string{}
		for _, r := range p.refs() {
			if r.Type != model.TypeObligation && r.Type != model.TypeMeeting && r.Type != model.TypeTask {
				continue
			}
			d, ok := p.get(r.ID)
			if !ok || !eventLike(d.Meta) {
				continue
			}
			for _, l := range append(model.HardDeps(d.Meta), model.OptionalDeps(d.Meta)...) {
				if t := l.Target(); !slices.Contains(p.serves[t], d.ID) {
					p.serves[t] = append(p.serves[t], d.ID)
				}
			}
		}
	}
	return p.serves[id]
}

// partLinks reads the links of an item a Part of can come from.
func partLinks(m model.Meta) (parent, goal string) {
	switch x := m.(type) {
	case *model.Task:
		return x.Parent.Target(), x.Project.Target()
	case *model.Obligation:
		return x.Parent.Target(), x.Project.Target()
	case *model.RFX:
		return x.Parent.Target(), x.Project.Target()
	case *model.Question:
		return "", x.Scope.Target()
	}
	return "", ""
}

// partOf lists what an item is part of, nearest first: its parent, the
// obligations and questions it waits on, is put off until, or serves, its
// goal, then its tags. A type with none of these links has none.
func (p *partIndex) partOf(doc model.Document) []PartOfRef {
	out := []PartOfRef{}
	seen := map[string]bool{doc.ID: true}
	add := func(kind, via, id, title string) {
		key := kind + ":" + id
		if id == "" || seen[id] || seen[key] {
			return
		}
		seen[id], seen[key] = true, true
		out = append(out, PartOfRef{Kind: kind, Via: via, ID: id, Title: firstOf(title, id)})
	}
	parent, goal := partLinks(doc.Meta)
	if parent != "" {
		if pd, ok := p.get(parent); ok {
			add(PartParent, ViaParent, pd.ID, pd.Title)
		}
	}
	theme := func(id, via string) {
		td, ok := p.get(id)
		if !ok {
			return
		}
		switch {
		case eventLike(td.Meta):
			add(PartObligation, via, td.ID, td.Title)
		case td.Meta.Header().Type == model.TypeQuestion:
			add(PartQuestion, via, td.ID, td.Title)
		}
	}
	// A deferral's targets first: defer puts them in depends_on too.
	// A defer until an obligation starts or ends names it too.
	if df := deferralOf(doc.Meta); df != nil {
		if id, _ := df.Follows(); id != "" {
			theme(normID(id), ViaPutOff)
		}
		for _, id := range df.DeferredAfter {
			theme(normID(id), ViaPutOff)
		}
	}
	for _, l := range model.HardDeps(doc.Meta) {
		theme(l.Target(), ViaDepends)
	}
	for _, l := range model.OptionalDeps(doc.Meta) {
		theme(l.Target(), ViaOptional)
	}
	if model.HardDeps(doc.Meta) != nil || doc.Meta.Header().Type == model.TypeTask {
		for _, id := range p.servesOf(doc.ID) {
			theme(id, ViaServes)
		}
	}
	if goal != "" {
		if gd, ok := p.get(goal); ok {
			if _, isGoal := gd.Meta.(*model.Project); isGoal {
				add(PartGoal, ViaGoal, gd.ID, gd.Title)
			}
		}
	}
	for _, t := range normTags(doc.Meta.Header().Tags) {
		if !slices.Contains(systemTags, t) {
			add(PartTag, ViaTag, t, t)
		}
	}
	return out
}

// partOfID is Part of by id; an id with no document has none.
func (p *partIndex) partOfID(id string) []PartOfRef {
	d, ok := p.get(normID(id))
	if !ok {
		return []PartOfRef{}
	}
	return p.partOf(d)
}

// --- theme_items ---

// ThemeItemsInput is the input of theme_items: a document id, or a tag as #tag.
type ThemeItemsInput struct {
	Theme string `json:"theme" query:"theme" required:"true"`
}

// ThemeItem is one open item that hangs on a theme. Why lists how it hangs
// on it (parent, goal, depends_on, optional_after, put_off_until, serves,
// tag); WhyText says it in words. PartOf is all it is part of.
type ThemeItem struct {
	ID      string      `json:"id"`
	Title   string      `json:"title"`
	Type    model.Type  `json:"type"`
	Kind    string      `json:"kind,omitempty"`
	Life    Lifecycle   `json:"life"`
	Why     []string    `json:"why"`
	WhyText string      `json:"why_text"`
	PartOf  []PartOfRef `json:"part_of"`
}

// ThemeItemsOutput is the output of theme_items. Kind is goal, obligation,
// question, parent, or tag; Open is false for a theme that has ended.
type ThemeItemsOutput struct {
	Theme string      `json:"theme"`
	Title string      `json:"title"`
	Kind  string      `json:"kind"`
	Open  bool        `json:"open"`
	Items []ThemeItem `json:"items"`
}

// ThemeItemsDescription is theme_items' description.
const ThemeItemsDescription = "List the open items (open or put off) that hang on a theme: a goal (items under it), an obligation or meeting (its action items, items that wait on it, hard or optional, are put off until it, or that it waits on), a question or decision (items that wait on it or are put off until it), a parent task (its action items), or a tag given as #tag (items with it). " +
	"An item that only mentions the theme in text is not listed. Each item has why (parent, goal, depends_on, optional_after, put_off_until, serves, tag), why_text, and part_of (all it is part of, nearest first). " +
	"Read it before you close a theme: the owner decides what becomes of these items"

// ThemeItems lists the open items that hang on a theme.
var ThemeItems = defineRead("theme_items", ThemeItemsDescription, func(ctx context.Context, d Deps, in ThemeItemsInput) (ThemeItemsOutput, error) {
	return themeItems(ctx, d, in.Theme)
})

// themeKey is a theme as given: a tag (from #tag), or a document id.
func themeKey(s string) (tag, id string) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		return normTag(s), ""
	}
	return "", normID(s)
}

func themeItems(ctx context.Context, d Deps, theme string) (ThemeItemsOutput, error) {
	tag, id := themeKey(theme)
	out := ThemeItemsOutput{Items: []ThemeItem{}}
	switch {
	case tag != "":
		out.Theme, out.Title, out.Kind, out.Open = "#"+tag, tag, PartTag, true
	case id != "":
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			return out, linkErr("theme", id, err)
		}
		out.Theme, out.Title, out.Open = doc.ID, doc.Title, lifeOf(doc.Meta) == LifeOpen
		out.Kind = themeKind(doc)
		id = doc.ID
	default:
		return out, errors.New("theme is empty: give an id, or a tag as #tag")
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return out, err
	}
	px := snapPartIndex(sn)
	for _, doc := range sn.Docs() {
		if !slices.Contains(bulkTypes, doc.Meta.Header().Type) || doc.ID == id {
			continue
		}
		life := sn.Need(doc.ID).Life
		if life == LifeClosed {
			continue
		}
		parts := px.partOf(doc)
		var why []string
		for _, p := range parts {
			if (tag != "" && p.Kind == PartTag && p.ID == tag) || (id != "" && p.Kind != PartTag && p.ID == id) {
				why = append(why, p.Via)
			}
		}
		if len(why) == 0 {
			continue
		}
		kind := ""
		if t, ok := doc.Meta.(*model.Task); ok {
			kind = string(t.EffectiveKind())
		} else if o, ok := doc.Meta.(*model.Obligation); ok {
			kind = string(o.Kind)
		}
		out.Items = append(out.Items, ThemeItem{ID: doc.ID, Title: doc.Title, Type: doc.Meta.Header().Type, Kind: kind, Life: life,
			Why: why, WhyText: whyWords(why, out.Title), PartOf: parts})
	}
	return out, nil
}

// themeKind is the kind of theme a document is.
func themeKind(doc model.Document) string {
	switch {
	case eventLike(doc.Meta):
		return PartObligation
	case doc.Meta.Header().Type == model.TypeQuestion:
		return PartQuestion
	case doc.Meta.Header().Type == model.TypeProject:
		return PartGoal
	}
	return PartParent
}

var viaWords = map[string]string{
	ViaParent:   "part of %s",
	ViaGoal:     "under the goal %s",
	ViaDepends:  "waits on %s",
	ViaOptional: "better after %s",
	ViaPutOff:   "put off until %s",
	ViaServes:   "%s waits on it",
	ViaTag:      "tagged %s",
}

func whyWords(why []string, title string) string {
	var parts []string
	for _, w := range why {
		parts = append(parts, fmt.Sprintf(viaWords[w], title))
	}
	return strings.Join(parts, "; ")
}

// hanging is the open items that hang on a document, for a close's output.
// A failed read lists none: the close itself went through.
func hanging(ctx context.Context, d Deps, id string) []ThemeItem {
	doc, err := d.Store.Get(ctx, normID(id))
	if err != nil {
		return nil
	}
	switch {
	case eventLike(doc.Meta), doc.Meta.Header().Type == model.TypeQuestion, doc.Meta.Header().Type == model.TypeProject:
	default:
		return nil
	}
	out, err := themeItems(ctx, d, doc.ID)
	if err != nil || len(out.Items) == 0 {
		return nil
	}
	return out.Items
}

// HangingNote is what a close says about the items that hang on what it closed.
const HangingNote = "these open items hang on what you closed: ask the owner what becomes of each (they review them in the app, or with para theme close --drop/--keep); never drop them yourself"

// --- theme_close ---

// ThemeKeep is an item the owner keeps; Area, when set, moves it to that area
// (out of the goal that ends).
type ThemeKeep struct {
	ID   string `json:"id"`
	Area string `json:"area,omitempty"`
}

// ThemeCloseInput is the input of theme_close. Outcome, Reason, and Fields
// close the theme as close does; Change closes an obligation as
// obligation_change does (action cancel), with its notices. A theme closed
// already (a question just answered), or a tag, is not closed again.
type ThemeCloseInput struct {
	Theme   string                 `json:"theme"`
	Outcome string                 `json:"outcome,omitempty"`
	Reason  string                 `json:"reason,omitempty"`
	Fields  map[string]any         `json:"fields,omitempty"`
	Change  *ObligationChangeInput `json:"change,omitempty"`
	Drop    []string               `json:"drop,omitempty"`
	Keep    []ThemeKeep            `json:"keep,omitempty"`
	// DropReason is kept on each dropped item; default "<theme> ended".
	DropReason string `json:"drop_reason,omitempty"`
}

// ThemeCloseOutput is what theme_close did. Batch is the id bulk undo takes:
// it undoes the theme's close and every drop and move. Closed is the op that
// closed the theme ("" when it was closed already); Items are the drops and
// moves, as bulk reports them. Left are items that still hang on the theme
// and were in neither list.
type ThemeCloseOutput struct {
	Theme   string         `json:"theme"`
	Batch   string         `json:"batch,omitempty"`
	Closed  string         `json:"closed,omitempty"`
	Receipt string         `json:"receipt,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
	Applied int            `json:"applied"`
	Refused int            `json:"refused"`
	Items   []BulkItem     `json:"items"`
	Left    []string       `json:"left"`
}

// ThemeCloseDescription is theme_close's description.
const ThemeCloseDescription = "Close a theme (goal, obligation, question, or a #tag) after the owner reviewed the items that hang on it (theme_items): drop the items in drop, keep those in keep (area moves one to that area, out of the goal), and close the theme (outcome, reason, fields as close; or change, an obligation_change cancel with its notices). " +
	"A theme closed already, or a tag, is not closed again: only the drops and moves run. Everything shares one batch: bulk undo with it undoes the close and every drop and move. " +
	"An item refused does not stop the others; the theme still closes. Person only: an agent asks the owner"

// ThemeClose closes a theme and applies the owner's review of what hangs on it.
var ThemeClose = func() *Op[ThemeCloseInput, ThemeCloseOutput] {
	op := definePerson("theme_close", ThemeCloseDescription, runThemeClose)
	// Each part runs its own op, which gates and files its receipt.
	op.verb = true
	return op
}()

func runThemeClose(ctx context.Context, d Deps, in ThemeCloseInput) (ThemeCloseOutput, error) {
	out := ThemeCloseOutput{Items: []BulkItem{}, Left: []string{}}
	ti, err := themeItems(ctx, d, in.Theme)
	if err != nil {
		return out, err
	}
	out.Theme = ti.Theme
	tag, themeID := themeKey(in.Theme)
	if tag == "" {
		themeID = ti.Theme
	}
	if tag != "" && (in.Outcome != "" || in.Reason != "" || len(in.Fields) > 0 || in.Change != nil) {
		return out, errors.New("a tag has no close: give only drop and keep")
	}
	if in.Change != nil && (in.Outcome != "" || in.Reason != "" || len(in.Fields) > 0) {
		return out, errors.New("give change or outcome, reason, and fields, not both")
	}
	if in.Change != nil && (normID(in.Change.ID) != themeID || in.Change.Action != "cancel") {
		return out, errors.New("change must cancel the theme itself (action cancel, id the theme)")
	}
	hangs := map[string]bool{}
	for _, it := range ti.Items {
		hangs[it.ID] = true
	}
	decided := map[string]bool{}
	check := func(id string) (string, error) {
		id = normID(id)
		switch {
		case id == "":
			return "", errors.New("an id in drop or keep is empty")
		case decided[id]:
			return "", fmt.Errorf("%s is in drop or keep twice", id)
		case !hangs[id]:
			return "", fmt.Errorf("%s does not hang on %s (theme_items lists what does)", id, ti.Theme)
		}
		decided[id] = true
		return id, nil
	}
	var drops []string
	for _, id := range in.Drop {
		id, err := check(id)
		if err != nil {
			return out, err
		}
		drops = append(drops, id)
	}
	var keeps []ThemeKeep
	for _, k := range in.Keep {
		id, err := check(k.ID)
		if err != nil {
			return out, err
		}
		keeps = append(keeps, ThemeKeep{ID: id, Area: normID(k.Area)})
	}
	for _, it := range ti.Items {
		if !decided[it.ID] {
			out.Left = append(out.Left, it.ID)
		}
	}
	out.Batch = newBatchID(d)
	m := writeMeta(ctx)
	m.batch = out.Batch
	ctx = WithWrite(ctx, m)
	reason := strings.TrimSpace(in.DropReason)
	if reason == "" {
		reason = ti.Title + " ended"
	}
	for _, id := range drops {
		out.Items = append(out.Items, themeDrop(ctx, d, id, reason))
	}
	for _, k := range keeps {
		if k.Area != "" {
			out.Items = append(out.Items, themeMove(ctx, d, k, ti))
		}
	}
	if tag == "" && ti.Open {
		rt, err := themeCloseRoute(ctx, d, themeID, in, ti.Kind == PartGoal)
		if err != nil {
			return bulkTally(out), err
		}
		vo, err := runRoute(ctx, d, rt)
		if err == nil {
			if f, ok := vo.raw.(Failing); ok {
				err = f.Failed()
			}
		}
		if err != nil {
			return bulkTally(out), fmt.Errorf("the drops and moves ran (batch %s), but the close was refused: %w", out.Batch, err)
		}
		out.Closed, out.Receipt, out.Result = vo.Op, vo.Receipt, vo.Result
		if r, ok := vo.Result["receipt"].(string); ok && out.Receipt == "" {
			out.Receipt = r
		}
	}
	return bulkTally(out), nil
}

func bulkTally(out ThemeCloseOutput) ThemeCloseOutput {
	out.Applied, out.Refused = 0, 0
	for _, it := range out.Items {
		if it.State == BulkRefused {
			out.Refused++
		} else {
			out.Applied++
		}
	}
	if out.Applied == 0 && out.Closed == "" {
		out.Batch = ""
	}
	return out
}

// themeCloseRoute is the route that closes the theme: obligation_change for a
// change, else the close verb's route. A goal closes with force: the owner
// kept what is left.
func themeCloseRoute(ctx context.Context, d Deps, id string, in ThemeCloseInput, goal bool) (verbRoute, error) {
	if in.Change != nil {
		ch := *in.Change
		ch.ID = id
		// obligation_change files its own receipt, with the batch.
		return newRoute(ObligationChange, ch, false)
	}
	fields := in.Fields
	if goal {
		fields = map[string]any{}
		for k, v := range in.Fields {
			fields[k] = v
		}
		fields["force"] = true
	}
	return closeRoute(ctx, d, CloseInput{ID: id, Outcome: in.Outcome, Reason: in.Reason, Fields: fields})
}

// dropOutcome is the close outcome that drops an item of a type.
func dropOutcome(doc model.Document) string {
	switch doc.Meta.(type) {
	case *model.RFX, *model.Incident:
		return ""
	}
	return string(model.OutcomeDropped)
}

// themeDrop drops one item as bulk close does.
func themeDrop(ctx context.Context, d Deps, id, reason string) BulkItem {
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return BulkItem{ID: id, State: BulkRefused, Error: PublicError(linkErr("id", id, err), nil)}
	}
	r := reason
	if _, ok := doc.Meta.(*model.RFX); ok {
		r = "" // rfx_close keeps no reason
	}
	return bulkOne(ctx, d, BulkClose, id, BulkInput{Outcome: dropOutcome(doc), Reason: r})
}

// themeMove moves a kept item to an area with doc_edit: area set, and the
// goal or parent that ends taken off it.
func themeMove(ctx context.Context, d Deps, k ThemeKeep, ti ThemeItemsOutput) (it BulkItem) {
	it = BulkItem{ID: k.ID, State: BulkApplied, Op: DocEdit.Name()}
	refuse := func(err error) BulkItem {
		it.State, it.Error = BulkRefused, PublicError(err, nil)
		return it
	}
	ad, err := d.Store.Get(ctx, k.Area)
	if err != nil {
		return refuse(linkErr("area", k.Area, err))
	}
	if _, ok := ad.Meta.(*model.Area); !ok {
		return refuse(fmt.Errorf("%s is a %s, not an area", ad.ID, typeWord(ad)))
	}
	doc, err := d.Store.Get(ctx, k.ID)
	if err != nil {
		return refuse(linkErr("id", k.ID, err))
	}
	it.Title = doc.Title
	fields := map[string]any{"area": "[[" + ad.ID + "]]"}
	parent, goal := partLinks(doc.Meta)
	if ti.Kind == PartGoal && goal == ti.Theme {
		fields["project"] = nil
	}
	if parent == ti.Theme {
		fields["parent"] = nil
	}
	rt, err := newRoute(DocEdit, DocEditInput{ID: doc.ID, Fields: fields}, true)
	if err != nil {
		return refuse(err)
	}
	vo, err := runRoute(ctx, d, rt)
	if err != nil {
		return refuse(err)
	}
	it.Receipt = vo.Receipt
	return it
}
