package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/server"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/mcpserver"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
)

// MCP over HTTP at /mcp: the same tools as `para mcp`, for a bearer key of
// the vault the Host names. The gate has run (client IP, Host, IP rule,
// token, scope vault:read, Origin); each tool then needs the scope its REST
// route needs. One MCP server per vault and key name, made on first use: the
// key name is the actor. A browser session is refused: MCP is for agents.
//
// A service restart on a new binary ends every session. The client gets 404
// for its old session id, initializes again at the same URL, and gets the
// new tools; mcpserver tells it para was upgraded (Options.HTTP).

const surfaceMCP = "mcp"

// mcpSessionIdle ends a session no request touched for this long, and drops
// the MCP server of a key no request used for this long.
const mcpSessionIdle = 24 * time.Hour

// mcpSessionsPerKey caps the live sessions of one key: a new session evicts
// the one used least recently, whose next request gets 404 and initializes
// again.
const mcpSessionsPerKey = 8

// mcpStreamMax ends a GET event stream after this long; the client opens
// another. No global WriteTimeout: it would cut every stream.
const mcpStreamMax = time.Hour

// mcpSweepEvery is how often servers of revoked or unused keys are dropped.
const mcpSweepEvery = 10 * time.Minute

type mcpHTTP struct {
	s  *Server
	mu sync.Mutex
	by map[string]*mcpEntry
	// ctx ends the watchers of every server and the sweep; close cancels it.
	ctx    context.Context
	cancel context.CancelFunc
	// sweeping starts the sweep on the first MCP server, not in New: a
	// goroutine from New would keep each Server alive until Close, and New
	// also runs where no MCP is served (para schema dump).
	sweeping sync.Once
}

type mcpEntry struct {
	t    *Tenant
	name string // the key name
	srv  *mcpserver.Server
	h    *mcpgo.StreamableHTTPServer
	ids  *mcpSessions
	stop func()
	used atomic.Int64 // unix nanoseconds of the last request
}

func newMCPHTTP(s *Server) *mcpHTTP {
	ctx, cancel := context.WithCancel(context.Background())
	return &mcpHTTP{s: s, by: map[string]*mcpEntry{}, ctx: ctx, cancel: cancel}
}

