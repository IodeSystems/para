package ops

import (
	"context"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/presence"
	"github.com/iodesystems/para/internal/proc"
)

// Which agents are connected is a fact about this machine: each `para mcp`
// process writes a presence file in the vault's state directory
// (internal/presence) and removes it when it exits. agent_list reads them, so
// it shows only agents on this machine.

// AgentListInput is the input of agent_list.
type AgentListInput struct{}

// AgentClaim is a request an agent holds now.
type AgentClaim struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind,omitempty"`
	ClaimedAt string `json:"claimed_at,omitempty"`
}

// AgentRow is one MCP session connected to this vault on this machine.
type AgentRow struct {
	Actor      string `json:"actor"`
	Client     string `json:"client,omitempty"`
	Version    string `json:"version,omitempty"`
	PID        int    `json:"pid"`
	StartedAt  string `json:"started_at"`
	LastCallAt string `json:"last_call_at,omitempty"`
	// Push is how para can reach the session: message (notifications/message,
	// always) and channel (Claude Code channel events, para mcp --channel).
	Push []string `json:"push"`
	// Claims are the requests the session's actor holds now. Two sessions of
	// one actor show the same claims.
	Claims []AgentClaim `json:"claims"`
}

// AgentListOutput is the output of agent_list.
type AgentListOutput struct {
	Agents []AgentRow `json:"agents"`
	// QueuedCount is the requests this install can claim now.
	QueuedCount int `json:"queued_count"`
	// NoListener: work is queued and no agent is connected on this machine.
	NoListener bool `json:"no_listener"`
}

// AgentList lists the agents connected to the vault on this machine.
var AgentList = defineRead("agent_list", "List the agents connected to this vault over MCP on this machine: actor, client and version, when it started and last called a tool, how para can push to it (message; channel with --channel), and the requests it holds. queued_count is the work this install can claim; no_listener is true when work is queued and no agent is connected", func(ctx context.Context, d Deps, _ AgentListInput) (AgentListOutput, error) {
	info := d.Store.Info()
	out := AgentListOutput{Agents: []AgentRow{}}
	recs, err := presence.Read(info.StateDir)
	if err != nil {
		return out, err
	}
	docs, err := loadRequests(ctx, d.Store)
	if err != nil {
		return out, err
	}
	now := d.Store.Now()
	held := map[string][]AgentClaim{}
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if claimable(r, info.Install, now) {
			out.QueuedCount++
		}
		if r.Status != model.RequestClaimed || r.Claimed == "" {
			continue
		}
		held[r.Claimed] = append(held[r.Claimed], AgentClaim{
			ID: doc.ID, Title: plainTitle(ctx, d.Store, doc.Title), Kind: requestKind(doc.Title, r), ClaimedAt: r.ClaimedAt,
		})
	}
	for _, rec := range recs {
		// A process that ended without removing its file (killed, crashed) is gone.
		if !proc.Alive(rec.PID) {
			if err := presence.Remove(info.StateDir, rec.PID); err != nil {
				return out, err
			}
			continue
		}
		claims := held[rec.Actor]
		if claims == nil {
			claims = []AgentClaim{}
		}
		push := rec.Push
		if push == nil {
			push = []string{}
		}
		out.Agents = append(out.Agents, AgentRow{
			Actor: rec.Actor, Client: rec.Client, Version: rec.ClientVersion, PID: rec.PID,
			StartedAt: rec.StartedAt, LastCallAt: rec.LastCallAt, Push: push, Claims: claims,
		})
	}
	out.NoListener = out.QueuedCount > 0 && len(out.Agents) == 0
	return out, nil
})
