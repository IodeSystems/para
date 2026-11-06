package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// One predicate decides what needs the owner. Every view that lists what needs
// the owner (order, Today, agenda, next, the review stack, and their counts)
// reads it and adds only its own sort; a view that shows more (Waiting, Later,
// Done) says so by name and filter.
//
//	needs_you = open ∧ ¬later_in_force ∧ ¬agent_turn ∧ owner_turn(kind)
//
// later_in_force is the item's own deferral in force, an open deferred_after
// item, or a put-off goal or scope task (deferredSet below). agent_turn is an
// open request (queued or claimed) linked to the item, a task assigned to an
// agent, or an approved io draft not sent yet. owner_turn is per kind (need).
// Some owner turns start on a date (On): a meeting due_soon_days before it
// starts, an ask on its nudge date, an area on its review date, an exception
// or a constraint 14 days before review_by. Before that day the item is
// upcoming: dated views (agenda) list it, the needs-you set does not.

// Lifecycle is the one state every kind has: open, later, or closed.
type Lifecycle string

const (
	LifeOpen   Lifecycle = "open"
	LifeLater  Lifecycle = "later"
	LifeClosed Lifecycle = "closed"
)

// Turn is whose move an open item waits on.
type Turn string

const (
	TurnOwner   Turn = "owner"   // the owner's
	TurnAgent   Turn = "agent"   // an agent has it
	TurnWaiting Turn = "waiting" // a person, or another open item
	TurnNone    Turn = "none"    // asks nothing: a goal, a person, a skipped meeting
)

// reviewAheadDays is how long before review_by an exception or a constraint
// needs the owner.
const reviewAheadDays = 14

// Need is what the predicate says of one document on the snapshot's date.
type Need struct {
	Life Lifecycle
	Turn Turn
	// On is the first day the owner's turn counts (YYYY-MM-DD); "" is now.
	On string
	// Yes is needs_you. Reason says why in words, either way.
	Yes    bool
	Reason string
}

// Upcoming is an open item whose owner turn starts on a later day: a dated
// view lists it, the needs-you set does not yet.
func (n Need) Upcoming() bool { return n.Life == LifeOpen && n.Turn == TurnOwner && !n.Yes }

// Pending is an open item that is the owner's turn now or on a later day.
func (n Need) Pending() bool { return n.Life == LifeOpen && n.Turn == TurnOwner }

// lifeOf is open or closed by a type's status values; a type without a
// lifecycle (person, group, journal, ...) is open. Later is added by the
// snapshot.
func lifeOf(m model.Meta) Lifecycle {
	open := true
	switch x := m.(type) {
	case *model.Task:
		open = !taskTerminal(x.Status)
	case *model.Meeting:
		open = x.Status == model.MeetingRequested || x.Status == model.MeetingScheduled
	case *model.Question:
		open = x.Status == model.QuestionOpen
	case *model.Project:
		open = x.Status == model.ProjectActive || x.Status == model.ProjectPaused
	case *model.RFX:
		open = x.Status == model.RFXOpen
	case *model.Request:
		open = requestOpen(x)
	case *model.IO:
		open = x.State.Open()
	case *model.Incident:
		open = x.Status != model.IncidentResolved
	case *model.PolicyException:
		open = x.Status != model.ExceptionResolved
	case *model.Proposal:
		open = x.Status != model.ProposalSuperseded
	}
	if open {
		return LifeOpen
	}
	return LifeClosed
}

// requestOpen: proposed, queued, and claimed are open, and so is a request
// whose last turn ended needing the owner (stuck, blocked,
// needs_clarification) or whose result the owner has not seen.
func requestOpen(r *model.Request) bool {
	switch r.Status {
	case model.RequestProposed, model.RequestQueued, model.RequestClaimed:
		return true
	case model.RequestDone:
		return r.Resolution.NeedsOwner() || resultUnseen(r)
	case model.RequestFailed:
		return resultUnseen(r)
	}
	return false
}

// resultUnseen: work that ended done, partial, or failed whose result the
// owner has not opened (seen). A sweep's result is what it wrote in the
// vault, so it has nothing to check. Work with no active_at ended before
// 0.3.0 made seen, so it does not wait either.
func resultUnseen(r *model.Request) bool {
	if r.Seen != "" || r.RequiresSource != "" || r.ActiveAt == "" {
		return false
	}
	return r.Status == model.RequestFailed || (r.Status == model.RequestDone && !r.Resolution.NeedsOwner())
}

