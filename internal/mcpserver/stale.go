package mcpserver

import (
	"fmt"
	"os"
	"strings"
)

// A long-lived `para mcp` keeps running the binary it started with. After an
// upgrade it can lack types the vault now holds, and a tool then reports a
// document as missing while the CLI finds it. The server compares its binary
// on disk with the one it started from at the start of each tool call and, if
// they differ, refuses with a message that tells the user to restart the MCP
// server. It does not re-exec: the client owns the stdio pipes and the session
// state, and a restart is the client's to do.
type binaryStamp struct {
	path string
	info os.FileInfo
}

// StaleMessage is the tool error returned after an upgrade, without versions.
const StaleMessage = "para was upgraded after this MCP server started. In Claude Code: run /mcp, then reconnect para. Elsewhere: restart this MCP server."

// staleMessage names the version this server runs and the version now on disk,
// when installed can say it, and what to do. After the reconnect, the changes
// tool says what is new.
func staleMessage(running string, installed func() string) string {
	if running == "" {
		running = "unknown"
	}
	now := "unknown"
	if installed != nil {
		if v := strings.TrimSpace(installed()); v != "" && !strings.ContainsAny(v, "\r\n") {
			now = v
		}
	}
	return fmt.Sprintf("para was upgraded after this MCP server started: it runs %s, the installed binary is %s. In Claude Code: run /mcp, then reconnect para. Elsewhere: restart this MCP server. Then call changes to see what is new.", running, now)
}

// ExecutablePath is the path of the running binary. Linux appends " (deleted)"
// to the path of a binary that an upgrade replaced.
func ExecutablePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(p, " (deleted)")
}

func stampBinary(path string) *binaryStamp {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return &binaryStamp{path: path, info: info}
}

// changed reports whether the file at the path is no longer the one stamped.
func (b *binaryStamp) changed() bool {
	if b == nil {
		return false
	}
	cur, err := os.Stat(b.path)
	if err != nil {
		return true
	}
	return !os.SameFile(b.info, cur) || cur.Size() != b.info.Size() || !cur.ModTime().Equal(b.info.ModTime())
}
