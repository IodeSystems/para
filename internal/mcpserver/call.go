package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/opstats"
	"github.com/iodesystems/para/internal/upgrade"
)

// Tool listing modes (Options.Tools, install.properties mcp.tools). Every op
// an agent may call stays callable in both: op_run runs an op that is not
// listed, through the same path as its own tool.
const (
	ToolsAll  = "all"  // a tool per op
	ToolsCore = "core" // the tools in Core
)

// Core names the tools a core listing shows (owner 2026-11-11: exactly
// these 15). The rest are reached with ops_describe and op_run.
var Core = []string{
	"search", "show", "list", "add", "edit", "link", "tag",
	"close", "later", "reopen", "work", "undo", "thread",
	opsDescribeTool, opRunTool,
}

const (
	opRunTool       = "op_run"
	opsDescribeTool = "ops_describe"
	describeMax     = 10
)

// listed reports whether a tool appears in tools/list in mode.
func listed(mode, name string) bool {
	return mode != ToolsCore || slices.Contains(Core, name)
}

// handler is the tool of op e: it decodes the arguments and calls call.
func (s *Server) handler(e ops.Entry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		s.presence.call(ctx)
		var input json.RawMessage
		if args := req.GetRawArguments(); args != nil {
			b, err := json.Marshal(args)
			if err != nil {
				return mcp.NewToolResultError(e.Name() + ": input: not JSON"), nil
			}
			input = b
		}
		return s.call(ctx, e, input), nil
	}
}

// call runs op e with input. It is the one path from MCP into an op: a tool
// of its own and op_run both come here, so the upgrade check, the scope
// check, the receipt fields, and the write accounting are the same for both.
// The person check and the gate sit in the op itself. A receipt names e.
func (s *Server) call(ctx context.Context, e ops.Entry, input json.RawMessage) (res *mcp.CallToolResult) {
	defer func() {
		if r := recover(); r != nil {
			s.logPanic(e.Name(), r)
			res = mcp.NewToolResultError(e.Name() + ": internal error")
		}
	}()
	info := s.deps.Store.Info()
	opstats.Record(info.StateDir, e.Name(), s.surface, info.Actor)
	refuse, note, upgraded := s.upgraded.verdict(e.Name(), s.version, s.installed)
	if refuse != "" {
		return mcp.NewToolResultError(refuse)
	}
	if s.allow != nil {
		if err := s.allow(e.Name(), e.Risk() == ops.RiskRead); err != nil {
			return mcp.NewToolResultError(e.Name() + ": " + err.Error())
		}
	}
	if s.write != nil && e.Risk() != ops.RiskRead && !IsWait(e.Name()) {
		defer s.write(e.Name())()
	}
	// The vault guard after an upgrade: the new para may have raised the
	// vault's format, which para mcp refuses at start.
	if upgraded {
		if err := upgrade.Check(s.deps.Store); err != nil {
			return mcp.NewToolResultError(s.clean(fmt.Errorf("%s: %w", e.Name(), err)))
		}
	}
	if ops.NeedsWhy(e) {
		var err error
		if ctx, input, err = takeWriteMeta(ctx, e, input); err != nil {
			return mcp.NewToolResultError(e.Name() + ": " + err.Error())
		}
	}
	out, err := e.Invoke(ctx, s.deps, input)
	if err != nil {
		return mcp.NewToolResultError(s.clean(err))
	}
	// A decision made before the id was tracked is found now.
	if s.track(out) && s.runCtx.Load() != nil {
		s.check(ctx)
	}
	// An image (attachment_read) goes as image content, the rest as text.
	var img *mcp.ImageContent
	if im, ok := out.(ops.Imaging); ok {
		if mime, data, rest, has := im.MCPImage(); has {
			c := mcp.NewImageContent(base64.StdEncoding.EncodeToString(data), mime)
			img, out = &c, rest
		}
	}
	text, err := encode(out)
	if err != nil {
		return mcp.NewToolResultError(e.Name() + ": output: encode failed")
	}
	res = mcp.NewToolResultText(text)
	if img != nil {
		res.Content = append(res.Content, *img)
	}
	if note != "" {
		res.Content = append(res.Content, mcp.NewTextContent(note))
	}
	for _, n := range s.upgradeNotes(ctx) {
		res.Content = append(res.Content, mcp.NewTextContent(n))
	}
	// The Output is emitted first, then marked failed (ops.Failing).
	if f, ok := out.(ops.Failing); ok && f.Failed() != nil {
		res.IsError = true
	}
	return res
}

