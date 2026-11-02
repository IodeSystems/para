// Package registry is the install registry: $PARA_HOME/install.properties and
// $PARA_HOME/vaults/<id>/vault.ref. It maps vault ids to paths and selects
// the vault for a command.
package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	ashid "github.com/IodeSystems/ashid/go"

	"github.com/iodesystems/parasolve/internal/atomicfile"
	"github.com/iodesystems/parasolve/internal/props"
)

// DefaultHome returns $PARA_HOME, or ~/.para.
func DefaultHome(env func(string) string) (string, error) {
	if h := env("PARA_HOME"); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("registry: home: set PARA_HOME: %w", err)
	}
	return filepath.Join(h, ".para"), nil
}

// Install is one para home.
type Install struct {
	Home string
}

// Vault is one registered vault.
type Vault struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// How a vault was selected.
const (
	ViaFlag   = "flag"
	ViaEnv    = "env"
	ViaSingle = "single"
)

// Selection is the outcome of Select.
type Selection struct {
	Vault
	Via string `json:"via"`
}

// Entry is one registry directory. Err is set when its vault.ref is
// unreadable or has no path; Path is then empty.
type Entry struct {
	Vault
	Err string `json:"error,omitempty"`
}

// Entries returns every registered vault sorted by id, including broken
// entries (Err set). A directory with no vault.ref is not an entry: it holds
// the state of a pruned vault. Only an unreadable registry is an error.
func (in Install) Entries() ([]Entry, error) {
	entries, err := os.ReadDir(filepath.Join(in.Home, "vaults"))
	if errors.Is(err, fs.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registry: entries: %w", err)
	}
	out := []Entry{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ref, err := props.Load(filepath.Join(in.Home, "vaults", e.Name(), "vault.ref"))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			out = append(out, Entry{Vault: Vault{ID: e.Name()}, Err: err.Error()})
		case ref["path"] == "":
			out = append(out, Entry{Vault: Vault{ID: e.Name()}, Err: "vault.ref has no path"})
		default:
			out = append(out, Entry{Vault: Vault{ID: e.Name(), Path: ref["path"]}})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// List returns the registered vaults sorted by id. A broken entry is an
// error: treating it as absent could turn two vaults into a "single" vault.
func (in Install) List() ([]Vault, error) {
	entries, err := in.Entries()
	if err != nil {
		return nil, fmt.Errorf("registry: list: %w", err)
	}
	out := []Vault{}
	for _, e := range entries {
		if e.Err != "" {
			return nil, fmt.Errorf("registry: list: vault %s: %s", e.ID, e.Err)
		}
		out = append(out, e.Vault)
	}
	return out, nil
}

// Pruned is a registry entry whose vault directory does not exist.
type Pruned struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// Prune removes the vault.ref of each entry whose path does not exist. It
// keeps the rest of vaults/<id>/ (keys, state): a path on an unmounted drive
// or sync folder looks missing. A path that exists, or that cannot be checked
// for another reason, is never touched. A broken entry is not pruned. With
// dry it changes nothing.
func (in Install) Prune(dry bool) ([]Pruned, error) {
	entries, err := in.Entries()
	if err != nil {
		return nil, fmt.Errorf("registry: prune: %w", err)
	}
	out := []Pruned{}
	for _, e := range entries {
		if e.Err != "" {
			continue
		}
		if _, err := os.Stat(e.Path); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if !dry {
			ref := filepath.Join(in.Home, "vaults", e.ID, "vault.ref")
			if err := os.Remove(ref); err != nil {
				return out, fmt.Errorf("registry: prune: %w", err)
			}
		}
		out = append(out, Pruned{ID: e.ID, Path: e.Path})
	}
	return out, nil
}

// Add registers a vault, creating the install on first use. path must be
// absolute. The id may have state left by Prune; it is reused.
func (in Install) Add(id, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("registry: add: %q is not absolute", path)
	}
	if err := in.ensure(); err != nil {
		return err
	}
	dir := filepath.Join(in.Home, "vaults", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("registry: add: %w", err)
	}
	refPath := filepath.Join(dir, "vault.ref")
	if _, err := os.Stat(refPath); err == nil {
		return fmt.Errorf("registry: add: vault %s is already registered", id)
	}
	ref := props.Format(map[string]string{"path": path})
	if err := atomicfile.Write(refPath, ref, 0o600); err != nil {
		return fmt.Errorf("registry: add: %w", err)
	}
	return nil
}

// ID returns the install id, adding one to install.properties when the file
// has none.
func (in Install) ID() (string, error) {
	if err := in.ensure(); err != nil {
		return "", err
	}
	file := filepath.Join(in.Home, "install.properties")
	cfg, err := props.Load(file)
	if err != nil {
		return "", fmt.Errorf("registry: install id: %w", err)
	}
	if id := cfg["install.id"]; id != "" {
		return id, nil
	}
	cfg["install.id"] = ashid.New()
	if err := atomicfile.Write(file, props.Format(cfg), 0o600); err != nil {
		return "", fmt.Errorf("registry: install id: %w", err)
	}
	return cfg["install.id"], nil
}

func (in Install) ensure() error {
	if err := os.MkdirAll(filepath.Join(in.Home, "vaults"), 0o700); err != nil {
		return fmt.Errorf("registry: install: %w", err)
	}
	file := filepath.Join(in.Home, "install.properties")
	if _, err := os.Stat(file); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("registry: install: %w", err)
	}
	cfg := props.Format(map[string]string{
		"install.id":      ashid.New(),
		"install.created": time.Now().UTC().Format(time.RFC3339),
	})
	if err := atomicfile.Write(file, cfg, 0o600); err != nil {
		return fmt.Errorf("registry: install: %w", err)
	}
	return nil
}

