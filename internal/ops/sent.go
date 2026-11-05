package ops

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Asked of agents is the list of agent work the owner asked for. Send to
// agent makes it from the review stack (an item or a vault question sent back
// with a note) and from the composer on an item's page (agent_send): kind
// SentKind; older ones are known by their title. An agent records what the
// owner asked it in the chat: kind OwnerAskKind.

// SentKind marks a request the owner made with Send to agent.
const SentKind = "sent-to-agent"

// isSent reports whether a request came from Send to agent.
func isSent(title string, r *model.Request) bool {
	return r.Kind == SentKind || strings.HasPrefix(title, askPrefix) || strings.HasPrefix(title, composerPrefix)
}

// isAsked reports whether the owner asked for the work: Send to agent, or an
// owner-ask an agent recorded from the chat.
func isAsked(title string, r *model.Request) bool {
	return r.Kind == OwnerAskKind || isSent(title, r)
}

// askedOpen reports whether work is not finished: proposed, queued, or
// claimed.
func askedOpen(r *model.Request) bool {
	return r.Status == model.RequestProposed || r.Status == model.RequestQueued || r.Status == model.RequestClaimed
}

// AskedCounts says whether a request is open work the owner asked for, and
// whether it is stale at now. For Today.
func AskedCounts(title string, r *model.Request, now time.Time) (open, stale bool) {
	if !isAsked(title, r) || !askedOpen(r) {
		return false, false
	}
	return true, requestStale(r, now)
}

// SentListInput is the input of sent_list.
type SentListInput struct {
	Limit int `json:"limit,omitempty" query:"limit"` // 0 means all
}

// SentItem is one piece of agent work the owner asked for.
type SentItem struct {
	Request string `json:"request"`
	Title   string `json:"title"`
	// Kind is sent-to-agent (Send to agent) or owner-ask (asked in the chat);
	// in the stack's needs-you list, any kind of agent work.
	Kind   string              `json:"kind"`
	Item   string              `json:"item,omitempty"` // the item it is about
	Note   string              `json:"note,omitempty"` // the first line of the owner's words
	Status model.RequestStatus `json:"status"`
	// ClaimedBy is the agent that has it, since ClaimedAt; Resolution and
	// Summary are how its last turn ended, in the agent's words.
	ClaimedBy  string                  `json:"claimed_by,omitempty"`
	ClaimedAt  string                  `json:"claimed_at,omitempty"`
	Resolution model.RequestResolution `json:"resolution,omitempty"`
	Summary    string                  `json:"summary,omitempty"`
	QueuedAt   string                  `json:"queued_at"`
	// ActiveAt is when the work last changed. Stale: queued or claimed with
	// no change in a day.
	ActiveAt string   `json:"active_at,omitempty"`
	Stale    bool     `json:"stale"`
	Results  []string `json:"results,omitempty"`
	// Receipts are the writes made under the work, oldest first.
	Receipts []string `json:"receipts,omitempty"`
	// NeedsYou: the turn ended stuck, blocked, or needs_clarification and
	// waits for the owner's response.
	NeedsYou bool `json:"needs_you"`
	// Undoable: no agent has claimed it, so agent_unsend takes it back.
	Undoable bool `json:"undoable"`
	// ItemName is the title of Item, for people.
	ItemName string `json:"item_name,omitempty" nameof:"Item"`
}

// SentListOutput is the output of sent_list, newest first. Open counts the
// work not finished; Stale the open work with no change in a day.
type SentListOutput struct {
	Items []SentItem `json:"items"`
	Open  int        `json:"open"`
	Stale int        `json:"stale"`
}

// SentList lists the agent work the owner asked for, newest first.
var SentList = defineRead("sent_list", "List the agent work the owner asked for, newest first: Send to agent (kind sent-to-agent) and what the owner asked an agent in the chat (kind owner-ask). Each has the item, the owner's words, the status, the agent that has it and since when, how it ended, the receipts made under it, and stale (queued or claimed with no change in a day)", func(ctx context.Context, d Deps, in SentListInput) (SentListOutput, error) {
	if in.Limit < 0 {
		return SentListOutput{}, errors.New("limit is negative")
	}
	docs, err := loadRequests(ctx, d.Store)
	if err != nil {
		return SentListOutput{}, err
	}
	receipts, err := requestReceipts(ctx, d)
	if err != nil {
		return SentListOutput{}, err
	}
	now := d.Store.Now()
	out := SentListOutput{Items: []SentItem{}}
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if !isAsked(doc.Title, r) {
			continue
		}
		it := askedItem(ctx, d, doc, r, now)
		it.Receipts = receipts[doc.ID]
		if askedOpen(r) {
			out.Open++
			if it.Stale {
				out.Stale++
			}
		}
		out.Items = append(out.Items, it)
	}
	// Newest first; of two queued at the same time, the later file first.
	slices.Reverse(out.Items)
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].QueuedAt > out.Items[j].QueuedAt })
	if in.Limit > 0 && len(out.Items) > in.Limit {
		out.Items = out.Items[:in.Limit]
	}
	return out, nil
})

// askedItem is the row of one request in the list of asked work.
func askedItem(ctx context.Context, d Deps, doc model.Document, r *model.Request, now time.Time) SentItem {
	kind := r.Kind
	if kind == "" && isSent(doc.Title, r) {
		kind = SentKind
	}
	it := SentItem{Request: doc.ID, Title: plainTitle(ctx, d.Store, doc.Title), Kind: kind, Status: r.Status, ClaimedBy: r.Claimed,
		ClaimedAt: r.ClaimedAt, Resolution: r.Resolution, QueuedAt: firstOf(r.Queued, string(r.Created)), ActiveAt: r.ActiveAt,
		Stale: requestStale(r, now), Results: targets(r.Results),
		Note:     firstLine(firstOf(bodySection(doc.Body, "Ask"), bodySection(doc.Body, "Owner's words"))),
		Summary:  firstLine(lastSection(doc.Body, "Result")),
		NeedsYou: r.Status == model.RequestDone && r.Resolution.NeedsOwner(),
		Undoable: r.Status == model.RequestQueued || r.Status == model.RequestProposed}
	switch {
	case len(r.Links) > 0:
		it.Item = r.Links[0].Target()
	case len(r.Context) > 0:
		it.Item = r.Context[0].Target()
	}
	return it
}

// requestReceipts maps a request id to the receipts of the writes made under
// it, from the recent events.
func requestReceipts(ctx context.Context, d Deps) (map[string][]string, error) {
	evs, err := d.Store.Events(ctx, activityWindow)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, e := range evs {
		if e.Op == "receipt" && e.From != "" && !slices.Contains(out[e.From], e.To) {
			out[e.From] = append(out[e.From], e.To)
		}
	}
	return out, nil
}

// bodySection is the text of the first "## name" section of a body.
func bodySection(body, name string) string {
	for _, sec := range strings.Split("\n"+body, "\n## ")[1:] {
		head, text, _ := strings.Cut(sec, "\n")
		if strings.TrimSpace(head) == name {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// lastSection is the text of the last section whose heading starts with name.
func lastSection(body, name string) string {
	out := ""
	for _, sec := range strings.Split("\n"+body, "\n## ")[1:] {
		head, text, _ := strings.Cut(sec, "\n")
		if strings.HasPrefix(strings.TrimSpace(head), name) {
			out = strings.TrimSpace(text)
		}
	}
	return out
}

// firstLine is the first line of text that is not empty, cut to 200 runes.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if rs := []rune(l); len(rs) > 200 {
				return string(rs[:200]) + "..."
			}
			return l
		}
	}
	return ""
}
