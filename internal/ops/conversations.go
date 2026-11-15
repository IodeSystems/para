package ops

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// --- conversations ---

// A conversation is an incoming io item (the owner's words, io_add) that is
// the thread of at least one owner turn to an agent (thread_post, a
// sent-to-agent request). The /pinned composer makes them.

// ConversationsInput limits the list. IncludeDone also lists conversations
// that are finished: the io is dealt with (filed or dropped), and every turn
// ended and the owner saw its result (request_seen).
type ConversationsInput struct {
	Limit       int  `json:"limit,omitempty" query:"limit"` // 0 means 20
	IncludeDone bool `json:"include_done,omitempty" query:"include_done"`
}

// ConversationRow is one conversation. LastAt is the newest activity of a
// turn; LastKind and LastStatus are of that turn. LastReply is the start of
// the newest agent reply. Queued and Claimed count the turns waiting and
// held. WaitingNoAgent: a turn is queued and no agent is connected on this
// machine (as in thread).
type ConversationRow struct {
	ID             string                `json:"id"`
	Title          string                `json:"title"`
	State          model.IOState         `json:"state"`
	Created        string                `json:"created"`
	LastAt         string                `json:"last_at"`
	LastKind       model.RequestTurnKind `json:"last_kind,omitempty"`
	LastStatus     model.RequestStatus   `json:"last_status"`
	LastReply      string                `json:"last_reply,omitempty"`
	Turns          int                   `json:"turns"`
	Queued         int                   `json:"queued"`
	Claimed        int                   `json:"claimed"`
	WaitingNoAgent bool                  `json:"waiting_no_agent"`
}

// ConversationsOutput is the conversations, newest activity first.
type ConversationsOutput struct {
	Items []ConversationRow `json:"items"`
}

const conversationsLimit = 20

// requestActive is when a request last changed.
func requestActive(r *model.Request) time.Time {
	t := turnTime(string(r.Created))
	for _, s := range []string{r.Queued, r.ActiveAt} {
		if u := turnTime(s); u.After(t) {
			t = u
		}
	}
	return t
}

// Conversations lists the owner's io conversations with agents.
var Conversations = defineRead("conversations", "List the conversations: incoming io items that are the thread of at least one owner turn to an agent (thread_post). "+
	"For each id, title, io state, created, the newest turn's time, kind, and status, the start of the newest agent reply, turns queued and held, and waiting_no_agent (a turn is queued and no agent is connected). "+
	"Newest activity first; limit (default 20). By default only open ones: the io is not dealt with, or a turn is not finished, or a finished turn's result is not seen; include_done lists all", func(ctx context.Context, d Deps, in ConversationsInput) (ConversationsOutput, error) {
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return ConversationsOutput{}, err
	}
	type conv struct {
		row     ConversationRow
		last    time.Time
		replyAt time.Time
		open    bool
	}
	ios := map[string]*conv{}
	docs := sn.Docs()
	for _, doc := range docs {
		if m, ok := doc.Meta.(*model.IO); ok && m.Direction == model.IOIn {
			ios[doc.ID] = &conv{row: ConversationRow{ID: doc.ID, Title: doc.Title, State: m.State, Created: string(m.Created)}, open: m.State.Open()}
		}
	}
	var hit []*conv
	for _, doc := range docs {
		r, ok := doc.Meta.(*model.Request)
		if !ok || r.Kind != SentKind {
			continue
		}
		// A turn is in the thread of its key, or of an item it links (as in thread).
		seen := map[string]bool{}
		keys := append([]string{r.Thread}, doc.Meta.Header().Threads...)
		for _, l := range r.Links {
			keys = append(keys, l.Target())
		}
		at := requestActive(r)
		for _, k := range keys {
			c := ios[k]
			if c == nil || seen[k] {
				continue
			}
			seen[k] = true
			if c.row.Turns == 0 {
				hit = append(hit, c)
			}
			c.row.Turns++
			switch r.Status {
			case model.RequestQueued:
				c.row.Queued++
			case model.RequestClaimed:
				c.row.Claimed++
			}
			switch r.Status {
			case model.RequestDone, model.RequestFailed:
				c.open = c.open || r.Seen == ""
			case model.RequestCancelled:
			default:
				c.open = true
			}
			if !at.Before(c.last) {
				c.last = at
				c.row.LastAt, c.row.LastKind, c.row.LastStatus = at.UTC().Format(time.RFC3339), r.TurnKind, r.Status
			}
			if r.Reply != "" && !at.Before(c.replyAt) {
				c.replyAt = at
				c.row.LastReply = clip(strings.Join(strings.Fields(r.Reply), " "), pinReplyLen)
			}
		}
	}
	agents, err := AgentList.Run(ctx, d, AgentListInput{})
	if err != nil {
		return ConversationsOutput{}, err
	}
	hit = slices.DeleteFunc(hit, func(c *conv) bool { return !in.IncludeDone && !c.open })
	slices.SortStableFunc(hit, func(a, b *conv) int {
		if c := b.last.Compare(a.last); c != 0 {
			return c
		}
		return strings.Compare(b.row.ID, a.row.ID)
	})
	limit := in.Limit
	if limit <= 0 {
		limit = conversationsLimit
	}
	out := ConversationsOutput{Items: []ConversationRow{}}
	for _, c := range hit[:min(limit, len(hit))] {
		c.row.WaitingNoAgent = c.row.Queued > 0 && len(agents.Agents) == 0
		out.Items = append(out.Items, c.row)
	}
	return out, nil
})
