package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/ops"
)

// TestReadOnlyListsNoWrites: --read-only lists only ops that change nothing,
// so a worker can read policy and search but has no write tool to call.
func TestReadOnlyListsNoWrites(t *testing.T) {
	s, _ := newVault(t)
	srv, err := New(ops.Deps{Store: s}, Options{Version: "test", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.NewInProcessClient(srv.MCP())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	init, err := c.Initialize(ctx, mcp.InitializeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(init.Instructions, "read-only") {
		t.Fatal("instructions do not say read-only")
	}
	list, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tl := range list.Tools {
		got[tl.Name] = true
	}
	for _, e := range ops.ForAgents() {
		if got[e.Name()] != (e.Risk() == ops.RiskRead) {
			t.Errorf("%s (risk %s): listed %v", e.Name(), e.Risk(), got[e.Name()])
		}
	}
	for _, want := range []string{"search", "rules_in_effect", "show", opRunTool, opsDescribeTool} {
		if !got[want] {
			t.Errorf("%s is not listed", want)
		}
	}
	res, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "task_add", Arguments: map[string]any{"title": "x", "why": "y"}}})
	if err == nil && !res.IsError {
		t.Fatal("a write tool ran on a read-only server")
	}
}
