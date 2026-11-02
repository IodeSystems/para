// Package atomicfile replaces a file in one step: temp file, fsync, rename,
// fsync of the directory. A reader sees the old bytes or the new bytes.
package atomicfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Write replaces path with data. tmpDir holds the temp file and must be on
// the same filesystem as path. A vault passes .para/tmp, so file watchers
// never see a partial file in a document folder.
func Write(path, tmpDir string, data []byte, perm fs.FileMode) (err error) {
	tmp, err := os.CreateTemp(tmpDir, ".write-*")
	if err != nil {
		return fmt.Errorf("atomicfile: write: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("atomicfile: write: %w", err)
	}
	if err = tmp.Chmod(perm); err != nil {
		return fmt.Errorf("atomicfile: write: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("atomicfile: write: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("atomicfile: write: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("atomicfile: write: %w", err)
	}
	return SyncDir(filepath.Dir(path))
}

// SyncDir flushes a directory entry, so a rename survives a crash.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("atomicfile: sync dir: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("atomicfile: sync dir: %w", err)
	}
	return nil
}