// requestNeedsOwner: the owner's turn on agent work: accept it, respond to
// it, or check its result.
func requestNeedsOwner(r *model.Request) bool {
	return r.Status == model.RequestProposed || (r.Status == model.RequestDone && r.Resolution.NeedsOwner()) || resultUnseen(r)
}

// agentHas: an agent holds the request now.
func agentHas(r *model.Request) bool {
	return r.Status == model.RequestQueued || r.Status == model.RequestClaimed
}

// resolved is true for an item that holds nothing up: closed, or a meeting
// the owner skips (what waits on it can go).
func resolved(d model.Document) bool {
	if t, ok := d.Meta.(*model.Task); ok && meetingSkipped(t) {
		return true
	}
	return lifeOf(d.Meta) == LifeClosed
}

// meetingSkipped reports whether the owner said they do not go.
func meetingSkipped(t *model.Task) bool {
	return t != nil && t.EffectiveKind() == model.KindMeeting && t.Attend == model.AttendNo
}

// meetingQuiet reports whether the meeting asks nothing of the owner: no
// health finding, no row in the value order, no notes prompt once it passed.
func meetingQuiet(t *model.Task) bool {
	return t != nil && attendQuiet(t.EffectiveKind(), t.Optional, t.Attend)
}

// attendQuiet is the attendance rule: a meeting the owner skips (attend no)
// asks nothing, and an optional meeting asks nothing unless the owner said
// they go (attend yes). An older `type: meeting` document is required.
func attendQuiet(kind model.TaskKind, optional bool, attend model.Attend) bool {
	return kind == model.KindMeeting && (attend == model.AttendNo || optional && attend != model.AttendYes)
}

// Snapshot is the vault read once for one request: documents by id, the Later
// set, and which items an agent holds. Views read it instead of the store.
type Snapshot struct {
	ctx   context.Context
	s     store.Store
	Today string
	Now   time.Time
	// DueSoonDays is order.due_soon_days: how long before a meeting the owner
	// needs to see it.
	DueSoonDays int
	refs        []model.Ref
	docs        map[string]model.Document
	missing     map[string]bool
	later       map[string]deferHold
	agent       map[string]string // item id -> request id an agent holds
	needs       map[string]Need
}

// snapTypes are the types a snapshot loads at once; any other document is
// read on first use.
var snapTypes = []model.Type{model.TypeTask, model.TypeMeeting, model.TypeQuestion, model.TypeProject, model.TypeRFX,
	model.TypeRequest, model.TypeIO, model.TypeInbox, model.TypeIncident, model.TypeArea, model.TypePolicyException, model.TypeConstraint}

// NewSnapshot reads the vault for the store's date.
func NewSnapshot(ctx context.Context, s store.Store) (*Snapshot, error) {
	return SnapshotAt(ctx, s, s.Now().Format(dateLayout))
}

// SnapshotAt reads the vault for the local date today (YYYY-MM-DD).
func SnapshotAt(ctx context.Context, s store.Store, today string) (*Snapshot, error) {
	refs, err := s.List(ctx, store.Filter{})
	if err != nil {
		return nil, fmt.Errorf("ops: snapshot: %w", err)
	}
	days, err := configInt(s, DueSoonKey, DefaultDueSoonDays)
	if err != nil {
		return nil, fmt.Errorf("ops: snapshot: %w", err)
	}
	sn := &Snapshot{ctx: ctx, s: s, Today: today, Now: s.Now(), DueSoonDays: days, docs: map[string]model.Document{},
		missing: map[string]bool{}, agent: map[string]string{}, needs: map[string]Need{}}
	for _, r := range refs {
		if !slices.Contains(snapTypes, r.Type) {
			continue
		}
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, fmt.Errorf("ops: snapshot: %w", err)
		}
		sn.refs = append(sn.refs, r)
		sn.docs[doc.ID] = doc
	}
	if err := sn.index(); err != nil {
		return nil, err
	}
	return sn, nil
}

