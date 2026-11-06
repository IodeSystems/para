package ops

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	evlog "github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
)

// history is the timeline of one item: what happened to it and what was said
// about it, newest first. It joins what the vault already holds: the events
// (who, on which surface, when), receipts (why, what changed, undo), the dated
// entries of the item's notes, the agent work linked to it, and the io items
// threaded to it. It writes nothing and stores nothing.

// Kinds of history rows.
const (
	HistCreated      = "created"
	HistEdited       = "edited"
	HistState        = "state"
	HistMoved        = "moved"
	HistRenamed      = "renamed"
	HistNote         = "note"
	HistAskedAgent   = "asked_agent"
	HistAgentClaimed = "agent_claimed"
	HistAgentResult  = "agent_result"
	HistMessageIn    = "message_in"
	HistMessageOut   = "message_out"
	HistReceipt      = "receipt"
)

// Who of a history row, besides agent:<name>.
const (
	WhoOwner    = "owner"
	WhoTick     = "tick"
	WhoHandEdit = "hand edit"
	WhoSender   = "person"
)

// HistoryFilter is history.filter.
type HistoryFilter string

// History filters: every row, or only what was said (notes, messages, agent
// results, and what the owner asked).
const (
	HistoryAll          HistoryFilter = "all"
	HistoryConversation HistoryFilter = "conversation"
)

// HistoryFilters lists every HistoryFilter.
var HistoryFilters = []HistoryFilter{HistoryAll, HistoryConversation}

// Enum implements the MCP enum hook.
func (HistoryFilter) Enum() []string { return enumStrings(HistoryFilters) }

// HistoryInput is the input of history.
type HistoryInput struct {
	ID     string        `json:"id" query:"id" required:"true"`
	Filter HistoryFilter `json:"filter,omitempty" query:"filter"` // all (default) or conversation
	Limit  int           `json:"limit,omitempty" query:"limit"`   // 0 means 50
	Before string        `json:"before,omitempty" query:"before"` // RFC 3339: only rows before it (next_before of the last page)
}

// HistoryRow is one thing that happened to an item. At is RFC 3339; DateOnly
// says only its day is known. Who is owner, agent:<name>, tick, hand edit, or
// person; Actor is the name behind it. Ref is the receipt, agent work, or io
// item the row comes from. Undo is true when the receipt can still be undone.
type HistoryRow struct {
	At       string `json:"at"`
	DateOnly bool   `json:"date_only,omitempty"`
	Who      string `json:"who"`
	Actor    string `json:"actor,omitempty"`
	Kind     string `json:"kind"`
	Summary  string `json:"summary"`
	Text     string `json:"text,omitempty"`
	Ref      string `json:"ref,omitempty"`
	URL      string `json:"url,omitempty"`
	Undo     bool   `json:"undo"`

	t   time.Time
	seq int
}

// HistoryOutput is one page of history, newest first. NextBefore is the
// before of the next page when More.
type HistoryOutput struct {
	ID         string       `json:"id"`
	Items      []HistoryRow `json:"items"`
	More       bool         `json:"more"`
	NextBefore string       `json:"next_before,omitempty"`
}

const (
	historyWindow = 20000
	historyLimit  = 50
	historyText   = 2000
	// receiptSlack is how long before its receipt the write of a receipt may
	// have been logged.
	receiptSlack = 10 * time.Second
)