func (m *mcpHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := netguard.From(r.Context())
	t, tok := req.Tenant.(*Tenant)
	if !ok || !tok {
		netguard.WriteError(w, http.StatusForbidden, "no vault")
		return
	}
	if req.Principal.Session {
		netguard.WriteError(w, http.StatusForbidden, "MCP takes a bearer key, not a browser session: para key create --scope agent")
		return
	}
	e, err := m.entry(t, req.Principal)
	if err != nil {
		m.s.log.Error("mcp server", "vault", t.id, "error", err)
		netguard.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	e.serve(w, r)
}

// serve passes one request to the key's MCP server. GET and DELETE name a
// session this key holds, or get 404: mcp-go would open a stream for any id.
// A GET stream ends after mcpStreamMax or when its session ends.
func (e *mcpEntry) serve(w http.ResponseWriter, r *http.Request) {
	e.used.Store(time.Now().UnixNano())
	id := r.Header.Get(mcpgo.HeaderKeySessionID)
	switch r.Method {
	case http.MethodGet, http.MethodDelete:
		if !e.ids.known(id) {
			netguard.WriteError(w, http.StatusNotFound, "unknown MCP session: initialize again")
			return
		}
	}
	if r.Method == http.MethodGet {
		ctx, cancel := context.WithTimeout(r.Context(), mcpStreamMax)
		defer cancel()
		done := e.ids.stream(id, cancel)
		defer done()
		r = r.WithContext(ctx)
	}
	e.h.ServeHTTP(w, r)
	for _, old := range e.ids.takeEvicted() {
		e.purge(old)
	}
}

// purge drops mcp-go's state of an evicted session: a DELETE of it, which
// the session manager no longer refuses or counts.
func (e *mcpEntry) purge(id string) {
	r, err := http.NewRequest(http.MethodDelete, "/mcp", nil)
	if err != nil {
		return
	}
	r.Header.Set(mcpgo.HeaderKeySessionID, id)
	e.h.ServeHTTP(discardWriter{h: http.Header{}}, r)
}

// shutdown ends the entry's watcher, its sessions, and their streams.
func (e *mcpEntry) shutdown() {
	e.stop()
	e.ids.endAll()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = e.h.Shutdown(ctx)
}

func (m *mcpHTTP) entry(t *Tenant, p netguard.Principal) (*mcpEntry, error) {
	scopes := slices.Clone(p.Scopes)
	slices.Sort(scopes)
	key := t.id + "\x00" + p.Name + "\x00" + strings.Join(scopes, ",")
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.by[key]; ok {
		return e, nil
	}
	routes := m.s.routes
	allow := func(op string, read bool) error {
		scope, ok := routes.byOp[op]
		if !ok {
			scope = netguard.ScopeVaultWrite
			if read {
				scope = netguard.ScopeVaultRead
			}
		}
		if !p.Has(scope) {
			return fmt.Errorf("key lacks scope %s", scope)
		}
		return nil
	}
	deps := ops.Deps{Store: t.store.As(p.Name, surfaceMCP), Authorize: func(op string) error {
		scope, ok := routes.byOp[op]
		if !ok || !p.Has(scope) {
			return fmt.Errorf("%w: approving %s needs scope %s", ops.ErrPersonOnly, op, scope)
		}
		return nil
	}}
	srv, err := mcpserver.New(deps, mcpserver.Options{
		Version:     m.s.opt.Version,
		Hide:        t.hide,
		EventsDir:   filepath.Join(t.path, "events"),
		SeenPath:    filepath.Join(t.state, "mcp_seen_http."+fileToken(p.Name)+".json"),
		PresenceDir: t.state,
		HTTP:        true,
		Notice:      m.s.opt.UpdateNotice,
		Tools:       m.s.opt.MCPTools,
		Allow:       allow,
		Write:       func(string) func() { return m.s.live.act.write() },
	})
	if err != nil {
		return nil, err
	}
	ids := newMCPSessions(mcpSessionsPerKey, srv.ForgetSession)
	stop := srv.Start(m.ctx)
	h := mcpgo.NewStreamableHTTPServer(srv.MCP(),
		mcpgo.WithEndpointPath("/mcp"),
		// The gate checked Host against the vault names; *.localhost is not
		// "localhost" to mcp-go's own check.
		mcpgo.WithDisableLocalhostProtection(true),
		mcpgo.WithMaxRequestBodyBytes(maxBody),
		// Stateful: a session id this process did not issue (one from before a
		// restart) gets 404, and the client initializes again. The manager
		// caps the sessions of this key.
		mcpgo.WithSessionIdManager(ids),
		mcpgo.WithSessionIdleTTL(mcpSessionIdle),
	)
	e := &mcpEntry{t: t, name: p.Name, srv: srv, h: h, ids: ids, stop: stop}
	e.used.Store(time.Now().UnixNano())
	m.by[key] = e
	m.sweeping.Do(func() { go m.sweepEvery(mcpSweepEvery) })
	return e, nil
}

func (m *mcpHTTP) sweepEvery(d time.Duration) {
	tick := time.NewTicker(d)
	defer tick.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-tick.C:
			m.sweep(now)
		}
	}
}

// sweep drops the MCP server of each key that is revoked, expired, or gone,
// or that no request used for mcpSessionIdle, and stops its watcher.
func (m *mcpHTTP) sweep(now time.Time) {
	m.mu.Lock()
	var gone []*mcpEntry
	for k, e := range m.by {
		if now.Sub(time.Unix(0, e.used.Load())) >= mcpSessionIdle || !keyLive(e.t, e.name, now) {
			gone = append(gone, e)
			delete(m.by, k)
		}
	}
	m.mu.Unlock()
	for _, e := range gone {
		e.shutdown()
	}
}

// keyLive reports whether key name of t still works at now. The admin token
// has no key file and lives while the vault does.
func keyLive(t *Tenant, name string, now time.Time) bool {
	if name == keys.AdminName {
		return true
	}
	list, err := t.keys.List()
	if err != nil {
		return true // an unreadable directory: keep it; the gate refuses on its own
	}
	for _, k := range list {
		if k.Name == name {
			return k.Revoked == nil && (k.Expires == nil || k.Expires.After(now))
		}
	}
	return false
}

