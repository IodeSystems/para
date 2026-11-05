package ops

import (
	"fmt"

	"github.com/iodesystems/para/internal/model"
)

// agentWorkStale names work the owner asked of agents (owner-ask,
// sent-to-agent) that is queued or claimed with no change in
// AgentWorkStaleAfter.
func (h *healther) agentWorkStale() {
	for id, d := range h.docs {
		r, ok := d.Meta.(*model.Request)
		if !ok || !isAsked(d.Title, r) || !requestStale(r, h.now) {
			continue
		}
		if r.Status == model.RequestClaimed {
			h.add(HealthAgentWorkStale, id, "", fmt.Sprintf("claimed by %s; no note or end since %s", firstOf(r.Claimed, "an agent"), firstOf(dayOf(firstOf(r.ActiveAt, r.ClaimedAt, r.Queued)), "it was queued")),
				fmt.Sprintf("ask %s to end it with request_done or to report progress with request_note; or cancel it with para request cancel %s", firstOf(r.Claimed, "the agent"), id))
			continue
		}
		h.add(HealthAgentWorkStale, id, "", fmt.Sprintf("queued %s; no agent claimed it", firstOf(dayOf(r.Queued), r.Queued)),
			"run /para loop in an agent that can do it (request_next claims it), or cancel it with para request cancel "+id)
	}
}
