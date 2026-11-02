package cli

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/parasolve/internal/ops"
)

// TestMain lets a test run the para binary: the test executable re-runs
// itself as `para` when PARA_TEST_BINARY is set.
func TestMain(m *testing.M) {
	if os.Getenv("PARA_TEST_BINARY") == "1" {
		if err := Execute("test", ""); err != nil {
			os.Stderr.WriteString("para: " + err.Error() + "\n")
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

// startMCP launches `para mcp` as a subprocess and returns an initialized
// client.
func startMCP(t *testing.T, e *env, args ...string) *client.Client {
	t.Helper()
	cmdEnv := append(os.Environ(), "PARA_TEST_BINARY=1", "PARA_HOME="+e.home, "PARA_VAULT=")
	c, err := client.NewStdioMCPClient(os.Args[0], cmdEnv, append([]string{"mcp"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return c
}

func callText(t *testing.T, c *client.Client, name string, args map[string]any) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, e := range ops.All() {
		if e.Name() == name && ops.NeedsWhy(e) {
			if args == nil {
				args = map[string]any{}
			}
			if _, has := args["why"]; !has {
				args["why"] = "test"
			}
		}
	}
	res, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("%s: content is %T", name, res.Content[0])
	}
	return tc.Text, res.IsError
}

func TestMCPSubprocess(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantActor string
	}{
		{"default actor", nil, "agent"},
		{"named actor", []string{"--actor", "claude-desktop"}, "claude-desktop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.init("acme")
			c := startMCP(t, e, tc.args...)

			tools, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			have := map[string]bool{}
			for _, tl := range tools.Tools {
				have[tl.Name] = true
			}
			for _, o := range ops.ForAgents() {
				if !have[o.Name()] {
					t.Errorf("op %s is not a tool", o.Name())
				}
			}
			for _, admin := range []string{"init", "vault", "vault_list", "mcp", "version"} {
				if have[admin] {
					t.Errorf("admin command %s is a tool", admin)
				}
			}
			// request_wait is a tool of this surface only; it blocks on the session watcher.
			if !have["request_wait"] || len(have) != len(ops.ForAgents())+1 {
				t.Errorf("tools = %d, ops = %d plus request_wait", len(have), len(ops.ForAgents()))
			}

			if out, isErr := callText(t, c, "inbox_add", map[string]any{"text": "synthetic capture"}); isErr {
				t.Fatalf("inbox_add: %s", out)
			}

			// The tool result is the --json output of the command.
			for tool, cmd := range map[string][]string{
				"events_tail": {"events", "tail"},
				"lint":        {"lint"},
			} {
				got, isErr := callText(t, c, tool, map[string]any{})
				if isErr {
					t.Fatalf("%s: %s", tool, got)
				}
				want := e.ok(append(cmd, "--json")...)
				if got != want {
					t.Errorf("%s differs from para %v --json:\n got: %s\nwant: %s", tool, cmd, got, want)
				}
			}

			var tail ops.EventsTailOutput
			e.json(&tail, "events", "tail")
			var found bool
			for _, ev := range tail.Events {
				if ev.Op == "create" {
					found = true
					if ev.Actor != tc.wantActor || ev.Surface != "mcp" {
						t.Errorf("event actor/surface = %s/%s, want %s/mcp", ev.Actor, ev.Surface, tc.wantActor)
					}
				}
			}
			if !found {
				t.Errorf("no create event: %+v", tail.Events)
			}
		})
	}
}

func TestMCPLintFailureIsToolError(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	if err := os.WriteFile(path+"/inbox/bad.md", []byte("---\ntype: nonsense\n---\n# Bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := startMCP(t, e)
	out, isErr := callText(t, c, "lint", map[string]any{})
	if !isErr {
		t.Fatal("lint with errors: want tool error")
	}
	if !strings.Contains(out, `"errors"`) {
		t.Errorf("failing result lost its Output: %s", out)
	}
}

// exitsWith runs `para mcp` and returns stderr when it exits early.
func exitsWith(t *testing.T, e *env, args ...string) (string, bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"mcp"}, args...)...)
	cmd.Env = append(os.Environ(), "PARA_TEST_BINARY=1", "PARA_HOME="+e.home, "PARA_VAULT=")
	cmd.Stdin = strings.NewReader("") // EOF
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err != nil
}

func TestMCPVaultSelectionFailsClosed(t *testing.T) {
	e := newEnv(t)
	if msg, failed := exitsWith(t, e); !failed || !strings.Contains(msg, "no vaults") {
		t.Errorf("no vaults: failed=%v stderr=%q", failed, msg)
	}
	e.init("acme")
	e.init("other")
	if msg, failed := exitsWith(t, e); !failed || !strings.Contains(msg, "2 vaults") {
		t.Errorf("two vaults: failed=%v stderr=%q", failed, msg)
	}
	if msg, failed := exitsWith(t, e, "--vault", "nope"); !failed || !strings.Contains(msg, "unknown vault") {
		t.Errorf("unknown vault: failed=%v stderr=%q", failed, msg)
	}
	if msg, failed := exitsWith(t, e, "--vault", "acme"); failed {
		t.Errorf("chosen vault: stderr=%q", msg)
	}
	if msg, failed := exitsWith(t, e, "--vault", "acme", "--actor", ""); !failed || !strings.Contains(msg, "actor") {
		t.Errorf("empty actor: failed=%v stderr=%q", failed, msg)
	}
}

func TestMCPOnlyChosenVault(t *testing.T) {
	e := newEnv(t)
	acme := e.init("acme")
	other := e.init("other")
	c := startMCP(t, e, "--vault", "acme")
	if out, isErr := callText(t, c, "inbox_add", map[string]any{"text": "only acme"}); isErr {
		t.Fatal(out)
	}
	for path, want := range map[string]int{acme: 1, other: 0} {
		ents, err := os.ReadDir(path + "/inbox")
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, de := range ents {
			if strings.HasPrefix(de.Name(), "inbox-") {
				n++
			}
		}
		if n != want {
			t.Errorf("%s: inbox files = %d, want %d", path, n, want)
		}
	}
	if out, isErr := callText(t, c, "inbox_add", map[string]any{"text": "x", "vault": "other"}); !isErr || strings.Contains(out, other) {
		t.Errorf("vault argument: isErr=%v out=%q", isErr, out)
	}
}
