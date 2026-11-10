package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// The pre-send check. A draft the owner approved may go out hours later,
// after the people it answers wrote again. Right before it sends, the agent
// reads the channel again, adds what is new with io_add, and calls
// io_send_check: if an incoming message joined the draft's conversation after
// the owner approved, the draft goes back to the owner (held), and the agent
// does not send it. io_send from an agent is refused without a passing check
// after the approval and within sendCheckWindow.
//
// The conversation of a draft is what para records, never a vendor's thread
// model:
//
//  1. thread links: the items the draft's thread chain names, and every io
//     item whose thread names one of them, until nothing is added;
//  2. via thread: io items whose via has the same source (resolved to a
//     source document when it names one) and the same via.thread as an item
//     of step 1, or as the draft;
//  3. people: incoming io items of the draft's medium from a recipient of
//     the draft (the same person or group link, or the same address).
//
// A message is new when its at is after ApprovedSeen: the newest at of the
// incoming messages of the conversation when the owner approved. So a
// message that came before the approval but was read in after it counts.

// sendCheckWindow is how long a passing io_send_check lets an agent send: long
// enough to send, short enough that the channel has not moved on.
const sendCheckWindow = 30 * time.Minute

// conversation returns the incoming io items in the conversation of the
// outgoing draft (rules above), oldest first.
func conversation(ctx context.Context, d Deps, draft model.Document, dm *model.IO) ([]model.Document, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeIO})
	if err != nil {
		return nil, err
	}
	sources, err := loadSources(ctx, d.Store)
	if err != nil {
		return nil, err
	}
	res := newSourceResolver(sources)
	viaKey := func(v *model.IOVia) string {
		if v == nil || strings.TrimSpace(v.Thread) == "" {
			return ""
		}
		src := res.resolve(v.Source)
		if src == "" {
			src = strings.ToLower(ioLine(v.Source))
		}
		return src + "\x00" + strings.TrimSpace(v.Thread)
	}
	all := map[string]*model.IO{}
	var docs []model.Document
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if m, ok := doc.Meta.(*model.IO); ok && doc.Folder != model.FolderArchive {
			all[doc.ID] = m
			docs = append(docs, doc)
		}
	}
	// 1. thread links: the chain up from the draft, then down to fixpoint.
	in := map[string]bool{draft.ID: true}
	for t := dm.Thread.Target(); t != "" && !in[t]; {
		in[t] = true
		next := ""
		if m := all[t]; m != nil {
			next = m.Thread.Target()
		}
		t = next
	}
	for grew := true; grew; {
		grew = false
		for id, m := range all {
			if !in[id] && in[m.Thread.Target()] {
				in[id], grew = true, true
			}
		}
	}
	// 2. via thread.
	keys := map[string]bool{}
	for id := range in {
		if m := all[id]; m != nil {
			if k := viaKey(m.Via); k != "" {
				keys[k] = true
			}
		}
	}
	if k := viaKey(dm.Via); k != "" {
		keys[k] = true
	}
	// 3. people: the draft's recipients, on its medium.
	to := map[string]bool{}
	for _, p := range dm.To {
		to[strings.ToLower(ioLine(p))] = true
	}
	var out []model.Document
	for _, doc := range docs {
		m := all[doc.ID]
		if m.Direction != model.IOIn || doc.ID == draft.ID {
			continue
		}
		if in[doc.ID] || keys[viaKey(m.Via)] && viaKey(m.Via) != "" ||
			m.Medium == dm.Medium && m.From != "" && to[strings.ToLower(ioLine(m.From))] {
			out = append(out, doc)
		}
	}
	slices.SortStableFunc(out, func(a, b model.Document) int {
		return strings.Compare(a.Meta.(*model.IO).At, b.Meta.(*model.IO).At)
	})
	return out, nil
}

// newestAt is the latest at among the messages, RFC 3339 UTC; "" for none.
func newestAt(docs []model.Document) string {
	var best time.Time
	for _, doc := range docs {
		if t, err := time.Parse(time.RFC3339Nano, doc.Meta.(*model.IO).At); err == nil && t.After(best) {
			best = t
		}
	}
	if best.IsZero() {
		return ""
	}
	return best.UTC().Format(time.RFC3339Nano)
}

