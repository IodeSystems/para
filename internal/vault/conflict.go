package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iodesystems/parasolve/internal/events"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/paramd"
	"github.com/iodesystems/parasolve/internal/syncname"
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

// CodeLostUpdate is the finding for a write a file-sync tool dropped.
const CodeLostUpdate = "sync-lost-update"

// LostUpdates lists writes from other installs, or this one, that a file-sync
// tool replaced. Paths are not in events, so findings name the document id.
func (v *Vault) LostUpdates() ([]events.LostUpdate, error) {
	evs, err := v.log.All()
	if err != nil {
		return nil, err
	}
	current := func(doc string) string {
		f, err := v.find(doc)
		if err != nil {
			return ""
		}
		b, err := os.ReadFile(v.abs(f.rel))
		if err != nil {
			return ""
		}
		return events.Hash(b)
	}
	return events.LostUpdates(evs, current), nil
}

func lostFindings(lost []events.LostUpdate) []model.Finding {
	out := make([]model.Finding, 0, len(lost))
	for _, l := range lost {
		out = append(out, model.Finding{Path: l.Doc, Code: CodeLostUpdate, Severity: string(paramd.Warn),
			Message: fmt.Sprintf("an update from install %s at %s was replaced by sync: check the sync tool's history or a conflict copy, then edit the document from that install", l.Install, l.Event.TS.Format("2006-01-02 15:04"))})
	}
	return out
}