// History shows the timeline of an item.
var History = defineRead("history", "Show the timeline of an item, newest first: created, edited, state, moved, renamed, note (each dated entry of its notes), asked_agent, agent_claimed, agent_result (the agent work linked to it), message_in, message_out (io items threaded to it), and receipt (an agent write: why, what changed, undo). who is owner, agent:<name>, tick, hand edit, or person. filter conversation keeps notes, messages, agent results, and what the owner asked. Page with before (next_before)", func(ctx context.Context, d Deps, in HistoryInput) (HistoryOutput, error) {
	if in.Filter != "" && !slices.Contains(HistoryFilters, in.Filter) {
		return HistoryOutput{}, fmt.Errorf("filter %q: want all or conversation", in.Filter)
	}
	var before time.Time
	if in.Before != "" {
		t, err := time.Parse(time.RFC3339Nano, in.Before)
		if err != nil {
			return HistoryOutput{}, fmt.Errorf("before %q: want an RFC 3339 time", in.Before)
		}
		before = t
	}
	limit := in.Limit
	if limit <= 0 {
		limit = historyLimit
	}
	rows, id, err := historyRows(ctx, d, in.ID)
	if err != nil {
		return HistoryOutput{}, err
	}
	out := HistoryOutput{ID: id, Items: []HistoryRow{}}
	for _, r := range rows {
		if in.Filter == HistoryConversation && !conversational(r) {
			continue
		}
		if !before.IsZero() && !r.t.Before(before) {
			continue
		}
		// A page never splits rows of one instant, so before loses none.
		if len(out.Items) >= limit && !r.t.Equal(out.Items[len(out.Items)-1].t) {
			out.More = true
			break
		}
		out.Items = append(out.Items, r)
	}
	if out.More {
		out.NextBefore = out.Items[len(out.Items)-1].At
	}
	return out, nil
})

// conversational reports whether a row is part of the conversation on an item.
func conversational(r HistoryRow) bool {
	switch r.Kind {
	case HistNote, HistMessageIn, HistMessageOut, HistAgentResult:
		return true
	case HistAskedAgent:
		return r.Who == WhoOwner
	}
	return false
}

// whoOf names who made an event: the owner on a person's surface, the tick, a
// hand edit, or an agent.
func whoOf(actor, surface, op string) string {
	switch {
	case op == evlog.OpExternalEdit:
		return WhoHandEdit
	case surface == TickSurface:
		return WhoTick
	case slices.Contains(PersonSurfaces, surface):
		return WhoOwner
	}
	return AgentPrefix + actor
}

// histEvent is one event of the item, and whether a row already tells it.
type histEvent struct {
	e    model.Event
	idx  int
	used bool // a receipt, an undo, or a note tells it
	note bool // a notes entry took its time
}

// itemOps are the vault ops on the item itself that make rows.
var itemOps = []string{evlog.OpCreate, evlog.OpUpdate, evlog.OpMove, evlog.OpRename, evlog.OpExternalEdit}

// historyRows returns every row of an item, newest first, and its id.
func historyRows(ctx context.Context, d Deps, raw string) ([]HistoryRow, string, error) {
	id := normID(raw)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return nil, id, linkErr("id", id, err)
	}
	id = doc.ID
	evs, err := d.Store.Events(ctx, historyWindow)
	if err != nil {
		return nil, id, err
	}
	// The ids the item had: a rename keeps its earlier events.
	ids := []string{id}
	for i := len(evs) - 1; i >= 0; i-- {
		if e := evs[i]; e.Op == evlog.OpRename && slices.Contains(ids, e.To) && !slices.Contains(ids, e.From) {
			ids = append(ids, e.From)
		}
	}
	var mine []*histEvent
	receiptAt := map[string]int{}
	byDoc := map[string][]idxEvent{}
	for i, e := range evs {
		byDoc[e.Doc] = append(byDoc[e.Doc], idxEvent{e, i})
		switch {
		case e.Op == "receipt":
			receiptAt[e.To] = i
		case slices.Contains(ids, e.Doc) && slices.Contains(itemOps, e.Op):
			mine = append(mine, &histEvent{e: e, idx: i})
		}
	}
	h := &histBuild{ctx: ctx, d: d, ids: ids, mine: mine}
	if err := h.receipts(receiptAt); err != nil {
		return nil, id, err
	}
	h.undos(evs)
	h.notes(doc)
	if err := h.requests(byDoc); err != nil {
		return nil, id, err
	}
	if err := h.messages(byDoc); err != nil {
		return nil, id, err
	}
	h.events()
	slices.SortStableFunc(h.rows, func(a, b HistoryRow) int {
		if c := b.t.Compare(a.t); c != 0 {
			return c
		}
		return b.seq - a.seq
	})
	return h.rows, id, nil
}