// announce sends text to every live MCP session.
func (m *mcpHTTP) announce(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.by {
		e.srv.Announce(text)
	}
}

func (m *mcpHTTP) close() {
	m.cancel()
	m.mu.Lock()
	gone := make([]*mcpEntry, 0, len(m.by))
	for k, e := range m.by {
		gone = append(gone, e)
		delete(m.by, k)
	}
	m.mu.Unlock()
	for _, e := range gone {
		e.shutdown()
	}
}

// mcpSessions is the session id manager of one key's MCP server: it issues
// ids, knows the live ones, caps them, and tells end when one ends.
type mcpSessions struct {
	mu      sync.Mutex
	max     int
	live    map[string]time.Time // id -> last use
	streams map[string][]*context.CancelFunc
	evicted []string
	end     func(id string)
}

func newMCPSessions(max int, end func(string)) *mcpSessions {
	return &mcpSessions{max: max, live: map[string]time.Time{}, streams: map[string][]*context.CancelFunc{}, end: end}
}

const mcpSessionPrefix = "mcp-session-"

// Generate issues a new id. At the cap the session used least recently ends.
func (s *mcpSessions) Generate() string {
	id := mcpSessionPrefix + rand.Text()
	s.mu.Lock()
	var old string
	if len(s.live) >= s.max {
		var at time.Time
		for k, t := range s.live {
			if old == "" || t.Before(at) {
				old, at = k, t
			}
		}
		s.evicted = append(s.evicted, old)
	}
	s.live[id] = time.Now()
	s.mu.Unlock()
	if old != "" {
		s.ended(old)
	}
	return id
}

// Validate refuses an id this manager does not hold (mcp-go answers 404).
func (s *mcpSessions) Validate(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live[id]; !ok {
		return false, errors.New("unknown session")
	}
	s.live[id] = time.Now()
	return false, nil
}

// Terminate ends a session: DELETE, or mcp-go's idle sweep.
func (s *mcpSessions) Terminate(id string) (bool, error) {
	s.ended(id)
	return false, nil
}

// ended forgets id, cancels its streams, and tells end once.
func (s *mcpSessions) ended(id string) {
	s.mu.Lock()
	_, was := s.live[id]
	delete(s.live, id)
	streams := s.streams[id]
	delete(s.streams, id)
	s.mu.Unlock()
	for _, c := range streams {
		(*c)()
	}
	if was && s.end != nil {
		s.end(id)
	}
}

func (s *mcpSessions) known(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.live[id]
	return ok
}

// stream records a GET stream of id; the session's end cancels it. The func
// returned removes it.
func (s *mcpSessions) stream(id string, cancel context.CancelFunc) func() {
	c := &cancel
	s.mu.Lock()
	s.streams[id] = append(s.streams[id], c)
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.streams[id] = slices.DeleteFunc(s.streams[id], func(x *context.CancelFunc) bool { return x == c })
		if len(s.streams[id]) == 0 {
			delete(s.streams, id)
		}
	}
}

func (s *mcpSessions) takeEvicted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.evicted
	s.evicted = nil
	return out
}

// endAll ends every session, as when the key's server is dropped.
func (s *mcpSessions) endAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.live))
	for id := range s.live {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.ended(id)
	}
}

// discardWriter is the response of an internal request nobody reads.
type discardWriter struct{ h http.Header }

func (d discardWriter) Header() http.Header         { return d.h }
func (d discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d discardWriter) WriteHeader(int)             {}

var unsafeToken = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// fileToken makes a key name safe as part of a file name.
func fileToken(s string) string {
	s = unsafeToken.ReplaceAllString(s, "_")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// AnnounceUpdate sends the update notice to every live MCP session.
func (s *Server) AnnounceUpdate(text string) { s.mcp.announce(text) }

// Close stops the MCP servers' watchers.
func (s *Server) Close() { s.mcp.close() }