// markApproved records the owner's approval of a draft: the hash of its
// text, when, and the newest incoming message of its conversation then. A
// hold or an earlier check is cleared: the owner has seen the situation. A
// for_me draft becomes as_me first: the stamp comes off, and the owner
// approves the text without it (delegation.go).
func markApproved(ctx context.Context, d Deps, doc *model.Document, m *model.IO) error {
	asMe(doc, m)
	conv, err := conversation(ctx, d, *doc, m)
	if err != nil {
		return err
	}
	m.State, m.ApprovedHash = model.IOReady, m.DraftHash(doc.Body)
	m.ApprovedAt, m.ApprovedSeen = nowString(d.Store), newestAt(conv)
	m.CheckedAt, m.Held, m.HeldBy = "", "", nil
	return nil
}

// newSince lists the messages whose at is after seen; with seen empty, all.
func newSince(conv []model.Document, seen string) []model.Document {
	base, err := time.Parse(time.RFC3339Nano, seen)
	var out []model.Document
	for _, doc := range conv {
		t, terr := time.Parse(time.RFC3339Nano, doc.Meta.(*model.IO).At)
		if seen == "" || err != nil || terr != nil || t.After(base) {
			out = append(out, doc)
		}
	}
	return out
}

// IOSendCheckInput is the input of io_send_check.
type IOSendCheckInput struct {
	ID string `json:"id"`
}

// IOSendCheckMessage is one incoming message that came after the approval.
type IOSendCheckMessage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	From  string `json:"from,omitempty"`
	At    string `json:"at,omitempty"`
}

// IOSendCheckOutput is the output of io_send_check.
type IOSendCheckOutput struct {
	ID string `json:"id"`
	// OK: nothing new; send now with your own tool, then io_send within
	// sendCheckWindow. Held: the draft went back to the owner; do not send.
	OK        bool                 `json:"ok"`
	Held      bool                 `json:"held"`
	Reason    string               `json:"reason,omitempty"`
	New       []IOSendCheckMessage `json:"new"`
	CheckedAt string               `json:"checked_at,omitempty"`
	SendBy    string               `json:"send_by,omitempty"` // io_send is refused after this time
	State     model.IOState        `json:"state"`
	Next      string               `json:"next"`
	Rev       string               `json:"rev"`
}

