// Package raglit talks to a raglit daemon over its MCP endpoint, for the
// optional meaning search of one vault. It has no raglit Go dependency: it
// calls the daemon's `search` tool through the mcp-go client. The config is
// per machine, in the vault's state dir under $PARA_HOME, never in the vault.
// Everything a daemon returns is untrusted text.
package raglit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/parasolve/internal/atomicfile"
)

// FileName is the config file in the vault's state dir.
const FileName = "raglit.json"

// Timeout is how long a search waits for the daemon before the caller falls
// back to exact hits.
const Timeout = 1500 * time.Millisecond

// Config says where the daemon is and which project holds this vault's index.
type Config struct {
	URL     string `json:"url"`     // the daemon, such as http://127.0.0.1:7770
	Project string `json:"project"` // raglit project name; the MCP path is /mcp/<project>
	Token   string `json:"token"`   // bearer key of that project
}

// Path is where the config of the vault with this state dir lives.
func Path(stateDir string) string { return filepath.Join(stateDir, FileName) }

// Load reads the config. ok is false when this machine has none.
func Load(stateDir string) (c Config, ok bool, err error) {
	if stateDir == "" {
		return Config{}, false, nil
	}
	b, err := os.ReadFile(Path(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("raglit: load: %w", err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, false, fmt.Errorf("raglit: load %s: %w", FileName, err)
	}
	if c.URL == "" || c.Project == "" || c.Token == "" {
		return Config{}, false, fmt.Errorf("raglit: load %s: url, project and token are required", FileName)
	}
	return c, true, nil
}

// Save writes the config with mode 0600.
func Save(stateDir string, c Config) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("raglit: save: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("raglit: save: %w", err)
	}
	if err := atomicfile.Write(Path(stateDir), append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("raglit: save: %w", err)
	}
	return nil
}

// Remove deletes the config. A missing file is not an error.
func Remove(stateDir string) error {
	if err := os.Remove(Path(stateDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("raglit: remove: %w", err)
	}
	return nil
}

// MCPURL is the project's MCP endpoint.
func (c Config) MCPURL() string {
	return strings.TrimRight(c.URL, "/") + "/mcp/" + url.PathEscape(c.Project)
}

// Call runs one MCP tool and returns its text. A tool error is an error.
func (c Config) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	cl, err := client.NewStreamableHttpClient(c.MCPURL(),
		transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer " + c.Token}))
	if err != nil {
		return "", fmt.Errorf("raglit: %s: %w", tool, err)
	}
	defer func() { _ = cl.Close() }()
	if err := cl.Start(ctx); err != nil {
		return "", fmt.Errorf("raglit: %s: %w", tool, err)
	}
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "para", Version: "0"}
	if _, err := cl.Initialize(ctx, init); err != nil {
		return "", fmt.Errorf("raglit: %s: %w", tool, err)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name, req.Params.Arguments = tool, args
	res, err := cl.CallTool(ctx, req)
	if err != nil {
		return "", fmt.Errorf("raglit: %s: %w", tool, err)
	}
	var b strings.Builder
	for _, part := range res.Content {
		if t, ok := part.(mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	if res.IsError {
		return "", fmt.Errorf("raglit: %s: %w", tool, ErrTool)
	}
	return b.String(), nil
}

// ErrTool is a tool that answered with an error. Its text is not kept: it is
// untrusted and may hold paths.
var ErrTool = errors.New("the daemon refused the call")

// Hit is one fragment hit. Every string is untrusted.
type Hit struct {
	DocID   string  `json:"doc_id"`
	Rel     string  `json:"rel"`
	Title   string  `json:"title"`
	Page    int     `json:"page"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
	Origin  string  `json:"origin"` // "", identity, fields, frontmatter
	Caveat  string  `json:"caveat"`
}

// Search calls the daemon's search tool. The hits come back in rank order,
// best first; scores are not comparable with para's and are not used.
func (c Config) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	text, err := c.Call(ctx, "search", map[string]any{"query": query, "limit": limit})
	if err != nil {
		return nil, err
	}
	var out struct {
		Hits []Hit `json:"hits"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("raglit: search: %w", ErrBadReply)
	}
	return out.Hits, nil
}

// ErrBadReply is a reply that is not the JSON the search tool documents.
var ErrBadReply = errors.New("unreadable reply")

// Reason is a short, fixed-vocabulary cause for the degraded line. It never
// echoes daemon text or the token.
func Reason(err error) string {
	var nerr net.Error
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &nerr) && nerr.Timeout()):
		return "raglit timed out"
	case errors.Is(err, context.Canceled):
		return "raglit search cancelled"
	case strings.Contains(msg, "401") || strings.Contains(msg, "403") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "authorization"):
		return "raglit refused the token"
	case strings.Contains(msg, "404") || strings.Contains(msg, "no such project"):
		return "raglit has no such project"
	case errors.Is(err, ErrBadReply):
		return "raglit reply unreadable"
	case errors.Is(err, ErrTool):
		return "raglit refused the search"
	case strings.Contains(msg, "refused") || strings.Contains(msg, "no such host") || strings.Contains(msg, "unreachable") || strings.Contains(msg, "eof"):
		return "raglit unreachable"
	}
	return "raglit failed"
}

// Clean makes untrusted daemon text safe to print: control characters out,
// whitespace collapsed, cut to max runes.
func Clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + "..."
	}
	return s
}
