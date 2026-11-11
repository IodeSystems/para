package mcpserver

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/ops"
)

// budget is what an agent reads before its first call: the tools/list bytes
// and the initialize instructions.
type budget struct {
	total, meta, instr int
	sizes              map[string]int
}

func measure(t *testing.T, o Options) budget {
	t.Helper()
	s, _ := newVault(t)
	o.Version = "test"
	srv, err := New(ops.Deps{Store: s}, o)
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
	list, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(list.Tools)
	if err != nil {
		t.Fatal(err)
	}
	b := budget{total: len(raw), instr: len(init.Instructions), sizes: map[string]int{}}
	byName := map[string]ops.Entry{}
	for _, e := range ops.All() {
		byName[e.Name()] = e
	}
	for _, tl := range list.Tools {
		one, err := json.Marshal(tl)
		if err != nil {
			t.Fatal(err)
		}
		b.sizes[tl.Name] = len(one)
		e, ok := byName[tl.Name]
		if !ok || !ops.NeedsWhy(e) {
			continue
		}
		own := fieldNames(e.InputType())
		for name, p := range metaProps {
			if own[name] {
				continue
			}
			pj, _ := json.Marshal(p)
			b.meta += len(name) + 4 + len(pj) // "name":{...},
		}
	}
	return b
}

func (b budget) log(t *testing.T, mode string) {
	t.Helper()
	names := make([]string, 0, len(b.sizes))
	for n := range b.sizes {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return b.sizes[names[i]] > b.sizes[names[j]] })
	t.Logf("%s: %d tools, tools/list %d bytes (~%d tokens), metaProps %d bytes (%.0f%%), instructions %d bytes",
		mode, len(names), b.total, b.total/4, b.meta, 100*float64(b.meta)/float64(max(b.total, 1)), b.instr)
	for i, n := range names {
		if i == 20 {
			break
		}
		t.Logf("  %5d %s", b.sizes[n], n)
	}
}

// Budget limits. Each is the size reached so far plus a little; lower it when
// a change makes the surface smaller. Targets (TIGHT D2): instructions of at
// most 8 KB; the core caps are the measured size plus 10%.
//
// Baseline at v0.3.1 (all, 258 tools with aliases): tools/list 321848 bytes,
// metaProps 176969 (55%), instructions 8114. At v0.4.0 (core, the 15 tools of
// Core): tools/list 10915 bytes, largest tool list 1027, instructions 7318.
const (
	allMax   = 188_000 // after S1 + W1: 185466
	coreMax  = 12_007  // 10915 + 10%
	instrMax = 8_192
	toolMax  = 3_750 // all mode: obligation_add 3063
	coreTool = 1_130 // core mode, any tool: list 1027 + 10%
)

func (b budget) check(t *testing.T, mode string, most, tool int) {
	t.Helper()
	if b.total > most {
		t.Errorf("%s: tools/list is %d bytes, budget %d", mode, b.total, most)
	}
	if b.instr > instrMax {
		t.Errorf("%s: instructions are %d bytes, budget %d", mode, b.instr, instrMax)
	}
	for n, size := range b.sizes {
		if size > tool {
			t.Errorf("%s: tool %s is %d bytes, budget %d", mode, n, size, tool)
		}
	}
}

func TestToolBudget(t *testing.T) {
	b := measure(t, Options{})
	b.log(t, "all")
	b.check(t, "all", allMax, toolMax)
	c := measure(t, Options{Tools: ToolsCore})
	c.log(t, "core")
	c.check(t, "core", coreMax, coreTool)
	if len(c.sizes) != len(Core) {
		t.Errorf("core: %d tools listed, want the %d in Core", len(c.sizes), len(Core))
	}
	for _, name := range Core {
		if _, ok := c.sizes[name]; !ok {
			t.Errorf("core: %s is in Core and not listed", name)
		}
	}
}
