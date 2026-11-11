package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/upgrade"
)

// A long-lived `para mcp` keeps running the binary it started with, and the
// client keeps the tool list it got at initialize. On Linux and macOS a stdio
// server with Options.Reexec execs the new binary at a quiet moment and hands
// it the session (reexec.go). Until then, and when the new binary does not
// take the session, the rules below apply.
//
// After an upgrade the old server keeps serving when that is safe. para mcp
// refuses a vault newer than this binary at start (upgrade.Check); once the
// binary on disk changed, the server runs that check before every tool call
// too, as the new binary can raise the vault's format_version. It asks the new
// binary for its Surface once per install (InstalledSurface) and compares:
//   - format or document types differ, or the new binary cannot say: every
//     call is refused with the reconnect message, as an old binary can lack
//     types the vault now holds;
//   - same format and types: a tool whose input schema changed or that the new
//     binary removed is refused with the reconnect message; the others keep
//     serving. The first answer after each install carries one note: the
//     changed and new tools, or, when no tool differs, that the tools keep
//     working and a reconnect brings the fixes inside them.
type binaryStamp struct {
	path string
	info os.FileInfo
}

// Surface is what a binary's MCP server looks like to a vault and to a
// client: the vault format it writes, the document types it reads, and a hash
// of each tool's input schema. `para mcp --surface` prints it as JSON.
type Surface struct {
	Version string            `json:"version,omitempty"`
	Format  int               `json:"format"`
	Types   []string          `json:"types"`
	Tools   map[string]string `json:"tools"`
}

// CurrentSurface is the Surface of this binary, over every agent tool.
func CurrentSurface(version string) (Surface, error) {
	s := Surface{Version: version, Format: upgrade.Latest(), Types: strs(model.Types), Tools: map[string]string{}}
	for _, e := range ops.ForAgents() {
		raw, err := toolSchema(e)
		if err != nil {
			return Surface{}, fmt.Errorf("mcpserver: surface: op %s: %w", e.Name(), err)
		}
		sum := sha256.Sum256(raw)
		s.Tools[e.Name()] = hex.EncodeToString(sum[:])
	}
	return s, nil
}

// upgradeState is what the server decided about the binary now on disk.
type upgradeState struct {
	mu      sync.Mutex
	binary  *binaryStamp
	running Surface
	ask     func() (Surface, error)
	seen    os.FileInfo // the file the verdict below is for; nil: none yet
	all     bool        // refuse every tool
	changed map[string]bool
	note    string // said once, on the first answer after the upgrade
	noted   bool
	now     string // the installed version, for messages
}

// verdict returns the refusal for tool name, or "" when it may run, a note to
// add to its answer, said once per upgrade, and whether the binary changed.
func (u *upgradeState) verdict(name, running string, installed func() string) (refuse, note string, upgraded bool) {
	if u == nil || u.binary == nil {
		return "", "", false
	}
	cur, err := os.Stat(u.binary.path)
	if err == nil && !u.binary.differs(cur) {
		return "", "", false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.seen == nil || err != nil || !os.SameFile(u.seen, cur) || cur.Size() != u.seen.Size() || !cur.ModTime().Equal(u.seen.ModTime()) {
		u.decide(cur, err)
	}
	if u.all || u.changed[name] {
		if u.now == "" {
			u.now = "unknown"
			if installed != nil {
				if v := clean(installed()); v != "" {
					u.now = v
				}
			}
		}
		return name + ": " + staleMessage(running, u.now, u.all), "", true
	}
	if u.note != "" && !u.noted {
		u.noted = true
		return "", u.note, true
	}
	return "", "", true
}

// decide compares the binary now on disk with the running one. It runs once
// per installed file.
func (u *upgradeState) decide(cur os.FileInfo, statErr error) {
	u.seen, u.all, u.changed, u.note, u.noted, u.now = cur, true, nil, "", false, ""
	if statErr != nil || u.ask == nil {
		return
	}
	got, err := u.ask()
	if err != nil || got.Format != u.running.Format || !slices.Equal(got.Types, u.running.Types) {
		return
	}
	u.all = false
	if v := clean(got.Version); v != "" {
		u.now = v
	}
	u.changed = map[string]bool{}
	var changed, added []string
	for name, h := range u.running.Tools {
		if got.Tools[name] != h {
			u.changed[name] = true
			changed = append(changed, name)
		}
	}
	for name := range got.Tools {
		if _, has := u.running.Tools[name]; !has {
			added = append(added, name)
		}
	}
	if len(changed) == 0 && len(added) == 0 {
		// Fixes inside the ops reach the agent only after a reconnect.
		to := u.now
		if to == "" {
			to = "a new version"
		}
		u.note = "note: para was upgraded to " + to + "; your tools are unchanged and keep working. Reconnect when convenient to get fixes inside the tools: in Claude Code run /mcp, then reconnect para; elsewhere restart this MCP server. Then call changes."
		return
	}
	slices.Sort(changed)
	slices.Sort(added)
	var b strings.Builder
	b.WriteString("note: para was upgraded after this MCP server started; this server keeps serving.")
	if len(changed) > 0 {
		b.WriteString(" Changed tools refuse until you reconnect: " + strings.Join(changed, ", ") + ".")
	}
	if len(added) > 0 {
		b.WriteString(" New tools appear after you reconnect: " + strings.Join(added, ", ") + ".")
	}
	b.WriteString(" When convenient: in Claude Code run /mcp, then reconnect para; elsewhere restart this MCP server. Then call changes.")
	u.note = b.String()
}

// noticeMax caps a notice line, which goes into instructions and answers.
const noticeMax = 240

// clean makes a notice one line of at most noticeMax bytes (cut at a rune
// boundary), or "" for text with a line break.
func clean(v string) string {
	v = strings.TrimSpace(v)
	if strings.ContainsAny(v, "\r\n") {
		return ""
	}
	if len(v) > noticeMax {
		cut := noticeMax
		for cut > 0 && !utf8.RuneStart(v[cut]) {
			cut--
		}
		v = v[:cut]
	}
	return v
}

// StaleMessage is the tool error returned after an upgrade, without versions.
const StaleMessage = "para was upgraded after this MCP server started. In Claude Code: run /mcp, then reconnect para. Elsewhere: restart this MCP server. " + httpHint

// httpHint names the transport that needs no reconnect after an upgrade.
const httpHint = "Ask the owner to reconnect, or to switch this agent to the HTTP transport (para setup --agent claude registers it while the para service runs), which picks up an upgrade by itself."

// staleMessage names the version this server runs and the version now on disk,
// and what to do. all says the whole server is refused, not only this tool.
// After the reconnect, the changes tool says what is new.
func staleMessage(running, now string, all bool) string {
	if running == "" {
		running = "unknown"
	}
	if now == "" {
		now = "unknown"
	}
	what := "this server cannot serve it"
	if !all {
		what = "this tool changed"
	}
	return fmt.Sprintf("para was upgraded after this MCP server started: it runs %s, the installed binary is %s, and %s. In Claude Code: run /mcp, then reconnect para. Elsewhere: restart this MCP server. Then call changes to see what is new. %s", running, now, what, httpHint)
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

// differs reports whether cur is no longer the file stamped.
func (b *binaryStamp) differs(cur os.FileInfo) bool {
	return !os.SameFile(b.info, cur) || cur.Size() != b.info.Size() || !cur.ModTime().Equal(b.info.ModTime())
}
