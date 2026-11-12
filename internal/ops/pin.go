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

// Pinned items are the ones the owner watches. A pin is the field "pinned"
// (an RFC 3339 time) on the item's own frontmatter, so it stays with the file
// under any sync tool and no shared list exists. A thread key that is not an
// item id (a "#tag" or a "channel:<source>/<thread>") is pinned on a small
// thread document (type thread, field key). Only the owner pins and unpins;
// an agent may suggest a pin in a thread reply.

// pinStaleDays is how long an item may be quiet before a pinned dashboard
// suggests unpinning it.
const pinStaleDays = 30

// notPinnable are the types that are not items the owner watches: agent
// work and the receipts of writes.
var notPinnable = []model.Type{model.TypeReceipt, model.TypeRequest}

// PinInput names what to pin or unpin: id (an item) or key (an item id,
// "#tag", or "channel:<source>/<thread>").
type PinInput struct {
	ID  string `json:"id,omitempty"`
	Key string `json:"key,omitempty"`
}

// PinOutput is the item or thread document that holds the pin. Changed is
// false when it already was as asked.
type PinOutput struct {
	ID      string     `json:"id"`
	Type    model.Type `json:"type"`
	Title   string     `json:"title"`
	Key     string     `json:"key,omitempty"` // the thread key, for a thread document
	Pinned  string     `json:"pinned,omitempty"`
	Changed bool       `json:"changed"`
	Rev     string     `json:"rev"`
}

// pinTarget finds the document that holds the pin of id or key. A key that is
// not an item id finds the thread document of the key, and make says to
// create it when there is none.
func pinTarget(ctx context.Context, d Deps, in PinInput, make bool) (model.Document, error) {
	id, key := normID(in.ID), strings.TrimSpace(in.Key)
	if (id == "") == (key == "") {
		return model.Document{}, errors.New("give id or key, one of them")
	}
	if id == "" {
		k, err := model.NormalizeThreadKey(key)
		if err != nil {
			return model.Document{}, fmt.Errorf("key: %w", err)
		}
		kind, v, _ := model.ParseThreadKey(k)
		if kind == model.ThreadItem {
			id = v
		} else {
			return threadDocOf(ctx, d, k, make)
		}
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return model.Document{}, linkErr("id", id, err)
	}
	if t := doc.Meta.Header().Type; slices.Contains(notPinnable, t) {
		return model.Document{}, fmt.Errorf("%s is %s: pin an item you watch, such as a goal, a thread, a question, an Ask, an obligation, or a task; for agent work, pin the item it is about", doc.ID, t)
	}
	return doc, nil
}

// threadDocOf is the thread document of key. With make it creates the
// document when there is none; without it a missing one is an error naming
// the key.
func threadDocOf(ctx context.Context, d Deps, key string, make bool) (model.Document, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeThread})
	if err != nil {
		return model.Document{}, err
	}
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return model.Document{}, err
		}
		if m, ok := doc.Meta.(*model.ThreadDoc); ok && m.Key == key {
			return doc, nil
		}
	}
	if !make {
		return model.Document{}, fmt.Errorf("key %q: not pinned", key)
	}
	return d.Store.Create(ctx, model.Document{Folder: model.FolderThreads, Title: key,
		Meta: &model.ThreadDoc{Common: model.Common{Type: model.TypeThread}, Key: key}})
}

func pinOutput(doc model.Document, changed bool) PinOutput {
	h := doc.Meta.Header()
	out := PinOutput{ID: doc.ID, Type: h.Type, Title: doc.Title, Pinned: h.Pinned, Changed: changed, Rev: doc.Rev}
	if m, ok := doc.Meta.(*model.ThreadDoc); ok {
		out.Key = m.Key
	}
	return out
}

// Pin pins an item or a thread key.
var Pin = withOneReceipt(definePerson("pin", "Pin an item or a thread key so the owner watches it on the Pinned dashboard: id (any item: a goal, a thread, a question, an Ask, an obligation, a task, a person, ...) or key (an item id, #tag, or channel:<source>/<thread>; a key that is not an item gets a small thread document). "+
	"Not a receipt or agent work. Pinning a pinned item changes nothing. One receipt; receipt_undo reverses it. People only: an agent suggests a pin in its reply", func(ctx context.Context, d Deps, in PinInput) (PinOutput, error) {
	doc, err := pinTarget(ctx, d, in, true)
	if err != nil {
		return PinOutput{}, err
	}
	h := doc.Meta.Header()
	if h.Pinned != "" {
		return pinOutput(doc, false), nil
	}
	h.Pinned = d.Store.Now().UTC().Format(time.RFC3339)
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return PinOutput{}, err
	}
	return pinOutput(saved, true), nil
}))

