package ops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// The request queue holds asks for agents. A person queues a request; an
// agent claims it with request_next and works under its own key scopes. Para
// starts an agent only when a person put a command in agent.json on this
// machine (internal/dispatch); with no such file it starts nothing. Events
// carry ids and states, never request text.
//
// A claim is atomic: Store.Update compares Rev inside the vault lock, so of
// two agents that read the same queued request, one wins and the other
// retries. The lock is per machine, so only the runner's install claims.

const (
	// DefaultLease is how long a claim lasts when the caller names none.
	DefaultLease = 30 * time.Minute
	// MaxLease is the longest lease a caller can ask for.
	MaxLease = 24 * time.Hour

	claimRetries = 25
	queueConfig  = "request.surfaces" // vault.properties: surfaces that can queue
	runnerConfig = "request.runner"   // vault.properties: default runner install
)

// DefaultQueueSurfaces can queue a request by default. An agent surface is not
// among them, so hostile text an agent reads cannot queue work.
var DefaultQueueSurfaces = []string{"cli", "pwa"}

// RequestView is one request in a list.
type RequestView struct {
	// NeedsYou is ops.NeedsYou for the row: open, not put off, not an
	// agent's turn, and the owner's turn. Reason says why, in words.
	NeedsYou   bool                `json:"needs_you"`
	Reason     string              `json:"reason,omitempty"`
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Status     model.RequestStatus `json:"status"`
	Runner     string              `json:"runner"`
	QueuedAt   string              `json:"queued_at"`
	ClaimedBy  string              `json:"claimed_by,omitempty"`
	LeaseUntil string              `json:"lease_until,omitempty"`
	Links      []string            `json:"links,omitempty"`
	// LinkNames has one entry per link, for people.
	LinkNames []string `json:"link_names,omitempty" nameof:"Links"`
	// Investigates is true while the request is open agent work made by
	// investigate. The items in Links then show "Agent looking".
	Investigates bool `json:"investigates"`
	// Delegates is true while the request is open agent work made by
	// task_delegate. The task in Links then shows "Agent working".
	Delegates bool   `json:"delegates"`
	Kind      string `json:"kind,omitempty"`
	// Resolution is how the last turn ended. Turn counts responses.
	Resolution model.RequestResolution `json:"resolution,omitempty"`
	Turn       int                     `json:"turn,omitempty"`
	// Reply is the resolution note of finished Ask-agent or investigate work:
	// the items in Links then show "Agent replied". Respond on this request.
	Reply string `json:"reply,omitempty"`
	// RequiresSource is the source only a declaring agent may sweep.
	RequiresSource string `json:"requires_source,omitempty"`
	// ClaimedAt is when ClaimedBy took it; ActiveAt is when it last changed.
	// Stale: queued or claimed with no change in AgentWorkStaleAfter.
	ClaimedAt string `json:"claimed_at,omitempty"`
	ActiveAt  string `json:"active_at,omitempty"`
	Stale     bool   `json:"stale"`
	// Seen is when the owner opened the result of finished work.
	Seen string `json:"seen,omitempty"`
	// Thread is the thread key of an owner turn (thread_post); TurnKind
	// says what it asks for (modify, ask, redirect, other) and Quote is the
	// text it is about. Read the turns before it with thread.
	Thread   string                `json:"thread,omitempty"`
	TurnKind model.RequestTurnKind `json:"turn_kind,omitempty"`
	Quote    string                `json:"quote,omitempty"`
}

// ContextDoc is one document a request links as context. Required says a rule
// demands it: the agent must cite it at the end.
type ContextDoc struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

// RequestDetail is a request with its body: the ask, notes, and result, each
// response in turn. Context is what the work must follow.
type RequestDetail struct {
	RequestView
	Body    string       `json:"body"`
	Context []ContextDoc `json:"context"`
}

func contextDocs(ctx context.Context, s store.Store, r *model.Request) []ContextDoc {
	out := []ContextDoc{}
	for _, l := range r.Context {
		c := ContextDoc{ID: l.Target(), Required: slices.Contains(r.Required, l)}
		if doc, err := s.Get(ctx, c.ID); err == nil {
			c.Title, c.Type = doc.Title, string(doc.Meta.Header().Type)
		}
		out = append(out, c)
	}
	return out
}

func requestView(d model.Document, r *model.Request) RequestView {
	v := RequestView{
		ID: d.ID, Title: d.Title, Status: r.Status, Runner: r.Runner, QueuedAt: r.Queued,
		ClaimedBy: r.Claimed, LeaseUntil: r.Lease, Kind: requestKind(d.Title, r), Resolution: r.Resolution, Turn: r.Turn,
		ClaimedAt: r.ClaimedAt, ActiveAt: r.ActiveAt, Seen: r.Seen, Thread: r.Thread, TurnKind: r.TurnKind, Quote: r.Quote,
	}
	if len(r.Links) > 0 {
		v.Links = targets(r.Links)
	}
	v.Investigates = openFind(d.Title, r)
	v.Delegates = openWork(d.Title, r)
	v.Reply = replyNote(d.Title, d.Body, r)
	v.RequiresSource = r.RequiresSource.Target()
	return v
}

func nowString(s store.Store) string { return s.Now().UTC().Format(time.RFC3339Nano) }

// AgentWorkStaleAfter is how long queued or claimed agent work may go with no
// change before it is stale.
const AgentWorkStaleAfter = 24 * time.Hour