type histBuild struct {
	ctx  context.Context
	d    Deps
	ids  []string
	mine []*histEvent
	rows []HistoryRow
}

// idxEvent is an event and its place in the log.
type idxEvent struct {
	model.Event
	idx int
}

// add appends a row at t. seq orders rows of one instant: the event index.
func (h *histBuild) add(t time.Time, seq int, r HistoryRow) {
	r.t, r.seq = t, seq
	r.At = t.UTC().Format(time.RFC3339Nano)
	r.Text = clip(r.Text, historyText)
	h.rows = append(h.rows, r)
}

// clip cuts text to n runes.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if rs := []rune(s); len(rs) > n {
		return string(rs[:n]) + "..."
	}
	return s
}

// eventAt is the event nearest t, or nil.
func eventAt(evs []idxEvent, t time.Time) *idxEvent {
	var best *idxEvent
	for i := range evs {
		if best == nil || absDur(evs[i].TS.Sub(t)) < absDur(best.TS.Sub(t)) {
			best = &evs[i]
		}
	}
	return best
}

// --- receipts ---

type histReceipt struct {
	doc  model.Document
	r    *model.Receipt
	part model.ReceiptPart
	all  []model.ReceiptPart
	t    time.Time
	idx  int
}

// receipts adds a row per receipt of a write to the item. The vault events of
// that write are used: the receipt tells them.
func (h *histBuild) receipts(receiptAt map[string]int) error {
	refs, err := h.d.Store.List(h.ctx, store.Filter{Folder: model.FolderReceipts, Type: model.TypeReceipt, All: true})
	if err != nil {
		return err
	}
	var found []histReceipt
	for _, ref := range refs {
		doc, r, err := getReceipt(h.ctx, h.d.Store, ref.ID)
		if err != nil {
			continue
		}
		if !slices.Contains(h.ids, r.Doc) && !slices.ContainsFunc(h.ids, func(id string) bool { return strings.Contains(doc.Body, id) }) {
			continue
		}
		c, err := model.ParseReceiptBody(doc.Body)
		if err != nil {
			continue
		}
		parts := receiptParts(r.Doc, c)
		at := slices.IndexFunc(parts, func(p model.ReceiptPart) bool { return slices.Contains(h.ids, p.Doc) })
		if at < 0 {
			continue
		}
		hr := histReceipt{doc: doc, r: r, part: parts[at], all: parts, idx: -1}
		if t, err := time.Parse(time.RFC3339Nano, r.At); err == nil {
			hr.t = t
		} else if t, err := time.ParseInLocation(dateLayout, string(r.Created), time.Local); err == nil {
			hr.t = t
		}
		if i, ok := receiptAt[doc.ID]; ok {
			hr.idx = i
		}
		found = append(found, hr)
	}
	// Earlier receipts take earlier events.
	slices.SortStableFunc(found, func(a, b histReceipt) int { return a.t.Compare(b.t) })
	for _, hr := range found {
		seq := hr.idx
		for _, e := range h.mine {
			if e.used || e.e.Actor != hr.r.Actor || e.e.Op == evlog.OpExternalEdit || e.e.TS.After(hr.t.Add(time.Second)) || e.e.TS.Before(hr.t.Add(-receiptSlack)) {
				continue
			}
			if hr.idx >= 0 && e.idx > hr.idx {
				continue
			}
			e.used = true
			seq = max(seq, e.idx)
		}
		summary := opWords(hr.r.Op)
		if fields := changedFields(hr.part.Before, hr.part.After); len(fields) > 0 {
			summary += ": " + strings.Join(fields, ", ")
		}
		if hr.r.Undone != "" {
			summary += " (undone)"
		}
		h.add(hr.t, seq, HistoryRow{Who: whoOf(hr.r.Actor, hr.r.Surface, ""), Actor: hr.r.Actor, Kind: HistReceipt,
			Summary: summary, Text: hr.r.Why, Ref: hr.doc.ID, Undo: h.undoable(hr)})
	}
	return nil
}

