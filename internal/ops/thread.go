package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// A thread is the conversation about one key (model/thread.go): an item id, a
// "#tag" named in threads, or "channel:<source>/<thread>". Each owner turn is
// one request (thread_post): kind sent-to-agent, thread the key, linked to the
// item when the key is an item, so the item leaves Next while an agent has
// the turn. A turn on finished work reopens that work as its next turn
// (request_respond, an alias of thread_post). thread reads the turns in time
// order: requests, the io of the key, and the receipts made under each.

// ThreadPostInput is one owner turn. Key or id names the thread: id is an
// item, and its own thread when key is empty.
type ThreadPostInput struct {
	Key  string `json:"key,omitempty"` // item id, #tag, or channel:<source>/<thread>
	ID   string `json:"id,omitempty"`  // the item the turn is about
	Text string `json:"text"`
	// Kind is modify (change the item or draft), ask (a question), redirect
	// (change the direction of the work), or other. Default other.
	Kind        model.RequestTurnKind `json:"kind,omitempty"`
	Quote       string                `json:"quote,omitempty"`       // the text the turn is about, quoted from the item
	Attachments []string              `json:"attachments,omitempty"` // vault paths (attachment_add)
}

// ThreadPostOutput is the request that carries the turn. Reopened is true
// when the turn reopened finished work (id named a done or failed request).
// Note says why nothing may run it yet.
type ThreadPostOutput struct {
	Request  string              `json:"request"`
	Status   model.RequestStatus `json:"status"`
	Thread   string              `json:"thread"`
	Title    string              `json:"title"`
	Reopened bool                `json:"reopened"`
	Rev      string              `json:"rev"`
	Note     string              `json:"note,omitempty"`
}

// threadPlan is what one turn resolves to.
type threadPlan struct {
	key  string
	item *model.Document // the item the turn is about, if any
	// respond is set when the turn reopens finished work.
	respond bool
}

// resolveThread finds the thread key and the item of key or id. An item's
// key is its id; a request's is the thread it is in (requestThread). With
// key and id both, key is the thread and id the item. With only a key that
// is an item id, that item is the item.
func resolveThread(ctx context.Context, d Deps, key, id string) (string, *model.Document, error) {
	var item *model.Document
	k := ""
	if id = normID(id); id != "" {
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			return "", nil, linkErr("id", id, err)
		}
		item, k = &doc, model.ThreadKeyItem(doc.ID)
		if r, ok := doc.Meta.(*model.Request); ok {
			k = requestThread(doc, r)
		}
	}
	if strings.TrimSpace(key) != "" {
		var err error
		if k, err = model.NormalizeThreadKey(key); err != nil {
			return "", nil, fmt.Errorf("key: %w", err)
		}
	}
	if k == "" {
		return "", nil, errors.New("give key or id")
	}
	if item == nil {
		if kind, v, _ := model.ParseThreadKey(k); kind == model.ThreadItem {
			doc, err := d.Store.Get(ctx, v)
			if err != nil {
				return "", nil, linkErr("key", v, err)
			}
			item = &doc
		}
	}
	return k, item, nil
}

// metaOf is the meta of doc, nil for no document.
func metaOf(doc *model.Document) model.Meta {
	if doc == nil {
		return nil
	}
	return doc.Meta
}

// planTurn resolves the thread key and the item of a turn.
func planTurn(ctx context.Context, d Deps, in ThreadPostInput) (threadPlan, error) {
	key, item, err := resolveThread(ctx, d, in.Key, in.ID)
	if err != nil {
		return threadPlan{}, err
	}
	p := threadPlan{key: key, item: item}
	if item == nil || normID(in.ID) != item.ID {
		return p, nil
	}
	if r, ok := item.Meta.(*model.Request); ok {
		p.respond = strings.TrimSpace(in.Key) == "" && (r.Status == model.RequestDone || r.Status == model.RequestFailed)
	}
	return p, nil
}

// requestThread is the thread a request is in: its thread key, else the item
// it links, else its own id.
func requestThread(doc model.Document, r *model.Request) string {
	switch {
	case r.Thread != "":
		return r.Thread
	case len(r.Links) > 0:
		return r.Links[0].Target()
	}
	return doc.ID
}

