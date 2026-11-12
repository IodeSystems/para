package vault

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/iodesystems/para/internal/atomicfile"
)

// Snapshot writes the whole vault (every file but .git) to
// <state dir>/snapshots/<name>.tar.gz, on this machine and never in the vault,
// and returns its path. An upgrade step that changes ids writes one first;
// restore is by hand: stop para on every machine, untar over the vault.
func (v *Vault) Snapshot(_ context.Context, name string) (string, error) {
	if v.stateDir == "" {
		return "", errors.New("vault: snapshot: no state directory")
	}
	if !batchRe.MatchString(name) {
		return "", fmt.Errorf("vault: snapshot: name %q is not valid", name)
	}
	dir := filepath.Join(v.stateDir, "snapshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("vault: snapshot: %w", err)
	}
	dst := filepath.Join(dir, name+".tar.gz")
	tmp, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return "", fmt.Errorf("vault: snapshot: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := v.writeTar(tmp); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("vault: snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("vault: snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("vault: snapshot: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return "", fmt.Errorf("vault: snapshot: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", fmt.Errorf("vault: snapshot: %w", err)
	}
	if err := atomicfile.SyncDir(dir); err != nil {
		return "", fmt.Errorf("vault: snapshot: %w", err)
	}
	return dst, nil
}

func (v *Vault) writeTar(w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(v.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(v.root, p)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return nil // a link or device is not vault content
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			h.Name += "/"
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return errors.Join(err, f.Close())
	})
	return errors.Join(err, tw.Close(), gz.Close())
}
