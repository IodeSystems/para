package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iodesystems/para/internal/model"
)

// Reads for the Output page: open proposals (outgoing drafts) grouped, and
// the history of what went out. Both are generic: no per-medium code.

// ioNamed is a linked document with its title.
type ioNamed struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
}

// ioWhen is when an item came or went: at, else the day it was made.
func ioWhen(m *model.IO) string { return firstOf(m.At, string(m.Created)) }

// ioThreadKey is the thread key of a draft: what it answers, else its
// conversation, else its own id.
func ioThreadKey(id string, m *model.IO) string {
	k := m.ThreadKey()
	if k == "" && m.Via != nil {
		k = m.Via.ThreadKey()
	}
	return firstOf(k, id)
}

// ioAttemptLast is the last attempt, nil for none.
func ioAttemptLast(m *model.IO) *model.IOAttempt {
	if len(m.Attempts) == 0 {
		return nil
	}
	a := m.Attempts[len(m.Attempts)-1]
	return &a
}

// ioVariantRoot is the id of the first draft of the variation set a draft is in.
func ioVariantRoot(id string, m *model.IO) string { return firstOf(normID(m.VariantOf), id) }

// IOThreadTurns is the turn counts of a thread.
type IOThreadTurns struct {
	Key     string `json:"key"`
	Queued  int    `json:"queued"`
	Claimed int    `json:"claimed"`
	Done    int    `json:"done"`
}

// IOProposal is one outgoing draft in io_proposals. Approval is approved
// (ready with the approved text), changed (ready, but the text changed since
// the approval), or not_approved. Revision is the number of the text now in
// the draft; Revisions counts the saved earlier ones. Answers is the item it
// answers; Request is the request that made it.
type IOProposal struct {
	ID           string               `json:"id"`
	Title        string               `json:"title"`
	To           []string             `json:"to"`
	Medium       model.Medium         `json:"medium"`
	Kind         string               `json:"kind,omitempty"`
	Subject      string               `json:"subject,omitempty"`
	Preview      string               `json:"preview"`
	State        model.IOState        `json:"state"`
	Created      model.Date           `json:"created"`
	NeedsYou     bool                 `json:"needs_you"`
	Why          string               `json:"why,omitempty"`
	Impact       string               `json:"impact,omitempty"`
	Assessments  []model.IOAssessment `json:"assessments"`
	NotStated    []string             `json:"not_stated"`
	Revision     int                  `json:"revision"`
	Revisions    int                  `json:"revisions"`
	Approval     string               `json:"approval"`
	ApprovedHash string               `json:"approved_hash,omitempty"`
	DraftHash    string               `json:"draft_hash"`
	Held         string               `json:"held,omitempty"`
	HeldBy       []string             `json:"held_by"`
	Links        []ioNamed            `json:"links"`
	Answers      *ioNamed             `json:"answers,omitempty"`
	Request      string               `json:"request,omitempty"`
	Thread       IOThreadTurns        `json:"thread"`
	LastAttempt  *model.IOAttempt     `json:"last_attempt,omitempty"`
}

// IOVariationSet is a draft and the variants of it. Root is the first draft;
// Drafts holds the ones listed, needs-owner first, then by age.
type IOVariationSet struct {
	Root     string       `json:"root"`
	NeedsYou bool         `json:"needs_you"`
	Drafts   []IOProposal `json:"drafts"`
}

// IOProposalGroup is a batch of drafts: the group key, else the request that
// made them, else the draft id.
type IOProposalGroup struct {
	Group    string           `json:"group"`
	NeedsYou bool             `json:"needs_you"`
	Sets     []IOVariationSet `json:"sets"`
}

// IOProposalsInput is the input of io_proposals. With no state, open drafts
// (draft, ready) are listed; all adds sent and dropped ones.
type IOProposalsInput struct {
	Group string        `json:"group,omitempty" query:"group"`
	State model.IOState `json:"state,omitempty" query:"state"`
	All   bool          `json:"all,omitempty" query:"all"`
}

// IOProposalsOutput is the output of io_proposals. Count is the number of
// drafts; NeedsYou counts those that need the owner.
type IOProposalsOutput struct {
	Groups   []IOProposalGroup `json:"groups"`
	Count    int               `json:"count"`
	NeedsYou int               `json:"needs_you"`
}

