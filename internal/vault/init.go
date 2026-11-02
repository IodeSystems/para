package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/iodesystems/paradize/internal/atomicfile"
	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/props"
)

const (
	dirTemplates = "templates"
	dirEvents    = "events"
	dirState     = ".para"
)

// gitignore keeps everything in .para/ out of git except the vault config, and
// ignores the dotted temp files of atomic writes. Per-machine state (lock,
// manifest, index) is outside the vault, so the ignore holds with or without
// git at Init: a person may run git init later.
const gitignore = ".para/*\n!.para/vault.properties\n.*.tmp-*\n"

// InitOptions configures Init.
type InitOptions struct {
	// NoGit skips git init (and the need for git on PATH).
	NoGit bool
}

// idRe limits a vault id to one DNS label: para serve routes vaults by
// hostname.
var idRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidID reports whether id is a legal vault id.
func ValidID(id string) bool { return idRe.MatchString(id) }

// Init creates a vault at root: folders, .para/vault.properties, .gitignore,
// templates, and (unless o.NoGit) a git repository. It writes no per-machine
// state: the first Open that writes baselines the manifest. It logs no event:
// there is no vault to log against until it returns, and no document has
// changed. root must be absent or empty. Templates skip Lint: they are
// placeholders, and Check does not read them.
func Init(root, id string, templates fs.FS, o InitOptions) error {
	if !ValidID(id) {
		return fmt.Errorf("vault: init: id %q must be one lowercase DNS label", id)
	}
	if !o.NoGit {
		if _, err := exec.LookPath("git"); err != nil {
			return fmt.Errorf("vault: init: %w", err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("vault: init: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("vault: init: %s is not empty", root)
	}
	dirs := []string{filepath.Join(root, dirState), filepath.Join(root, dirEvents), filepath.Join(root, dirTemplates)}
	for _, f := range model.Folders {
		dirs = append(dirs, filepath.Join(root, string(f)))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("vault: init: %w", err)
		}
	}
	cfg := props.Format(map[string]string{"vault.id": id, "egress.default": "deny"})
	if err := atomicfile.Write(filepath.Join(root, dirState, "vault.properties"), cfg, 0o600); err != nil {
		return fmt.Errorf("vault: init: %w", err)
	}
	if err := atomicfile.Write(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o600); err != nil {
		return fmt.Errorf("vault: init: %w", err)
	}
	if err := copyTemplates(templates, filepath.Join(root, dirTemplates)); err != nil {
		return fmt.Errorf("vault: init: %w", err)
	}
	if o.NoGit {
		return nil
	}
	git := exec.Command("git", "init", "-q", root)
	if out, err := git.CombinedOutput(); err != nil {
		return fmt.Errorf("vault: init: git init: %w: %s", err, out)
	}
	return nil
}

func copyTemplates(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return atomicfile.Write(filepath.Join(dst, filepath.Base(p)), data, 0o600)
	})
}

// trackedDirs are the folders the manifest covers.
func trackedDirs() []string {
	out := []string{dirTemplates}
	for _, f := range model.Folders {
		out = append(out, string(f))
	}
	return out
}
