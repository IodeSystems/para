package mcpserver

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// notifyLogger is the logger name of notifications/message. dun lifts those
// into its conversation queue as "[<server>] <data>".
const notifyLogger = "para"

// fallbackCheck is how often the session re-checks with no event: a lease
// that ran out makes a request claimable without any file change.
const fallbackCheck = 30 * time.Second

// Kinds of session notification, as the channel meta "kind".
const (
	kindRequestClaimable = "request_claimable"
	kindRequestCancelled = "request_cancelled"
)

// notifier tells the client about the two things that concern this session:
// a request became claimable by this install, and a request it claimed was
// cancelled. Nothing else is sent; there is no
// vault-wide stream.
//
// Every message holds ids, kinds, and states only. Vault text can be hostile,
// so a title, a body, or an evidence quote never goes into one. A request that
// becomes claimable is announced by count, with no id, because its file name
// is not text this session produced; the agent reads it through request_next.
// The messages are plain strings, not JSON: dun files a JSON notice in a
// server log, not in the conversation.
type notifier struct {
	mu     sync.Mutex
	claims map[string]bool // claimed by this session, not yet closed
	seen   map[string]bool // ids claimable at the last check
	primed bool
}

func newNotifier() *notifier {
	return &notifier{claims: map[string]bool{}, seen: map[string]bool{}}
}

// note is one notification: a plain sentence, and the structured fields a
// channel carries as attributes.
type note struct {
	text string
	meta map[string]string
}

// track records ids from a tool result that this session now answers for.
func (s *Server) track(out any) (added bool) {
	n := s.notes
	n.mu.Lock()
	switch v := out.(type) {
	case ops.RequestNextOutput:
		if v.Request != nil {
			n.claims[v.Request.ID], added = true, true
		}
	}
	n.mu.Unlock()
	return added
}

// check looks at the state behind the two cases and sends what is new.
func (s *Server) check(ctx context.Context) {
	n := s.notes
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []note
	for _, id := range slices.Sorted(mapKeys(n.claims)) {
		doc, err := s.deps.Store.Get(ctx, id)
		if err != nil {
			continue
		}
		r, ok := doc.Meta.(*model.Request)
		if !ok || r.Status == model.RequestClaimed {
			continue
		}
		delete(n.claims, id)
		if r.Status == model.RequestCancelled {
			out = append(out, note{
				text: fmt.Sprintf("para: request %s you claimed was cancelled. Stop work on it; call show for its state.", id),
				meta: map[string]string{"kind": kindRequestCancelled, "id": id, "status": string(r.Status)},
			})
		}
	}
	s.reqMu.Lock()
	ids, err := s.reqWatch.ClaimableIDs(ctx)
	s.reqMu.Unlock()
	if err == nil {
		fresh := 0
		now := make(map[string]bool, len(ids))
		for _, id := range ids {
			now[id] = true
			if !n.seen[id] {
				fresh++
			}
		}
		n.seen = now
		if n.primed && fresh > 0 {
			out = append(out, note{
				text: fmt.Sprintf("para: %d request(s) can be claimed by this install. Call work {action: next}.", fresh),
				meta: map[string]string{"kind": kindRequestClaimable, "count": fmt.Sprint(fresh)},
			})
		}
		n.primed = true
	}
	for _, o := range out {
		s.send(o)
	}
}

func mapKeys(m map[string]bool) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// send writes one notification to the client: notifications/message always,
// and Claude Code's channel event when --channel is on.
func (s *Server) send(o note) {
	s.mcp.SendNotificationToAllClients("notifications/message", map[string]any{
		"level": "info", "logger": notifyLogger, "data": o.text,
	})
	if s.channel {
		s.mcp.SendNotificationToAllClients("notifications/claude/channel", map[string]any{
			"content": o.text, "meta": o.meta,
		})
	}
}