// OwnerAskKind marks a request an agent recorded for what the owner asked in
// the chat: work that does not end in the same reply.
const OwnerAskKind = "owner-ask"

// requestStale reports whether queued or claimed work had no change in
// AgentWorkStaleAfter: queued with no claim, or claimed with no note or end.
func requestStale(r *model.Request, now time.Time) bool {
	var at string
	switch r.Status {
	case model.RequestQueued:
		at = firstOf(r.Queued, r.ActiveAt)
	case model.RequestClaimed:
		at = firstOf(r.ActiveAt, r.ClaimedAt, r.Queued)
	default:
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	return err == nil && now.Sub(t) > AgentWorkStaleAfter
}

// requeues reports whether a claim on r goes back to the queue when its lease
// ends. An owner-ask stays with the agent that recorded it: a dropped one
// shows as stale, so no other agent picks up text an agent wrote.
func requeues(r *model.Request) bool { return r.Kind != OwnerAskKind }

// claimRank orders claimable work: sweeps first, then what the owner asked
// (owner-ask, sent-to-agent), then filling gaps (fill-gap), then other agent
// work.
func claimRank(title string, r *model.Request) int {
	switch {
	case r.RequiresSource != "":
		return 0
	case isAsked(title, r):
		return 1
	case r.Kind == FillGapKind:
		return 2
	}
	return 3
}

// identity returns the caller's identity. A request op needs an install id.
func identity(d Deps) (store.Info, error) {
	info := d.Store.Info()
	if info.Install == "" {
		return info, errors.New("no install id: the surface did not name this install")
	}
	if info.Actor == "" {
		return info, errors.New("no actor: the surface did not name the caller")
	}
	return info, nil
}

// canQueue reports whether the caller's surface can queue work. The vault
// property request.surfaces (comma separated) replaces the default list.
func canQueue(d Deps, surface string) (bool, error) {
	list := DefaultQueueSurfaces
	v, err := d.Store.Config(queueConfig)
	if err != nil {
		return false, err
	}
	if v != "" {
		list = nil
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				list = append(list, s)
			}
		}
	}
	return surface != "" && slices.Contains(list, surface), nil
}

func getRequest(ctx context.Context, s store.Store, id string) (model.Document, *model.Request, error) {
	doc, err := s.Get(ctx, normID(id))
	if err != nil {
		return doc, nil, err
	}
	r, ok := doc.Meta.(*model.Request)
	if !ok {
		return doc, nil, fmt.Errorf("%s is %s, not request", doc.ID, doc.Meta.Header().Type)
	}
	return doc, r, nil
}