// respondKindOf is the request_respond kind of a turn kind.
func respondKindOf(k model.RequestTurnKind) RespondKind {
	if k == model.RequestTurnAsk {
		return RespondClarify
	}
	return RespondChange
}

// turnKindNote says what each turn kind asks of the agent.
func turnKindNote(k model.RequestTurnKind) string {
	switch k {
	case model.RequestTurnModify:
		return "modify: change the item (or its draft) as the words say."
	case model.RequestTurnAsk:
		return "ask: answer the question in your reply; change nothing the words do not ask for."
	case model.RequestTurnRedirect:
		return "redirect: the owner changes the direction of the work; follow the new direction and say what changes."
	}
	return "other: read the words and act on them."
}

// quoteBlock is text as a Markdown quote.
func quoteBlock(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

// threadAsk is the body of a turn: the thread, the item, the kind, the quote,
// the owner's words, and the attachments.
func threadAsk(key string, item *model.Document, kind model.RequestTurnKind, quote, text string, atts []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The owner wrote a turn in the thread %s", key)
	if item != nil {
		fmt.Fprintf(&b, " about [[%s]] (%s: %s)", item.ID, item.Meta.Header().Type, firstOf(item.Title, item.ID))
	}
	b.WriteString(". Read the thread (thread) for the turns before this one.\n\n")
	b.WriteString("Kind: " + turnKindNote(kind) + "\n\n")
	b.WriteString("End the work with request_done and reply: your answer to the owner, shown in the thread.")
	if quote != "" {
		b.WriteString("\n\n## Quote\n\n" + quoteBlock(quote))
	}
	b.WriteString("\n\n## Owner's words\n\n")
	if text != "" {
		b.WriteString(flattenHeadings(text))
	} else {
		b.WriteString("(none: see the attachments)")
	}
	if len(atts) > 0 {
		b.WriteString("\n\n## Attachments\n")
		for _, a := range atts {
			b.WriteString("\n- " + model.AttachmentEmbed(a))
		}
	}
	return b.String()
}

// postTurn queues one owner turn: every owner turn comes here (thread_post,
// agent_send). The request carries the thread key, kind, and quote.
func postTurn(ctx context.Context, d Deps, add RequestAddInput, turn ownerTurn) (RequestAddOutput, error) {
	if _, _, err := model.ParseThreadKey(turn.thread); err != nil {
		return RequestAddOutput{}, err
	}
	if turn.kind != "" && !slices.Contains(model.RequestTurnKinds, turn.kind) {
		return RequestAddOutput{}, fmt.Errorf("kind %q: want one of %v", turn.kind, model.RequestTurnKinds)
	}
	add.turn = turn
	return RequestAdd.Run(ctx, d, add)
}

// turnLinkTypes are the item types a turn links, so the item is the agent's
// turn (needs.go agent_turn); any other item goes in context.
var turnLinkTypes = []model.Type{model.TypeTask, model.TypeObligation, model.TypeProject, model.TypeProposal, model.TypeJournal, model.TypeQuestion,
	model.TypeMeeting, model.TypeRFX, model.TypeRequest, model.TypeIO, model.TypeArea, model.TypeNote, model.TypeTrackedDoc, model.TypePerson, model.TypeGroup}

// ThreadPost is one owner turn in a thread.
var ThreadPost = definePerson("thread_post", "Post the owner's turn in a thread: key (an item id, #tag, or channel:<source>/<thread>) or id (an item; its own thread). kind modify, ask, redirect, or other; quote is the text the turn is about. "+
	"It queues one request per turn (kind sent-to-agent, linked to the item) even while an agent holds earlier turns; on a done or failed request (id) it reopens that work as its next turn. People only", func(ctx context.Context, d Deps, in ThreadPostInput) (ThreadPostOutput, error) {
	text := strings.TrimSpace(strings.ReplaceAll(in.Text, "\r\n", "\n"))
	quote := strings.TrimSpace(strings.ReplaceAll(in.Quote, "\r\n", "\n"))
	kind := in.Kind
	if kind == "" {
		kind = model.RequestTurnOther
	}
	if !slices.Contains(model.RequestTurnKinds, kind) {
		return ThreadPostOutput{}, fmt.Errorf("kind %q: want one of %v", kind, model.RequestTurnKinds)
	}
	if text == "" && len(in.Attachments) == 0 {
		return ThreadPostOutput{}, errors.New("write the turn, or attach a file")
	}
	for name, v := range map[string]string{"text": text, "quote": quote} {
		if v != "" {
			if err := lintText(name, v); err != nil {
				return ThreadPostOutput{}, err
			}
		}
	}
	p, err := planTurn(ctx, d, in)
	if err != nil {
		return ThreadPostOutput{}, err
	}
	var atts []string
	for _, a := range in.Attachments {
		st, err := d.Store.AttachmentStat(ctx, strings.TrimSpace(a))
		if err != nil {
			return ThreadPostOutput{}, fmt.Errorf("attachment %q: %w", a, err)
		}
		if !slices.Contains(atts, st.Path) {
			atts = append(atts, st.Path)
		}
	}
	if p.respond {
		words := text
		for _, a := range atts {
			words += "\n\n- " + model.AttachmentEmbed(a)
		}
		doc, r, err := respondTo(ctx, d, RequestRespondInput{ID: p.item.ID, Kind: respondKindOf(kind), Text: strings.TrimSpace(words)}, ownerTurn{kind: kind, quote: quote})
		if err != nil {
			return ThreadPostOutput{}, err
		}
		return ThreadPostOutput{Request: doc.ID, Status: r.Status, Thread: p.key, Title: doc.Title, Reopened: true, Rev: doc.Rev}, nil
	}
	add := RequestAddInput{Kind: SentKind, Ask: threadAsk(p.key, p.item, kind, quote, text, atts)}
	switch {
	case p.item != nil:
		add.Title = composerPrefix + firstOf(p.item.Title, p.item.ID)
		if slices.Contains(turnLinkTypes, p.item.Meta.Header().Type) {
			add.Links = []string{p.item.ID}
		} else {
			add.Context = []string{p.item.ID}
		}
		// A turn on agent work is about what that work is about too.
		if r, ok := p.item.Meta.(*model.Request); ok {
			for _, l := range r.Links {
				if !slices.Contains(add.Links, l.Target()) {
					add.Links = append(add.Links, l.Target())
				}
			}
		}
	default:
		add.Title = composerPrefix + p.key
	}
	r, err := postTurn(ctx, d, add, ownerTurn{thread: p.key, kind: kind, quote: quote})
	if err != nil {
		return ThreadPostOutput{}, err
	}
	out := ThreadPostOutput{Request: r.ID, Status: r.Status, Thread: p.key, Title: add.Title, Rev: r.Rev}
	if r.Status == model.RequestProposed {
		out.Note = "Proposed: this surface cannot queue agent work (" + queueConfig + "); accept it with request_accept."
	}
	return out, nil
})

// thread_post routes to request_respond on finished work, so request_respond
// is its alias (opalias.go); any other turn runs thread_post itself.
var threadPostRouted = func() bool {
	verbRoutes[ThreadPost.Name()] = func(ctx context.Context, d Deps, raw json.RawMessage) (verbRoute, error) {
		var in ThreadPostInput
		if err := decodeStrict(raw, &in); err != nil {
			return verbRoute{}, err
		}
		if in.Quote == "" && len(in.Attachments) == 0 {
			if p, err := planTurn(ctx, d, in); err == nil && p.respond {
				return newRoute(RequestRespond, RequestRespondInput{ID: in.ID, Kind: respondKindOf(in.Kind), Text: in.Text}, false)
			}
		}
		return verbRoute{op: ThreadPost, input: raw}, nil
	}
	return true
}()

// --- thread ---

// ThreadInput names a thread: key, or id (an item and its own thread, with
// the keys in its threads field).
type ThreadInput struct {
	Key string `json:"key,omitempty" query:"key"`
	ID  string `json:"id,omitempty" query:"id"`
}

// ThreadNote is a progress note an agent added (request_note).
type ThreadNote struct {
	At    string `json:"at"`
	Actor string `json:"actor"`
	Text  string `json:"text"`
}

// ThreadStep is one earlier part of a request that went more than one turn:
// a response, a result, a failure, or a reply, in body order.
type ThreadStep struct {
	Heading string `json:"heading"`
	Text    string `json:"text"`
}

// ThreadReceipt is one write made under a turn.
type ThreadReceipt struct {
	ID      string `json:"id"`
	At      string `json:"at,omitempty"`
	Actor   string `json:"actor"`
	Op      string `json:"op"`
	Doc     string `json:"doc"`
	DocName string `json:"doc_name,omitempty" nameof:"Doc"`
	Why     string `json:"why,omitempty"`
	Undone  string `json:"undone,omitempty"`
}

// ThreadTurnKind is what a thread turn is: request (an owner turn and the
// agent's answer) or io (a message in or out).
type ThreadTurnKind string

const (
	ThreadTurnRequest ThreadTurnKind = "request"
	ThreadTurnIO      ThreadTurnKind = "io"
)

// ThreadTurnKinds lists every ThreadTurnKind.
var ThreadTurnKinds = []ThreadTurnKind{ThreadTurnRequest, ThreadTurnIO}

// Enum lists the allowed values, for schema generation.
func (ThreadTurnKind) Enum() []string { return enumStrings(ThreadTurnKinds) }

// ThreadTurn is one turn. A request turn has the owner's words, kind, quote,
// and attachments; who holds it and since when; how it ended (resolution,
// result, reply, error); the agent's notes; and Steps when it went more than
// one turn. An io turn has the message and its attempts to go out. Receipts
// are the writes made under it (by request, or with the io as input).
type ThreadTurn struct {
	At   string         `json:"at"`
	Kind ThreadTurnKind `json:"kind"`

	Request     string                  `json:"request,omitempty"`
	Title       string                  `json:"title,omitempty"`
	RequestKind string                  `json:"request_kind,omitempty"`
	OwnerWords  string                  `json:"owner_words,omitempty"`
	TurnKind    model.RequestTurnKind   `json:"turn_kind,omitempty"`
	Quote       string                  `json:"quote,omitempty"`
	Attachments []string                `json:"attachments,omitempty"`
	Status      model.RequestStatus     `json:"status,omitempty"`
	ClaimedBy   string                  `json:"claimed_by,omitempty"`
	ClaimedAt   string                  `json:"claimed_at,omitempty"`
	ActiveAt    string                  `json:"active_at,omitempty"`
	Resolution  model.RequestResolution `json:"resolution,omitempty"`
	Turn        int                     `json:"turn,omitempty"`
	Result      string                  `json:"result,omitempty"`
	Reply       string                  `json:"reply,omitempty"`
	Error       string                  `json:"error,omitempty"`
	Notes       []ThreadNote            `json:"notes,omitempty"`
	Steps       []ThreadStep            `json:"steps,omitempty"`
	// Edited holds the words a turn had before each thread_edit, oldest first.
	Edited []ThreadStep `json:"edited,omitempty"`

	IO        string            `json:"io,omitempty"`
	Direction model.IODirection `json:"direction,omitempty"`
	Medium    model.Medium      `json:"medium,omitempty"`
	State     model.IOState     `json:"state,omitempty"`
	From      string            `json:"from,omitempty"`
	To        []string          `json:"to,omitempty"`
	Subject   string            `json:"subject,omitempty"`
	Body      string            `json:"body,omitempty"`
	Attempts  []model.IOAttempt `json:"attempts,omitempty"`

	Receipts []ThreadReceipt `json:"receipts"`
}

// ThreadOutput is a thread. Keys are the keys read: the key, and for an item
// the keys in its threads field. Members are the other items that name a key
// in their threads. Queued and Claimed count the turns waiting for an agent
// and held by one. WaitingNoAgent: a turn is queued and no agent is connected
// on this machine (agent_list), so nothing will pick it up yet.
type ThreadOutput struct {
	Key            string       `json:"key"`
	Kind           string       `json:"kind"` // item, tag, or channel
	Keys           []string     `json:"keys"`
	Item           string       `json:"item,omitempty"`
	ItemName       string       `json:"item_name,omitempty" nameof:"Item"`
	Members        []string     `json:"members"`
	MemberNames    []string     `json:"member_names,omitempty" nameof:"Members"`
	Turns          []ThreadTurn `json:"turns"`
	Queued         int          `json:"queued"`
	Claimed        int          `json:"claimed"`
	Agents         int          `json:"agents"`
	WaitingNoAgent bool         `json:"waiting_no_agent"`
}

// threadNoteLine is one request_note line: "- <time> <actor>: <text>".
var threadNoteLine = regexp.MustCompile(`^- (\S+) (\S+): (.*)$`)

// attachPath finds the path of an embed.
var attachPath = regexp.MustCompile(`\]\(([^)]+)\)`)

// bodyParts splits a body into its lead text and its "## " sections, in order.
func bodyParts(body string) (lead string, secs []ThreadStep) {
	parts := strings.Split("\n"+body, "\n## ")
	lead = strings.TrimSpace(parts[0])
	for _, sec := range parts[1:] {
		head, text, _ := strings.Cut(sec, "\n")
		secs = append(secs, ThreadStep{Heading: strings.TrimSpace(head), Text: strings.TrimSpace(text)})
	}
	return lead, secs
}

// requestTurn is the turn of one request.
func requestTurn(doc model.Document, r *model.Request) ThreadTurn {
	t := ThreadTurn{Kind: ThreadTurnRequest, Request: doc.ID, Title: doc.Title, RequestKind: requestKind(doc.Title, r), TurnKind: r.TurnKind, Quote: r.Quote,
		Status: r.Status, ClaimedBy: r.Claimed, ClaimedAt: r.ClaimedAt, ActiveAt: r.ActiveAt, Resolution: r.Resolution, Turn: r.Turn, Reply: r.Reply,
		Notes: []ThreadNote{}, Receipts: []ThreadReceipt{}}
	// The first turn starts when it was queued; a reopened request was
	// queued again, so its first turn starts on the day it was made.
	t.At = string(r.Created)
	if r.Turn == 0 {
		t.At = firstOf(r.Queued, t.At)
	}
	lead, secs := bodyParts(doc.Body)
	words := ""
	for _, s := range secs {
		switch {
		case s.Heading == "Owner's words":
			words = s.Text
		case s.Heading == "Attachments":
			for _, m := range attachPath.FindAllStringSubmatch(s.Text, -1) {
				t.Attachments = append(t.Attachments, m[1])
			}
		case strings.HasPrefix(s.Heading, "Edited "):
			t.Edited = append(t.Edited, ThreadStep{Heading: strings.TrimPrefix(s.Heading, "Edited "), Text: s.Text})
		case s.Heading == "Notes":
			for _, l := range strings.Split(s.Text, "\n") {
				if m := threadNoteLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
					t.Notes = append(t.Notes, ThreadNote{At: m[1], Actor: m[2], Text: m[3]})
				}
			}
		case strings.HasPrefix(s.Heading, "Response"), strings.HasPrefix(s.Heading, "Result"), s.Heading == "Failure", s.Heading == "Reply":
			t.Steps = append(t.Steps, s)
			if strings.HasPrefix(s.Heading, "Result") {
				t.Result = s.Text
			}
			if s.Heading == "Failure" {
				t.Error = s.Text
			}
			if strings.HasPrefix(s.Heading, "Response") {
				t.Result, t.Error = "", "" // a result before the last response is history
			}
		}
	}
	t.OwnerWords = firstOf(words, lead)
	if r.Status != model.RequestFailed {
		t.Error = ""
	}
	// One turn needs no history: its parts are the fields above.
	if r.Turn == 0 {
		t.Steps = nil
	}
	return t
}

