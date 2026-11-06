package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/server"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestLoginDays(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	for _, d := range []string{"0", "91"} {
		if _, _, err := e.run("serve", "login", "--print", "--days", d); err == nil || !strings.Contains(err.Error(), "--days") {
			t.Errorf("--days %s: %v", d, err)
		}
	}
}

func TestOpener(t *testing.T) {
	none := func(string) string { return "" }
	found := func(string) (string, error) { return "/usr/bin/x", nil }
	missing := func(string) (string, error) { return "", os.ErrNotExist }
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, tc := range []struct {
		goos   string
		getenv func(string) string
		look   func(string) (string, error)
		reason string
	}{
		{"linux", none, found, ops.OpenNoDisplay},
		{"linux", env(map[string]string{"SSH_CONNECTION": "1 2 3 4"}), found, ops.OpenRemote},
		{"linux", env(map[string]string{"DISPLAY": ":0"}), missing, ops.OpenNoOpener},
		{"linux", env(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}), found, ""},
		{"darwin", none, found, ""},
		{"plan9", none, found, ops.OpenNoOpener},
	} {
		_, err := opener(tc.goos, tc.getenv, tc.look)
		var oe *ops.OpenError
		switch {
		case tc.reason == "" && err != nil:
			t.Errorf("%s: %v", tc.goos, err)
		case tc.reason != "" && (!errors.As(err, &oe) || oe.Reason != tc.reason):
			t.Errorf("%s: %v, want %s", tc.goos, err, tc.reason)
		}
	}
}

// TestOpenSignsIn: the owner's para open mints a login key when none is
// active and puts its token on the browser command line only. An agent never
// mints: MCP has no open tool, and at the CLI its output says the owner is
// not signed in.
func TestOpenSignsIn(t *testing.T) {
	e := newProposalEnv(t)
	filed := struct{ ID string }{e.source}
	if err := server.WriteRecord(e.home, server.Record{PID: os.Getpid(), Addr: "127.0.0.1:7799", Vault: "acme", Vaults: []string{"acme"}}); err != nil {
		t.Fatal(err)
	}
	var sessions []loginSession
	e.json(&sessions, "serve", "sessions")
	if len(sessions) != 0 {
		t.Fatalf("sessions before = %+v", sessions)
	}

	// MCP: proposal_open is people only, so there is no tool to call.
	tools, err := e.agent.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools.Tools {
		if tl.Name == "proposal_open" {
			t.Fatal("MCP lists proposal_open")
		}
	}
	e.json(&sessions, "serve", "sessions")
	if len(sessions) != 0 {
		t.Fatalf("the agent minted a key: %+v", sessions)
	}

	// The owner's CLI: the browser gets the token; nothing else does.
	var targets []string
	saved := openURL
	openURL = func(u string) (string, error) { targets = append(targets, u); return "test-open " + u, nil }
	t.Cleanup(func() { openURL = saved })
	out, _, err := e.run("open", filed.ID, "--json")
	if err != nil {
		t.Fatal(err)
	}
	want := "http://acme.localhost:7799/doc/" + filed.ID
	if len(targets) != 1 || !strings.HasPrefix(targets[0], want+"#token=para_viewer-") {
		t.Fatalf("browser got %v", targets)
	}
	if strings.Contains(out, "token") || strings.Contains(out, "para_viewer") || !strings.Contains(out, `"opened": true`) {
		t.Fatalf("output leaks or did not open: %s", out)
	}
	tok := strings.TrimPrefix(targets[0], want+"#token=")
	_ = filepath.WalkDir(e.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.Contains(p, "events") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), tok) {
			t.Errorf("%s holds the token", p)
		}
		return nil
	})
	e.json(&sessions, "serve", "sessions")
	if len(sessions) != 1 || sessions[0].Vault != "acme" || sessions[0].Expires == nil ||
		sessions[0].Expires.Sub(sessions[0].Created).Round(time.Hour) != 30*24*time.Hour {
		t.Fatalf("sessions = %+v", sessions)
	}
	// A key is active: the next open is plain, and MCP has no note.
	targets = nil
	e.ok("open", filed.ID)
	if len(targets) != 1 || targets[0] != want {
		t.Fatalf("second open %v", targets)
	}

	// Logout revokes the login keys; sessions is empty.
	if out := e.ok("serve", "logout"); !strings.Contains(out, "revoked "+sessions[0].ID) {
		t.Fatalf("logout: %s", out)
	}
	e.json(&sessions, "serve", "sessions")
	if len(sessions) != 0 {
		t.Fatalf("after logout = %+v", sessions)
	}
	// An agent at the CLI cannot open (proposal_open is people only), so it mints nothing.
	e.vars["PARA_ACTOR"] = "bot"
	targets = nil
	if _, stderr, err := e.run("open", filed.ID); err == nil || !strings.Contains(err.Error()+stderr, "only a person") || len(targets) != 0 {
		t.Fatalf("agent cli open: %v %s %v", err, stderr, targets)
	}
	e.json(&sessions, "serve", "sessions")
	if len(sessions) != 0 {
		t.Fatalf("an agent minted: %+v", sessions)
	}
}
