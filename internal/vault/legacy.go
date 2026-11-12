package vault

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/props"
)

// Below model.GoalsFormat a goal is a file of type project in projects/ and
// its links are project: keys. The vault reads such files as format 11 (type
// goal, goal: keys) and writes them back in the old form, so a machine on an
// older para still reads every file until the owner runs upgrade step goals.
// Ops see only the format-11 form.

// formatCache holds format_version and whether step goals ran, read again
// when vault.properties changes.
type formatCache struct {
	mu      sync.Mutex
	mod     time.Time
	size    int64
	version int
	goals   bool
	ok      bool
}

// reset drops the cached value: the next read loads the file.
func (c *formatCache) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.ok = false
	c.mu.Unlock()
}

// Native returns the same open vault without the legacy reading: a file of
// type project reads as a project at any format. Upgrade step goals reads and
// writes through it.
func (v *Vault) Native() *Vault {
	c := *v
	c.native = true
	return &c
}

// legacy reports whether the vault holds the files of below
// model.GoalsFormat: format_version is below it and step goals has not run
// (model.GoalsStepKey).
func (v *Vault) legacy() bool {
	return !v.native && legacyAt(v.root, v.format)
}

// legacyAt reads vault.properties of the vault at root; a vault without
// format_version is at 0. c caches it by the file's size and time; nil reads
// every time.
func legacyAt(root string, c *formatCache) bool {
	p := filepath.Join(root, dirState, "vault.properties")
	st, err := os.Stat(p)
	if err != nil {
		return true
	}
	if c != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.ok && c.mod.Equal(st.ModTime()) && c.size == st.Size() {
			return c.version < model.GoalsFormat && !c.goals
		}
	}
	cfg, err := props.Load(p)
	if err != nil {
		return true
	}
	n, err := strconv.Atoi(strings.TrimSpace(cfg["format_version"]))
	if err != nil || n < 0 {
		n = 0
	}
	goals := strings.TrimSpace(cfg[model.GoalsStepKey]) != ""
	if c != nil {
		c.mod, c.size, c.version, c.goals, c.ok = st.ModTime(), st.Size(), n, goals, true
	}
	return n < model.GoalsFormat && !goals
}

// legacyParsed changes a parsed file below model.GoalsFormat into the format-11
// form, in place, so decode and lint read it as format 11.
func legacyParsed(pf *paramd.File) {
	if pf.Frontmatter != nil {
		model.LegacyReadMap(pf.Frontmatter)
	}
	pf.FMNode = model.LegacyRead(pf.FMNode)
}

// legacyFolder is where a document of folder f lives below model.GoalsFormat:
// goals and tasks stay in projects/.
func legacyFolder(f model.Folder) model.Folder {
	if f == model.FolderGoals {
		return model.FolderProjects
	}
	return f
}

// folderFor is the folder a write puts a document in.
func (v *Vault) folderFor(f model.Folder) model.Folder {
	if v.legacy() {
		return legacyFolder(f)
	}
	return f
}

// typeOf is the type a listing gives a file whose type key is t.
func typeOf(t model.Type, legacy bool) model.Type {
	if legacy {
		return model.LegacyType(t)
	}
	return t
}