// undoable reports whether receipt_undo would put the write back now.
func (h *histBuild) undoable(hr histReceipt) bool {
	if hr.r.Undone != "" || hr.r.Op == opAlignRecord || hr.r.Op == opActionRecord {
		return false
	}
	for _, p := range hr.all {
		cur, err := h.d.Store.Get(h.ctx, p.Doc)
		if err != nil || !sameText(renderText(cur), p.After) {
			return false
		}
	}
	return true
}

// changedFields names what a write changed: the frontmatter keys, notes, and
// body. A write that made the document says created.
func changedFields(before, after string) []string {
	if before == "" {
		return []string{"created"}
	}
	a, b := paramd.Parse("x.md", []byte(before)), paramd.Parse("x.md", []byte(after))
	var out []string
	keys := map[string]bool{}
	for k := range a.Frontmatter {
		keys[k] = true
	}
	for k := range b.Frontmatter {
		keys[k] = true
	}
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		if k == "updated" {
			continue
		}
		if fmt.Sprint(a.Frontmatter[k]) != fmt.Sprint(b.Frontmatter[k]) {
			out = append(out, k)
		}
	}
	if headingText(a.Body, notesHeading) != headingText(b.Body, notesHeading) {
		out = append(out, "notes")
	}
	if strings.TrimSpace(dropSection(a.Body, notesHeading)) != strings.TrimSpace(dropSection(b.Body, notesHeading)) {
		out = append(out, "body")
	}
	return out
}

// undos adds a row per receipt_undo of a write to the item; the vault events
// of the undo are used.
func (h *histBuild) undos(evs []model.Event) {
	for i, e := range evs {
		if e.Op != opReceiptUndo || !slices.Contains(h.ids, e.To) {
			continue
		}
		for _, m := range h.mine {
			if !m.used && m.e.Actor == e.Actor && m.idx < i && !m.e.TS.Before(e.TS.Add(-receiptSlack)) && m.e.Op != evlog.OpExternalEdit {
				m.used = true
			}
		}
		h.add(e.TS, i, HistoryRow{Who: whoOf(e.Actor, e.Surface, e.Op), Actor: e.Actor, Kind: HistEdited, Summary: "Undid a write", Ref: e.Doc})
	}
}

// --- notes ---

var (
	// noteHead is the header of an entry the composer writes: **YYYY-MM-DD**.
	noteHead = regexp.MustCompile(`^\*\*(\d{4}-\d{2}-\d{2})\*\*\s*$`)
	// noteLine is a dated line an op writes: - YYYY-MM-DD: text.
	noteLine = regexp.MustCompile(`^- (\d{4}-\d{2}-\d{2}): (.*)$`)
)

// noteEntry is one entry of an item's notes. Date is empty for the text
// before the first dated entry.
type noteEntry struct {
	Date string
	Text string
}

// noteEntries splits notes into dated entries. Text before the first entry is
// one entry with no date.
func noteEntries(md string) []noteEntry {
	var out []noteEntry
	var cur *noteEntry
	var plain []string
	for _, l := range strings.Split(md, "\n") {
		if m := noteHead.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			out = append(out, noteEntry{Date: m[1]})
			cur = &out[len(out)-1]
			continue
		}
		if m := noteLine.FindStringSubmatch(l); m != nil {
			out = append(out, noteEntry{Date: m[1], Text: m[2]})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			plain = append(plain, l)
			continue
		}
		cur.Text += "\n" + l
	}
	for i := range out {
		out[i].Text = strings.TrimSpace(out[i].Text)
	}
	if p := strings.TrimSpace(strings.Join(plain, "\n")); p != "" {
		out = append([]noteEntry{{Text: p}}, out...)
	}
	return out
}

