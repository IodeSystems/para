package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/iodesystems/para/internal/dispatch"
	"github.com/iodesystems/para/internal/egress"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// activity is what this process saw of writes, for the auto-install's quiet
// moment: requests that may write (anything but GET and HEAD) in flight and
// the last one, and the last vault event any process appended (Live sees
// every one, CLI and stdio MCP writes included). A request to /mcp is not
// counted here: the MCP server counts each tool call that may write
// (write), and never a read or a wait, so a polling agent does not hold
// the install off.
type activity struct {
	mu        sync.Mutex
	inFlight  int
	lastWrite time.Time
	lastEvent time.Time
}

func (a *activity) begin(r *http.Request) func() {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.URL.Path == "/mcp" {
		return func() {}
	}
	return a.write()
}

// write marks one write in flight; the func returned ends it.
func (a *activity) write() func() {
	a.mu.Lock()
	a.inFlight++
	a.lastWrite = time.Now()
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		a.inFlight--
		a.lastWrite = time.Now()
		a.mu.Unlock()
	}
}

func (a *activity) event() {
	a.mu.Lock()
	a.lastEvent = time.Now()
	a.mu.Unlock()
}

// busy says why now is not quiet: a write in flight, or a write or event
// less than window ago.
func (a *activity) busy(now time.Time, window time.Duration) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.inFlight > 0:
		return fmt.Sprintf("%d requests in flight", a.inFlight)
	case !a.lastWrite.IsZero() && now.Sub(a.lastWrite) < window:
		return "a request " + now.Sub(a.lastWrite).Round(time.Second).String() + " ago"
	case !a.lastEvent.IsZero() && now.Sub(a.lastEvent) < window:
		return "a vault event " + now.Sub(a.lastEvent).Round(time.Second).String() + " ago"
	}
	return ""
}

// Busy reports why the server is not quiet at now, or "" when no request
// that may write is in flight and none, and no vault event, came in the
// last window.
func (s *Server) Busy(now time.Time, window time.Duration) string {
	return s.live.act.busy(now, window)
}

// AgentsBusy reports why agent work runs now, or "": a dispatched agent
// whose process lives, or a claimed request whose lease has not run out, in
// any vault this process serves.
func (h *Hub) AgentsBusy(ctx context.Context, now time.Time) string {
	vaults, err := h.install.List()
	if err != nil {
		return "cannot list vaults: " + err.Error()
	}
	for _, v := range vaults {
		t, err := h.tenant(v.ID)
		if err != nil || t == nil {
			continue
		}
		if st := dispatch.ReadStatus(t.state, now); len(st.Running) > 0 {
			return fmt.Sprintf("vault %s: %d dispatched agents run", v.ID, len(st.Running))
		}
		if id := claimedLease(ctx, t.store, now); id != "" {
			return "vault " + v.ID + ": request " + id + " is claimed"
		}
	}
	return ""
}

// claimedLease returns a request that is claimed with a lease after now.
func claimedLease(ctx context.Context, s store.Store, now time.Time) string {
	refs, err := s.List(ctx, store.Filter{Folder: model.FolderRequests, Type: model.TypeRequest})
	if err != nil {
		return ""
	}
	for _, ref := range refs {
		doc, err := s.Get(ctx, ref.ID)
		if err != nil {
			continue
		}
		r, ok := doc.Meta.(*model.Request)
		if !ok || r.Status != model.RequestClaimed {
			continue
		}
		if until, err := time.Parse(time.RFC3339, r.Lease); err == nil && until.After(now) {
			return doc.ID
		}
	}
	return ""
}

// LogEgress logs one egress record for dest in every vault this process
// serves (ids only: no document, the default in force, the destination). It
// is the update check's record: a path out of the machine, not of a vault,
// so each vault the machine holds sees it. A vault whose decision does not
// allow dest stops it with an error: nothing goes out.
func (h *Hub) LogEgress(ctx context.Context, dest string) error {
	vaults, err := h.install.List()
	if err != nil {
		return fmt.Errorf("server: egress: %w", err)
	}
	for _, v := range vaults {
		t, err := h.tenant(v.ID)
		if err != nil || t == nil {
			continue
		}
		d, err := t.store.Egress(ctx, "", dest)
		if err != nil {
			return fmt.Errorf("server: egress: vault %s: %w", v.ID, err)
		}
		if !d.Allowed {
			return fmt.Errorf("server: egress: vault %s does not allow %s", v.ID, dest)
		}
	}
	return nil
}

// UpdateDenied names a vault this process serves whose egress.update is
// deny, or "". A vault that cannot be read counts as a deny.
func (h *Hub) UpdateDenied(context.Context) (string, error) {
	vaults, err := h.install.List()
	if err != nil {
		return "", fmt.Errorf("server: egress: %w", err)
	}
	for _, v := range vaults {
		t, err := h.tenant(v.ID)
		if err != nil {
			return v.ID, fmt.Errorf("server: egress: vault %s: %w", v.ID, err)
		}
		if t == nil {
			continue
		}
		val, err := t.store.Config(egress.KeyUpdate)
		if err != nil {
			return v.ID, fmt.Errorf("server: egress: vault %s: %w", v.ID, err)
		}
		if !egress.Check(map[string]string{egress.KeyUpdate: val}, egress.DestUpdate+"manifest").Allowed {
			return v.ID, nil
		}
	}
	return "", nil
}