// Select picks the vault: flag, then env, then the only vault. Several vaults
// with no choice is an error that names them.
func (in Install) Select(flag, env string) (Selection, error) {
	entries, err := in.Entries()
	if err != nil {
		return Selection{}, fmt.Errorf("registry: select: %w", err)
	}
	var vaults []Vault
	var broken []string
	for _, e := range entries {
		if e.Err != "" {
			broken = append(broken, e.ID+" ("+e.Err+")")
			continue
		}
		vaults = append(vaults, e.Vault)
	}
	// A broken entry only matters when it is the choice, or when there is no
	// choice: it could be the vault the user meant.
	for _, c := range []struct{ id, via string }{{flag, ViaFlag}, {env, ViaEnv}} {
		if c.id == "" {
			continue
		}
		for _, e := range entries {
			if e.ID == c.id && e.Err != "" {
				return Selection{}, fmt.Errorf("registry: select: vault %q is broken: %s", c.id, e.Err)
			}
		}
		return pick(vaults, c.id, c.via)
	}
	if len(broken) > 0 {
		return Selection{}, fmt.Errorf("registry: select: broken entries: %s; choose with --vault or PARA_VAULT", strings.Join(broken, ", "))
	}
	switch len(vaults) {
	case 0:
		return Selection{}, fmt.Errorf("registry: select: no vaults in %s; run para init <id>", in.Home)
	case 1:
		return Selection{Vault: vaults[0], Via: ViaSingle}, nil
	}
	return Selection{}, fmt.Errorf("registry: select: %d vaults (%s); choose with --vault or PARA_VAULT", len(vaults), ids(vaults))
}

func pick(vaults []Vault, id, via string) (Selection, error) {
	for _, v := range vaults {
		if v.ID == id {
			return Selection{Vault: v, Via: via}, nil
		}
	}
	return Selection{}, fmt.Errorf("registry: select: unknown vault %q (%s: %s)", id, via, ids(vaults))
}

func ids(vaults []Vault) string {
	if len(vaults) == 0 {
		return "none registered"
	}
	out := make([]string, len(vaults))
	for i, v := range vaults {
		out[i] = v.ID
	}
	return strings.Join(out, ", ")
}
