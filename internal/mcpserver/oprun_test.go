package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/opstats"
)

func connectWith(t *testing.T, deps ops.Deps, o Options) *client.Client {
	t.Helper()
	o.Version = "test"
	srv, err := New(deps, o)
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.NewInProcessClient(srv.MCP())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(t.Context(), mcp.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	return c
}

// opRun calls op_run as a client does: no why is added for the inner op.
func opRun(t *testing.T, c *client.Client, op, input string) (string, bool) {
	t.Helper()
	args := map[string]any{"op": op}
	if input != "" {
		var in any
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			t.Fatal(err)
		}
		args["input"] = in
	}
	res, err := c.CallTool(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: opRunTool, Arguments: args}})
	if err != nil {
		t.Fatalf("op_run %s: protocol error: %v", op, err)
	}
	var b strings.Builder
	for _, p := range res.Content {
		if tc, ok := p.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

// op_run applies what the op's own tool applies: the why rule, the person
// check, the read-only listing, and the receipt names the inner op.
func TestOpRunGates(t *testing.T) {
	s, _ := newVault(t)
	seedSamples(t, s)
	var allowed []string
	c := connectWith(t, ops.Deps{Store: s}, Options{Allow: func(op string, _ bool) error {
		allowed = append(allowed, op)
		return nil
	}})
	for _, tc := range []struct {
		name, op, input, want string
	}{
		{"no why", "io_add", `{"direction":"in","text":"x"}`, "io_add: why is required"},
		{"person only", "publish_set", `{}`, "publish_set: " + ops.ErrPersonOnly.Error()},
		{"removed name", "task_done", `{"id":"task-x"}`, "task_done removed in 0.5.0: use close"},
		{"internal op", "task_edit", `{"id":"task-x"}`, "task_edit removed in 0.5.0: use edit"},
		{"unknown", "io_ad", `{}`, "unknown op io_ad; closest: io_add"},
		{"no op", "", ``, "op is required"},
		{"unknown field", "io_add", `{"direction":"in","text":"x","why":"t","minor":true}`, `unknown field "minor"`},
	} {
		out, isErr := opRun(t, c, tc.op, tc.input)
		if !isErr || !strings.Contains(out, tc.want) {
			t.Errorf("%s: %v %q, want %q", tc.name, isErr, out, tc.want)
		}
	}
	out, isErr := opRun(t, c, "io_add", `{"direction":"in","text":"Through op_run","why":"The owner pasted it.","sources":["https://example.test/a"]}`)
	if isErr {
		t.Fatal(out)
	}
	var added ops.IOAddOutput
	if err := json.Unmarshal([]byte(out), &added); err != nil || !strings.HasPrefix(added.ID, "io-") {
		t.Fatalf("io_add = %v %q", err, out)
	}
	rs, err := ops.ReceiptList.Run(t.Context(), ops.Deps{Store: s}, ops.ReceiptListInput{Doc: added.ID})
	if err != nil || len(rs.Items) != 1 || rs.Items[0].Op != "io_add" || rs.Items[0].Why != "The owner pasted it." {
		t.Fatalf("receipt = %v %+v", err, rs.Items)
	}
	// The scope check (para serve's per-op scope) saw the inner op.
	if len(allowed) == 0 || allowed[len(allowed)-1] != "io_add" {
		t.Fatalf("allow saw %v", allowed)
	}
	for _, a := range allowed {
		if a == opRunTool {
			t.Fatalf("allow checked op_run itself: %v", allowed)
		}
	}
}

func TestOpRunReadOnly(t *testing.T) {
	s, _ := newVault(t)
	seedSamples(t, s)
	c := connectWith(t, ops.Deps{Store: s}, Options{ReadOnly: true})
	if out, isErr := opRun(t, c, "io_add", `{"direction":"in","text":"x","why":"t"}`); !isErr || !strings.Contains(out, "read-only") {
		t.Fatalf("read-only op_run wrote: %v %q", isErr, out)
	}
	if out, isErr := opRun(t, c, "lint", ""); isErr {
		t.Fatalf("read-only op_run lint: %q", out)
	}
}

// The core listing hides the other ops; op_run and ops_describe reach them.
func TestCoreListing(t *testing.T) {
	s, _ := newVault(t)
	seedSamples(t, s)
	c := connectWith(t, ops.Deps{Store: s}, Options{Tools: ToolsCore})
	list, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range list.Tools {
		if !listed(ToolsCore, tl.Name) {
			t.Errorf("core lists %s", tl.Name)
		}
	}
	if len(list.Tools) > len(Core) {
		t.Errorf("core lists %d tools, Core has %d", len(list.Tools), len(Core))
	}
	out, isErr := opRun(t, c, "task_add", `{"project":"project-sample","title":"Not listed, still callable","done_when":"It runs.","why":"test"}`)
	if isErr || !strings.Contains(out, `"id": "task-`) {
		t.Fatalf("op_run task_add in core: %v %q", isErr, out)
	}
	describe := func(args map[string]any) string {
		t.Helper()
		res, err := c.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: opsDescribeTool, Arguments: args}})
		if err != nil || res.IsError {
			t.Fatalf("ops_describe %v: %v %+v", args, err, res)
		}
		return res.Content[0].(mcp.TextContent).Text
	}
	one := describe(map[string]any{"op": "task_add"})
	if !strings.HasPrefix(one, "task_add (") || !strings.Contains(one, "title: string (required)") || !strings.Contains(one, "why: string (required)") {
		t.Fatalf("ops_describe task_add:\n%s", one)
	}
	found := describe(map[string]any{"query": "task"})
	// Heads: up to describeMax ops, a "(more)" line, and a people-only line.
	if heads := strings.Count(found, "\n") - strings.Count(found, "\n  "); heads > describeMax+2 || !strings.Contains(found, "task_add (") {
		t.Fatalf("ops_describe query task:\n%s", found)
	}
	if all := describe(nil); !strings.Contains(all, "task_add") || strings.Contains(all, "task_done") {
		t.Fatalf("ops_describe names: %s", all)
	}
}