// Unpin unpins an item or a thread key.
var Unpin = withOneReceipt(definePerson("unpin", "Unpin an item or a thread key: id or key as for pin. Unpinning an item that is not pinned changes nothing. One receipt; receipt_undo reverses it. People only", func(ctx context.Context, d Deps, in PinInput) (PinOutput, error) {
	doc, err := pinTarget(ctx, d, in, false)
	if err != nil {
		return PinOutput{}, err
	}
	h := doc.Meta.Header()
	if h.Pinned == "" {
		return pinOutput(doc, false), nil
	}
	h.Pinned = ""
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return PinOutput{}, err
	}
	return pinOutput(saved, true), nil
}))

// --- pinned ---

// PinnedInput is the input of pinned.
type PinnedInput struct{}

// PinnedLast is the latest change of a pinned item.
type PinnedLast struct {
	At      string `json:"at"`
	Who     string `json:"who"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}

// PinnedCard is one pinned item with what the owner needs to watch it.
// Matters is the goal, area, or parent that says why it matters. NextDate is
// the earliest of due, scheduled, nudge, and the day the owner's turn starts;
// NextKind says which. WaitingOn lists the people and open items it waits
// on; Queued and Claimed count the agent turns waiting and held. Turns is
// the open thread turns (queued plus claimed) of the item's own thread, and
// LastReply the start of the newest agent reply there. Stale says why
// unpinning could be right: closed, or quiet for 30 days.
type PinnedCard struct {
	ID           string      `json:"id"`
	Type         model.Type  `json:"type"`
	Key          string      `json:"key,omitempty"`
	Title        string      `json:"title"`
	Pinned       string      `json:"pinned"`
	State        string      `json:"state,omitempty"`
	Life         Lifecycle   `json:"life"`
	Matters      string      `json:"matters,omitempty"`
	MattersName  string      `json:"matters_name,omitempty" nameof:"Matters"`
	NextDate     string      `json:"next_date,omitempty"`
	NextKind     string      `json:"next_kind,omitempty"`
	WaitingOn    []string    `json:"waiting_on"`
	WaitingNames []string    `json:"waiting_names,omitempty" nameof:"WaitingOn"`
	Queued       int         `json:"queued"`
	Claimed      int         `json:"claimed"`
	Last         *PinnedLast `json:"last,omitempty"`
	Turns        int         `json:"turns"`
	LastReply    string      `json:"last_reply,omitempty"`
	NeedsYou     bool        `json:"needs_you"`
	Reason       string      `json:"reason"`
	Stale        string      `json:"stale,omitempty"`
}

// PinnedOutput is every pinned item, in order: needs you first, then the
// next date, earliest first (none last), then the newest pin.
type PinnedOutput struct {
	Items []PinnedCard `json:"items"`
}

// reply snippet length.
const pinReplyLen = 160

// matters is the id of what the item is for: its goal, area, or parent.
func matters(m model.Meta) string {
	var ls []model.Link
	switch x := m.(type) {
	case *model.Task:
		ls = []model.Link{x.Goal, x.Area, x.Parent}
	case *model.Obligation:
		ls = []model.Link{x.Goal, x.Area, x.Parent}
	case *model.RFX:
		ls = []model.Link{x.Goal, x.Parent}
	case *model.Question:
		if x.Scope != model.ScopeRoot {
			ls = []model.Link{x.Scope}
		}
	case *model.Goal:
		ls = []model.Link{x.Area}
	case *model.Note:
		ls = []model.Link{x.Goal, x.Area}
	case *model.Incident:
		ls = []model.Link{x.Area}
	}
	for _, l := range ls {
		if l != "" {
			return l.Target()
		}
	}
	return ""
}

// nextDate is the earliest date the item names, and which kind it is.
func nextDate(m model.Meta, n Need) (date, kind string) {
	cands := [][2]string{{n.On, "turn"}}
	day := func(s string) string {
		if len(s) >= 10 {
			return s[:10]
		}
		return s
	}
	switch x := m.(type) {
	case *model.Task:
		cands = append(cands, [2]string{string(x.Due), "due"}, [2]string{day(x.Scheduled), "scheduled"})
	case *model.Obligation:
		cands = append(cands, [2]string{string(x.Due), "due"}, [2]string{day(x.Scheduled), "scheduled"})
	case *model.RFX:
		cands = append(cands, [2]string{string(x.NextNudge), "nudge"})
	case *model.Meeting:
		cands = append(cands, [2]string{day(x.Start), "scheduled"})
	}
	for _, c := range cands {
		if c[0] != "" && (date == "" || c[0] < date) {
			date, kind = c[0], c[1]
		}
	}
	return date, kind
}

// pinWaiting is who the item waits on: open hard dependencies, and for an
// Ask the people it was sent to who have not replied.
func pinWaiting(sn *Snapshot, m model.Meta) []string {
	out := []string{}
	add := func(id string) {
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, l := range model.HardDeps(m) {
		if sn.Need(l.Target()).Life != LifeClosed {
			add(l.Target())
		}
	}
	if r, ok := m.(*model.RFX); ok && r.Direction == model.RFXOut {
		for _, t := range r.Targets {
			switch t.State {
			case model.TargetDrafted, model.TargetSent, model.TargetRequested, model.TargetScheduled:
				add(t.To.Target())
			}
		}
	}
	return out
}

// Pinned reads every pinned item as a card.
var Pinned = defineRead("pinned", "List the pinned items, the ones the owner watches: for each id, type, title, state, lifecycle, why it matters (goal, area, or parent), the next date (due, scheduled, nudge, or when the owner's turn starts), who or what it waits on, "+
	"agent turns queued and held, the last change (when, who, what), open thread turns with the newest reply, and needs_you with the reason. Needs you first, then the next date, then the newest pin. stale says why unpinning could be right (closed, or quiet for 30 days)", func(ctx context.Context, d Deps, in PinnedInput) (PinnedOutput, error) {
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return PinnedOutput{}, err
	}
	// The pin is not activity of the item: a card shows the last change after it.
	rows, err := activityFeedSkip(ctx, d, ActivityInput{}, time.Time{}, time.Time{}, []string{"pin", "unpin"})
	if err != nil {
		return PinnedOutput{}, err
	}
	last := map[string]ChangeRow{}
	for _, r := range rows {
		if _, ok := last[r.Doc]; !ok {
			last[r.Doc] = r
		}
	}
	// Turns by thread key: a request is in the thread of its key, or of the
	// item it links.
	type turns struct {
		queued, claimed int
		reply, replyAt  string
	}
	byKey := map[string]*turns{}
	note := func(key string, doc model.Document, r *model.Request) {
		if key == "" {
			return
		}
		t := byKey[key]
		if t == nil {
			t = &turns{}
			byKey[key] = t
		}
		switch r.Status {
		case model.RequestQueued:
			t.queued++
		case model.RequestClaimed:
			t.claimed++
		}
		if r.Reply != "" && string(r.Created) >= t.replyAt {
			t.reply, t.replyAt = r.Reply, string(r.Created)
		}
	}
	for _, doc := range sn.Docs() {
		r, ok := doc.Meta.(*model.Request)
		if !ok {
			continue
		}
		seen := map[string]bool{}
		for _, k := range append([]string{requestThread(doc, r)}, doc.Meta.Header().Threads...) {
			if !seen[k] {
				seen[k] = true
				note(k, doc, r)
			}
		}
		for _, l := range r.Links {
			if k := l.Target(); !seen[k] {
				seen[k] = true
				note(k, doc, r)
			}
		}
	}
	now := d.Store.Now()
	quiet := now.AddDate(0, 0, -pinStaleDays)
	out := PinnedOutput{Items: []PinnedCard{}}
	refs, err := d.Store.List(ctx, store.Filter{})
	if err != nil {
		return PinnedOutput{}, err
	}
	for _, ref := range refs {
		if ref.Pinned == "" {
			continue
		}
		doc, err := sn.Get(ref.ID)
		if err != nil {
			return PinnedOutput{}, err
		}
		h := doc.Meta.Header()
		if h.Pinned == "" {
			continue
		}
		n := sn.Need(doc.ID)
		c := PinnedCard{ID: doc.ID, Type: h.Type, Title: doc.Title, Pinned: h.Pinned, State: statusOf(doc.Meta), Life: n.Life,
			Matters: matters(doc.Meta), WaitingOn: pinWaiting(sn, doc.Meta), NeedsYou: n.Yes, Reason: n.Reason}
		c.NextDate, c.NextKind = nextDate(doc.Meta, n)
		threadKey := doc.ID
		if m, ok := doc.Meta.(*model.ThreadDoc); ok {
			c.Key, threadKey, c.Title = m.Key, m.Key, m.Key
			c.Life = LifeOpen
		}
		if t := byKey[threadKey]; t != nil {
			c.Queued, c.Claimed, c.Turns = t.queued, t.claimed, t.queued+t.claimed
			c.LastReply = clip(strings.Join(strings.Fields(t.reply), " "), pinReplyLen)
		}
		if r, ok := last[doc.ID]; ok {
			c.Last = &PinnedLast{At: r.At, Who: r.Who, Kind: r.Kind, Summary: r.Summary}
		}
		// Quiet counts from the last change, else from the pin.
		since := turnTime(h.Pinned)
		if c.Last != nil {
			since = turnTime(c.Last.At)
		}
		switch {
		case c.Life == LifeClosed:
			c.Stale = "closed"
		case since.Before(quiet):
			c.Stale = fmt.Sprintf("quiet for %d days", pinStaleDays)
		}
		out.Items = append(out.Items, c)
	}
	slices.SortStableFunc(out.Items, func(a, b PinnedCard) int {
		if a.NeedsYou != b.NeedsYou {
			if a.NeedsYou {
				return -1
			}
			return 1
		}
		if a.NextDate != b.NextDate {
			switch {
			case a.NextDate == "":
				return 1
			case b.NextDate == "":
				return -1
			}
			return strings.Compare(a.NextDate, b.NextDate)
		}
		if c := strings.Compare(b.Pinned, a.Pinned); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
})
