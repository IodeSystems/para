package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/atomicfile"
	"github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
)

// Op values a batch rewrite logs, one event per document. From holds the
// batch id and To the folder.
const (
	OpUpgrade     = "upgrade"
	OpUpgradeUndo = "upgrade_undo"
)

// batchRe limits a batch id: it is a directory name and an event value.
var batchRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,63}$`)

type batchEntry struct {
	Rel string `json:"rel"`
	ID  string `json:"id"`
	Old string `json:"old"`
	New string `json:"new"`
	// To is the new path when the rewrite moved the document to another
	// folder; empty when it stayed at Rel.
	To string `json:"to,omitempty"`
}

// at is where the document is after the batch.
func (e batchEntry) at() string {
	if e.To != "" {
		return e.To
	}
	return e.Rel
}

func (v *Vault) batchDir(batch string) string { return filepath.Join(v.stateDir, "batches", batch) }

// Scan calls fn for each document that decodes. A file that does not decode is
// skipped: lint reports it. It reads only, so it works on a read-only vault.
func (v *Vault) Scan(_ context.Context, fn func(model.Document) error) error {
	files, err := v.docFiles()
	if err != nil {
		return fmt.Errorf("vault: scan: %w", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(v.abs(f.rel))
		if err != nil {
			return fmt.Errorf("vault: scan: %w", err)
		}
		pf := paramd.Parse(f.rel, data)
		d, err := decodeFile(f, pf)
		if err != nil {
			continue
		}
		d.Rev = events.Hash(data)
		if err := fn(d); err != nil {
			return err
		}
	}
	return nil
}

// Rewrite applies fn to every document under the lock, as one batch: the old
// bytes of each changed file go to this machine's state directory (never the
// vault), each change logs one OpUpgrade event, and UndoBatch restores them.
// fn may set another folder on the document: the file then moves there under
// the same name, so links still resolve.
// A failure part way restores what it wrote. With dry set it writes nothing
// and logs nothing, and lists the documents it would change.
func (v *Vault) Rewrite(ctx context.Context, batch string, dry bool, fn model.RewriteFunc) (model.BatchResult, error) {
	res := model.BatchResult{Batch: batch, DryRun: dry, Docs: []string{}}
	if !batchRe.MatchString(batch) {
		return res, fmt.Errorf("vault: rewrite: batch id %q must be lowercase letters, digits, '.' and '-'", batch)
	}
	err := v.withLock(ctx, func(m *events.Manifest) error {
		files, err := v.docFiles()
		if err != nil {
			return err
		}
		type change struct {
			entry batchEntry
			old   []byte
			data  []byte
			mode  os.FileMode
			fold  model.Folder
		}
		var plan []change
		for _, f := range files {
			old, err := os.ReadFile(v.abs(f.rel))
			if err != nil {
				return err
			}
			pf := paramd.Parse(f.rel, old)
			d, err := decodeFile(f, pf)
			if err != nil {
				continue
			}
			d.Rev = events.Hash(old)
			nd, ok, err := fn(d)
			if err != nil {
				return fmt.Errorf("%s: %w", f.id, err)
			}
			if !ok {
				continue
			}
			to := f.folder
			if nd.Folder != "" && nd.Folder != f.folder {
				if !nd.Folder.Valid() {
					return fmt.Errorf("%s: unknown folder %q", f.id, nd.Folder)
				}
				to = nd.Folder
			}
			nd.ID, nd.Folder = f.id, to
			nd.Meta.Header().Updated = v.today()
			entry := batchEntry{Rel: f.rel, ID: f.id, Old: events.Hash(old)}
			if to != f.folder {
				entry.To = path.Join(string(to), path.Base(f.rel))
				if _, err := os.Stat(v.abs(entry.To)); err == nil {
					return fmt.Errorf("%s: %s already exists", f.id, entry.To)
				}
			}
			data, err := renderChecked(entry.at(), nd, nil)
			if err != nil {
				return fmt.Errorf("%s: %w", f.id, err)
			}
			if string(data) == string(old) && entry.To == "" {
				continue
			}
			st, err := os.Stat(v.abs(f.rel))
			if err != nil {
				return err
			}
			entry.New = events.Hash(data)
			plan = append(plan, change{entry: entry, old: old, data: data, mode: st.Mode().Perm(), fold: to})
			res.Docs = append(res.Docs, f.id)
		}
		if dry || len(plan) == 0 {
			return nil
		}
		dir := v.batchDir(batch)
		if _, err := os.Stat(dir); err == nil {
			return fmt.Errorf("batch %s already exists", batch)
		}
		prepared := false
		defer func() {
			if !prepared {
				_ = os.RemoveAll(dir)
			}
		}()
		index := make([]batchEntry, 0, len(plan))
		for _, c := range plan {
			index = append(index, c.entry)
			bak := filepath.Join(dir, "files", filepath.FromSlash(c.entry.Rel))
			if err := os.MkdirAll(filepath.Dir(bak), 0o700); err != nil {
				return err
			}
			if err := atomicfile.Write(bak, c.old, 0o600); err != nil {
				return err
			}
		}
		b, err := json.MarshalIndent(index, "", "  ")
		if err != nil {
			return err
		}
		if err := atomicfile.Write(filepath.Join(dir, "index.json"), b, 0o600); err != nil {
			return err
		}
		prepared = true
		written := 0
		restore := func(cause error) error {
			var errs []error
			for _, c := range plan[:written] {
				if err := v.write(c.entry.Rel, c.old, c.mode); err != nil {
					errs = append(errs, err)
				}
				if c.entry.To != "" {
					if err := os.Remove(v.abs(c.entry.To)); err != nil {
						errs = append(errs, err)
					}
				}
			}
			_ = os.RemoveAll(dir)
			return errors.Join(append([]error{cause}, errs...)...)
		}
		recs := make([]events.Rec, 0, len(plan))
		for _, c := range plan {
			if err := v.noteExternalEdit(m, c.entry.Rel, c.entry.Old); err != nil {
				return restore(err)
			}
			if err := v.moveWrite(c.entry.Rel, c.entry.at(), c.data, c.mode); err != nil {
				return restore(err)
			}
			written++
			recs = append(recs, events.Rec{Op: OpUpgrade, Doc: c.entry.ID, From: batch, To: string(c.fold), Hash: c.entry.New, Prev: c.entry.Old})
		}
		if err := v.log.AppendMany(recs); err != nil {
			return restore(err)
		}
		for _, c := range plan {
			delete(m.Files, c.entry.Rel)
			m.Files[c.entry.at()] = c.entry.New
		}
		return v.saveManifest(*m)
	})
	if err != nil {
		return res, fmt.Errorf("vault: rewrite: %w", err)
	}
	return res, nil
}

// UndoBatch restores the files a batch changed on this machine. A document
// edited since the batch is left alone and listed in Skipped. It logs one
// OpUpgradeUndo event per restored document and removes the batch record.
func (v *Vault) UndoBatch(ctx context.Context, batch string) (model.BatchResult, error) {
	res := model.BatchResult{Batch: batch, Docs: []string{}}
	if !batchRe.MatchString(batch) {
		return res, fmt.Errorf("vault: undo batch: batch id %q is not valid", batch)
	}
	dir := v.batchDir(batch)
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("vault: undo batch: no batch %s on this machine: the backup of a batch is kept only where it ran", batch)
	}
	if err != nil {
		return res, fmt.Errorf("vault: undo batch: %w", err)
	}
	var index []batchEntry
	if err := json.Unmarshal(raw, &index); err != nil {
		return res, fmt.Errorf("vault: undo batch: %w", err)
	}
	err = v.withLock(ctx, func(m *events.Manifest) error {
		var recs []events.Rec
		for _, e := range index {
			cur, err := os.ReadFile(v.abs(e.at()))
			if err != nil || events.Hash(cur) != e.New {
				res.Skipped = append(res.Skipped, e.ID)
				continue
			}
			old, err := os.ReadFile(filepath.Join(dir, "files", filepath.FromSlash(e.Rel)))
			if err != nil {
				return err
			}
			st, err := os.Stat(v.abs(e.at()))
			if err != nil {
				return err
			}
			if err := v.moveWrite(e.at(), e.Rel, old, st.Mode().Perm()); err != nil {
				return err
			}
			delete(m.Files, e.at())
			m.Files[e.Rel] = e.Old
			res.Docs = append(res.Docs, e.ID)
			recs = append(recs, events.Rec{Op: OpUpgradeUndo, Doc: e.ID, From: batch, To: string(folderOf(e.Rel)), Hash: e.Old, Prev: e.New})
		}
		if err := v.log.AppendMany(recs); err != nil {
			return err
		}
		return v.saveManifest(*m)
	})
	if err != nil {
		return res, fmt.Errorf("vault: undo batch: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return res, fmt.Errorf("vault: undo batch: %w", err)
	}
	return res, nil
}

// moveWrite writes data at to and, when to is another path, removes from. The
// new file is written first, so a failure leaves at least one copy.
func (v *Vault) moveWrite(from, to string, data []byte, mode os.FileMode) error {
	if from != to {
		if err := os.MkdirAll(filepath.Dir(v.abs(to)), 0o700); err != nil {
			return err
		}
	}
	if err := v.write(to, data, mode); err != nil {
		return err
	}
	if from == to {
		return nil
	}
	if err := os.Remove(v.abs(from)); err != nil {
		return err
	}
	return atomicfile.SyncDir(filepath.Dir(v.abs(from)))
}

func folderOf(rel string) model.Folder {
	f, _, _ := strings.Cut(rel, "/")
	return model.Folder(f)
}

var configKeyRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// SetConfig sets one key of .para/vault.properties and keeps the other lines,
// comments included. A key that already has the value changes nothing.
func (v *Vault) SetConfig(ctx context.Context, key, value string) error {
	if !configKeyRe.MatchString(key) || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("vault: set config: bad key or value")
	}
	return v.editConfig(ctx, "set config", key, func(lines []string, at int) []string {
		if at >= 0 {
			lines[at] = key + "=" + value
			return lines
		}
		return append(lines, key+"="+value)
	})
}

// UnsetConfig removes one key of .para/vault.properties and keeps the other
// lines, comments included. A key that is not set changes nothing.
func (v *Vault) UnsetConfig(ctx context.Context, key string) error {
	if !configKeyRe.MatchString(key) {
		return fmt.Errorf("vault: unset config: bad key")
	}
	return v.editConfig(ctx, "unset config", key, func(lines []string, at int) []string {
		if at < 0 {
			return lines
		}
		return slices.Delete(lines, at, at+1)
	})
}

// editConfig applies edit to the lines of vault.properties under the vault
// lock and writes the file atomically. at is the index of the last line that
// sets key, or -1.
func (v *Vault) editConfig(ctx context.Context, what, key string, edit func(lines []string, at int) []string) error {
	p := filepath.Join(v.root, dirState, "vault.properties")
	err := v.lock(ctx, func() error {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		at := -1
		for i, l := range lines {
			k, _, ok := strings.Cut(strings.TrimSpace(l), "=")
			if ok && !strings.HasPrefix(strings.TrimSpace(l), "#") && strings.TrimSpace(k) == key {
				at = i
			}
		}
		out := strings.Join(edit(lines, at), "\n") + "\n"
		if out == string(data) {
			return nil
		}
		return atomicfile.Write(p, []byte(out), 0o600)
	})
	if err != nil {
		return fmt.Errorf("vault: %s: %w", what, err)
	}
	return nil
}