// loadRequests returns every request with its meta, oldest queued first.
func loadRequests(ctx context.Context, s store.Store) ([]model.Document, error) {
	refs, err := s.List(ctx, store.Filter{Folder: model.FolderRequests, Type: model.TypeRequest})
	if err != nil {
		return nil, err
	}
	out := make([]model.Document, 0, len(refs))
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if _, ok := doc.Meta.(*model.Request); ok {
			out = append(out, doc)
		}
	}
	slices.SortStableFunc(out, func(a, b model.Document) int {
		ra, rb := a.Meta.(*model.Request), b.Meta.(*model.Request)
		if c := strings.Compare(ra.Queued, rb.Queued); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func leaseExpired(r *model.Request, now time.Time) bool {
	t, err := time.Parse(time.RFC3339Nano, r.Lease)
	return err != nil || !now.Before(t)
}

// claimable reports whether the install can claim r now: queued, or claimed
// with an expired lease (next returns it to the queue first).
func claimable(r *model.Request, install string, now time.Time) bool {
	if r.Runner != install {
		return false
	}
	return r.Status == model.RequestQueued || (r.Status == model.RequestClaimed && requeues(r) && leaseExpired(r, now))
}

func leaseFor(secs int) (time.Duration, error) {
	switch {
	case secs < 0:
		return 0, errors.New("lease_seconds is negative")
	case secs == 0:
		return DefaultLease, nil
	case time.Duration(secs)*time.Second > MaxLease:
		return 0, fmt.Errorf("lease_seconds is over %d", int(MaxLease/time.Second))
	}
	return time.Duration(secs) * time.Second, nil
}

// requestLinks resolves ids to wikilinks. Types limits what they may point at;
// none means any document.
func requestLinks(ctx context.Context, d Deps, field string, ids []string, types ...model.Type) ([]model.Link, error) {
	var out []model.Link
	for _, raw := range ids {
		var l model.Link
		var err error
		if len(types) > 0 {
			l, err = linkTo(ctx, d, field, raw, types...)
		} else {
			id := normID(raw)
			if id != "" {
				if _, err = d.Store.Get(ctx, id); err != nil {
					err = fmt.Errorf("%s %s: %w", field, id, err)
				}
				l = model.LinkTo(id)
			}
		}
		if err != nil {
			return nil, err
		}
		if l == "" {
			return nil, fmt.Errorf("%s: empty id", field)
		}
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out, nil
}

// RequestAddInput is the input of request_add.
type RequestAddInput struct {
	// Title is a short human name for the work, such as "Check backups". It
	// never holds a document id: ids go in links. Required from an agent. A
	// person may leave it out; the first line of ask is then the title.
	Title  string   `json:"title,omitempty"`
	Ask    string   `json:"ask"`              // the ask; with no title, its first line is the title
	Links  []string `json:"links,omitempty"`  // task, project, question, meeting, ask, io, request, proposal, person, or group ids the ask is about
	Runner string   `json:"runner,omitempty"` // install id that may claim; default request.runner, else this install
	// Context links what the work must follow: policies, conventions, a goal, documents.
	Context []string `json:"context,omitempty"`
	// Kind is free text. A rule in .para/proposals.yaml can require a policy or
	// a convention for a kind.
	Kind string `json:"kind,omitempty"`
	// OwnerSaid is the owner's message from the chat, verbatim. Required for
	// kind owner-ask from an agent: it is the source of the work.
	OwnerSaid string `json:"owner_said,omitempty"`
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
	// turn is set only by an owner turn (thread.go): no surface sets it.
	turn ownerTurn
}

// ownerTurn is the thread key, kind, and quote of an owner turn (thread_post).
type ownerTurn struct {
	thread string
	kind   model.RequestTurnKind
	quote  string
}

// idWord finds a document id in text: a type word, a dash, and a slug.
var idWord = regexp.MustCompile(`\b(?:project|task|rfx|constraint|policy|person|team|role|domain|convention|journal|inbox|group|interaction|request|area|note|incident|proposal|question|meeting|exception|receipt)-[a-z0-9][a-z0-9.-]*\b`)

// requestTitle returns the title and body of a request. A given title is the
// name and the whole ask is the body; no title is the first line of the ask,
// and an agent must give one.
func requestTitle(d Deps, in RequestAddInput) (title, body string, err error) {
	if strings.TrimSpace(in.Title) == "" {
		if agentSurface(d) {
			return "", "", errors.New("title is required: a short human name for the work, such as \"Check backups\". Keep ids out of it; put them in links")
		}
		title, body, err = splitCapture(in.Ask)
		if err != nil {
			return "", "", fmt.Errorf("ask: %w", err)
		}
		return title, body, nil
	}
	if title, err = oneLine("title", in.Title); err != nil {
		return "", "", err
	}
	if m := idWord.FindString(title); m != "" && agentSurface(d) {
		return "", "", fmt.Errorf("title holds the id %q: name it in words and put the id in links", m)
	}
	body = strings.TrimSpace(strings.ReplaceAll(in.Ask, "\r\n", "\n"))
	if body == "" {
		return "", "", errors.New("ask is empty")
	}
	return title, body, nil
}

// plainTitle replaces the document ids in a title with the titles of the
// documents, so an old request that was titled from its ask reads in words.
func plainTitle(ctx context.Context, s store.Store, title string) string {
	return idWord.ReplaceAllStringFunc(title, func(id string) string {
		if doc, err := s.Get(ctx, id); err == nil && strings.TrimSpace(doc.Title) != "" {
			return doc.Title
		}
		return id
	})
}

// RequestAddOutput is the output of request_add. Status is proposed when the
// surface cannot queue work, and claimed for an owner-ask an agent recorded.
type RequestAddOutput struct {
	ID         string              `json:"id"`
	Folder     model.Folder        `json:"folder"`
	Status     model.RequestStatus `json:"status"`
	Runner     string              `json:"runner"`
	LeaseUntil string              `json:"lease_until,omitempty"`
	Rev        string              `json:"rev"`
}

// RequestAdd queues a request, or proposes one from a surface that cannot queue.
var RequestAdd = defineSafe("request_add", "Queue an ask for an agent; from an agent surface it is only proposed until a person accepts it. Kind owner-ask records what the owner asked you in the chat that does not end in this reply: title in the owner's words, owner_said their message verbatim, links the items it is about. You hold it at once (claimed); renew with request_note and end it with request_done", func(ctx context.Context, d Deps, in RequestAddInput) (RequestAddOutput, error) {
	return addRequest(ctx, d, in, "")
})

// addRequest makes one request: every entry point that asks an agent for
// work comes here. src, for a sweep the tick makes, is the source only a
// declaring agent may claim; no op input sets it.
func addRequest(ctx context.Context, d Deps, in RequestAddInput, src model.Link) (RequestAddOutput, error) {
	info, err := identity(d)
	if err != nil {
		return RequestAddOutput{}, err
	}
	title, body, err := requestTitle(d, in)
	if err != nil {
		return RequestAddOutput{}, err
	}
	ownerAsk := strings.TrimSpace(in.Kind) == OwnerAskKind
	said := strings.TrimSpace(strings.ReplaceAll(in.OwnerSaid, "\r\n", "\n"))
	switch {
	case ownerAsk && said == "" && agentSurface(d):
		return RequestAddOutput{}, errors.New("owner_said is empty: an owner-ask quotes the owner's message from the chat, verbatim")
	case !ownerAsk && said != "":
		return RequestAddOutput{}, fmt.Errorf("owner_said: only kind %s takes it", OwnerAskKind)
	}
	if said != "" {
		if err := lintText("owner_said", said); err != nil {
			return RequestAddOutput{}, err
		}
		body += "\n\n## Owner's words\n\n" + flattenHeadings(said)
	}
	links, err := requestLinks(ctx, d, "links", in.Links, model.TypeTask, model.TypeObligation, model.TypeProject, model.TypeProposal, model.TypeJournal, model.TypeQuestion, model.TypeMeeting, model.TypeRFX, model.TypeRequest, model.TypeIO, model.TypeArea, model.TypeNote, model.TypeTrackedDoc, model.TypePerson, model.TypeGroup)
	if err != nil {
		return RequestAddOutput{}, err
	}
	runner := strings.TrimSpace(in.Runner)
	if runner == "" {
		if runner, err = d.Store.Config(runnerConfig); err != nil {
			return RequestAddOutput{}, err
		}
	}
	if runner == "" {
		runner = info.Install
	}
	if strings.ContainsAny(runner, " \t\r\n") {
		return RequestAddOutput{}, fmt.Errorf("runner %q: want an install id", runner)
	}
	status := model.RequestProposed
	if ok, err := canQueue(d, info.Surface); err != nil {
		return RequestAddOutput{}, err
	} else if ok || ctx.Value(forceQueueKey{}) != nil {
		status = model.RequestQueued
	}
	// An agent records an owner-ask for its own work: it is claimed by the
	// caller on this install, never queued for another agent.
	claim := ownerAsk && agentSurface(d)
	if claim {
		if runner != info.Install {
			return RequestAddOutput{}, errors.New("runner: an owner-ask you record runs on this install")
		}
		status = model.RequestClaimed
	}
	rules, err := loadProposalRules(d)
	if err != nil {
		return RequestAddOutput{}, err
	}
	context, err := requestLinks(ctx, d, "context", in.Context)
	if err != nil {
		return RequestAddOutput{}, err
	}
	kind := strings.TrimSpace(in.Kind)
	var required []model.Link
	if kind != "" {
		if required, err = requestLinks(ctx, d, "rule for "+kind, rules.Work[kind].Require); err != nil {
			return RequestAddOutput{}, err
		}
		for _, l := range required {
			if !slices.Contains(context, l) {
				context = append(context, l)
			}
		}
	}
	now := nowString(d.Store)
	meta := &model.Request{
		Common: model.Common{Type: model.TypeRequest},
		Status: status, Runner: runner, Queued: now, ActiveAt: now, Links: links,
		Kind: kind, Context: context, Required: required, RequiresSource: src,
		Thread: in.turn.thread, TurnKind: in.turn.kind, Quote: in.turn.quote,
	}
	if claim {
		meta.Claimed, meta.ClaimedAt = info.Actor, now
		meta.Lease = d.Store.Now().Add(DefaultLease).UTC().Format(time.RFC3339Nano)
	}
	doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderRequests, Title: title, Body: body, Meta: meta})
	if err != nil {
		return RequestAddOutput{}, err
	}
	return RequestAddOutput{ID: doc.ID, Folder: doc.Folder, Status: status, Runner: runner, LeaseUntil: meta.Lease, Rev: doc.Rev}, nil
}

// RequestListInput is the input of request_list. Empty fields match all.
type RequestListInput struct {
	Status model.RequestStatus `json:"status,omitempty" query:"status"`
	Limit  int                 `json:"limit,omitempty" query:"limit"` // 0 means all
}

// RequestListOutput is the output of request_list.
type RequestListOutput struct {
	Items []RequestView `json:"items"`
}

// RequestList lists requests, oldest queued first.
var RequestList = defineRead("request_list", "List agent requests, oldest first", func(ctx context.Context, d Deps, in RequestListInput) (RequestListOutput, error) {
	if in.Status != "" && !slices.Contains(model.RequestStatuses, in.Status) {
		return RequestListOutput{}, fmt.Errorf("status %q: want one of %v", in.Status, model.RequestStatuses)
	}
	if in.Limit < 0 {
		return RequestListOutput{}, errors.New("limit is negative")
	}
	docs, err := loadRequests(ctx, d.Store)
	if err != nil {
		return RequestListOutput{}, err
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return RequestListOutput{}, err
	}
	out := RequestListOutput{Items: []RequestView{}}
	now := d.Store.Now()
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if in.Status != "" && r.Status != in.Status {
			continue
		}
		v := requestView(doc, r)
		need := sn.Need(doc.ID)
		v.NeedsYou, v.Reason = need.Yes, need.Reason
		v.Stale = requestStale(r, now)
		v.Title = plainTitle(ctx, d.Store, v.Title)
		out.Items = append(out.Items, v)
	}
	if in.Limit > 0 && len(out.Items) > in.Limit {
		out.Items = out.Items[:in.Limit]
	}
	return out, nil
})

