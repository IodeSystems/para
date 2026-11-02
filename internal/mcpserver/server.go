// Package mcpserver serves every op in ops.All as an MCP tool over stdio.
// It knows no op by name: a tool is built from each Entry, so an op added
// elsewhere appears with no change here. One server holds one open vault; no
// tool takes a vault argument.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/iodesystems/paradize/internal/ops"
)

// Options configures a Server.
type Options struct {
	Name    string   // server name; empty means "para"
	Version string   // server version
	Hide    []string // path prefixes to remove from error text (vault root, home)
}

// Server is an MCP server over one vault.
type Server struct {
	mcp  *server.MCPServer
	deps ops.Deps
	hide []string
}

// New registers a tool for each op in ops.All.
func New(deps ops.Deps, o Options) (*Server, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("mcpserver: new: no store")
	}
	name := o.Name
	if name == "" {
		name = "para"
	}
	s := &Server{
		mcp:  server.NewMCPServer(name, o.Version, server.WithToolCapabilities(false)),
		deps: deps,
		hide: o.Hide,
	}
	for _, e := range ops.All() {
		raw, err := inputSchema(e.InputType())
		if err != nil {
			return nil, fmt.Errorf("mcpserver: new: op %s: %w", e.Name(), err)
		}
		s.mcp.AddTool(mcp.NewToolWithRawSchema(e.Name(), e.Description(), raw), s.handler(e))
	}
	return s, nil
}

// MCP returns the underlying server, for in-process clients.
func (s *Server) MCP() *server.MCPServer { return s.mcp }

// Serve reads JSON-RPC from in and writes to out until in closes or ctx ends.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	return server.NewStdioServer(s.mcp).Listen(ctx, in, out)
}

func (s *Server) handler(e ops.Entry) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (res *mcp.CallToolResult, _ error) {
		defer func() {
			if recover() != nil {
				res = mcp.NewToolResultError(e.Name() + ": internal error")
			}
		}()
		var input json.RawMessage
		if args := req.GetRawArguments(); args != nil {
			b, err := json.Marshal(args)
			if err != nil {
				return mcp.NewToolResultError(e.Name() + ": input: not JSON"), nil
			}
			input = b
		}
		out, err := e.Invoke(ctx, s.deps, input)
		if err != nil {
			return mcp.NewToolResultError(s.clean(err)), nil
		}
		text, err := encode(out)
		if err != nil {
			return mcp.NewToolResultError(e.Name() + ": output: encode failed"), nil
		}
		res = mcp.NewToolResultText(text)
		// The Output is emitted first, then marked failed (ops.Failing).
		if f, ok := out.(ops.Failing); ok && f.Failed() != nil {
			res.IsError = true
		}
		return res, nil
	}
}

// encode matches the CLI --json encoding byte for byte.
func encode(v any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return b.String(), nil
}

// quotedPath finds a quoted absolute path, which may hold spaces.
var quotedPath = regexp.MustCompile(`(["'])/[^"'\n]*(["'])`)

// absPath finds absolute filesystem paths in error text.
var absPath = regexp.MustCompile(`(^|[\s"'(=:])(/[^\s"'):,;]+)`)

// clean returns the error text on one line with filesystem paths removed.
func (s *Server) clean(err error) string {
	msg := err.Error()
	for _, h := range s.hide {
		if h != "" && h != "/" {
			msg = strings.ReplaceAll(msg, h, "<vault>")
		}
	}
	msg = quotedPath.ReplaceAllString(msg, "${1}<path>${2}")
	msg = absPath.ReplaceAllString(msg, "${1}<path>")
	if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
		msg = msg[:i]
	}
	return msg
}
