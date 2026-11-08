// Package netguard is para's request gate. Every request passes the stages in
// one order: client IP, Host, IP rule, token, scope, Origin. Each stage denies
// by default. It knows nothing of vaults or keys: a Tenant supplies the rules
// and resolves credentials.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Scopes a key can hold.
const (
	ScopeVaultRead    = "vault:read"
	ScopeVaultWrite   = "vault:write"
	ScopeRFXDraft     = "rfx:draft"
	ScopeRFXSend      = "rfx:send"
	ScopeRequestQueue = "request:queue"
	ScopeRequestRun   = "request:run"
	// ScopeReviewDecide decides review items a person answered: draft,
	// submit, and stack_decide, and marks a tracked document seen (doc_seen),
	// on a browser session. It authors nothing, and
	// an approved item runs under it whatever its own op's scope.
	ScopeReviewDecide = "review:decide"

	// ScopeAdmin is held by the admin token only and is never granted to a key.
	ScopeAdmin = "admin"
	// ScopeAnyKey marks a route that any valid credential can call. It is the
	// scope of the session routes and of /api/graphql, whose fields check their
	// own scopes.
	ScopeAnyKey = "any"
)

// Scopes lists every scope a key can be given.
var Scopes = []string{ScopeVaultRead, ScopeVaultWrite, ScopeRFXDraft, ScopeRFXSend, ScopeRequestQueue, ScopeRequestRun, ScopeReviewDecide}

// ScopeSets are names for groups of scopes, for a person creating a key. A key
// stores the scopes a set expands to, never the set's name, so a later change
// to a set does not change a key made before it.
//   - web: the web app, as the owner uses it (what para serve login grants).
//   - agent: an agent that reads and writes the vault, drafts asks, and works
//     the agent queue. It cannot mark an ask sent or decide for the owner.
//   - admin: every scope a key can hold. It is not the admin token, whose
//     powers no key gets.
var ScopeSets = map[string][]string{
	"web":   {ScopeVaultRead, ScopeReviewDecide},
	"agent": {ScopeVaultRead, ScopeVaultWrite, ScopeRFXDraft, ScopeRequestQueue, ScopeRequestRun},
	"admin": Scopes,
}

// ScopeSetNames are the names of ScopeSets, in the order help text lists them.
var ScopeSetNames = []string{"web", "agent", "admin"}

// ExpandScopes replaces each set name with its scopes and checks the rest,
// keeping the first place of each scope.
func ExpandScopes(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range in {
		if set, ok := ScopeSets[s]; ok {
			for _, v := range set {
				add(v)
			}
			continue
		}
		if !slices.Contains(Scopes, s) {
			return nil, fmt.Errorf("netguard: unknown scope %q: one of %s, or a set: %s", s, strings.Join(Scopes, ", "), strings.Join(ScopeSetNames, ", "))
		}
		add(s)
	}
	return out, nil
}

// Errors a Tenant returns from Authenticate.
var (
	// ErrUnauthenticated gives 401: no valid credential.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrForbidden gives 403: a valid credential from a place it cannot be used.
	ErrForbidden = errors.New("forbidden")
)

// Credential is what a request presented.
type Credential struct {
	Token  string
	Cookie bool // came from the session cookie, not an Authorization header
}

// Principal is who a credential belongs to.
type Principal struct {
	Name    string
	Scopes  []string
	Admin   bool
	Session bool // authenticated by a browser session cookie
	// Expires is when the credential ends, or nil.
	Expires *time.Time
	// Login: a browser sign-in key, which can renew.
	Login bool
}