// RequestShowInput is the input of request_show.
type RequestShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// RequestShowOutput is the output of request_show.
type RequestShowOutput struct {
	Doc model.Document `json:"doc"`
}

// RequestShow shows one request.
var RequestShow = defineRead("request_show", "Show one agent request", func(ctx context.Context, d Deps, in RequestShowInput) (RequestShowOutput, error) {
	doc, _, err := getRequest(ctx, d.Store, in.ID)
	return RequestShowOutput{Doc: doc}, err
})

// RequestChangeOutput is the output of an op that changes one request's state.
type RequestChangeOutput struct {
	ID         string              `json:"id"`
	Status     model.RequestStatus `json:"status"`
	LeaseUntil string              `json:"lease_until,omitempty"`
	Rev        string              `json:"rev"`
}

func changeOutput(doc model.Document) RequestChangeOutput {
	r := doc.Meta.(*model.Request)
	return RequestChangeOutput{ID: doc.ID, Status: r.Status, LeaseUntil: r.Lease, Rev: doc.Rev}
}

// RequestAcceptInput is the input of RequestAccept.
type RequestAcceptInput struct {
	ID string `json:"id"`
}

// RequestCancelInput is the input of request_cancel.
type RequestCancelInput struct {
	ID string `json:"id"`
}

