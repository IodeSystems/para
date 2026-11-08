// Package presence keeps which agents are connected to a vault on this
// machine. Each `para mcp` process writes one file in the vault's state
// directory ($PARA_HOME/vaults/<id>/agents/<pid>.json) and removes it when it
// exits. It is a fact about this machine, so it is never in the vault and a
// sync tool never carries it. A record holds the actor, the client's name and
// version, times, and how the session can be pushed: no vault text.
package presence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/iodesystems/para/internal/atomicfile"
)

// Dir is the directory of presence files in a state directory.
const Dir = "agents"

// How a session can be pushed. Every session gets notifications/message;
// a --channel session also gets Claude Code channel events.
const (
	PushMessage = "message"
	PushChannel = "channel"
)

// Record is one connected MCP session.
type Record struct {
	PID           int      `json:"pid"`
	Actor         string   `json:"actor"`
	Client        string   `json:"client,omitempty"`
	ClientVersion string   `json:"client_version,omitempty"`
	StartedAt     string   `json:"started_at"`             // RFC 3339
	LastCallAt    string   `json:"last_call_at,omitempty"` // RFC 3339, the last tool call
	Push          []string `json:"push"`
}

// File is the presence file of a process.
func File(stateDir string, pid int) string {
	return filepath.Join(stateDir, Dir, strconv.Itoa(pid)+".json")
}

// Write replaces the record of r.PID in stateDir.
func Write(stateDir string, r Record) error {
	if stateDir == "" {
		return errors.New("presence: write: no state directory: presence lives on this machine, outside the vault")
	}
	if r.PID <= 0 {
		return fmt.Errorf("presence: write: pid %d", r.PID)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("presence: write: %w", err)
	}
	path := File(stateDir, r.PID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("presence: write: %w", err)
	}
	if err := atomicfile.Write(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("presence: write: %w", err)
	}
	return nil
}

// Remove deletes the record of pid. A record that is not there is no error.
func Remove(stateDir string, pid int) error {
	if stateDir == "" {
		return nil
	}
	if err := os.Remove(File(stateDir, pid)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("presence: remove: %w", err)
	}
	return nil
}

// Read reads every record in stateDir, sorted by actor, then pid. No state
// directory, or none written yet, is no record. A file that does not parse,
// or whose name is not its pid, is skipped.
func Read(stateDir string) ([]Record, error) {
	if stateDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, Dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("presence: read: %w", err)
	}
	var out []Record
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if e.IsDir() || !ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(stateDir, Dir, e.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue // the session exited while we read
		}
		if err != nil {
			return nil, fmt.Errorf("presence: read: %w", err)
		}
		var r Record
		if json.Unmarshal(b, &r) != nil || strconv.Itoa(r.PID) != name {
			continue
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Record) int {
		if c := strings.Compare(a.Actor, b.Actor); c != 0 {
			return c
		}
		return a.PID - b.PID
	})
	return out, nil
}