// notes adds a row per entry of the item's notes. A dated entry takes the time
// and author of the last write to the item that day that made no other row;
// with none, it is the owner's, on its day.
func (h *histBuild) notes(doc model.Document) {
	entries := noteEntries(headingText(doc.Body, notesHeading))
	last := len(h.mine)
	for i := len(entries) - 1; i >= 0; i-- {
		en := entries[i]
		row := HistoryRow{Who: WhoOwner, Kind: HistNote, Summary: firstLine(en.Text), Text: en.Text}
		if en.Date == "" {
			day, _ := time.ParseInLocation(dateLayout, string(doc.Meta.Header().Updated), time.Local)
			row.DateOnly = true
			h.add(day.Add(12*time.Hour), -1, row)
			continue
		}
		day, err := time.ParseInLocation(dateLayout, en.Date, time.Local)
		if err != nil {
			continue
		}
		j := last - 1
		for ; j >= 0; j-- {
			e := h.mine[j]
			if e.note || e.e.Op != evlog.OpUpdate || e.e.From != e.e.To || e.e.TS.In(time.Local).Format(dateLayout) != en.Date {
				continue
			}
			break
		}
		if j < 0 {
			row.DateOnly = true
			h.add(day.Add(12*time.Hour), -1, row)
			continue
		}
		e := h.mine[j]
		e.note, e.used, last = true, true, j
		row.Who, row.Actor = whoOf(e.e.Actor, e.e.Surface, e.e.Op), e.e.Actor
		h.add(e.e.TS, e.idx, row)
	}
}

// --- agent work ---

// reqSection is one "## " section of a request body.
type reqSection struct{ head, text string }

func reqSections(body string) []reqSection {
	var out []reqSection
	for _, sec := range strings.Split("\n"+body, "\n## ")[1:] {
		head, text, _ := strings.Cut(sec, "\n")
		out = append(out, reqSection{head: strings.TrimSpace(head), text: strings.TrimSpace(text)})
	}
	return out
}

// nth is the text and heading of the n-th section (0-based) whose heading
// starts with prefix.
func nth(secs []reqSection, prefix string, n int) (string, string) {
	for _, s := range secs {
		if strings.HasPrefix(s.head, prefix) {
			if n == 0 {
				return s.head, s.text
			}
			n--
		}
	}
	return "", ""
}

// ownerWords is what the owner wrote in a request: the composer's words, or
// the text of an Ask.
func ownerWords(secs []reqSection) string {
	for _, s := range secs {
		if s.head == "Owner's words" || s.head == "Ask" {
			return s.text
		}
	}
	return ""
}

// requests adds rows for the agent work linked to the item: asked, claimed,
// and how each turn ended.
func (h *histBuild) requests(byDoc map[string][]idxEvent) error {
	docs, err := loadRequests(h.ctx, h.d.Store)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if !slices.ContainsFunc(append(slices.Clone(r.Links), r.Context...), func(l model.Link) bool { return slices.Contains(h.ids, l.Target()) }) {
			continue
		}
		h.request(doc, r, byDoc[doc.ID])
	}
	return nil
}