// ioTurn is the turn of one io item.
func ioTurn(doc model.Document, m *model.IO) ThreadTurn {
	return ThreadTurn{At: firstOf(m.At, string(m.Created)), Kind: ThreadTurnIO, IO: doc.ID, Title: doc.Title, Direction: m.Direction, Medium: m.Medium,
		State: m.State, From: m.From, To: m.To, Subject: m.Subject, Body: doc.Body, Attempts: m.Attempts, Receipts: []ThreadReceipt{}}
}

// turnTime parses a turn's time for sorting: RFC 3339 or a day.
func turnTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	t, _ := time.Parse(dateLayout, s)
	return t
}

// Thread reads one thread.
var Thread = defineRead("thread", "Read a thread: key (an item id, #tag, or channel:<source>/<thread>) or id (an item: its own thread and the keys in its threads). "+
	"Turns in time order: each owner turn (request) with the owner's words, kind, quote, status, who holds it and since when, resolution, result, reply, error, and the agent's notes; "+
	"each io in or out of the thread with its attempts; and the receipts made under each. Counts queued and claimed turns; waiting_no_agent is true when a turn is queued and no agent is connected", func(ctx context.Context, d Deps, in ThreadInput) (ThreadOutput, error) {
	var out ThreadOutput
	key, item, err := resolveThread(ctx, d, in.Key, in.ID)
	if err != nil {
		return out, err
	}
	// A request is a turn of its thread, not the item of one.
	self := ""
	if _, ok := metaOf(item).(*model.Request); ok {
		self, item = item.ID, nil
		if kd, v, _ := model.ParseThreadKey(key); kd == model.ThreadItem && v != self {
			if doc, err := d.Store.Get(ctx, v); err == nil {
				item = &doc
			}
		}
	}
	kind, _, err := model.ParseThreadKey(key)
	if err != nil {
		return out, err
	}
	out = ThreadOutput{Key: key, Kind: string(kind), Keys: []string{key}, Members: []string{}, Turns: []ThreadTurn{}}
	items := map[string]bool{} // item ids in the key set: requests that link one join
	if kind == model.ThreadItem {
		items[key] = true
	}
	if item != nil {
		out.Item = item.ID
		items[item.ID] = true
		extra := item.Meta.Header().Threads
		if m, ok := item.Meta.(*model.IO); ok && m.Via != nil {
			extra = append(slices.Clone(extra), m.Via.ThreadKey())
		}
		for _, k := range extra {
			if k != "" && !slices.Contains(out.Keys, k) {
				out.Keys = append(out.Keys, k)
			}
		}
	}
	for _, k := range out.Keys {
		if kd, v, err := model.ParseThreadKey(k); err == nil && kd == model.ThreadItem {
			items[v] = true
		}
	}
	inKeys := func(ks ...string) bool {
		for _, k := range ks {
			if k != "" && slices.Contains(out.Keys, k) {
				return true
			}
		}
		return false
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return out, err
	}
	byRequest, byIO := map[string]int{}, map[string]int{}
	for _, doc := range sn.Docs() {
		h := doc.Meta.Header()
		switch m := doc.Meta.(type) {
		case *model.Request:
			hit := doc.ID == self || inKeys(m.Thread) || inKeys(h.Threads...)
			for _, l := range m.Links {
				hit = hit || items[l.Target()]
			}
			if !hit {
				continue
			}
			byRequest[doc.ID] = len(out.Turns)
			out.Turns = append(out.Turns, requestTurn(doc, m))
			switch m.Status {
			case model.RequestQueued:
				out.Queued++
			case model.RequestClaimed:
				out.Claimed++
			}
		case *model.IO:
			via := ""
			if m.Via != nil {
				via = m.Via.ThreadKey()
			}
			if doc.ID == out.Item || !(inKeys(m.ThreadKey(), via) || inKeys(h.Threads...)) {
				continue
			}
			byIO[doc.ID] = len(out.Turns)
			out.Turns = append(out.Turns, ioTurn(doc, m))
		default:
			if doc.ID != out.Item && inKeys(h.Threads...) {
				out.Members = append(out.Members, doc.ID)
			}
		}
	}
	if len(out.Turns) > 0 {
		receipts, err := listReceipts(ctx, d.Store)
		if err != nil {
			return out, err
		}
		slices.Reverse(receipts) // oldest first
		for _, rc := range receipts {
			tr := ThreadReceipt{ID: rc.ID, At: rc.At, Actor: rc.Actor, Op: rc.Op, Doc: rc.Doc, Why: rc.Why, Undone: rc.Undone}
			if i, ok := byRequest[rc.Request]; ok {
				out.Turns[i].Receipts = append(out.Turns[i].Receipts, tr)
			} else if i, ok := byIO[rc.Input]; ok {
				out.Turns[i].Receipts = append(out.Turns[i].Receipts, tr)
			}
		}
	}
	slices.SortStableFunc(out.Turns, func(a, b ThreadTurn) int {
		if c := turnTime(a.At).Compare(turnTime(b.At)); c != 0 {
			return c
		}
		return strings.Compare(a.Request+a.IO, b.Request+b.IO)
	})
	agents, err := AgentList.Run(ctx, d, AgentListInput{})
	if err != nil {
		return out, err
	}
	out.Agents = len(agents.Agents)
	out.WaitingNoAgent = out.Queued > 0 && out.Agents == 0
	return out, nil
})