func preview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "..."
}

// namedLinks resolves ids to titles; an id that is gone keeps no title.
func namedLinks(sn *Snapshot, ids []string) []ioNamed {
	out := []ioNamed{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		n := ioNamed{ID: id}
		if doc, err := sn.Get(id); err == nil {
			n.Title, n.Type = doc.Title, string(doc.Meta.Header().Type)
		}
		out = append(out, n)
	}
	return out
}

// madeBy is the request that wrote the first receipt of a document; rs is
// oldest first.
func madeBy(rs []ReceiptView, doc string) string {
	for _, r := range rs {
		if r.Doc == doc && r.Request != "" {
			return r.Request
		}
	}
	return ""
}

// IOProposals lists open outgoing drafts, grouped.
var IOProposals = defineRead("io_proposals", "List open outgoing drafts (proposals) grouped for review: group (the group key, else the request that made them, else the draft id), then variation sets (a draft and its variants), then each draft with its subject, preview, why, impact, assessments (not_stated lists the unstated), revision count, approval state and approved hash, held, links and the item it answers, the turns of its thread (queued, claimed, done), and its last attempt. Needs-owner first, then by age. all adds sent and dropped drafts", func(ctx context.Context, d Deps, in IOProposalsInput) (IOProposalsOutput, error) {
	out := IOProposalsOutput{Groups: []IOProposalGroup{}}
	if in.State != "" && !slices.Contains(model.IOStates, in.State) {
		return out, fmt.Errorf("state %q: want one of %v", in.State, model.IOStates)
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return out, err
	}
	rs, err := listReceipts(ctx, d.Store)
	if err != nil {
		return out, err
	}
	slices.Reverse(rs) // oldest first
	turns := map[string]IOThreadTurns{}
	type row struct {
		p     IOProposal
		group string
		root  string
		when  string
	}
	var rows []row
	for _, doc := range sn.Docs() {
		m, ok := doc.Meta.(*model.IO)
		if !ok || m.Direction != model.IOOut {
			continue
		}
		if in.State != "" && m.State != in.State || in.State == "" && !in.All && !m.State.Open() {
			continue
		}
		it := ioItem(doc, m)
		req := madeBy(rs, doc.ID)
		group := firstOf(m.Group, req, doc.ID)
		if in.Group != "" && group != in.Group {
			continue
		}
		p := IOProposal{ID: doc.ID, Title: doc.Title, To: it.To, Medium: m.Medium, Kind: m.Kind, Subject: m.Subject, Preview: preview(doc.Body, 200),
			State: m.State, Created: m.Created, NeedsYou: sn.Need(doc.ID).Yes, Why: m.Why, Impact: m.Impact, Assessments: it.Assessments,
			NotStated: append([]string{}, it.NotStated...), Revision: currentRevision(m), Revisions: len(m.Revisions),
			ApprovedHash: m.ApprovedHash, DraftHash: it.DraftHash, Held: m.Held, HeldBy: targets(m.HeldBy), Request: req, LastAttempt: ioAttemptLast(m)}
		switch {
		case m.Approved(doc.Body):
			p.Approval = "approved"
		case m.State == model.IOReady:
			p.Approval = "changed"
		default:
			p.Approval = "not_approved"
		}
		p.Links = namedLinks(sn, targets(m.Links))
		if a := namedLinks(sn, []string{m.Thread.Target()}); len(a) > 0 {
			p.Answers = &a[0]
		}
		key := ioThreadKey(doc.ID, m)
		tt, ok := turns[key]
		if !ok {
			tt = IOThreadTurns{Key: key}
			th, err := Thread.Run(ctx, d, ThreadInput{Key: key})
			switch {
			case err == nil:
				tt.Queued, tt.Claimed = th.Queued, th.Claimed
				for _, t := range th.Turns {
					if t.Kind == ThreadTurnRequest && t.Status == model.RequestDone {
						tt.Done++
					}
				}
			case !errors.Is(err, model.ErrNotFound):
				return out, err
			}
			turns[key] = tt
		}
		p.Thread = tt
		rows = append(rows, row{p: p, group: group, root: ioVariantRoot(doc.ID, m), when: ioWhen(m)})
	}
	// Needs-owner first, then oldest first: groups, sets, and drafts keep it.
	slices.SortStableFunc(rows, func(a, b row) int {
		if a.p.NeedsYou != b.p.NeedsYou {
			if a.p.NeedsYou {
				return -1
			}
			return 1
		}
		if c := turnTime(a.when).Compare(turnTime(b.when)); c != 0 {
			return c
		}
		return strings.Compare(a.p.ID, b.p.ID)
	})
	gAt := map[string]int{}
	for _, r := range rows {
		gi, ok := gAt[r.group]
		if !ok {
			gi = len(out.Groups)
			gAt[r.group] = gi
			out.Groups = append(out.Groups, IOProposalGroup{Group: r.group, Sets: []IOVariationSet{}})
		}
		g := &out.Groups[gi]
		si := slices.IndexFunc(g.Sets, func(s IOVariationSet) bool { return s.Root == r.root })
		if si < 0 {
			si = len(g.Sets)
			g.Sets = append(g.Sets, IOVariationSet{Root: r.root})
		}
		g.Sets[si].Drafts = append(g.Sets[si].Drafts, r.p)
		if r.p.NeedsYou {
			g.NeedsYou, g.Sets[si].NeedsYou = true, true
			out.NeedsYou++
		}
		out.Count++
	}
	return out, nil
})