// Has reports whether p can call a route with this scope.
func (p Principal) Has(scope string) bool {
	if scope == ScopeAnyKey {
		return p.Name != ""
	}
	if p.Admin {
		return true
	}
	if scope == ScopeAdmin {
		return false
	}
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Tenant is a routed vault.
type Tenant interface {
	// Rules returns the vault's net.rules. An error denies the request.
	Rules() (*Rules, error)
	// Authenticate resolves a credential for a request from client.
	Authenticate(cred Credential, client Client) (Principal, error)
}

// Route is what the gate must check for one method and canonical path.
type Route struct {
	Public bool   // no token: the UI shell and /healthz
	Scope  string // required when not Public; "" means ScopeAnyKey
}

// CookieName is the browser session cookie.
const CookieName = "para_session"

// Config configures a Gate.
type Config struct {
	Proxies Proxies
	Router  *Router
	// Lookup returns the tenant of a vault id, or false.
	Lookup func(id string) (Tenant, bool)
	// Policy classifies a request.
	Policy func(method, path string) Route
	// Own reports whether an address belongs to this machine. Nil means the
	// interfaces of this machine.
	Own func(netip.Addr) bool
	Log *slog.Logger // nil discards
}

// Gate is the request gate.
type Gate struct{ c Config }

// New returns a Gate.
func New(c Config) *Gate {
	if c.Log == nil {
		c.Log = slog.New(slog.DiscardHandler)
	}
	if c.Own == nil {
		c.Own = OwnAddr
	}
	return &Gate{c: c}
}

type ctxKey struct{}

// Request is what the gate proved about a request.
type Request struct {
	Vault     string
	Tenant    Tenant
	Client    Client
	Path      string
	Principal Principal // empty on a public route
	// Cookie is the session token when the request used one.
	Cookie string
}

// From returns the gate's result for a request context.
func From(ctx context.Context) (Request, bool) {
	r, ok := ctx.Value(ctxKey{}).(Request)
	return r, ok
}

// CookieFrom returns the session token of the request, or "".
func CookieFrom(ctx context.Context) string {
	r, _ := From(ctx)
	return r.Cookie
}

// WithRequest puts a gate result on a context. The gate is the only caller
// outside tests.
func WithRequest(ctx context.Context, r Request) context.Context {
	return context.WithValue(ctx, ctxKey{}, r)
}

// Wrap runs the stages, then next.
func (g *Gate) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secureHeaders(w.Header())

		// Stage 1: client IP.
		client := g.client(r)

		// Stage 2: Host. One answer for every name, known or not.
		id, ok := g.c.Router.Route(r.Host)
		var tenant Tenant
		if ok {
			tenant, ok = g.c.Lookup(id)
		}
		if !ok {
			g.deny(w, r, client, "", "host", http.StatusMisdirectedRequest, "unknown host")
			return
		}

		// Stage 3: IP rule, on the canonical path.
		p, ok := Canonical(r.URL.EscapedPath(), r.URL.Path)
		if !ok {
			g.deny(w, r, client, id, "path", http.StatusBadRequest, "path is not canonical")
			return
		}
		rules, err := tenant.Rules()
		if err != nil {
			g.c.Log.Error("net.rules unreadable", "vault", id, "error", err)
			rules = nil
		}
		if !rules.Allow(p, client) {
			g.deny(w, r, client, id, "ip_rule", http.StatusForbidden, "forbidden")
			return
		}

		res := Request{Vault: id, Tenant: tenant, Client: client, Path: p}
		route := g.c.Policy(r.Method, p)
		if route.Public {
			next.ServeHTTP(w, r.WithContext(WithRequest(r.Context(), res)))
			return
		}

		// Stage 4: token.
		cred, ok := credential(r)
		if !ok {
			g.unauthenticated(w, r, client, id)
			return
		}
		pr, err := tenant.Authenticate(cred, client)
		switch {
		case errors.Is(err, ErrForbidden):
			g.deny(w, r, client, id, "token", http.StatusForbidden, "forbidden")
			return
		case err != nil:
			g.unauthenticated(w, r, client, id)
			return
		}
		res.Principal = pr
		if cred.Cookie {
			res.Cookie = cred.Token
		}

		// Stage 5: scope.
		scope := route.Scope
		if scope == "" {
			scope = ScopeAnyKey
		}
		if !pr.Has(scope) {
			g.deny(w, r, client, id, "scope", http.StatusForbidden, "key lacks scope "+scope)
			return
		}

		// Stage 6: Origin.
		if !originOK(r, client, cred.Cookie) {
			g.deny(w, r, client, id, "origin", http.StatusForbidden, "cross-origin request denied")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithRequest(r.Context(), res)))
	})
}

// client resolves the client of r. A direct peer on one of this machine's own
// addresses is local like a loopback peer: not a trusted proxy, no forwarding
// header.
func (g *Gate) client(r *http.Request) Client {
	c := g.c.Proxies.Resolve(r)
	if !c.Local && c.Peer.IsValid() && c.Addr == c.Peer && !g.c.Proxies.has(c.Peer) && !forwarded(r) && g.c.Own(c.Peer) {
		c.Local = true
	}
	return c
}