// RequestCancel cancels a request that is not finished. A claimed request is
// cancelled too: its agent's next note, done, or fail then fails.
var RequestCancel = define("request_cancel", "Cancel an agent request that is not finished", func(ctx context.Context, d Deps, in RequestCancelInput) (RequestChangeOutput, error) {
	doc, r, err := getRequest(ctx, d.Store, in.ID)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	if !slices.Contains([]model.RequestStatus{model.RequestProposed, model.RequestQueued, model.RequestClaimed}, r.Status) {
		return RequestChangeOutput{}, fmt.Errorf("%s is %s: only a proposed, queued, or claimed request can be cancelled", doc.ID, r.Status)
	}
	r.Status, r.Claimed, r.Lease, r.Resolution, r.ActiveAt = model.RequestCancelled, "", "", model.ResolutionCancelled, nowString(d.Store)
	doc, err = d.Store.Update(ctx, doc)
	return changeOutput(doc), err
})

// RequestAcceptOp is request_accept: a person-only op. No agent surface can
// call it or list it, so an agent cannot accept its own request.
var RequestAcceptOp = definePerson("request_accept", "Accept a proposed agent request into the queue. People only", RequestAccept)

// RequestAccept moves a proposed request to the queue. The surface check for
// queuing still applies.
func RequestAccept(ctx context.Context, d Deps, in RequestAcceptInput) (RequestChangeOutput, error) {
	info, err := identity(d)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	if ok, err := canQueue(d, info.Surface); err != nil {
		return RequestChangeOutput{}, err
	} else if !ok {
		return RequestChangeOutput{}, fmt.Errorf("request_accept: surface %q cannot queue requests", info.Surface)
	}
	doc, r, err := getRequest(ctx, d.Store, in.ID)
	if err != nil {
		return RequestChangeOutput{}, fmt.Errorf("request_accept: %w", err)
	}
	if r.Status != model.RequestProposed {
		return RequestChangeOutput{}, fmt.Errorf("request_accept: %s is %s, not proposed", doc.ID, r.Status)
	}
	r.Status, r.Queued = model.RequestQueued, nowString(d.Store)
	r.ActiveAt = r.Queued
	doc, err = d.Store.Update(ctx, doc)
	if err != nil {
		return RequestChangeOutput{}, fmt.Errorf("request_accept: %w", err)
	}
	return changeOutput(doc), nil
}

// RequestNextInput is the input of request_next.
type RequestNextInput struct {
	LeaseSeconds int `json:"lease_seconds,omitempty"` // default 1800, at most 86400
}

// RequestNextOutput is the output of request_next. Request is null when
// nothing is claimable. Requeued lists requests whose lease had expired.
type RequestNextOutput struct {
	Request  *RequestDetail `json:"request" required:"false"`
	Requeued []string       `json:"requeued"`
}

// RequestNext claims queued work for the caller: sweeps first, then what the
// owner asked, then other work, each oldest first.
var RequestNext = defineSafe("request_next", "Claim the next queued request for this actor, with a lease: sweeps of sources you declared first, then what the owner asked (owner-ask, sent-to-agent), then gaps to fill (fill-gap), then other agent work, each oldest first; null when none", func(ctx context.Context, d Deps, in RequestNextInput) (RequestNextOutput, error) {
	if d.ClaimHold != nil {
		if err := d.ClaimHold(); err != nil {
			return RequestNextOutput{}, err
		}
	}
	info, err := identity(d)
	if err != nil {
		return RequestNextOutput{}, err
	}
	lease, err := leaseFor(in.LeaseSeconds)
	if err != nil {
		return RequestNextOutput{}, err
	}
	out := RequestNextOutput{Requeued: []string{}}
	for range claimRetries {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		docs, err := loadRequests(ctx, d.Store)
		if err != nil {
			return out, err
		}
		// An expired lease returns to the queue. A conflict means someone
		// else changed the request: read again.
		conflict := false
		now := d.Store.Now()
		for _, doc := range docs {
			r := doc.Meta.(*model.Request)
			if r.Runner != info.Install || r.Status != model.RequestClaimed || !requeues(r) || !leaseExpired(r, now) {
				continue
			}
			r.Status, r.Claimed, r.Lease, r.ClaimedAt = model.RequestQueued, "", "", ""
			if _, err := d.Store.Update(ctx, doc); errors.Is(err, model.ErrConflict) {
				conflict = true
			} else if err != nil {
				return out, err
			} else {
				out.Requeued = append(out.Requeued, doc.ID)
			}
		}
		if conflict {
			continue
		}
		if len(out.Requeued) > 0 {
			if docs, err = loadRequests(ctx, d.Store); err != nil {
				return out, err
			}
		}
		slices.SortStableFunc(docs, func(a, b model.Document) int {
			return claimRank(a.Title, a.Meta.(*model.Request)) - claimRank(b.Title, b.Meta.(*model.Request))
		})
		for _, doc := range docs {
			r := doc.Meta.(*model.Request)
			if r.Runner != info.Install || r.Status != model.RequestQueued {
				continue
			}
			if r.RequiresSource != "" {
				ok, err := sourceReach(d, info.Actor, r.RequiresSource.Target())
				if err != nil {
					return out, err
				}
				if !ok {
					continue // only an actor that declared the source claims it
				}
			}
			r.Status, r.Claimed, r.Lease = model.RequestClaimed, info.Actor, d.Store.Now().Add(lease).UTC().Format(time.RFC3339Nano)
			r.ClaimedAt = nowString(d.Store)
			r.ActiveAt = r.ClaimedAt
			got, err := d.Store.Update(ctx, doc)
			if errors.Is(err, model.ErrConflict) {
				conflict = true
				break
			}
			if err != nil {
				return out, err
			}
			m := got.Meta.(*model.Request)
			out.Request = &RequestDetail{RequestView: requestView(got, m), Body: got.Body, Context: contextDocs(ctx, d.Store, m)}
			return out, nil
		}
		if !conflict {
			return out, nil
		}
	}
	return out, fmt.Errorf("could not claim after %d tries: the queue keeps changing", claimRetries)
})

