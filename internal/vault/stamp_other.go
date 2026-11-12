//go:build !linux && !darwin

package vault

import "io/fs"

// sysStamp has no device, inode, or change time here: the stamp is the
// modification time and size, with the racy guard.
func sysStamp(fs.FileInfo) (dev, ino uint64, chg int64) { return 0, 0, 0 }
