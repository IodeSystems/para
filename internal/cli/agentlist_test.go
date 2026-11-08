package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/ops"
)

// startMCPAs starts para mcp with the client name and version given at
// initialize, over the legacy handshake when legacy is set.
func startMCPAs(t *testing.T, e *env, name, version string, legacy bool, args ...string) *client.Client {
	t.Helper()
	cmdEnv := append(os.Environ(), "PARA_TEST_BINARY=1", "PARA_HOME="+e.home, "PARA_VAULT=")
	c, err := client.NewStdioMCPClient(os.Args[0], cmdEnv, append([]string{"mcp"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req := mcp.InitializeRequest{Params: mcp.InitializeParams{ClientInfo: mcp.Implementation{Name: name, Version: version}}}
	if legacy {
		req.Params.ProtocolVersion = mcp.LATEST_LEGACY_PROTOCOL_VERSION
	}
	if _, err := c.Initialize(ctx, req); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return c
}

// Each para mcp session writes a presence file on this machine, outside the
// vault, and removes it when it exits; para agent list shows it with the
// requests its actor holds.
func TestAgentListShowsMCPSessions(t *testing.T) {
	e := newEnv(t)
	vault := e.init("acme")
	e.ok("request", "add", "Check the backups")

	var out ops.AgentListOutput
	e.json(&out, "agent", "list")
	if len(out.Agents) != 0 || out.QueuedCount != 1 || !out.NoListener {
		t.Fatalf("before = %+v", out)
	}
	if text := e.ok("agent", "list"); !strings.Contains(text, "no agent is connected on this machine") || !strings.Contains(text, "/para loop") {
		t.Errorf("text = %q", text)
	}

	claude := startMCPAs(t, e, "claude-code", "2.1.0", true, "--actor", "claude")
	dun := startMCPAs(t, e, "dun", "0.9.1", false, "--actor", "dun", "--channel")
	next := tool[ops.RequestNextOutput](t, claude, "request_next", map[string]any{})
	if next.Request == nil {
		t.Fatal("nothing to claim")
	}
	tool[ops.AgentListOutput](t, dun, "agent_list", map[string]any{})

	e.json(&out, "agent", "list")
	if len(out.Agents) != 2 || out.NoListener || out.QueuedCount != 0 {
		t.Fatalf("connected = %+v", out)
	}
	byActor := map[string]ops.AgentRow{}
	for _, a := range out.Agents {
		byActor[a.Actor] = a
	}
	c, d := byActor["claude"], byActor["dun"]
	if c.Client != "claude-code" || c.Version != "2.1.0" || !slices.Equal(c.Push, []string{"message"}) || c.LastCallAt == "" || c.StartedAt == "" {
		t.Errorf("claude = %+v", c)
	}
	if len(c.Claims) != 1 || c.Claims[0].ID != next.Request.ID {
		t.Errorf("claude claims = %+v", c.Claims)
	}
	if d.Client != "dun" || !slices.Equal(d.Push, []string{"message", "channel"}) || len(d.Claims) != 0 {
		t.Errorf("dun = %+v", d)
	}

	// The files are in this machine's state directory, never in the vault.
	files, _ := filepath.Glob(filepath.Join(e.home, "vaults", "acme", "agents", "*.json"))
	if len(files) != 2 {
		t.Errorf("presence files = %v", files)
	}
	_ = filepath.WalkDir(vault, func(p string, _ os.DirEntry, _ error) error {
		if strings.Contains(p, string(filepath.Separator)+"agents"+string(filepath.Separator)) {
			t.Errorf("presence in the vault: %s", p)
		}
		return nil
	})

	// A clean exit removes the file.
	if err := claude.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		files, _ = filepath.Glob(filepath.Join(e.home, "vaults", "acme", "agents", "*.json"))
		if len(files) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("presence files after exit = %v", files)
		}
		time.Sleep(50 * time.Millisecond)
	}
	e.json(&out, "agent", "list")
	if len(out.Agents) != 1 || out.Agents[0].Actor != "dun" {
		t.Errorf("after exit = %+v", out.Agents)
	}
}
