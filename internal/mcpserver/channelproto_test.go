package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

// Claude Code does not register a channel server on protocol 2026-07-28. With
// --channel, para answers as a server older than that revision: initialize
// negotiates 2025-11-25, and server/discover and modern requests are refused,
// so the client uses the handshake. Without it, discover offers 2026-07-28.
func TestChannelNegotiatesALegacyProtocol(t *testing.T) {
	const modern = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"t","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`
	for _, channel := range []bool{false, true} {
		s, _ := newVault(t)
		srv, err := New(ops.Deps{Store: s}, Options{Channel: channel})
		if err != nil {
			t.Fatal(err)
		}
		in := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
			`{"jsonrpc":"2.0","id":2,"method":"server/discover","params":{` + modern + `}}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{` + modern + `}}`,
		}, "\n") + "\n"
		var out bytes.Buffer
		if err := srv.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
			t.Fatal(err)
		}
		type reply struct {
			Result struct {
				ProtocolVersion   string   `json:"protocolVersion"`
				SupportedVersions []string `json:"supportedVersions"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		got := map[int]reply{}
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			var msg struct {
				ID int `json:"id"`
				reply
			}
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				t.Fatalf("%s: %v", line, err)
			}
			got[msg.ID] = msg.reply
		}
		if v := got[1].Result.ProtocolVersion; v != "2025-11-25" {
			t.Errorf("channel %v: initialize protocolVersion = %q, want 2025-11-25", channel, v)
		}
		discover, list := got[2], got[3]
		if channel {
			if discover.Error == nil || list.Error == nil {
				t.Errorf("channel: a modern request was served: discover %+v, tools/list %+v", discover, list)
			}
			continue
		}
		if discover.Error != nil || !slices.Contains(discover.Result.SupportedVersions, "2026-07-28") {
			t.Errorf("no channel: discover = %+v", discover)
		}
		if list.Error != nil {
			t.Errorf("no channel: tools/list = %+v", list.Error)
		}
	}
}
