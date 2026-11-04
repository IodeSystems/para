// Package server is para's HTTP surface: REST under /api/v1 and GraphQL at
// /api/graphql, both from one huma registration per op. Every request first
// passes internal/netguard. A vault is chosen per request by its host name, the
// Store is bound to the caller per request, and no transport runs here: export
// renders text and logs the egress, and the caller moves the content.
package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/iodesystems/gwag/v2/gw/gat"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
)

// maxBody caps a request body. Ingest and policy text can exceed huma's 1 MiB.
const maxBody = 16 << 20

// surfaces record how a caller came in. A cookie is a person in the PWA, and
// the PWA is a surface that can queue requests. A bearer key is an API client.
const (
	surfaceAPI = "api"
	surfacePWA = "pwa"
)

// Options configures a Server.
type Options struct {
	Version string
	Web     http.Handler // the UI; nil answers 404
	Log     *slog.Logger // nil discards
	Live    *Live        // nil makes a private one
}

// Server holds the built API. Build it with New.
type Server struct {
	opt    Options
	log    *slog.Logger
	routes *routeTable
	gw     *gat.Gateway
	api    huma.API
	mux    chi.Router
	live   *Live
}

// New registers every route. It touches no vault: `para schema dump` runs it
// with no install. Vaults come in through the gate, per request.
func New(o Options) (*Server, error) {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	s := &Server{opt: o, log: o.Log, routes: newRouteTable(), mux: chi.NewRouter(), live: o.Live}
	if s.live == nil {
		l, err := NewLive()
		if err != nil {
			return nil, err
		}
		s.live = l
	}
	if err := s.build(); err != nil {
		return nil, fmt.Errorf("server: new: %w", err)
	}
	return s, nil
}

// Live returns the relay, for shutdown.
func (s *Server) Live() *Live { return s.live }

// SDL returns the GraphQL schema text.
func (s *Server) SDL() string { return s.gw.GraphQLSDL() }

// Handler returns the routes behind the gate.
func (s *Server) Handler(g *netguard.Gate) http.Handler {
	return s.recoverer(g.Wrap(s.access(s.shut(s.mux))))
}

// shut answers 404 for gat's pubsub routes. gat always mounts them, on the mux
// with no scope, and para publishes nothing.
func (s *Server) shut(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/_gat/") {
			notFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("handler panic", "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				netguard.WriteError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }

// Hijack lets the WebSocket upgrade take the connection.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// access writes one log line per request that passed the gate, and marks API
// answers as never cached. The query string is not logged: it can hold content.
func (s *Server) access(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if isAPIPath(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-store")
		}
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		if why := r.Header.Get("X-Para-Why"); why != "" || r.Header.Get("X-Para-Not-Duplicate") != "" {
			r = r.WithContext(ops.WithWrite(r.Context(), ops.WriteMeta{Why: why, Verify: r.Header.Get("X-Para-Verify"),
				Confidence: model.Confidence(r.Header.Get("X-Para-Confidence")), NotDuplicate: r.Header.Get("X-Para-Not-Duplicate")}))
		}
		next.ServeHTTP(sw, r)
		req, _ := netguard.From(r.Context())
		s.log.Info("request", "method", r.Method, "path", req.Path, "status", sw.status,
			"latency_ms", time.Since(start).Milliseconds(), "vault", req.Vault, "actor", req.Principal.Name)
	})
}

func isAPIPath(p string) bool {
	for _, pre := range serverPrefixes {
		if p == pre || strings.HasPrefix(p, pre+"/") {
			return true
		}
	}
	return false
}

// serverPrefixes are the paths the server owns. Anything else is the UI.
var serverPrefixes = []string{"/api", "/auth", "/mcp", "/healthz", "/ws"}

// ServerGETRoutes lists the paths a browser could navigate to that the server
// answers and the UI must not. The PWA service worker's navigateFallback
// denylist must cover each one.
func ServerGETRoutes() []string {
	return []string{"/api/", "/auth/", "/mcp", "/healthz", "/ws"}
}

