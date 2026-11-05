package mcpserver

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/ops"
)

// An image attachment comes back as image content, with the rest as text
// that holds no data field; the read logs an egress event with the path.
func TestAttachmentReadImage(t *testing.T) {
	s, _ := newVault(t)
	seedSamples(t, s)
	img := []byte("\x89PNG\r\n\x1a\n synthetic")
	a, err := s.AttachmentAdd(t.Context(), "task-sample", "20261101-090000-board.png", img)
	if err != nil {
		t.Fatal(err)
	}
	c := connect(t, ops.Deps{Store: s})
	res := call(t, c, "attachment_read", `{"file":"`+a.Path+`"}`)
	if res.IsError || len(res.Content) != 2 {
		t.Fatalf("result = %+v", res)
	}
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok || strings.Contains(text.Text, `"data"`) || !strings.Contains(text.Text, a.Path) {
		t.Fatalf("text = %+v", res.Content[0])
	}
	im, ok := res.Content[1].(mcp.ImageContent)
	if !ok || im.MIMEType != "image/png" || im.Data != base64.StdEncoding.EncodeToString(img) {
		t.Fatalf("image = %+v", res.Content[1])
	}
	evs, err := s.Events(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if e := evs[0]; e.Op != "egress" || e.Doc != a.Path || e.To != "agent:mcp" {
		t.Fatalf("event = %+v", e)
	}
}