// ownClaim loads a request the caller holds. A request that was cancelled or
// handed to another actor is refused.
func ownClaim(ctx context.Context, d Deps, id string) (model.Document, *model.Request, error) {
	info, err := identity(d)
	if err != nil {
		return model.Document{}, nil, err
	}
	doc, r, err := getRequest(ctx, d.Store, id)
	if err != nil {
		return doc, nil, err
	}
	if r.Status != model.RequestClaimed {
		return doc, nil, fmt.Errorf("%s is %s, not claimed", doc.ID, r.Status)
	}
	if r.Claimed != info.Actor {
		return doc, nil, fmt.Errorf("%s is claimed by another actor", doc.ID)
	}
	return doc, r, nil
}

// appendSection adds a "## name" section at the end of the body.
func appendSection(body, name, text string) string {
	if body != "" {
		body += "\n\n"
	}
	return body + "## " + name + "\n\n" + strings.TrimSpace(text)
}

// RequestNoteInput is the input of request_note.
type RequestNoteInput struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	LeaseSeconds int    `json:"lease_seconds,omitempty"` // new lease from now; default 1800
}

// RequestNote adds a progress note to a claimed request and renews the lease.
var RequestNote = defineSafe("request_note", "Add a progress note to a request you claimed, and renew its lease", func(ctx context.Context, d Deps, in RequestNoteInput) (RequestChangeOutput, error) {
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" {
		return RequestChangeOutput{}, errors.New("text is empty")
	}
	lease, err := leaseFor(in.LeaseSeconds)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	doc, r, err := ownClaim(ctx, d, in.ID)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	line := fmt.Sprintf("- %s %s: %s", d.Store.Now().UTC().Format(time.RFC3339), r.Claimed, text)
	if strings.Contains(doc.Body, "\n## Notes\n") {
		doc.Body += "\n" + line
	} else {
		doc.Body = appendSection(doc.Body, "Notes", line)
	}
	r.Lease = d.Store.Now().Add(lease).UTC().Format(time.RFC3339Nano)
	r.ActiveAt = nowString(d.Store)
	doc, err = d.Store.Update(ctx, doc)
	return changeOutput(doc), err
})

// RequestDoneInput is the input of request_done.
type RequestDoneInput struct {
	ID string `json:"id"`
	// Resolution is required: done, partial, stuck, blocked, or needs_clarification.
	Resolution model.RequestResolution `json:"resolution"`
	Result     string                  `json:"result"`             // summary of what was done
	Links      []string                `json:"links,omitempty"`    // ids of documents made or changed
	Followed   []string                `json:"followed,omitempty"` // context you followed
	// Reply is your answer to the owner, in words, for the thread of the
	// item (thread): what you did and why, or the answer to their question.
	Reply string `json:"reply,omitempty"`
}

// RequestDone ends a turn of a claimed request with a resolution and a summary.
// What the agent changed comes from events. A rule's required context must be
// cited. stuck, blocked, and needs_clarification wait for the owner's response.
var RequestDone = defineSafe("request_done", "End a request you claimed: resolution done, partial, stuck, blocked, or needs_clarification, a summary, links to what you made, the context you followed, and reply: your answer to the owner, shown in the item's thread", func(ctx context.Context, d Deps, in RequestDoneInput) (RequestChangeOutput, error) {
	if !slices.Contains([]model.RequestResolution{model.ResolutionDone, model.ResolutionPartial, model.ResolutionStuck, model.ResolutionBlocked, model.ResolutionNeedsClarification}, in.Resolution) {
		return RequestChangeOutput{}, fmt.Errorf("resolution %q: want done, partial, stuck, blocked, or needs_clarification", in.Resolution)
	}
	if strings.TrimSpace(in.Result) == "" {
		return RequestChangeOutput{}, errors.New("result is empty")
	}
	links, err := requestLinks(ctx, d, "links", in.Links)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	followed, err := requestLinks(ctx, d, "followed", in.Followed)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	doc, r, err := ownClaim(ctx, d, in.ID)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	for _, l := range r.Required {
		if !slices.Contains(followed, l) && in.Resolution == model.ResolutionDone {
			return RequestChangeOutput{}, fmt.Errorf("a rule requires %s for this work: follow it and cite it in followed", l.Target())
		}
	}
	heading := "Result"
	if in.Resolution != model.ResolutionDone {
		heading += " (" + string(in.Resolution) + ")"
	}
	doc.Body = appendSection(doc.Body, heading, in.Result)
	if err := setReply(&doc, r, in.Reply); err != nil {
		return RequestChangeOutput{}, err
	}
	r.Status, r.Lease, r.Results, r.Resolution, r.Followed = model.RequestDone, "", links, in.Resolution, followed
	r.ActiveAt = nowString(d.Store)
	doc, err = d.Store.Update(ctx, doc)
	return changeOutput(doc), err
})

