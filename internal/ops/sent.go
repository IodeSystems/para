package ops

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Sent to agent is the list of agent work the owner made with Send to agent:
// from the review stack (an item or a vault question sent back with a note)
// and from the composer on an item's page (agent_send). Requests made on
// those paths carry kind SentKind; older ones are known by their title.

// SentKind marks a request the owner made with Send to agent.
const SentKind = "sent-to-agent"

// isSent reports whether a request came from Send to agent.
func isSent(title string, r *model.Request) bool {
	return r.Kind == SentKind || strings.HasPrefix(title, askPrefix) || strings.HasPrefix(title, composerPrefix)
}

// SentListInput is the input of sent_list.
type SentListInput struct {
	Limit int `json:"limit,omitempty" query:"limit"` // 0 means all
}

// SentItem is one piece of agent work the owner sent.
type SentItem struct {
	Request string              `json:"request"`
	Title   string              `json:"title"`
	Item    string              `json:"item,omitempty"` // the item it is about
	Note    string              `json:"note,omitempty"` // the first line of the owner's words
	Status  model.RequestStatus `json:"status"`
	// ClaimedBy is the agent that has it; Resolution and Summary are how its
	// last turn ended, in the agent's words.
	ClaimedBy  string                  `json:"claimed_by,omitempty"`
	Resolution model.RequestResolution `json:"resolution,omitempty"`
	Summary    string                  `json:"summary,omitempty"`
	QueuedAt   string                  `json:"queued_at"`
	Results    []string                `json:"results,omitempty"`
	// Undoable: no agent has claimed it, so agent_unsend takes it back.
	Undoable bool `json:"undoable"`
	// ItemName is the title of Item, for people.
	ItemName string `json:"item_name,omitempty" nameof:"Item"`
}

// SentListOutput is the output of sent_list, newest first.
type SentListOutput struct {
	Items []SentItem `json:"items"`
}

// SentList lists the agent work the owner sent, newest first.
var SentList = defineRead("sent_list", "List the agent work the owner sent with Send to agent (from the review stack or an item's composer), newest first: the item, the owner's note, the status, the agent that has it, and how it ended", func(ctx context.Context, d Deps, in SentListInput) (SentListOutput, error) {
	if in.Limit < 0 {
		return SentListOutput{}, errors.New("limit is negative")
	}
	docs, err := loadRequests(ctx, d.Store)
	if err != nil {
		return SentListOutput{}, err
	}
	out := SentListOutput{Items: []SentItem{}}
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if !isSent(doc.Title, r) {
			continue
		}
		it := SentItem{Request: doc.ID, Title: plainTitle(ctx, d.Store, doc.Title), Status: r.Status, ClaimedBy: r.Claimed,
			Resolution: r.Resolution, QueuedAt: firstOf(r.Queued, string(r.Created)), Results: targets(r.Results),
			Note:     firstLine(firstOf(bodySection(doc.Body, "Ask"), bodySection(doc.Body, "Owner's words"))),
			Summary:  firstLine(lastSection(doc.Body, "Result")),
			Undoable: r.Status == model.RequestQueued || r.Status == model.RequestProposed}
		switch {
		case len(r.Links) > 0:
			it.Item = r.Links[0].Target()
		case len(r.Context) > 0:
			it.Item = r.Context[0].Target()
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