// build follows the gat order: humachi.New, gat.New, the two options, every
// gat.Register, raw chi routes, then gat.RegisterHuma last.
func (s *Server) build() error {
	cfg := huma.DefaultConfig("para", s.opt.Version)
	// No docs, spec, or schema routes: every route is in the route table, and
	// $schema links would make REST differ from `--json`.
	cfg.DocsPath, cfg.OpenAPIPath, cfg.SchemasPath = "", "", ""
	cfg.CreateHooks = nil
	cfg.Formats = map[string]huma.Format{
		"application/json": jsonFormat,
		"json":             jsonFormat,
	}
	// internal/today names Item and Group, which internal/export names too.
	cfg.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", func(t reflect.Type, hint string) string {
		name := huma.DefaultSchemaNamer(t, hint)
		if strings.HasSuffix(t.PkgPath(), "/internal/today") {
			return "Today" + name
		}
		return name
	})
	registerAliases(cfg.Components.Schemas)
	s.api = humachi.New(s.mux, cfg)
	s.api.UseMiddleware(s.scopeMiddleware)

	g, err := gat.New()
	if err != nil {
		return err
	}
	g.LongAsNumber(false)
	g.VersionedOnly(true)
	g.MaxRequestBytes(maxBody)
	s.gw = g

	r := &registrar{api: s.api, g: g, s: s}
	registerWork(r)
	registerLife(r)
	registerRFX(r)
	registerOrg(r)
	registerGovernance(r)
	registerRecords(r)
	registerRequests(r)
	registerProposals(r)
	registerReceipts(r)
	registerVault(r)
	registerUI(r)
	registerSession(r)
	registerAdmin(r)

	s.rawRoutes()
	if err := gat.RegisterHuma(s.api, g, "/api"); err != nil {
		return err
	}
	s.routes.add(http.MethodPost, "/api/graphql", netguard.ScopeAnyKey)
	s.routes.add(http.MethodGet, "/api/graphql", netguard.ScopeAnyKey)
	return nil
}

// jsonFormat writes what `para <cmd> --json` writes, byte for byte.
var jsonFormat = huma.Format{
	Marshal: func(w io.Writer, v any) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	},
	Unmarshal: func(data []byte, v any) error {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		return dec.Decode(v)
	},
}

// rawRoutes are the routes outside huma. Unmatched paths under a server prefix
// answer JSON 404, never the SPA: a 200 HTML page from a mistyped API path
// would make every existence check pass.
func (s *Server) rawRoutes() {
	// The relay gates each subscription by a signed cap. The connection itself
	// needs a key, and the gate checks Origin on the upgrade.
	s.routes.add(get, "/ws", netguard.ScopeAnyKey)
	s.mux.Get("/ws", s.live.Handler().ServeHTTP)
	s.mux.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	s.mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) || s.opt.Web == nil {
			notFound(w, r)
			return
		}
		s.opt.Web.ServeHTTP(w, r)
	})
	s.mux.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		netguard.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	netguard.WriteError(w, http.StatusNotFound, "no such endpoint")
}

// Policy classifies a request for the gate. A GET of a path the server does not
// own is the UI shell, which holds no data. Every other request needs a token.
func (s *Server) Policy(method, path string) netguard.Route {
	safe := method == http.MethodGet || method == http.MethodHead
	if path == "/healthz" && safe {
		return netguard.Route{Public: true}
	}
	if safe && !isAPIPath(path) {
		return netguard.Route{Public: true}
	}
	if scope, ok := s.routes.scope(method, path); ok {
		return netguard.Route{Scope: scope}
	}
	return netguard.Route{Scope: netguard.ScopeAnyKey}
}

// scopeMiddleware runs on every huma op, over REST and inside GraphQL. The gate
// has already checked the REST route. For GraphQL this is the only per-op
// check, so an op with no declared scope is refused here.
func (s *Server) scopeMiddleware(ctx huma.Context, next func(huma.Context)) {
	scope, _ := ctx.Operation().Metadata[metaScope].(string)
	req, ok := netguard.From(ctx.Context())
	switch {
	case scope == "":
		s.log.Error("route has no scope", "operation", ctx.Operation().OperationID)
		_ = huma.WriteErr(s.api, ctx, http.StatusForbidden, "route declares no scope")
	case !ok || !req.Principal.Has(scope):
		_ = huma.WriteErr(s.api, ctx, http.StatusForbidden, "key lacks scope "+scope)
	default:
		next(ctx)
	}
}

// bound returns the vault and principal of the calling request.
func (s *Server) bound(ctx context.Context) (*boundVault, error) {
	req, ok := netguard.From(ctx)
	if !ok {
		return nil, huma.Error403Forbidden("no gate")
	}
	t, ok := req.Tenant.(*Tenant)
	if !ok {
		return nil, huma.Error403Forbidden("no vault")
	}
	surface := surfaceAPI
	if req.Principal.Session {
		surface = surfacePWA
	}
	return &boundVault{t: t, req: req, surface: surface, routes: s.routes}, nil
}