// RequestFailInput is the input of request_fail.
type RequestFailInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Reply  string `json:"reply,omitempty"` // your answer to the owner, shown in the item's thread
}

// RequestFail ends a claimed request as failed. A person adds it again to retry.
var RequestFail = defineSafe("request_fail", "Mark a request you claimed as failed, with the reason, and reply: your answer to the owner, shown in the item's thread", func(ctx context.Context, d Deps, in RequestFailInput) (RequestChangeOutput, error) {
	if strings.TrimSpace(in.Reason) == "" {
		return RequestChangeOutput{}, errors.New("reason is empty")
	}
	doc, r, err := ownClaim(ctx, d, in.ID)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	doc.Body = appendSection(doc.Body, "Failure", in.Reason)
	if err := setReply(&doc, r, in.Reply); err != nil {
		return RequestChangeOutput{}, err
	}
	r.Status, r.Lease, r.Resolution, r.ActiveAt = model.RequestFailed, "", model.ResolutionFailed, nowString(d.Store)
	doc, err = d.Store.Update(ctx, doc)
	return changeOutput(doc), err
})

// setReply stores the agent's answer to the owner on the request and keeps it
// in the body under "## Reply", so an earlier turn's reply stays in the
// history when the work reopens. Empty leaves both as they are.
func setReply(doc *model.Document, r *model.Request, reply string) error {
	reply = strings.TrimSpace(strings.ReplaceAll(reply, "\r\n", "\n"))
	if reply == "" {
		return nil
	}
	if err := lintText("reply", reply); err != nil {
		return err
	}
	r.Reply = reply
	doc.Body = appendSection(doc.Body, "Reply", flattenHeadings(reply))
	return nil
}

// RequestSeenInput names finished work whose result the owner opened: id,
// ids, or both.
type RequestSeenInput struct {
	ID  string   `json:"id,omitempty"`
	IDs []string `json:"ids,omitempty"`
}

// RequestSeenOutput lists the work marked seen now, and the work skipped:
// not finished, or seen before.
type RequestSeenOutput struct {
	Seen    []string `json:"seen"`
	Skipped []string `json:"skipped"`
}

// RequestSeen marks the result of finished work seen: done or failed work no
// longer waits for the owner to check it. A turn that ended stuck, blocked,
// or needs_clarification still waits for a response.
var RequestSeen = definePerson("request_seen", "Mark the result of finished agent work (done or failed) seen by the owner, so it no longer needs them (result to check). Work not finished, or seen before, is skipped. People only", func(ctx context.Context, d Deps, in RequestSeenInput) (RequestSeenOutput, error) {
	ids := in.IDs
	if strings.TrimSpace(in.ID) != "" {
		ids = append([]string{in.ID}, ids...)
	}
	if len(ids) == 0 {
		return RequestSeenOutput{}, errors.New("give id or ids")
	}
	out := RequestSeenOutput{Seen: []string{}, Skipped: []string{}}
	for _, id := range ids {
		doc, r, err := getRequest(ctx, d.Store, id)
		if err != nil {
			return out, err
		}
		if slices.Contains(out.Seen, doc.ID) || slices.Contains(out.Skipped, doc.ID) {
			continue
		}
		if r.Seen != "" || (r.Status != model.RequestDone && r.Status != model.RequestFailed) {
			out.Skipped = append(out.Skipped, doc.ID)
			continue
		}
		r.Seen = nowString(d.Store)
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return out, fmt.Errorf("request_seen: %w", err)
		}
		out.Seen = append(out.Seen, doc.ID)
	}
	return out, nil
})

// RespondKind is what the owner's response asks for.
type RespondKind string

const (
	RespondChange  RespondKind = "change"
	RespondClarify RespondKind = "clarify"
	RespondRevert  RespondKind = "revert"
)

// RespondKinds lists every RespondKind.
var RespondKinds = []RespondKind{RespondChange, RespondClarify, RespondRevert}

// Enum lists the allowed values, for schema generation.
func (RespondKind) Enum() []string { return enumStrings(RespondKinds) }

