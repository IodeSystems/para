package publish

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/iodesystems/para/internal/atomicfile"
)

// ResolveDir expands a leading ~/ and requires an absolute path.
func ResolveDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if rest, ok := strings.CutPrefix(dir, "~/"); ok || dir == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("publish: dir: %w", err)
		}
		dir = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("publish: dir %q: want an absolute path or one under ~/", dir)
	}
	return filepath.Clean(dir), nil
}

// CheckDir refuses a folder that holds anything para publish did not write:
// a file without the generated header, a folder, or a link. A missing folder
// is fine.
func CheckDir(dir string) error {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("publish: check dir: %w", err)
	}
	var foreign []string
	for _, e := range ents {
		if !e.Type().IsRegular() || !generated(filepath.Join(dir, e.Name())) {
			foreign = append(foreign, e.Name())
		}
	}
	if len(foreign) > 0 {
		return fmt.Errorf("publish: nothing written: %s holds files para publish did not write (%s); para replaces the whole folder and deletes only its own files: pick an empty folder, or move these out", dir, strings.Join(foreign, ", "))
	}
	return nil
}

// generated reports whether the file's first line is para's header.
func generated(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	line, err := bufio.NewReader(f).ReadString('\n')
	return (err == nil || line != "") && strings.HasPrefix(line, HeaderPrefix)
}

// Write replaces dir with the plan's files. It stages every file in a temp
// folder beside dir, then swaps it in by rename, so a reader sees the old
// folder or the new one. dir must pass CheckDir.
func Write(dir string, p Plan) (err error) {
	if err := CheckDir(dir); err != nil {
		return err
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("publish: write: %w", err)
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+".para-publish-*")
	if err != nil {
		return fmt.Errorf("publish: write: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := os.Chmod(stage, 0o755); err != nil {
		return fmt.Errorf("publish: write: %w", err)
	}
	for _, f := range append(append([]File(nil), p.Files...), p.Index) {
		if err := atomicfile.Write(filepath.Join(stage, f.Name), f.Data, 0o644); err != nil {
			return fmt.Errorf("publish: write: %w", err)
		}
	}
	old := ""
	if _, err := os.Lstat(dir); err == nil {
		old = stage + ".old"
		if err := os.Rename(dir, old); err != nil {
			return fmt.Errorf("publish: write: %w", err)
		}
	}
	if err := os.Rename(stage, dir); err != nil {
		if old != "" {
			_ = os.Rename(old, dir)
		}
		return fmt.Errorf("publish: write: %w", err)
	}
	if old != "" {
		if err := os.RemoveAll(old); err != nil {
			return fmt.Errorf("publish: write: remove the old folder: %w", err)
		}
	}
	return atomicfile.SyncDir(parent)
}

// configFile is the per-machine publish config in the vault's state dir. A
// folder is a path on this machine, so it never goes in the vault, which a
// sync tool copies to other machines.
const configFile = "publish.json"

// Config is the publish config of one vault on this machine.
type Config struct {
	Sets map[string]SetConfig `json:"sets"`
}

// SetConfig is one set on this machine.
type SetConfig struct {
	Dir string `json:"dir"` // absolute, or under ~/
}

// LoadConfig reads the config; a missing file is an empty config.
func LoadConfig(stateDir string) (Config, error) {
	c := Config{Sets: map[string]SetConfig{}}
	if stateDir == "" {
		return c, errors.New("publish: config: no state dir for this vault")
	}
	b, err := os.ReadFile(filepath.Join(stateDir, configFile))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("publish: config: %w", err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("publish: config: %w", err)
	}
	if c.Sets == nil {
		c.Sets = map[string]SetConfig{}
	}
	return c, nil
}

// SaveDir sets the folder of a set on this machine; an empty dir removes it.
func SaveDir(stateDir, set, dir string) error {
	if err := CheckSet(set); err != nil {
		return err
	}
	c, err := LoadConfig(stateDir)
	if err != nil {
		return err
	}
	if dir == "" {
		delete(c.Sets, set)
	} else {
		if _, err := ResolveDir(dir); err != nil {
			return err
		}
		c.Sets[set] = SetConfig{Dir: dir}
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("publish: config: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("publish: config: %w", err)
	}
	return atomicfile.Write(filepath.Join(stateDir, configFile), append(b, '\n'), 0o600)
}

// SaveManifest keeps the last run's manifest of a set (ids and hashes) on
// this machine, beside the config; the egress event holds its hash.
func SaveManifest(stateDir, set string, manifest []byte) error {
	dir := filepath.Join(stateDir, "publish")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("publish: manifest: %w", err)
	}
	return atomicfile.Write(filepath.Join(dir, set+".manifest"), manifest, 0o600)
}