// index finds which items an agent holds and which are under Later.
func (sn *Snapshot) index() error {
	for _, r := range sn.refs {
		doc := sn.docs[r.ID]
		req, ok := doc.Meta.(*model.Request)
		if !ok || !agentHas(req) {
			continue
		}
		sn.agent[doc.ID] = doc.ID
		for _, l := range req.Links {
			if _, seen := sn.agent[l.Target()]; !seen {
				sn.agent[l.Target()] = doc.ID
			}
		}
	}
	// A question the owner sent back holds its request, which links it too
	// when made by question_review; older ones may only name it here.
	for _, r := range sn.refs {
		if q, ok := sn.docs[r.ID].Meta.(*model.Question); ok && q.ReviewRequest.Target() != "" {
			if rd, ok := sn.docs[q.ReviewRequest.Target()]; ok {
				if req, ok := rd.Meta.(*model.Request); ok && agentHas(req) {
					sn.agent[r.ID] = rd.ID
				}
			}
		}
	}
	later, err := sn.deferredSet()
	sn.later = later
	return err
}

// Docs returns the loaded documents in list order.
func (sn *Snapshot) Docs() []model.Document {
	out := make([]model.Document, 0, len(sn.refs))
	for _, r := range sn.refs {
		out = append(out, sn.docs[r.ID])
	}
	return out
}

// Get returns a document by id, read from the store the first time when it is
// not of a loaded type (or hidden). A missing id is model.ErrNotFound.
func (sn *Snapshot) Get(id string) (model.Document, error) {
	if d, ok := sn.docs[id]; ok {
		return d, nil
	}
	if sn.missing[id] || sn.s == nil {
		return model.Document{}, model.ErrNotFound
	}
	d, err := sn.s.Get(sn.ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		sn.missing[id] = true
		return d, err
	}
	if err != nil {
		return d, err
	}
	sn.docs[id] = d
	return d, nil
}

// Later is why an item is under Later, if it is.
func (sn *Snapshot) Later(id string) deferHold { return sn.later[id] }

// AgentHolds is the request an agent holds for the item, or "".
func (sn *Snapshot) AgentHolds(id string) string { return sn.agent[id] }

// Need is the predicate for the document with id; a missing id is closed.
func (sn *Snapshot) Need(id string) Need {
	if n, ok := sn.needs[id]; ok {
		return n
	}
	doc, err := sn.Get(id)
	if err != nil {
		return Need{Life: LifeClosed, Turn: TurnNone, Reason: "not found"}
	}
	n := sn.need(doc)
	sn.needs[id] = n
	return n
}

// NeedsYou is the predicate: whether the document needs the owner now, and
// why in words.
func NeedsYou(sn *Snapshot, doc model.Document, now time.Time) (bool, string) {
	if !now.IsZero() && now.Format(dateLayout) != sn.Today {
		at := *sn
		at.Today, at.Now, at.needs = now.Format(dateLayout), now, map[string]Need{}
		later, err := at.deferredSet()
		if err != nil {
			return false, "cannot read what is put off: " + err.Error()
		}
		at.later = later
		n := at.need(doc)
		return n.Yes, n.Reason
	}
	n := sn.Need(doc.ID)
	return n.Yes, n.Reason
}

// Set is every loaded document that needs the owner, in list order.
func (sn *Snapshot) Set() []string {
	var out []string
	for _, r := range sn.refs {
		if sn.Need(r.ID).Yes {
			out = append(out, r.ID)
		}
	}
	return out
}

// need applies the rule to one document.
func (sn *Snapshot) need(doc model.Document) Need {
	life := lifeOf(doc.Meta)
	if life == LifeClosed {
		return Need{Life: LifeClosed, Turn: TurnNone, Reason: "closed"}
	}
	if sn.later[doc.ID].any() {
		return Need{Life: LifeLater, Turn: TurnNone, Reason: "later"}
	}
	if req := sn.agent[doc.ID]; req != "" {
		return Need{Life: LifeOpen, Turn: TurnAgent, Reason: "an agent has it (" + req + ")"}
	}
	n := sn.ownerTurn(doc)
	n.Life = LifeOpen
	n.Yes = n.Turn == TurnOwner && (n.On == "" || n.On <= sn.Today)
	return n
}