// IOSentInput is the input of io_sent. Since is a day or RFC 3339; the
// default limit is 50.
type IOSentInput struct {
	Since  string       `json:"since,omitempty" query:"since"`
	Medium model.Medium `json:"medium,omitempty" query:"medium"`
	To     string       `json:"to,omitempty" query:"to"`
	Limit  int          `json:"limit,omitempty" query:"limit"`
}

// IOSentReply is the first incoming message on the thread after the send.
type IOSentReply struct {
	ID      string `json:"id"`
	At      string `json:"at,omitempty"`
	From    string `json:"from,omitempty"`
	Subject string `json:"subject,omitempty"`
}

// IOSentAsk is the document the send became, with its status when it has one
// (an Ask: open, answered, closed).
type IOSentAsk struct {
	ID     string `json:"id"`
	Type   string `json:"type,omitempty"`
	Status string `json:"status,omitempty"`
}

// IOSentRow is one message or action that went out. Variant is the place in
// its variation set (1 is the root) of Of drafts. TextChanged: the text was
// edited after the first draft (revision above 1). Reply is the first
// incoming message on the same thread after the send.
type IOSentRow struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	To          []string         `json:"to"`
	At          string           `json:"at"`
	Medium      model.Medium     `json:"medium"`
	URL         string           `json:"url,omitempty"`
	Group       string           `json:"group,omitempty"`
	VariantOf   string           `json:"variant_of,omitempty"`
	Variant     int              `json:"variant,omitempty"`
	Of          int              `json:"of,omitempty"`
	TextChanged bool             `json:"text_changed"`
	Reply       *IOSentReply     `json:"reply,omitempty"`
	Ask         *IOSentAsk       `json:"ask,omitempty"`
	LastAttempt *model.IOAttempt `json:"last_attempt,omitempty"`
	Reviewed    string           `json:"reviewed,omitempty"`
}

// IOSentOutput is the output of io_sent.
type IOSentOutput struct {
	Rows []IOSentRow `json:"rows"`
}

// ioEntry is an io document with its meta.
type ioEntry struct {
	doc model.Document
	m   *model.IO
}