func (g *Gate) unauthenticated(w http.ResponseWriter, r *http.Request, c Client, vault string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="para"`)
	g.deny(w, r, c, vault, "token", http.StatusUnauthorized, "unauthorized")
}

func (g *Gate) deny(w http.ResponseWriter, r *http.Request, c Client, vault, stage string, status int, detail string) {
	g.c.Log.Info("request denied", "stage", stage, "status", status, "vault", vault,
		"client", c.Addr.String(), "peer", c.Peer.String(), "method", r.Method)
	if stage == "ip_rule" && browserPage(r) {
		writeNotAllowedPage(w, c)
		return
	}
	WriteError(w, status, detail)
}

// browserPage reports whether r is a browser opening a page (not a fetch from
// the app or an API client), which gets a page it can read instead of JSON.
func browserPage(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if m := r.Header.Get("Sec-Fetch-Mode"); m != "" {
		return m == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// writeNotAllowedPage is the 403 of the IP rule for a browser: the address
// net.rules does not allow, and what to do. It names no vault and holds no
// vault text. It has no script and no style, so the CSP holds as is.
func writeNotAllowedPage(w http.ResponseWriter, c Client) {
	addr := "unknown"
	if c.Addr.IsValid() {
		addr = c.Addr.String()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark"><title>Not allowed from this network</title></head>
<body><main>
<h1>Not allowed from this network</h1>
<p>This para does not take requests from your address (` + html.EscapeString(addr) + `).</p>
<p>If you reach it over a VPN, connect the VPN and reload. On the machine that runs para, open it at <code>http://&lt;vault&gt;.localhost:&lt;port&gt;</code>, or run <code>para serve login</code>.</p>
<p>The owner allows addresses in <code>net.rules</code> of the vault, in <code>$PARA_HOME/vaults/&lt;id&gt;/server/</code>.</p>
</main></body></html>
`))
}

// WriteError writes huma's error shape, so a client parses one error form.
func WriteError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"title":"` + http.StatusText(status) + `","status":` + strconv.Itoa(status) + `,"detail":"` + detail + `"}` + "\n"))
}

// BearerFromHeader returns the token of an Authorization header.
func BearerFromHeader(h string) (string, bool) {
	scheme, tok, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	tok = strings.TrimSpace(tok)
	return tok, tok != ""
}

// credential takes the Authorization header first. A malformed header is not
// passed over for the cookie.
func credential(r *http.Request) (Credential, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		tok, ok := BearerFromHeader(h)
		return Credential{Token: tok}, ok
	}
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return Credential{Token: c.Value, Cookie: true}, true
	}
	return Credential{}, false
}

// originOK applies the browser checks. Writes and WebSocket upgrades must name
// this exact origin. A request with no Origin is not from a browser unless it
// rode on a cookie, and then Sec-Fetch-Site must say same-origin. Any
// cross-site fetch that rode on a cookie is refused, reads included.
func originOK(r *http.Request, c Client, cookie bool) bool {
	site := r.Header.Get("Sec-Fetch-Site")
	if cookie && (site == "cross-site" || site == "same-site") {
		return false
	}
	safe := r.Method == http.MethodGet || r.Method == http.MethodHead
	if safe && r.Header.Get("Upgrade") == "" {
		return true
	}
	vals := r.Header.Values("Origin")
	switch {
	case len(vals) > 1:
		return false
	case len(vals) == 0:
		return !cookie || site == "same-origin"
	}
	u, err := url.Parse(vals[0])
	if err != nil || u.Path != "" || u.RawQuery != "" || u.User != nil {
		return false
	}
	return u.Scheme == c.Proto && strings.EqualFold(u.Host, r.Host)
}

// CSP is the content security policy of every response. Style allows inline
// because Emotion injects style elements at run time. Nothing else is inline.
// media-src lets a page play the audio and video attached to an item, from
// this origin only.
const CSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; media-src 'self'; " +
	"font-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func secureHeaders(h http.Header) {
	h.Set("Content-Security-Policy", CSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
}