// Every call counts once, by surface and actor, whether it comes as the op's
// own tool or through op_run; ops_stats lists the ops nobody called with 0.
func TestOpStatsCountsBySurface(t *testing.T) {
	for _, tc := range []struct {
		opts    Options
		surface string
	}{{Options{}, opstats.MCPStdio}, {Options{HTTP: true}, opstats.MCPHTTP}} {
		s, _ := newVault(t)
		c := connectWith(t, ops.Deps{Store: s}, tc.opts)
		if _, isErr := opRun(t, c, "events_tail", ""); isErr {
			t.Fatal("events_tail failed")
		}
		if _, isErr := opRun(t, c, "events_tail", ""); isErr {
			t.Fatal("events_tail failed")
		}
		out, isErr := opRun(t, c, "ops_stats", `{"surface":"`+tc.surface+`"}`)
		if isErr {
			t.Fatalf("ops_stats: %s", out)
		}
		var st ops.OpsStatsOutput
		if err := json.Unmarshal([]byte(out), &st); err != nil {
			t.Fatal(err)
		}
		by := map[string]ops.OpStat{}
		for _, o := range st.Ops {
			by[o.Op] = o
		}
		if got := by["events_tail"]; got.Calls != 2 || got.Surfaces[tc.surface] != 2 || got.Actors["agent"] != 2 {
			t.Errorf("%s: events_tail = %+v", tc.surface, got)
		}
		if got, ok := by["lint"]; !ok || got.Calls != 0 {
			t.Errorf("%s: lint (never called) = %+v, %v", tc.surface, got, ok)
		}
		// since after today: nothing counts.
		out, _ = opRun(t, c, "ops_stats", `{"since":"2999-01-01"}`)
		if strings.Contains(out, `"calls":2`) {
			t.Errorf("since 2999 still counts: %s", out)
		}
		// A surface that was not used reads zero.
		other := opstats.CLI
		out, _ = opRun(t, c, "ops_stats", `{"surface":"`+other+`"}`)
		if strings.Contains(out, `"calls":2`) {
			t.Errorf("surface %s counted mcp calls: %s", other, out)
		}
	}
}

