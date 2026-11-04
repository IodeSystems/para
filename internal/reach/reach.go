// Package reach keeps which sources each agent declared it reaches, on this
// machine. A declaration is a fact about this machine and the agent's
// harness (a connector may exist only inside the agent), so it lives in the
// vault's state directory ($PARA_HOME/vaults/<id>/reach/<actor>.json), one
// file per actor, never in the vault: a sync tool never carries it. It holds
// source ids and a time, nothing else.
package reach

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/atomicfile"
)

// Dir is the directory of declarations in a state directory.
const Dir = "reach"

// Declaration is one actor's statement of the sources it reaches, at At.
type Declaration struct {
	Actor   string   `json:"actor"`
	Install string   `json:"install"`
	At      string   `json:"at"` // RFC 3339
	Sources []string `json:"sources"`
}

// Fresh reports whether the declaration was made at or after cut.
func (d Declaration) Fresh(cut time.Time) bool {
	t, err := time.Parse(time.RFC3339Nano, d.At)
	return err == nil && !t.Before(cut)
}

var plainActor = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// File is the file of an actor's declaration. An actor name that is not
// plain is hex-encoded, so no name can leave the directory.
func File(stateDir, actor string) string {
	name := actor
	if !plainActor.MatchString(actor) {
		name = "x-" + hex.EncodeToString([]byte(actor))
	}
	return filepath.Join(stateDir, Dir, name+".json")
}

// Read reads every declaration in stateDir, sorted by actor. No state
// directory, or none written yet, is no declaration; a file that does not
// parse declares nothing.
func Read(stateDir string) ([]Declaration, error) {
	if stateDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, Dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reach: read: %w", err)
	}
	var out []Declaration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(stateDir, Dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reach: read: %w", err)
		}
		var d Declaration
		if json.Unmarshal(b, &d) == nil {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b Declaration) int { return strings.Compare(a.Actor, b.Actor) })
	return out, nil
}

// Write replaces the declaration of d.Actor in stateDir.
func Write(stateDir string, d Declaration) error {
	if stateDir == "" {
		return errors.New("reach: write: no state directory: declarations live on this machine, outside the vault")
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("reach: write: %w", err)
	}
	path := File(stateDir, d.Actor)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("reach: write: %w", err)
	}
	if err := atomicfile.Write(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("reach: write: %w", err)
	}
	return nil
}