// IOSent lists what went out, newest first.
var IOSent = defineRead("io_sent", "List outgoing io items that went out (state sent), newest first: to, when, medium, url, its place in a variation set, whether the text changed after the first draft, the result (the first incoming message on the same thread after it, and the state of the Ask it became), the last attempt, and when the owner reviewed it. since is a day or RFC 3339; medium and to filter; limit defaults to 50. Not sent_list, which is agent work", func(ctx context.Context, d Deps, in IOSentInput) (IOSentOutput, error) {
	out := IOSentOutput{Rows: []IOSentRow{}}
	var since time.Time
	if s := strings.TrimSpace(in.Since); s != "" {
		if since = turnTime(s); since.IsZero() {
			return out, fmt.Errorf("since %q: want a day (YYYY-MM-DD) or RFC 3339", in.Since)
		}
	}
	if in.Limit < 0 {
		return out, errors.New("limit: want 0 or more")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 50
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return out, err
	}
	var sent, ins []ioEntry
	sets := map[string][]ioEntry{} // variation set root -> outgoing members
	for _, doc := range sn.Docs() {
		m, ok := doc.Meta.(*model.IO)
		if !ok {
			continue
		}
		e := ioEntry{doc, m}
		switch {
		case m.Direction == model.IOIn:
			ins = append(ins, e)
		case m.State == model.IOSent:
			sent = append(sent, e)
		}
		if m.Direction == model.IOOut {
			root := ioVariantRoot(doc.ID, m)
			sets[root] = append(sets[root], e)
		}
	}
	slices.SortStableFunc(sent, func(a, b ioEntry) int {
		if c := turnTime(ioWhen(b.m)).Compare(turnTime(ioWhen(a.m))); c != 0 {
			return c
		}
		return strings.Compare(b.doc.ID, a.doc.ID)
	})
	for _, s := range sent {
		m, doc := s.m, s.doc
		when := ioWhen(m)
		if !since.IsZero() && turnTime(when).Before(since) {
			continue
		}
		if in.Medium != "" && m.Medium != in.Medium {
			continue
		}
		if in.To != "" && !slices.Contains(m.To, in.To) && !slices.Contains(m.To, "[["+normID(in.To)+"]]") {
			continue
		}
		root := ioVariantRoot(doc.ID, m)
		row := IOSentRow{ID: doc.ID, Title: doc.Title, To: append([]string{}, m.To...), At: when, Medium: m.Medium, Group: m.Group,
			TextChanged: currentRevision(m) > 1, LastAttempt: ioAttemptLast(m), Reviewed: m.Reviewed, URL: firstOf(m.Ref, m.URL)}
		for _, a := range m.Attempts {
			if a.Result == model.AttemptSent && a.URL != "" {
				row.URL = a.URL
			}
		}
		if set := sets[root]; len(set) > 1 {
			// The root is first, the variants follow in the order made.
			slices.SortStableFunc(set, func(a, b ioEntry) int {
				if (a.doc.ID == root) != (b.doc.ID == root) {
					if a.doc.ID == root {
						return -1
					}
					return 1
				}
				if c := strings.Compare(string(a.m.Created), string(b.m.Created)); c != 0 {
					return c
				}
				return strings.Compare(a.doc.ID, b.doc.ID)
			})
			row.VariantOf, row.Of = root, len(set)
			row.Variant = slices.IndexFunc(set, func(x ioEntry) bool { return x.doc.ID == doc.ID }) + 1
		}
		if b := m.Became.Target(); b != "" {
			a := IOSentAsk{ID: b}
			if bd, err := sn.Get(b); err == nil {
				a.Type = string(bd.Meta.Header().Type)
				if r, ok := bd.Meta.(*model.RFX); ok {
					a.Status = string(r.Status)
				}
			}
			row.Ask = &a
		}
		keys := []string{doc.ID, m.ThreadKey(), m.Became.Target()}
		if m.Via != nil {
			keys = append(keys, m.Via.ThreadKey())
		}
		// The item it answers may sit in a conversation the draft does not name.
		if th, err := sn.Get(m.ThreadKey()); err == nil {
			if tm, ok := th.Meta.(*model.IO); ok && tm.Via != nil {
				keys = append(keys, tm.Via.ThreadKey())
			}
		}
		var first *ioEntry
		for i, x := range ins {
			via := ""
			if x.m.Via != nil {
				via = x.m.Via.ThreadKey()
			}
			onThread := x.m.ThreadKey() != "" && slices.Contains(keys, x.m.ThreadKey()) || via != "" && slices.Contains(keys, via)
			if !onThread || !turnTime(ioWhen(x.m)).After(turnTime(when)) {
				continue
			}
			if first == nil || turnTime(ioWhen(x.m)).Before(turnTime(ioWhen(first.m))) {
				first = &ins[i]
			}
		}
		if first != nil {
			row.Reply = &IOSentReply{ID: first.doc.ID, At: ioWhen(first.m), From: first.m.From, Subject: first.m.Subject}
		}
		out.Rows = append(out.Rows, row)
		if len(out.Rows) >= limit {
			break
		}
	}
	return out, nil
})
