package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/iodesystems/para/internal/atomicfile"
)

// Record is what a running `para serve` says about itself, so a client on this
// machine finds it without flags. It lives in $PARA_HOME/var, which is per
// machine and never in a vault. It holds no token.
type Record struct {
	PID       int       `json:"pid"`
	Addr      string    `json:"addr"` // the address it listens on, ip:port
	Version   string    `json:"version"`
	Vault     string    `json:"vault"` // the vault the bare address serves, or ""
	Vaults    []string  `json:"vaults"`
	BaseHosts []string  `json:"base_hosts"`
	Started   time.Time `json:"started"`
}

// VarDir is the per-machine runtime directory of an install.
func VarDir(home string) string { return filepath.Join(home, "var") }

// RecordPath is the file of the runtime record.
func RecordPath(home string) string { return filepath.Join(VarDir(home), "serve.json") }

// LockPath is the lock file that keeps a second server off one home. It holds
// nothing: the pid is in the record, the only source of it.
func LockPath(home string) string { return filepath.Join(VarDir(home), "serve.lock") }

// EnsureVarDir makes the runtime directory, mode 0700.
func EnsureVarDir(home string) error {
	dir := VarDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("server: var dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("server: var dir: %w", err)
	}
	return nil
}

// WriteRecord replaces the record, mode 0600.
func WriteRecord(home string, r Record) error {
	if err := EnsureVarDir(home); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("server: record: %w", err)
	}
	if err := atomicfile.Write(RecordPath(home), append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("server: record: %w", err)
	}
	return nil
}

// ReadRecord returns the record of a running server. A missing file, an
// unreadable one, or one whose pid is dead is no server: ok is false.
func ReadRecord(home string) (r Record, ok bool) {
	b, err := os.ReadFile(RecordPath(home))
	if err != nil || json.Unmarshal(b, &r) != nil || r.Addr == "" || !PIDAlive(r.PID) {
		return Record{}, false
	}
	return r, true
}

// RemoveRecord removes the record when it is this process's own.
func RemoveRecord(home string) {
	b, err := os.ReadFile(RecordPath(home))
	if err != nil {
		return
	}
	var r Record
	if json.Unmarshal(b, &r) == nil && r.PID == os.Getpid() {
		_ = os.Remove(RecordPath(home))
	}
}
