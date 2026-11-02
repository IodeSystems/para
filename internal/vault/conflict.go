package vault

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/paramd"
	"github.com/iodesystems/paradize/internal/syncname"
)

// Conflicts lists the sync-conflict copies in the vault (names by package
// syncname), as slash paths relative to the root, sorted. They are not
// documents: listing, lookup, lint, and reconcile skip them.
func (v *Vault) Conflicts() ([]string, error) { return conflictCopies(v.root, vaultDirs()) }

// vaultDirs are the folders conflict copies are looked for in: documents,
// templates, and events.
func vaultDirs() []string {
	out := append(trackedDirs(), dirEvents)
	return out
}

// conflictCopies walks dirs under root for regular files with a conflict name,
// of any extension (a ".md.orig" is never loaded but still worth a finding).
// Dotted directories are skipped.
func conflictCopies(root string, dirs []string) ([]string, error) {
	var out []string
	for _, dir := range dirs {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			switch {
			case errors.Is(err, fs.ErrNotExist):
				return nil
			case err != nil:
				return err
			case d.IsDir() && strings.HasPrefix(d.Name(), "."):
				return filepath.SkipDir
			case d.IsDir() || !d.Type().IsRegular() || !syncname.IsConflict(d.Name()):
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func conflictFindings(rels []string) []model.Finding {
	out := make([]model.Finding, 0, len(rels))
	for _, rel := range rels {
		out = append(out, model.Finding{Path: rel, Code: syncname.Code, Severity: string(paramd.Warn),
			Message: "looks like a sync conflict copy, not a document: compare it with the original, keep one, delete the other"})
	}
	return out
}