// OpRunInput is the input of op_run.
type OpRunInput struct {
	Op    string          `json:"op"`
	Input json.RawMessage `json:"input,omitempty"`
}

// OpsDescribeInput is the input of ops_describe.
type OpsDescribeInput struct {
	Query string `json:"query,omitempty"`
	Op    string `json:"op,omitempty"`
}

func (s *Server) addMetaTools() error {
	run := schema{"type": "object", "additionalProperties": false, "required": []string{"op"}, "properties": map[string]any{
		"op":    schema{"type": "string", "description": "Op name, from ops_describe"},
		"input": schema{"type": "object", "description": "Its fields, and why etc. on a write"},
	}}
	desc := schema{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"query": schema{"type": "string", "description": "Words to match in names and text"},
		"op":    schema{"type": "string", "description": "One op name: its full description"},
	}}
	for _, t := range []struct {
		name, desc string
		in         schema
		h          server.ToolHandlerFunc
	}{
		{opRunTool, "Run any agent op by name with its input, through the same checks, gate, and receipt as its own tool. Find ops and fields with ops_describe", run, s.opRun},
		{opsDescribeTool, "Describe ops: name, risk, one line, fields as name: type [enum] (required). query finds up to 10; op gives one in full; neither lists every name", desc, s.opsDescribe},
	} {
		raw, err := json.Marshal(t.in)
		if err != nil {
			return err
		}
		s.mcp.AddTool(mcp.NewToolWithRawSchema(t.name, t.desc, raw), t.h)
	}
	return nil
}

func decodeArgs(req mcp.CallToolRequest, v any) error {
	args := req.GetRawArguments()
	if args == nil {
		return nil
	}
	b, err := json.Marshal(args)
	if err != nil {
		return errors.New("input: not JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	return nil
}

// opRun runs the op it names through call.
func (s *Server) opRun(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.presence.call(ctx)
	var in OpRunInput
	if err := decodeArgs(req, &in); err != nil {
		return mcp.NewToolResultError(opRunTool + ": " + err.Error()), nil
	}
	if bytes.Equal(bytes.TrimSpace(in.Input), []byte("null")) {
		in.Input = nil
	}
	if in.Op == requestWaitTool {
		var args any
		if len(in.Input) > 0 {
			if err := json.Unmarshal(in.Input, &args); err != nil {
				return mcp.NewToolResultError(opRunTool + ": input: not JSON"), nil
			}
		}
		return s.requestWait(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: requestWaitTool, Arguments: args}})
	}
	e, err := s.resolve(in.Op)
	if err != nil {
		return mcp.NewToolResultError(opRunTool + ": " + err.Error()), nil
	}
	return s.call(ctx, e, in.Input), nil
}

// resolve finds the op an agent may run here by name. A refusal says why: a
// person's op, an old name, a write on a read-only server, or the closest
// names.
func (s *Server) resolve(name string) (ops.Entry, error) {
	if e, ok := s.ops[name]; ok {
		return e, nil
	}
	if name == "" {
		return nil, errors.New("op is required (ops_describe lists ops)")
	}
	if to := ops.AliasOf(name); to != "" {
		return nil, fmt.Errorf("%s is an old name: call %s", name, to)
	}
	for _, e := range ops.All() {
		if e.Name() != name {
			continue
		}
		if e.PersonOnly() {
			return nil, fmt.Errorf("%s: %w", name, ops.ErrPersonOnly)
		}
		return nil, fmt.Errorf("%s changes documents, and this server is read-only", name)
	}
	msg := "unknown op " + name
	if c := s.closest(name, 3); len(c) > 0 {
		msg += "; closest: " + strings.Join(c, ", ")
	}
	return nil, errors.New(msg + " (ops_describe finds ops)")
}

