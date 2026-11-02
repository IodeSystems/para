package mcpserver

import (
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

// StaleMessage is the tool error returned after an upgrade.
const StaleMessage = "para was upgraded after this MCP server started, so this server is out of date and may misread newer documents. Ask the user to restart the MCP server (in Claude Code: /mcp, then reconnect para), then retry. Nothing was changed."

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
