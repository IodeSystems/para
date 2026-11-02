//go:build linux

package vault

import (
	"os"

	"golang.org/x/sys/unix"
)

// syncBatch makes the written files and their directories durable. On Linux
// one syncfs call flushes the whole file system once, instead of one fsync per
// file (about 10 ms each on a journaling disk). It falls back to per-file
// fsync when syncfs is not available.
func syncBatch(root string, files, dirs []string) error {
	f, err := os.Open(root)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := unix.Syncfs(int(f.Fd())); err == nil {
		return nil
	}
	return syncEach(files, dirs)
}
