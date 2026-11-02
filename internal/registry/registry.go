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

	"github.com/iodesystems/paradize/internal/atomicfile"
	"github.com/iodesystems/paradize/internal/props"
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

// List returns the registered vaults sorted by id. An unreadable entry is an
// error: treating it as absent could turn two vaults into a "single" vault.
func (in Install) List() ([]Vault, error) {
	entries, err := os.ReadDir(filepath.Join(in.Home, "vaults"))
	if errors.Is(err, fs.ErrNotExist) {
		return []Vault{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registry: list: %w", err)
	}
	out := []Vault{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ref, err := props.Load(filepath.Join(in.Home, "vaults", e.Name(), "vault.ref"))
		if err != nil {
			return nil, fmt.Errorf("registry: list: vault %s: %w", e.Name(), err)
		}
		if ref["path"] == "" {
			return nil, fmt.Errorf("registry: list: vault %s: vault.ref has no path", e.Name())
		}
		out = append(out, Vault{ID: e.Name(), Path: ref["path"]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Add registers a vault, creating the install on first use. path must be
// absolute.
func (in Install) Add(id, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("registry: add: %q is not absolute", path)
	}
	if err := in.ensure(); err != nil {
		return err
	}
	dir := filepath.Join(in.Home, "vaults", id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return fmt.Errorf("registry: add: %w", err)
	}
	ref := props.Format(map[string]string{"path": path})
	if err := atomicfile.Write(filepath.Join(dir, "vault.ref"), ref, 0o600); err != nil {
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
	vaults, err := in.List()
	if err != nil {
		return Selection{}, err
	}
	if flag != "" {
		return pick(vaults, flag, ViaFlag)
	}
	if env != "" {
		return pick(vaults, env, ViaEnv)
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