// ownerTurn is whose turn an open item is, by kind.
func (sn *Snapshot) ownerTurn(doc model.Document) Need {
	owner := func(reason string) Need { return Need{Turn: TurnOwner, Reason: reason} }
	none := func(reason string) Need { return Need{Turn: TurnNone, Reason: reason} }
	switch m := doc.Meta.(type) {
	case *model.Question:
		if m.HasProposal() {
			return owner("answer to check")
		}
		return owner("question to answer")
	case *model.Incident:
		return owner("incident " + string(m.Severity) + " " + string(m.Status))
	case *model.Meeting:
		if m.Status != model.MeetingScheduled {
			return owner("meeting to book")
		}
		return sn.meetingTurn(doc, nil)
	case *model.Task:
		if mt, ok := readMeeting(doc); ok && mt.Status == model.MeetingScheduled {
			return sn.meetingTurn(doc, m)
		}
		if n, ok := assigneeTurn(m.Assignee); ok {
			return n
		}
		for _, dep := range targets(m.DependsOn) {
			if dd, err := sn.Get(dep); err == nil && !resolved(dd) {
				return Need{Turn: TurnWaiting, Reason: "waits on " + dep}
			}
		}
		switch {
		case m.EffectiveKind() == model.KindMeeting:
			return owner("meeting to book")
		case m.EffectiveKind() == model.KindCommitment:
			// Work owed to someone: the owner's turn while open. The due
			// date sorts it; it is not a start date, so large work owed
			// next week still shows now.
			if m.Due != "" {
				return owner("owed, due " + string(m.Due))
			}
			return owner("owed")
		case m.Status == model.TaskBlocked || len(m.BlockedBy) > 0:
			return owner("blocked: follow up")
		}
		return owner("task to do")
	case *model.RFX:
		if m.Direction == model.RFXIn {
			return owner("asked of you")
		}
		replied, drafted, noReply := false, false, len(m.Targets) > 0
		for _, t := range m.Targets {
			replied = replied || t.State == model.TargetReplied
			drafted = drafted || t.State == model.TargetDrafted
			noReply = noReply && t.State == model.TargetNoReply
		}
		switch {
		case len(m.Targets) == 0:
			return owner("no one asked yet")
		case drafted:
			return owner("ask drafted, not sent")
		case replied:
			return owner("reply to sort")
		case noReply:
			return owner("stalled: no reply from anyone")
		case m.NextNudge != "":
			return Need{Turn: TurnOwner, On: string(m.NextNudge), Reason: "follow up " + string(m.NextNudge)}
		}
		return Need{Turn: TurnWaiting, Reason: "waiting on a reply"}
	case *model.IO:
		switch {
		case m.Direction == model.IOIn:
			return owner("to sort")
		case m.State == model.IODraft:
			return owner("draft to approve")
		case m.State == model.IOReady && !m.Approved(doc.Body):
			return owner("draft changed after approval")
		case m.State == model.IOReady:
			return Need{Turn: TurnAgent, Reason: "approved, not sent"}
		}
		return none("io")
	case *model.Inbox:
		if doc.Folder == model.FolderInbox {
			return owner("to sort")
		}
		return none("filed capture")
	case *model.Request:
		switch {
		case m.Status == model.RequestProposed:
			return owner("agent work to accept")
		case m.Status == model.RequestDone && m.Resolution.NeedsOwner():
			return owner("agent work ended " + string(m.Resolution))
		case m.Status == model.RequestFailed && resultUnseen(m):
			return owner(strings.TrimSuffix("failed: "+firstLine(lastSection(doc.Body, "Failure")), ": "))
		case resultUnseen(m):
			return owner("result to check")
		}
		return Need{Turn: TurnAgent, Reason: "agent work " + string(m.Status)}
	case *model.Area:
		if m.Status != model.AreaActive || m.Cadence == "" {
			return none("area")
		}
		if m.LastReviewed == "" {
			return owner("review " + m.Cadence + "; never reviewed")
		}
		if next := nextReview(m); next != "" {
			return Need{Turn: TurnOwner, On: next, Reason: "review " + m.Cadence}
		}
		return none("area")
	case *model.PolicyException:
		if m.ReviewBy == "" {
			return none("exception")
		}
		return Need{Turn: TurnOwner, On: sn.ahead(string(m.ReviewBy), reviewAheadDays), Reason: "exception to review by " + string(m.ReviewBy)}
	case *model.Constraint:
		if m.ReviewBy == "" {
			return none("constraint")
		}
		return Need{Turn: TurnOwner, On: sn.ahead(string(m.ReviewBy), reviewAheadDays), Reason: "constraint to review by " + string(m.ReviewBy)}
	}
	return none(string(doc.Meta.Header().Type))
}

