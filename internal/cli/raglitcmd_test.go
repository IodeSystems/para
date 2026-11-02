package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/iodesystems/parasolve/internal/raglit"
)

// fakeDaemon is a raglit daemon's /mcp/acme endpoint. It records tool calls.
type fakeDaemon struct {
	*httptest.Server
	mu      sync.Mutex
	calls   []string
	indexes int
	meaning bool
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	f := &fakeDaemon{meaning: true}
	s := server.NewMCPServer("fake", "0")
	rec := func(name string) server.ToolHandlerFunc {
		return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := req.GetArguments()
			f.mu.Lock()
			defer f.mu.Unlock()
			f.calls = append(f.calls, name+":"+req.GetString("action", "")+":"+req.GetString("pattern", "")+req.GetString("dir", ""))
			if name == "manage_project" && args["action"] == "create" {
				f.indexes = 1
			}
			text := "{}"
			switch {
			case name == "search":
				text = `{"hits":[]}`
			case name == "list_indexes":
				b, _ := json.Marshal(map[string]any{"indexes": []raglit.Index{{Name: "acme__default", Documents: 7, Meaning: f.meaning}}[:f.indexes]})
				text = string(b)
			case args["action"] == "watches":
				text = `{"watches":[]}`
				for _, c := range f.calls {
					if strings.HasPrefix(c, "manage_project:watch:") {
						text = `{"watches":[{"dir":"` + strings.TrimPrefix(c, "manage_project:watch:") + `"}]}`
					}
				}
			}
			return mcp.NewToolResultText(text), nil
		}
	}
	s.AddTool(mcp.NewTool("manage_project", mcp.WithString("action"), mcp.WithString("dir"), mcp.WithString("caption_profile")), rec("manage_project"))
	s.AddTool(mcp.NewTool("manage_index", mcp.WithString("action"), mcp.WithString("pattern")), rec("manage_index"))
	s.AddTool(mcp.NewTool("search", mcp.WithString("query"), mcp.WithNumber("limit")), rec("search"))
	s.AddTool(mcp.NewTool("list_indexes"), rec("list_indexes"))
	h := server.NewStreamableHTTPServer(s, server.WithStateLess(true))
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/acme", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rk_tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeDaemon) took() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeRaglitBin writes a script that stands in for the raglit CLI and logs its
// arguments to log.
func fakeRaglitBin(t *testing.T, version, daemonURL, createReply string) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	script := `#!/bin/sh
echo "$@" >> "` + log + `"
case "$1" in
--version) echo "` + version + `" ;;
project) ` + createReply + ` ;;
esac
`
	bin = filepath.Join(dir, "raglit")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func runWith(e *env, bin string, args ...string) (string, error) {
	var out bytes.Buffer
	l := &loader{version: "test", raglitExe: bin, env: func(k string) string {
		if k == "PARA_HOME" {
			return e.home
		}
		return ""
	}}
	root := newRoot(l)
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestRaglitConnectStatusDisconnect(t *testing.T) {
	e := newEnv(t)
	vault := e.init("acme")
	d := newFakeDaemon(t)
	bin, log := fakeRaglitBin(t, "raglit version 0.4.1-0.20261102052510-44b40ad1311a", d.URL,
		`echo '{"project":"acme","token":"rk_tok","url":"`+d.URL+`/mcp/acme"}'`)

	out, err := runWith(e, bin, "raglit", "connect", "--url", d.URL)
	if err != nil {
		t.Fatalf("connect: %v\n%s", err, out)
	}
	cfgPath := filepath.Join(e.home, "vaults", "acme", "raglit.json")
	st, err := os.Stat(cfgPath)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("config: %v %v", st, err)
	}
	logged, _ := os.ReadFile(log)
	if !strings.Contains(string(logged), "project create acme --root "+vault+" --json --daemon "+d.URL) {
		t.Errorf("raglit calls:\n%s", logged)
	}
	calls := strings.Join(d.took(), " ")
	ign := strings.Index(calls, "manage_index:ignore:**/.para/**")
	watch := strings.Index(calls, "manage_project:watch:")
	if ign < 0 || watch < 0 || ign > watch {
		t.Errorf("ignore must come before watch:\n%s", calls)
	}
	for _, p := range ignorePatterns {
		if !strings.Contains(calls, "manage_index:ignore:"+p) {
			t.Errorf("no ignore for %s", p)
		}
	}
	if !strings.Contains(calls, "manage_project:create:") || !strings.Contains(calls, "search::") {
		t.Errorf("calls: %s", calls)
	}
	if !strings.Contains(out, "backlog") {
		t.Errorf("connect output must warn of a first-index backlog:\n%s", out)
	}

	// A second run creates nothing: no raglit project call, no second index
	// create, no second watch.
	out, err = runWith(e, bin, "raglit", "connect")
	if err != nil {
		t.Fatalf("second connect: %v\n%s", err, out)
	}
	logged, _ = os.ReadFile(log)
	if n := strings.Count(string(logged), "project create"); n != 1 {
		t.Errorf("project create ran %d times", n)
	}
	calls = strings.Join(d.took(), " ")
	if strings.Count(calls, "manage_project:create:") != 1 || strings.Count(calls, "manage_project:watch:") != 1 {
		t.Errorf("second run repeated work:\n%s", calls)
	}

	out, err = runWith(e, bin, "raglit", "status")
	if err != nil || !strings.Contains(out, "connected: yes") || !strings.Contains(out, "models:    on") || !strings.Contains(out, "7 documents") || strings.Contains(out, "rk_tok") {
		t.Errorf("status: %v\n%s", err, out)
	}
	d.meaning = false
	out, _ = runWith(e, bin, "raglit", "status")
	if !strings.Contains(out, "off (exact-only)") {
		t.Errorf("status without models:\n%s", out)
	}

	out, err = runWith(e, bin, "raglit", "disconnect")
	if err != nil || !strings.Contains(out, "remain on the daemon") || !strings.Contains(out, "raglit project delete acme --yes") {
		t.Errorf("disconnect: %v\n%s", err, out)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Error("config still there")
	}
	out, _ = runWith(e, bin, "raglit", "status")
	if !strings.Contains(out, "connected: no") {
		t.Errorf("status after disconnect:\n%s", out)
	}
}

