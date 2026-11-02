package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/parasolve/internal/ops"
)

func TestUpgradedBinaryIsRefusedWithARestartMessage(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "para")
	if err := os.WriteFile(bin, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, _ := newVault(t)
	srv, err := New(ops.Deps{Store: s}, Options{Version: "test", Executable: bin})
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
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	if res := call(t, c, "lint", ""); res.IsError {
		t.Fatalf("lint before the upgrade: %s", textOf(t, res))
	}
	// An upgrade installs by rename, as make install does.
	tmp := bin + ".new"
	if err := os.WriteFile(tmp, []byte("v2, longer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, bin); err != nil {
		t.Fatal(err)
	}
	res := call(t, c, "lint", "")
	if !res.IsError || !strings.Contains(textOf(t, res), "reconnect para") {
		t.Fatalf("after the upgrade: error=%v %s", res.IsError, textOf(t, res))
	}
}
