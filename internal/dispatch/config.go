// Package dispatch starts a configured command when a request can be claimed
// on this install. It is the one place para starts a process on its own, and it
// does so only when a person has put a command in $PARA_HOME/vaults/<id>/agent.json
// on this machine. That file is never in the vault, so nothing a vault holds,
// and nothing a sync tool copies, can name a command.
//
// What reaches the process is fixed: the argv from the file (a list, never run
// through a shell), the working directory from the file, and an environment
// of the server's own plus PARA_REQUEST, PARA_HOME, and PARA_VAULT. PARA_REQUEST
// is a request id checked against a strict pattern. No vault text, title, or
// ask is passed: the command claims the request itself and reads what it needs
// through para.
//
// Dispatch spends tokens when the command is an agent. Two limits bound it, a
// number of runs at once and a number of starts a day, and the count survives a
// restart of the server.
package dispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FileName is the per-machine config file in the vault's state directory.
const FileName = "agent.json"

// Limits.
const (
	DefaultMaxConcurrent = 1
	DefaultDailyLimit    = 10
	MaxMaxConcurrent     = 8
	MaxDailyLimit        = 1000
)

// Config is agent.json.
type Config struct {
	// Command is the argv to run: the program, then its arguments. It is a list,
	// not a shell string.
	Command []string `json:"command"`
	// MaxConcurrent is how many runs may be alive at once; default 1.
	MaxConcurrent int `json:"max_concurrent,omitempty"`
	// DailyLimit is how many runs may start in a local calendar day; default 10.
	DailyLimit int `json:"daily_limit,omitempty"`
	// Cwd is the working directory of the command: an absolute path. Default is
	// the "work" directory in the vault's state directory.
	Cwd string `json:"cwd,omitempty"`
}

// ConfigPath is the config file of a vault whose state directory is stateDir.
func ConfigPath(stateDir string) string { return filepath.Join(stateDir, FileName) }

// WorkDir is the default working directory.
func WorkDir(stateDir string) string { return filepath.Join(stateDir, "work") }

// LoadConfig reads agent.json from stateDir. It returns nil and no error when
// there is no file: para then starts nothing. A file that others can write is
// refused, because whoever writes it chooses what runs.
func LoadConfig(stateDir string) (*Config, error) {
	path := ConfigPath(stateDir)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dispatch: config: %w", err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("dispatch: config: %w", err)
	}
	if st.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("dispatch: config: %s is writable by others (mode %o): whoever can write it chooses what runs; chmod 600 it", FileName, st.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("dispatch: config: %w", err)
	}
	return ParseConfig(data, stateDir)
}

// ParseConfig reads and checks an agent.json document.
func ParseConfig(data []byte, stateDir string) (*Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		var ute *json.UnmarshalTypeError
		if errors.As(err, &ute) && ute.Field == "command" {
			return nil, fmt.Errorf("dispatch: %s: command must be a list of arguments such as [\"claude\", \"-p\"], not a shell string", FileName)
		}
		return nil, fmt.Errorf("dispatch: %s: %w", FileName, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("dispatch: %s: more than one JSON value", FileName)
	}
	if err := c.normalize(stateDir); err != nil {
		return nil, fmt.Errorf("dispatch: %s: %w", FileName, err)
	}
	return &c, nil
}

func (c *Config) normalize(stateDir string) error {
	if len(c.Command) == 0 || c.Command[0] == "" {
		return errors.New(`command is required: a list such as ["claude", "-p"]`)
	}
	for _, a := range c.Command {
		if strings.ContainsRune(a, 0) {
			return errors.New("command holds a NUL byte")
		}
	}
	// A shell string put in the first slot, such as "claude -p", is a mistake
	// that would never run; say so. A path with a space in it is fine.
	if prog := c.Command[0]; strings.ContainsAny(prog, " \t") && !strings.ContainsRune(prog, filepath.Separator) {
		return fmt.Errorf("command[0] %q has a space and is not a path: command is an argument list, not a shell string; put each argument in its own element", prog)
	}
	switch {
	case c.MaxConcurrent < 0 || c.MaxConcurrent > MaxMaxConcurrent:
		return fmt.Errorf("max_concurrent %d: want 1 to %d", c.MaxConcurrent, MaxMaxConcurrent)
	case c.MaxConcurrent == 0:
		c.MaxConcurrent = DefaultMaxConcurrent
	}
	switch {
	case c.DailyLimit < 0 || c.DailyLimit > MaxDailyLimit:
		return fmt.Errorf("daily_limit %d: want 1 to %d", c.DailyLimit, MaxDailyLimit)
	case c.DailyLimit == 0:
		c.DailyLimit = DefaultDailyLimit
	}
	if c.Cwd == "" {
		c.Cwd = WorkDir(stateDir)
	} else if !filepath.IsAbs(c.Cwd) || strings.ContainsRune(c.Cwd, 0) {
		return fmt.Errorf("cwd %q: want an absolute path", c.Cwd)
	}
	return nil
}

// Program resolves Command[0] on PATH, or checks it is an executable file when
// it holds a path separator.
func (c *Config) Program() (string, error) {
	p, err := exec.LookPath(c.Command[0])
	if err != nil {
		return "", fmt.Errorf("dispatch: command %q: %w", c.Command[0], err)
	}
	return p, nil
}

// CheckCwd reports whether the working directory exists. The default one is
// made on demand.
func (c *Config) CheckCwd(stateDir string) error {
	if c.Cwd == WorkDir(stateDir) {
		return os.MkdirAll(c.Cwd, 0o700)
	}
	st, err := os.Stat(c.Cwd)
	if err != nil {
		return fmt.Errorf("dispatch: cwd: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("dispatch: cwd %s is not a directory", c.Cwd)
	}
	return nil
}