// A core tool carries a short description; ops_describe {op} keeps the long.
func TestDescribeKeepsLongDescription(t *testing.T) {
	s, _ := newVault(t)
	c := connectWith(t, ops.Deps{Store: s}, Options{Tools: ToolsCore})
	list, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range list.Tools {
		if tl.Name == "io_add" && strings.Contains(tl.Description, "for_me is one you send") {
			t.Errorf("io_add tool carries the long description: %s", tl.Description)
		}
	}
	res, err := c.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: opsDescribeTool, Arguments: map[string]any{"op": "io_add"}}})
	if err != nil || res.IsError {
		t.Fatalf("ops_describe io_add: %v %+v", err, res)
	}
	if txt := res.Content[0].(mcp.TextContent).Text; !strings.Contains(txt, "for_me is one you send") {
		t.Errorf("ops_describe io_add lost the long text:\n%s", txt)
	}
}

// A query word finds an op by its stem: reviewed finds io_review, which is
// people only, so the agent is told to ask the owner.
func TestDescribeQueryStems(t *testing.T) {
	for in, want := range map[string]string{
		"reviewed": "review", "reviewing": "review", "replies": "reply", "dropped": "drop",
		"added": "add", "called": "call", "sends": "send", "access": "access", "io": "io", "is": "is",
	} {
		if got := stem(in); got != want {
			t.Errorf("stem(%q) = %q, want %q", in, got, want)
		}
	}
	s, _ := newVault(t)
	c := connectWith(t, ops.Deps{Store: s}, Options{Tools: ToolsCore})
	res, err := c.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: opsDescribeTool, Arguments: map[string]any{"query": "io reviewed"}}})
	if err != nil || res.IsError {
		t.Fatalf("ops_describe: %v %+v", err, res)
	}
	if txt := res.Content[0].(mcp.TextContent).Text; !strings.Contains(txt, "people only, ask the owner: io_review") {
		t.Errorf("query io reviewed missed io_review:\n%s", txt)
	}
}

// A receipt field the op has itself is listed once, with the op's type: the
// write fields line does not repeat it with the receipt's type.
func TestDescribeWriteFieldsOnce(t *testing.T) {
	for _, e := range ops.All() {
		if !ops.NeedsWhy(e) {
			continue
		}
		own := fieldNames(e.InputType())
		lines := strings.Split(describe(e, false), "\n")
		var meta string
		for _, l := range lines {
			if m, ok := strings.CutPrefix(l, "  + write fields: "); ok {
				meta = m
			}
		}
		for _, name := range writeMetaOrder {
			listed := strings.Contains(", "+meta, ", "+name+": ")
			if own[name] == listed {
				t.Errorf("%s: %s own=%v, in write fields=%v: %q", e.Name(), name, own[name], listed, meta)
			}
		}
	}
	if len(writeMetaOrder) != len(metaProps) {
		t.Errorf("writeMetaOrder has %d names, metaProps %d", len(writeMetaOrder), len(metaProps))
	}
	var qa ops.Entry
	for _, e := range ops.All() {
		if e.Name() == "question_answer" {
			qa = e
		}
	}
	d := describe(qa, false)
	if !strings.Contains(d, "  sources: array of object{ref*, quote}\n") || !strings.Contains(d, "  why: string (required)\n") ||
		strings.Contains(d, "write fields: why") || strings.Contains(d, "sources: array of string") {
		t.Errorf("question_answer:\n%s", d)
	}
}

func TestOpRemoved(t *testing.T) {
	defer ops.SetRemovedForTest("gone_op", "close")()
	s, _ := newVault(t)
	seedSamples(t, s)
	c := connectWith(t, ops.Deps{Store: s}, Options{})
	want := "gone_op removed in 0.5.0: use close"
	if out, isErr := opRun(t, c, "gone_op", `{}`); !isErr || !strings.Contains(out, want) {
		t.Errorf("op_run: %v %q", isErr, out)
	}
	res, err := c.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: opsDescribeTool, Arguments: map[string]any{"op": "gone_op"}}})
	if err != nil || !res.IsError || !strings.Contains(fmt.Sprint(res.Content), want) {
		t.Errorf("ops_describe: %v %+v", err, res)
	}
	if out, isErr := opRun(t, c, "io_ad", `{}`); !isErr || !strings.Contains(out, "closest: io_add") {
		t.Errorf("unknown: %v %q", isErr, out)
	}
}
