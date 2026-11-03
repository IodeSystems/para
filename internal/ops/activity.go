package ops

import (
	"context"
	"errors"
	"time"
)

const activityWindow = 5000

// ActivityRow is one thing an agent did: when, under which request, with its
// receipt. Why is the agent's word.
type ActivityRow struct {
	TS           time.Time `json:"ts"`
	Actor        string    `json:"actor"`
	Op           string    `json:"op"`
	Doc          string    `json:"doc"`
	DocName      string    `json:"doc_name,omitempty" nameof:"Doc"`
	Request      string    `json:"request,omitempty"`
	Receipt      string    `json:"receipt"`
	Why          string    `json:"why,omitempty"`
	SelfReported bool      `json:"self_reported,omitempty"`
}

// AgentActivityInput is the input of agent_activity.
type AgentActivityInput struct {
	Actor string `json:"actor,omitempty" query:"actor"`
	Since string `json:"since,omitempty" query:"since"` // RFC 3339, or YYYY-MM-DD
	Limit int    `json:"limit,omitempty" query:"limit"` // 0 means 200
}

// AgentActivityOutput is the output of agent_activity, newest first.
type AgentActivityOutput struct {
	Items []ActivityRow `json:"items"`
}

// AgentActivity lists what each agent did, from events and receipts.
var AgentActivity = defineRead("agent_activity", "Show what each agent did: when, under which request, with its receipt", func(ctx context.Context, d Deps, in AgentActivityInput) (AgentActivityOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	var since time.Time
	if in.Since != "" {
		t, err := time.Parse(time.RFC3339, in.Since)
		if err != nil {
			if t, err = time.Parse(dateLayout, in.Since); err != nil {
				return AgentActivityOutput{}, errors.New("since: want an RFC 3339 time or YYYY-MM-DD")
			}
		}
		since = t
	}
	rows, err := activityRows(ctx, d, in.Actor, since, time.Time{})
	if err != nil {
		return AgentActivityOutput{}, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return AgentActivityOutput{Items: rows}, nil
})

func activityRows(ctx context.Context, d Deps, actor string, since, until time.Time) ([]ActivityRow, error) {
	evs, err := d.Store.Events(ctx, activityWindow)
	if err != nil {
		return nil, err
	}
	out := []ActivityRow{}
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Op != "receipt" || (actor != "" && e.Actor != actor) || e.TS.Before(since) || (!until.IsZero() && !e.TS.Before(until)) {
			continue
		}
		row := ActivityRow{TS: e.TS, Actor: e.Actor, Doc: e.Doc, Request: e.From, Receipt: e.To}
		if _, r, err := getReceipt(ctx, d.Store, e.To); err == nil {
			row.Op, row.Why, row.SelfReported = r.Op, r.Why, r.SelfReported
		}
		out = append(out, row)
	}
	return out, nil
}

const (
	// DefaultWait is how long a waiting op waits when asked for no time.
	DefaultWait = 15 * time.Second
	// MaxWait is the longest wait a caller can ask of an op. The CLI command
	// waits as long as it is told.
	MaxWait = 60 * time.Second
)