// meetingTurn is the rule of a booked meeting (an obligation occurrence): it
// needs the owner from due_soon_days before it starts, unless the owner skips
// it or it is optional and the owner has not said they go. Once it ended it
// asks the owner to record what happened. t is nil for an older
// `type: meeting` document, which has no attendance.
func (sn *Snapshot) meetingTurn(doc model.Document, t *model.Task) Need {
	mt, _ := readMeeting(doc)
	if t != nil {
		switch {
		case t.Attend == model.AttendNo:
			return Need{Turn: TurnNone, Reason: "you skip it"}
		case t.Optional && t.Attend != model.AttendYes:
			return Need{Turn: TurnNone, Reason: "optional; you have not said you go"}
		}
		if n, ok := assigneeTurn(t.Assignee); ok {
			return n
		}
	}
	day, ok := meetingDay(mt.Start)
	if !ok {
		return Need{Turn: TurnOwner, Reason: "meeting with no readable time"}
	}
	if end, err := time.Parse(time.RFC3339, mt.End); err == nil && !end.After(sn.Now) {
		return Need{Turn: TurnOwner, Reason: "record what happened"}
	}
	return Need{Turn: TurnOwner, On: sn.ahead(day, sn.DueSoonDays), Reason: "meeting " + day}
}

// assigneeTurn: a task for an agent is the agent's turn, one for a person
// waits on them; self is not decided here.
func assigneeTurn(assignee string) (Need, bool) {
	switch {
	case IsAgent(assignee):
		return Need{Turn: TurnAgent, Reason: "assigned to " + assignee}, true
	case IsOther(assignee):
		return Need{Turn: TurnWaiting, Reason: "waiting on " + assignee}, true
	}
	return Need{}, false
}

// ahead is the day days before day; day itself when it does not parse.
func (sn *Snapshot) ahead(day string, days int) string {
	t, err := time.Parse(dateLayout, day)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, -days).Format(dateLayout)
}

// deferredSet returns, by id, why an item is under Later on the snapshot's
// date: an item with its own deferral in force or an open wait, a task or
// question whose goal is put off, and a question whose scope task is put off.
// An item with only Ready set is not under Later.
func (sn *Snapshot) deferredSet() (map[string]deferHold, error) {
	out := map[string]deferHold{}
	goal := map[string]string{} // task id -> goal id
	scope := map[string]string{}
	for _, r := range sn.refs {
		doc := sn.docs[r.ID]
		if df := deferralOf(doc.Meta); df != nil {
			h := deferHold{Date: df.IsDeferred(sn.Today)}
			if len(df.DeferredAfter) > 0 && lifeOf(doc.Meta) == LifeOpen {
				var err error
				if h.Waits, h.Ready, err = sn.waitState(*df); err != nil {
					return nil, err
				}
			}
			if h.Date || h.Waits != nil || h.Ready != "" {
				out[doc.ID] = h
			}
		}
		switch m := doc.Meta.(type) {
		case *model.Task:
			goal[doc.ID] = m.Project.Target()
		case *model.Question:
			scope[doc.ID] = scopeID(m)
		}
	}
	for id, g := range goal {
		if out[g].any() {
			h := out[id]
			h.Inherited = true
			out[id] = h
		}
	}
	for id, sc := range scope {
		if out[sc].any() || out[goal[sc]].any() {
			h := out[id]
			h.Inherited = true
			out[id] = h
		}
	}
	return out, nil
}

// waitState returns the open items of a deferral's waits, and a "ready"
// reason when the last one resolved within readyDays of today. A missing item
// does not hold.
func (sn *Snapshot) waitState(df model.Deferral) (waits []string, ready string, err error) {
	var latest, title string
	for _, id := range df.DeferredAfter {
		doc, gerr := sn.Get(id)
		if errors.Is(gerr, model.ErrNotFound) {
			continue
		}
		if gerr != nil {
			return nil, "", gerr
		}
		if !resolved(doc) {
			waits = append(waits, id)
			continue
		}
		when := string(doc.Meta.Header().Updated)
		if q, ok := doc.Meta.(*model.Question); ok && q.Answered != "" {
			when = string(q.Answered)
		}
		if when == "" {
			when = string(doc.Meta.Header().Created)
		}
		if latest == "" || when > latest {
			latest, title = when, doc.Title
		}
	}
	if len(waits) == 0 && latest != "" {
		if t, perr := time.Parse(dateLayout, sn.Today); perr == nil && latest >= t.AddDate(0, 0, -readyDays).Format(dateLayout) {
			ready = "ready: " + title + " done"
		}
	}
	return waits, ready, nil
}

// deferredSet is the Later set of the vault on the date today.
func deferredSet(ctx context.Context, s store.Store, today string) (map[string]deferHold, error) {
	sn, err := SnapshotAt(ctx, s, today)
	if err != nil {
		return nil, err
	}
	return sn.later, nil
}
