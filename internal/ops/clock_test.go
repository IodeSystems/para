package ops

import (
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// newDepsAt opens a vault whose clock is fixed at now, on the given surface.
func newDepsAt(t *testing.T, now time.Time, install string) Deps {
	t.Helper()
	root := filepath.Join(t.TempDir(), "v")
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "acme", tpl, vault.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	noGate(t, root)
	s, err := local.Open(root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: install,
		StateDir: filepath.Join(filepath.Dir(root), "state"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return Deps{Store: s}
}
