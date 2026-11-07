package server

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/opsample"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

const (
	hostA     = "acme.localhost:7700"
	hostB     = "beta.localhost:7700"
	localPeer = "127.0.0.1:50000"
	proxyIP   = "10.9.9.9"
	proxyPeer = proxyIP + ":50000"
	// ownIP stands for a LAN address of the machine that runs the server.
	ownIP   = "192.168.77.5"
	ownPeer = ownIP + ":50000"
)

var allScopes = netguard.Scopes

type fixture struct {
	t    *testing.T
	home string
	hub  *Hub
	srv  *Server
	h    http.Handler
	keyA *keys.Store
	keyB *keys.Store
}

var (
	template     string
	templateOnce sync.Once
	templateErr  error
)

// seededHome builds one install once: vaults acme (seeded) and beta. Each
// fixture copies it, because a copy costs milliseconds and a new vault seconds.
func seededHome(t *testing.T) string {
	t.Helper()
	templateOnce.Do(func() {
		base, err := os.MkdirTemp("", "para-server-template-")
		if err != nil {
			templateErr = err
			return
		}
		templateErr = buildHome(base)
		template = base
	})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	return template
}

func buildHome(base string) error {
	in := registry.Install{Home: filepath.Join(base, "home")}
	tmpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		return err
	}
	install, err := in.ID()
	if err != nil {
		return err
	}
	for _, id := range []string{"acme", "beta"} {
		root := filepath.Join(base, "v-"+id)
		if err := vault.Init(root, id, tmpl, vault.InitOptions{NoGit: true}); err != nil {
			return err
		}
		if err := in.Add(id, root); err != nil {
			return err
		}
	}
	root := filepath.Join(base, "v-acme")
	s, err := local.Open(root, vault.Options{ID: "acme", Actor: "seed", Surface: "cli", Install: install, StateDir: filepath.Join(in.Home, "vaults", "acme")})
	if err != nil {
		return err
	}
	if err := opsample.WriteConfigFiles(root); err != nil {
		return err
	}
	return opsample.Seed(contextBackground(), s)
}

// newFixture copies the template home, rewrites its vault paths, and builds the
// server with one trusted proxy.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	src := seededHome(t)
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dst, "home")
	for _, id := range []string{"acme", "beta"} {
		ref := filepath.Join(home, "vaults", id, "vault.ref")
		if err := os.WriteFile(ref, []byte("path="+filepath.Join(dst, "v-"+id)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	in := registry.Install{Home: home}
	log := slog.New(slog.DiscardHandler)
	hub := NewHub(in, log)
	hub.docDebounce = 50 * time.Millisecond
	live, err := NewLive()
	if err != nil {
		t.Fatal(err)
	}
	hub.SetLive(live)
	t.Cleanup(hub.Close)
	if _, err := hub.Prepare(); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{Version: "test", Log: log, Live: live,
		Web: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>shell</html>") })})
	if err != nil {
		t.Fatal(err)
	}
	proxies, err := netguard.ParseProxies([]string{proxyIP})
	if err != nil {
		t.Fatal(err)
	}
	gate := netguard.New(netguard.Config{
		Proxies: proxies, Router: netguard.NewRouter([]string{"para.vpn.example"}, "", nil),
		Lookup: hub.Lookup, Policy: srv.Policy,
		Own: func(a netip.Addr) bool { return a.IsLoopback() || a == netip.MustParseAddr(ownIP) },
	})
	return &fixture{t: t, home: home, hub: hub, srv: srv, h: srv.Handler(gate),
		keyA: keys.New(ServerDir(in, "acme")), keyB: keys.New(ServerDir(in, "beta"))}
}

func (f *fixture) key(s *keys.Store, name string, scopes ...string) string {
	f.t.Helper()
	_, tok, err := s.Create(name, scopes, nil, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	return tok
}

func (f *fixture) rules(id, text string) {
	f.t.Helper()
	p := filepath.Join(f.home, "vaults", id, "server", "net.rules")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

type call struct {
	method, target, body string
	host, peer, token    string
	header               map[string]string
}

func (f *fixture) do(c call) *httptest.ResponseRecorder {
	f.t.Helper()
	if c.method == "" {
		c.method = http.MethodGet
	}
	var body io.Reader
	if c.body != "" {
		body = strings.NewReader(c.body)
	}
	r := httptest.NewRequest(c.method, "http://placeholder"+c.target, body)
	r.Host = c.host
	if r.Host == "" {
		r.Host = hostA
	}
	r.RemoteAddr = c.peer
	if r.RemoteAddr == "" {
		r.RemoteAddr = localPeer
	}
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

// graphql posts one document with variables.
func (f *fixture) graphql(token, doc string, vars map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	f.t.Helper()
	return f.graphqlAs(call{token: token}, doc, vars)
}

// graphqlAs posts one document with the credentials of c.
func (f *fixture) graphqlAs(c call, doc string, vars map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	f.t.Helper()
	b, err := json.Marshal(map[string]any{"query": doc, "variables": vars})
	if err != nil {
		f.t.Fatal(err)
	}
	c.method, c.target, c.body = http.MethodPost, "/api/graphql", string(b)
	w := f.do(c)
	var out map[string]any
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.UseNumber()
	_ = dec.Decode(&out)
	return w, out
}

func wantStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
	}
}

func chmod(p string, m fs.FileMode) error { return os.Chmod(p, m) }

func removeFile(p string) error { return os.Remove(p) }