func enumStrings[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

// RequestRespondInput is the input of request_respond.
type RequestRespondInput struct {
	ID   string      `json:"id"`
	Kind RespondKind `json:"kind"` // change, clarify, or revert
	Text string      `json:"text"`
}

// RequestRespond reopens finished work as the next turn in one thread. History
// stays in the body. Revert is a turn the agent carries out through ops.
var RequestRespond = definePerson("request_respond", "Respond to finished agent work: change, clarify, or revert. It reopens the work as the next turn, for an agent to pick up. People only", func(ctx context.Context, d Deps, in RequestRespondInput) (RequestChangeOutput, error) {
	if !slices.Contains(RespondKinds, in.Kind) {
		return RequestChangeOutput{}, fmt.Errorf("kind %q: want change, clarify, or revert", in.Kind)
	}
	if strings.TrimSpace(in.Text) == "" {
		return RequestChangeOutput{}, errors.New("text is empty")
	}
	doc, _, err := respondTo(ctx, d, in, ownerTurn{kind: turnKindOf(in.Kind)})
	return changeOutput(doc), err
})

// turnKindOf is the thread turn kind of a response kind.
func turnKindOf(k RespondKind) model.RequestTurnKind {
	switch k {
	case RespondChange:
		return model.RequestTurnModify
	case RespondClarify:
		return model.RequestTurnAsk
	}
	return model.RequestTurnOther
}

// respondTo reopens finished work as its next turn. turn is the turn's kind
// and quote; its thread stays the request's own.
func respondTo(ctx context.Context, d Deps, in RequestRespondInput, turn ownerTurn) (model.Document, *model.Request, error) {
	doc, r, err := getRequest(ctx, d.Store, in.ID)
	if err != nil {
		return doc, nil, err
	}
	if r.Status != model.RequestDone && r.Status != model.RequestFailed {
		return doc, nil, fmt.Errorf("%s is %s: a response needs finished work", doc.ID, r.Status)
	}
	r.Turn++
	text := strings.TrimSpace(in.Text)
	if in.Kind == RespondRevert {
		text += "\n\nRevert with receipt_undo, newest receipt first: receipt_list names them."
	}
	if turn.quote != "" {
		text = quoteBlock(turn.quote) + "\n\n" + text
	}
	doc.Body = appendSection(doc.Body, fmt.Sprintf("Response %d (%s)", r.Turn, in.Kind), text)
	r.Status, r.Claimed, r.Lease, r.Resolution, r.Queued = model.RequestQueued, "", "", "", nowString(d.Store)
	r.ClaimedAt, r.ActiveAt = "", r.Queued
	r.TurnKind, r.Quote, r.Reply = turn.kind, turn.quote, ""
	doc, err = d.Store.Update(ctx, doc)
	return doc, r, err
}

// RequestChangesInput is the input of request_changes.
type RequestChangesInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// RequestChange is one document the work touched.
type RequestChange struct {
	Doc     string `json:"doc"`
	DocName string `json:"doc_name,omitempty" nameof:"Doc"`
	Op      string `json:"op"`
	Receipt string `json:"receipt"`
}

// RequestChangesOutput is the output of request_changes.
type RequestChangesOutput struct {
	Items []RequestChange `json:"items"`
}

// RequestChanges lists what the work touched, from events.
var RequestChanges = defineRead("request_changes", "List the documents an agent changed under a request, from events, with receipts", func(ctx context.Context, d Deps, in RequestChangesInput) (RequestChangesOutput, error) {
	doc, _, err := getRequest(ctx, d.Store, in.ID)
	if err != nil {
		return RequestChangesOutput{}, err
	}
	rows, err := activityRows(ctx, d, "", time.Time{}, time.Time{})
	if err != nil {
		return RequestChangesOutput{}, err
	}
	out := RequestChangesOutput{Items: []RequestChange{}}
	for _, row := range rows {
		if row.Request == doc.ID {
			out.Items = append(out.Items, RequestChange{Doc: row.Doc, Op: row.Op, Receipt: row.Receipt})
		}
	}
	return out, nil
})

// RequestWatch answers "can this install claim a request now?" cheaply, for
// a poll loop. It lists the requests folder and reads a file again only when
// its Rev changed. It uses no LLM and holds no lock.
type RequestWatch struct {
	d    Deps
	seen map[string]watched
	// actor, when set, is the one agent the watch answers for: a sweep counts
	// only when that actor declared its source, as request_next checks.
	actor string
}

type watched struct {
	rev string
	r   model.Request
}

// NewRequestWatch returns a watch over d's vault.
func NewRequestWatch(d Deps) *RequestWatch { return &RequestWatch{d: d, seen: map[string]watched{}} }

// NewActorRequestWatch is a watch for one agent's session: it lists only what
// request_next would claim for actor, so a session is not told of a sweep that
// only another agent can take.
func NewActorRequestWatch(d Deps, actor string) *RequestWatch {
	w := NewRequestWatch(d)
	w.actor = actor
	return w
}

// Claimable reports whether request_next would find a request now.
func (w *RequestWatch) Claimable(ctx context.Context) (bool, error) {
	ids, err := w.ClaimableIDs(ctx)
	return len(ids) > 0, err
}

// ClaimableIDs lists the requests request_next could claim now, sorted. The ids
// are for a caller's own bookkeeping: a file name is not trusted text.
func (w *RequestWatch) ClaimableIDs(ctx context.Context) ([]string, error) {
	info, err := identity(w.d)
	if err != nil {
		return nil, err
	}
	refs, err := w.d.Store.List(ctx, store.Filter{Folder: model.FolderRequests, Type: model.TypeRequest})
	if err != nil {
		return nil, err
	}
	live := make(map[string]watched, len(refs))
	for _, ref := range refs {
		if old, ok := w.seen[ref.ID]; ok && old.rev == ref.Rev {
			live[ref.ID] = old
			continue
		}
		doc, err := w.d.Store.Get(ctx, ref.ID)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if r, ok := doc.Meta.(*model.Request); ok {
			live[ref.ID] = watched{rev: doc.Rev, r: *r}
		}
	}
	w.seen = live
	now := w.d.Store.Now()
	var ids []string
	for id, v := range live {
		if !claimable(&v.r, info.Install, now) {
			continue
		}
		// A sweep waits for an agent that declared its source. Without an
		// actor the watch does not know which agent a command runs as: any
		// declaration on this machine will do, and request_next checks the
		// actor.
		if v.r.RequiresSource != "" {
			ok, err := sourceReach(w.d, w.actor, v.r.RequiresSource.Target())
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}