func (h *histBuild) request(doc model.Document, r *model.Request, evs []idxEvent) {
	secs := reqSections(doc.Body)
	title := firstOf(doc.Title, doc.ID)
	kind := requestKind(doc.Title, r)
	askWho := func(e *idxEvent) (string, string) {
		if kind == OwnerAskKind || (e == nil && isAsked(doc.Title, r)) {
			return WhoOwner, ""
		}
		if e == nil {
			return AgentPrefix + "unknown", ""
		}
		return whoOf(e.Actor, e.Surface, e.Op), e.Actor
	}
	asked := HistoryRow{Kind: HistAskedAgent, Summary: "Asked an agent: " + title, Text: ownerWords(secs), Ref: doc.ID}
	created := createEvent(evs)
	if created == nil {
		// The events of this work are past the window: its fields say when.
		asked.Who, asked.Actor = askWho(nil)
		if t, ok := reqTime(r.Queued); ok {
			h.add(t, -1, asked)
		}
		if t, ok := reqTime(r.ClaimedAt); ok && r.Claimed != "" && kind != OwnerAskKind {
			h.add(t, -1, HistoryRow{Who: AgentPrefix + r.Claimed, Actor: r.Claimed, Kind: HistAgentClaimed, Summary: "Agent took: " + title, Ref: doc.ID})
		}
		if t, ok := reqTime(r.ActiveAt); ok && (r.Status == model.RequestDone || r.Status == model.RequestFailed) {
			prefix := "Result"
			if r.Status == model.RequestFailed {
				prefix = "Failure"
			}
			n := -1
			for _, s := range secs {
				if strings.HasPrefix(s.head, prefix) {
					n++
				}
			}
			head, text := nth(secs, prefix, max(n, 0))
			h.add(t, -1, HistoryRow{Who: AgentPrefix + firstOf(r.Claimed, "unknown"), Actor: r.Claimed, Kind: HistAgentResult,
				Summary: resultWords(head, r.Status) + title, Text: text, Ref: doc.ID})
		}
		return
	}
	asked.Who, asked.Actor = askWho(created)
	h.add(created.TS, created.idx, asked)
	results, failures, responses := 0, 0, 0
	for _, e := range evs {
		if e.Op != evlog.OpUpdate || e.From == e.To {
			continue
		}
		i, who, actor := e.idx, whoOf(e.Actor, e.Surface, e.Op), e.Actor
		switch model.RequestStatus(e.To) {
		case model.RequestClaimed:
			h.add(e.TS, i, HistoryRow{Who: who, Actor: actor, Kind: HistAgentClaimed, Summary: "Agent took: " + title, Ref: doc.ID})
		case model.RequestDone:
			head, text := nth(secs, "Result", results)
			results++
			h.add(e.TS, i, HistoryRow{Who: who, Actor: actor, Kind: HistAgentResult, Summary: resultWords(head, model.RequestDone) + title, Text: text, Ref: doc.ID})
		case model.RequestFailed:
			_, text := nth(secs, "Failure", failures)
			failures++
			h.add(e.TS, i, HistoryRow{Who: who, Actor: actor, Kind: HistAgentResult, Summary: "Agent failed: " + title, Text: text, Ref: doc.ID})
		case model.RequestCancelled:
			h.add(e.TS, i, HistoryRow{Who: who, Actor: actor, Kind: HistState, Summary: "Agent work cancelled: " + title, Ref: doc.ID})
		case model.RequestQueued:
			if e.From != string(model.RequestDone) && e.From != string(model.RequestFailed) {
				continue
			}
			head, text := nth(secs, "Response", responses)
			responses++
			_, how, _ := strings.Cut(head, "(")
			h.add(e.TS, i, HistoryRow{Who: who, Actor: actor, Kind: HistAskedAgent, Summary: "Responded (" + firstOf(strings.TrimSuffix(how, ")"), "change") + "): " + title, Text: text, Ref: doc.ID})
		}
	}
}

// resultWords starts the summary of a turn's end from its heading: Result,
// or Result (partial).
func resultWords(head string, status model.RequestStatus) string {
	if status == model.RequestFailed {
		return "Agent failed: "
	}
	if _, res, ok := strings.Cut(head, "("); ok {
		return "Agent ended (" + strings.TrimSuffix(res, ")") + "): "
	}
	return "Agent done: "
}

// createEvent is the create event of a document, or nil.
func createEvent(evs []idxEvent) *idxEvent {
	for i := range evs {
		if evs[i].Op == evlog.OpCreate {
			return &evs[i]
		}
	}
	return nil
}

func reqTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

// --- messages ---

