package mcpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// HandoffVersion is the shape of Handoff this binary writes and reads. A
// binary that answers the probe (para mcp --handoff-probe) with another
// version, or not at all, does not get the session: the old server keeps
// serving with the reconnect message.
const HandoffVersion = 1

// HandoffEnv names the handoff file in the environment of the exec'd binary.
const HandoffEnv = "PARA_MCP_HANDOFF"

// Handoff is the stdio session a replaced para mcp hands to the binary that
// replaced it, on the same stdin and stdout. It holds protocol state only,
// never vault text.
type Handoff struct {
	V    int    `json:"v"`
	From string `json:"from"` // the version the old process ran
	// Init is the client's initialize params as it sent them; the new
	// process replays them to its own server and drops the answer.
	Init json.RawMessage `json:"init,omitempty"`
	// Protocol is the version the old server negotiated.
	Protocol string `json:"protocol,omitempty"`
	// Initialized: the client sent notifications/initialized.
	Initialized bool   `json:"initialized,omitempty"`
	LogLevel    string `json:"log_level,omitempty"`
	// Tools is the old server's listing mode, for the log when it differs.
	Tools string `json:"tools,omitempty"`
	// Args is the old process's argv: actor, vault, tools, and the rest of
	// its flags. The exec passes the same argv; the new process checks it.
	Args []string `json:"args"`
	// Leftover is what the old process read from stdin and did not give its
	// server: the new process handles it before anything it reads itself.
	Leftover []byte `json:"leftover,omitempty"`
}

// EncodeHandoff is the file form of h.
func EncodeHandoff(h Handoff) ([]byte, error) {
	h.V = HandoffVersion
	b, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: handoff: %w", err)
	}
	return b, nil
}

// DecodeHandoff reads a handoff file. One of another version is refused.
func DecodeHandoff(b []byte) (Handoff, error) {
	var h Handoff
	if err := json.Unmarshal(b, &h); err != nil {
		return Handoff{}, fmt.Errorf("mcpserver: handoff: %w", err)
	}
	if h.V != HandoffVersion {
		return Handoff{}, fmt.Errorf("mcpserver: handoff: version %d, want %d", h.V, HandoffVersion)
	}
	if len(h.Init) > 0 && !json.Valid(h.Init) {
		return Handoff{}, errors.New("mcpserver: handoff: init is not JSON")
	}
	return h, nil
}

// writeHandoff writes h to a new file in dir, mode 600, and returns its path.
func writeHandoff(dir string, h Handoff) (string, error) {
	b, err := EncodeHandoff(h)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mcpserver: handoff: %w", err)
	}
	f, err := os.CreateTemp(dir, "mcp-handoff-*.json")
	if err != nil {
		return "", fmt.Errorf("mcpserver: handoff: %w", err)
	}
	// CreateTemp makes the file 0600.
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("mcpserver: handoff: %w", werr)
	}
	return f.Name(), nil
}

// TakeHandoff reads and removes the handoff file HandoffEnv names, and
// clears HandoffEnv. It returns nil when none is named. A file outside dir,
// not a regular file of mode 600, or of another argv is refused (and still
// removed when it is in dir).
func TakeHandoff(dir string, args []string) (*Handoff, error) {
	path := os.Getenv(HandoffEnv)
	if path == "" {
		return nil, nil
	}
	_ = os.Unsetenv(HandoffEnv)
	path = filepath.Clean(path)
	if dir == "" || filepath.Dir(path) != filepath.Clean(dir) {
		return nil, fmt.Errorf("mcpserver: handoff: %s is not in %s", path, dir)
	}
	defer func() { _ = os.Remove(path) }()
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: handoff: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("mcpserver: handoff: %s: want a regular file of mode 600", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: handoff: %w", err)
	}
	h, err := DecodeHandoff(b)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(h.Args, args) {
		return nil, errors.New("mcpserver: handoff: the old process ran with other arguments")
	}
	return &h, nil
}
