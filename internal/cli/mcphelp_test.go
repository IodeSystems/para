package cli

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/client"
)

// tool calls an MCP tool and decodes its text result.
func tool[T any](t *testing.T, c *client.Client, name string, args map[string]any) T {
	t.Helper()
	text, isErr := callText(t, c, name, args)
	if isErr {
		t.Fatalf("%s: %s", name, text)
	}
	var out T
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, text)
	}
	return out
}