// closest lists up to n op names nearest to name: names that contain it or
// that it contains first, then by edit distance.
func (s *Server) closest(name string, n int) []string {
	type cand struct {
		name string
		d    int
	}
	var cs []cand
	for k := range s.ops {
		d := editDistance(name, k)
		if strings.Contains(k, name) || strings.Contains(name, k) {
			d = 0
		}
		if d <= max(3, len(name)/2) {
			cs = append(cs, cand{k, d})
		}
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].d != cs[j].d {
			return cs[i].d < cs[j].d
		}
		return cs[i].name < cs[j].name
	})
	var out []string
	for i := 0; i < len(cs) && i < n; i++ {
		out = append(out, cs[i].name)
	}
	return out
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// opsDescribe answers ops_describe in the compact form.
func (s *Server) opsDescribe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.presence.call(ctx)
	var in OpsDescribeInput
	if err := decodeArgs(req, &in); err != nil {
		return mcp.NewToolResultError(opsDescribeTool + ": " + err.Error()), nil
	}
	if in.Op != "" {
		e, err := s.resolve(in.Op)
		if err != nil {
			return mcp.NewToolResultError(opsDescribeTool + ": " + err.Error()), nil
		}
		return mcp.NewToolResultText(describe(e, true)), nil
	}
	names := make([]string, 0, len(s.ops))
	for k := range s.ops {
		names = append(names, k)
	}
	sort.Strings(names)
	q := strings.ToLower(strings.TrimSpace(in.Query))
	if q == "" {
		return mcp.NewToolResultText("ops (ops_describe {op} for one, {query} to search): " + strings.Join(names, ", ")), nil
	}
	words := strings.FieldsFunc(q, func(r rune) bool { return r == ' ' || r == '_' || r == ',' })
	type hit struct {
		name  string
		score int
	}
	var hits []hit
	for _, n := range names {
		e := s.ops[n]
		desc := strings.ToLower(ops.LongDescription(e))
		score := 0
		if n == q {
			score += 100
		}
		for _, w := range words {
			w = stem(w)
			if strings.Contains(n, w) {
				score += 10
			}
			if strings.Contains(desc, w) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, hit{n, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	people := personOps(words)
	if len(hits) == 0 && people == "" {
		msg := "no op matches " + strconv.Quote(in.Query)
		if c := s.closest(q, 3); len(c) > 0 {
			msg += "; closest: " + strings.Join(c, ", ")
		}
		return mcp.NewToolResultText(msg), nil
	}
	var b strings.Builder
	for i, h := range hits {
		if i == describeMax {
			fmt.Fprintf(&b, "(%d more: narrow the query)\n", len(hits)-describeMax)
			break
		}
		b.WriteString(describe(s.ops[h.name], false))
	}
	b.WriteString(people)
	return mcp.NewToolResultText(b.String()), nil
}

// personOps is a line naming the people-only ops whose name holds every
// query word, by its stem: an agent cannot run them, so it asks the owner.
func personOps(words []string) string {
	var names []string
	for _, e := range ops.All() {
		if !e.PersonOnly() || ops.AliasOf(e.Name()) != "" {
			continue
		}
		all := len(words) > 0
		for _, w := range words {
			all = all && strings.Contains(e.Name(), stem(w))
		}
		if all {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "people only, ask the owner: " + strings.Join(names, ", ") + "\n"
}

// stem cuts a common English ending off a query word so that it matches op
// names and text as a substring: reviewed and reviewing find io_review,
// replies finds rfx_reply, dropped finds drop. A stem keeps 3 letters at least.
func stem(w string) string {
	cut := func(suffix, with string) (string, bool) {
		if r, ok := strings.CutSuffix(w, suffix); ok && len(r)+len(with) >= 3 {
			return r + with, true
		}
		return w, false
	}
	if r, ok := cut("ies", "y"); ok {
		return r
	}
	for _, suffix := range []string{"ing", "ed"} {
		if r, ok := cut(suffix, ""); ok {
			if n := len(r); n > 3 && r[n-1] == r[n-2] && !strings.ContainsRune("aeiousl", rune(r[n-1])) {
				r = r[:n-1] // dropped, planned; not added, called
			}
			return r
		}
	}
	if !strings.HasSuffix(w, "ss") {
		if r, ok := cut("s", ""); ok {
			return r
		}
	}
	return w
}

// describe is one op in the compact form. full keeps the whole description;
// otherwise it is cut to its first sentence.
func describe(e ops.Entry, full bool) string {
	var b strings.Builder
	d := e.Description()
	if full {
		d = ops.LongDescription(e)
	} else {
		d = firstSentence(d)
	}
	fmt.Fprintf(&b, "%s (%s): %s\n", e.Name(), e.Risk(), d)
	write := ops.NeedsWhy(e)
	for _, f := range describeFields(e.InputType(), false) {
		if write && strings.HasPrefix(f, "why: ") && !strings.HasSuffix(f, " (required)") {
			f += " (required)" // the tool schema requires why on every write
		}
		b.WriteString("  " + f + "\n")
	}
	if write {
		if m := writeFieldWords(e); m != "" {
			b.WriteString("  + write fields: " + m + "\n")
		}
	}
	return b.String()
}

// writeMetaOrder is the order the receipt fields (metaProps) are listed in.
var writeMetaOrder = []string{"why", "sources", "verify", "confidence", "request", "input", "not_duplicate"}

// writeFieldWords lists the receipt fields a write op takes beside its own
// input, each with its type. A field the op has itself is listed with the
// op's fields, with the op's type, and not again here: an agent reading
// "sources" here would take the receipt's type for the op's.
func writeFieldWords(e ops.Entry) string {
	own := fieldNames(e.InputType())
	var out []string
	for _, name := range writeMetaOrder {
		if own[name] {
			continue
		}
		w := name + ": " + schemaWord(metaProps[name], nil)
		if name == "why" {
			w += " (required)"
		}
		out = append(out, w)
	}
	return strings.Join(out, ", ")
}

func firstSentence(d string) string {
	if i := strings.Index(d, ". "); i >= 0 {
		d = d[:i]
	}
	const most = 200
	if len(d) > most {
		cut := most
		for cut > 0 && d[cut]&0xC0 == 0x80 {
			cut--
		}
		d = d[:cut] + "..."
	}
	return d
}

// describeFields lists the fields of an input struct in order, as
// name: type [enum] (required), or, when short, as name or name* (required).
func describeFields(t reflect.Type, short bool) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				out = append(out, describeFields(ft, short)...)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		required := !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero")
		switch {
		case short && required:
			out = append(out, name+"*")
		case short:
			out = append(out, name)
		case required:
			out = append(out, name+": "+typeWord(f.Type)+" (required)")
		default:
			out = append(out, name+": "+typeWord(f.Type))
		}
	}
	return out
}

// typeWord is a field's type in a few words: string [a|b], array of
// object{id*, text}.
func typeWord(t reflect.Type) string {
	sc, err := typeSchema(t, 0)
	if err != nil {
		return "any"
	}
	return schemaWord(sc, t)
}

func schemaWord(sc schema, t reflect.Type) string {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	typ, _ := sc["type"].(string)
	switch typ {
	case "":
		return "any"
	case "string":
		if e, ok := sc["enum"].([]string); ok && len(e) > 0 {
			return "string [" + strings.Join(e, "|") + "]"
		}
		return "string"
	case "array":
		var et reflect.Type
		if t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
			et = t.Elem()
		}
		items, _ := sc["items"].(schema)
		return "array of " + schemaWord(items, et)
	case "object":
		if t == nil || t.Kind() != reflect.Struct {
			return "object"
		}
		return "object{" + strings.Join(describeFields(t, true), ", ") + "}"
	}
	return typ
}