// messages adds a row per io item threaded to the item: filed into it,
// linked to it, or answering it, and the replies to those.
func (h *histBuild) messages(byDoc map[string][]idxEvent) error {
	refs, err := h.d.Store.List(h.ctx, store.Filter{Type: model.TypeIO, All: true})
	if err != nil {
		return err
	}
	type ioDoc struct {
		doc model.Document
		m   *model.IO
	}
	var all []ioDoc
	for _, ref := range refs {
		doc, err := h.d.Store.Get(h.ctx, ref.ID)
		if err != nil {
			continue
		}
		if m, ok := doc.Meta.(*model.IO); ok && !slices.Contains(h.ids, doc.ID) {
			all = append(all, ioDoc{doc, m})
		}
	}
	in := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, x := range all {
			if in[x.doc.ID] {
				continue
			}
			hit := func(l model.Link) bool { t := l.Target(); return slices.Contains(h.ids, t) || in[t] }
			if hit(x.m.Thread) || (x.m.Became != "" && slices.Contains(h.ids, x.m.Became.Target())) ||
				slices.ContainsFunc(x.m.Links, func(l model.Link) bool { return slices.Contains(h.ids, l.Target()) }) {
				in[x.doc.ID], changed = true, true
			}
		}
	}
	for _, x := range all {
		if in[x.doc.ID] {
			h.message(x.doc, x.m, byDoc[x.doc.ID])
		}
	}
	return nil
}

func (h *histBuild) message(doc model.Document, m *model.IO, evs []idxEvent) {
	subject := firstOf(m.Subject, doc.Title, doc.ID)
	at, ok := reqTime(m.At)
	created := createEvent(evs)
	seq := -1
	if e := eventAt(evs, at); ok && e != nil {
		seq = e.idx
	}
	if !ok {
		switch {
		case created != nil:
			at, seq = created.TS, created.idx
		case m.Date != "":
			at, _ = time.ParseInLocation(dateLayout, string(m.Date), time.Local)
		default:
			at, _ = time.ParseInLocation(dateLayout, string(m.Created), time.Local)
		}
	}
	text := strings.TrimSpace(doc.Body)
	if m.Direction == model.IOIn {
		h.add(at, seq, HistoryRow{Who: WhoSender, Actor: h.name(m.From), Kind: HistMessageIn, Summary: subject, Text: text, Ref: doc.ID, URL: firstOf(m.Ref, m.URL)})
		return
	}
	who, actor := WhoOwner, ""
	if e := eventAt(evs, at); e != nil {
		who, actor = whoOf(e.Actor, e.Surface, e.Op), e.Actor
	}
	summary := subject
	if m.State != model.IOSent && !m.Logged() {
		summary = string(m.State) + ": " + subject
	}
	h.add(at, seq, HistoryRow{Who: who, Actor: actor, Kind: HistMessageOut, Summary: summary, Text: text, Ref: doc.ID, URL: firstOf(m.Ref, m.URL)})
}

// name is the title of a linked person or group, or the text as it is.
func (h *histBuild) name(from string) string {
	if t := model.Link(from).Target(); t != "" && t != from {
		if doc, err := h.d.Store.Get(h.ctx, t); err == nil {
			return firstOf(doc.Title, t)
		}
		return t
	}
	return from
}

// --- vault events ---

// events adds a row per event of the item that no other row tells.
func (h *histBuild) events() {
	for _, x := range h.mine {
		if x.used {
			continue
		}
		e := x.e
		row := HistoryRow{Who: whoOf(e.Actor, e.Surface, e.Op), Actor: e.Actor}
		switch {
		case e.Op == evlog.OpCreate:
			row.Kind, row.Summary = HistCreated, "Created in "+e.To
		case e.Op == evlog.OpMove:
			row.Kind, row.Summary = HistMoved, "Moved from "+e.From+" to "+e.To
		case e.Op == evlog.OpRename:
			row.Kind, row.Summary = HistRenamed, "Renamed from "+e.From+" to "+e.To
		case e.Op == evlog.OpExternalEdit:
			row.Kind, row.Summary = HistEdited, "Edited by hand, outside para"
		case e.From != e.To:
			row.Kind, row.Summary = HistState, firstOf(e.From, "none")+" → "+firstOf(e.To, "none")
		default:
			row.Kind, row.Summary = HistEdited, "Edited"
		}
		h.add(e.TS, x.idx, row)
	}
}