// IOSendCheck is the pre-send check: call it right before you send an
// approved draft, after you read the channel again.
var IOSendCheck = defineSafe("io_send_check", "Check an outgoing draft right before you send it: an approved draft, or a for_me draft a delegation rule allows (stamped). First read its channel again with your own tool and io_add what is new (with via {source, ref, thread}). If an incoming message joined the draft's conversation (thread links, the same via source and thread, or a recipient writing on the same medium) after the owner approved (for_me: after it was drafted), the draft goes to the owner (held: draft state, approval cleared, reason) and you must not send it; tell the owner what changed. If nothing is new, send within 30 minutes and record it with io_send, which refuses an agent's send without a passing check", func(ctx context.Context, d Deps, in IOSendCheckInput) (IOSendCheckOutput, error) {
	doc, m, err := getOpenIO(ctx, d, in.ID)
	if err != nil {
		return IOSendCheckOutput{}, err
	}
	out := IOSendCheckOutput{ID: doc.ID, New: []IOSendCheckMessage{}, State: m.State}
	since, sinceWhat := m.ApprovedSeen, "since you approved"
	switch {
	case m.Direction != model.IOOut:
		return out, fmt.Errorf("%s is incoming: only an outgoing draft is checked", doc.ID)
	case m.ForMe() && m.State == model.IODraft:
		if m.Held != "" {
			return out, fmt.Errorf("%s is held: %s; the owner approves it first", doc.ID, m.Held)
		}
		v, err := judge(ctx, d, doc, m)
		if err != nil {
			return out, err
		}
		if v.rule == "" {
			return out, fmt.Errorf("%s is for_me, but no delegation rule allows it: it waits for the owner's approval (io_approve); delegation_run says why", doc.ID)
		}
		if !stamped(doc.Body, v.stamp) || m.Stamp != v.stamp {
			return out, fmt.Errorf("%s does not end with its stamp %q: io_edit with voice for_me to stamp it again", doc.ID, v.stamp)
		}
		m.AllowedBy = v.rule
		since, sinceWhat = m.DraftedSeen, "since the agent drafted it"
	case m.State != model.IOReady || !m.Approved(doc.Body):
		return out, fmt.Errorf("%s is not approved as it is: the owner approves it first (io_approve)", doc.ID)
	case m.ApprovedAt == "":
		return out, fmt.Errorf("%s was approved before para recorded when: ask the owner to approve it again (io_approve)", doc.ID)
	}
	conv, err := conversation(ctx, d, doc, m)
	if err != nil {
		return out, err
	}
	fresh := newSince(conv, since)
	if len(fresh) == 0 {
		m.CheckedAt = nowString(d.Store)
		saved, err := d.Store.Update(ctx, doc)
		if err != nil {
			return out, err
		}
		t, _ := time.Parse(time.RFC3339Nano, m.CheckedAt)
		out.OK, out.CheckedAt, out.SendBy, out.Rev = true, m.CheckedAt, t.Add(sendCheckWindow).UTC().Format(time.RFC3339), saved.Rev
		out.Next = "nothing new since the owner approved: send the exact approved text now with your own tool, then io_send by send_by"
		if m.ForMe() {
			out.Next = "nothing new since you drafted it: " + m.AllowedBy + " allows it. Send the exact text, with its stamp, now with your own tool, then io_send by send_by"
		}
		return out, nil
	}
	m.State, m.ApprovedHash, m.CheckedAt = model.IODraft, "", ""
	m.HeldBy = nil
	for _, f := range fresh {
		fm := f.Meta.(*model.IO)
		out.New = append(out.New, IOSendCheckMessage{ID: f.ID, Title: f.Title, From: fm.From, At: fm.At})
		m.HeldBy = append(m.HeldBy, model.LinkTo(f.ID))
	}
	m.Held = fmt.Sprintf("Situation changed: %s %s", plural(len(fresh), "new message"), sinceWhat)
	m.Attempts = append(m.Attempts, model.IOAttempt{At: nowString(d.Store), Actor: d.Store.Info().Actor, Tool: "io_send_check",
		Medium: m.Medium, Result: model.AttemptHeld, Error: m.Held})
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return out, err
	}
	out.Held, out.Reason, out.State, out.Rev = true, m.Held, m.State, saved.Rev
	out.Next = "do not send: the draft is back with the owner. Tell the owner what came in (new), offer a revised draft with io_edit, and wait for their approval again"
	return out, nil
})

// sendGate refuses an agent's io_send of a draft that has no passing
// io_send_check after its approval, within sendCheckWindow.
func sendGate(d Deps, doc model.Document, m *model.IO) error {
	switch {
	case m.Held != "" && m.State == model.IODraft:
		return fmt.Errorf("%s is held: %s; the owner approves it again first", doc.ID, m.Held)
	case m.State != model.IOReady || !m.Approved(doc.Body):
		return fmt.Errorf("%s is not approved as it is: the owner approves it (io_approve), then io_send_check, then send; if the owner sent it themself and you only saw it, give observed", doc.ID)
	case m.ApprovedAt == "":
		return fmt.Errorf("%s has no pre-send check: read the channel again, io_add what is new, then io_send_check before you send", doc.ID)
	}
	return checkFresh(d, doc, m.CheckedAt, m.ApprovedAt)
}

// checkFresh refuses a send with no passing check, a check before the
// approval (approved empty: none), or one older than sendCheckWindow.
func checkFresh(d Deps, doc model.Document, checkedAt, approvedAt string) error {
	if checkedAt == "" {
		return fmt.Errorf("%s has no pre-send check: read the channel again, io_add what is new, then io_send_check before you send", doc.ID)
	}
	checked, err := time.Parse(time.RFC3339Nano, checkedAt)
	if err != nil {
		return fmt.Errorf("%s: pre-send check time: %w", doc.ID, err)
	}
	if approvedAt != "" {
		approved, err := time.Parse(time.RFC3339Nano, approvedAt)
		if err != nil {
			return fmt.Errorf("%s: approval time: %w", doc.ID, err)
		}
		if checked.Before(approved) {
			return fmt.Errorf("%s was approved again after its last check: io_send_check again before you send", doc.ID)
		}
	}
	if d.Store.Now().Sub(checked) > sendCheckWindow {
		return fmt.Errorf("%s: the pre-send check is older than %s: read the channel again and io_send_check again", doc.ID, sendCheckWindow)
	}
	return nil
}
