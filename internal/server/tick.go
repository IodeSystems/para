package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/iodesystems/para/internal/ops"
)

// DefaultTickInterval is how often para serve runs tick when tick.interval
// in server.properties names none.
const DefaultTickInterval = time.Minute

// MinTickInterval is the shortest tick.interval. A shorter one would run a
// full pass of every vault more than six times a minute.
const MinTickInterval = 10 * time.Second

// TickAll runs one tick on every registered vault and returns the results. A
// vault that fails is in the result with its Error and does not stop the rest.
func (h *Hub) TickAll(ctx context.Context) []ops.TickOutput {
	vaults, err := h.install.List()
	if err != nil {
		h.log.Error("tick: list vaults", "error", err)
		return nil
	}
	var out []ops.TickOutput
	for _, v := range vaults {
		if ctx.Err() != nil {
			return out
		}
		t, err := h.tenant(v.ID)
		if err != nil || t == nil {
			msg := "vault not open"
			if err != nil {
				msg = err.Error()
			}
			out = append(out, ops.TickOutput{Vault: v.ID, Error: msg})
			continue
		}
		res, err := ops.Tick(ctx, ops.Deps{Store: t.store}, ops.TickInput{})
		if err != nil {
			res.Vault, res.Error = v.ID, err.Error()
		}
		out = append(out, res)
	}
	return out
}

// Ticker runs TickAll on a timer and logs what changed. It says once per
// vault that the vault is skipped or names no runner, not every pass.
type Ticker struct {
	hub      *Hub
	every    time.Duration
	log      *slog.Logger
	mu       sync.Mutex
	reported map[string]string // vault -> last skip or error text logged
	// After, when set, runs after every pass with its results. The dispatcher
	// uses it to look at the request queue.
	After func(context.Context, []ops.TickOutput)
}

// NewTicker returns a Ticker. every must be at least MinTickInterval.
func NewTicker(h *Hub, every time.Duration) *Ticker {
	return &Ticker{hub: h, every: every, log: h.log, reported: map[string]string{}}
}

// Run ticks once now and then every interval until ctx ends.
func (t *Ticker) Run(ctx context.Context) {
	t.once(ctx)
	tm := time.NewTicker(t.every)
	defer tm.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tm.C:
			t.once(ctx)
		}
	}
}

func (t *Ticker) once(ctx context.Context) {
	res := t.hub.TickAll(ctx)
	for _, r := range res {
		t.report(r)
	}
	if t.After != nil {
		t.After(ctx, res)
	}
}

func (t *Ticker) report(r ops.TickOutput) {
	note := ""
	switch {
	case r.Error != "":
		note = "error: " + r.Error
	case !r.Ran:
		note = r.Skipped
	}
	t.mu.Lock()
	same := t.reported[r.Vault] == note
	t.reported[r.Vault] = note
	t.mu.Unlock()
	switch {
	case r.Error != "":
		if !same {
			t.log.Warn("tick failed", "vault", r.Vault, "error", r.Error)
		}
	case !r.Ran:
		if !same {
			t.log.Info("tick skipped", "vault", r.Vault, "reason", r.Skipped)
		}
	case len(r.Revived)+len(r.Requests)+len(r.Journals)+len(r.MeetingsSkipped) > 0:
		t.log.Info("tick", "vault", r.Vault, "revived", len(r.Revived), "journals", len(r.Journals), "requests", len(r.Requests), "proposed", r.Proposed, "nudges", len(r.Nudges), "meetings_skipped", len(r.MeetingsSkipped))
	}
}