func TestRaglitConnectRefusals(t *testing.T) {
	e := newEnv(t)
	e.init("acme")

	if _, err := runWith(e, "/nonexistent/raglit", "raglit", "connect"); err == nil {
		t.Error("connect with a missing binary succeeded")
	}
	old, _ := fakeRaglitBin(t, "raglit version 0.4.0", "", "")
	out, err := runWith(e, old, "raglit", "connect")
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Errorf("old raglit: %v\n%s", err, out)
	}
	lost, _ := fakeRaglitBin(t, "raglit version 0.5.0", "", `echo 'project acme already has a key' >&2; exit 1`)
	_, err = runWith(e, lost, "raglit", "connect")
	if err == nil || !strings.Contains(err.Error(), "raglit project key acme") || !strings.Contains(err.Error(), "--token") {
		t.Errorf("lost key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.home, "vaults", "acme", "raglit.json")); err == nil {
		t.Error("a failed connect left a config")
	}
}

func TestRaglitVersionOK(t *testing.T) {
	for v, want := range map[string]bool{
		"raglit version 0.4.0":                                false,
		"raglit version 0.3.9":                                false,
		"raglit version 0.4.1":                                true,
		"raglit version 0.5.0":                                true,
		"raglit version 0.4.1-0.20261102052510-44b40ad+dirty": true,
		"raglit version 0.4.1-0.20261030000000-aaaaaaa":       false,
		"dev": true,
	} {
		if got := raglitVersionOK(v); got != want {
			t.Errorf("raglitVersionOK(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestRaglitProjectName(t *testing.T) {
	for in, want := range map[string]string{"acme": "acme", "Life Work": "life-work", "a__b": "a_b", "root": "para-root", "x@y": "x-y"} {
		if got := raglitProject(in); got != want {
			t.Errorf("raglitProject(%q) = %q, want %q", in, got, want)
		}
	}
}
