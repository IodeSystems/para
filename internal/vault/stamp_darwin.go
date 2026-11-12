package vault

import (
	"io/fs"
	"syscall"
)

// sysStamp returns the device, inode, and change time (ns) of st.
func sysStamp(st fs.FileInfo) (dev, ino uint64, chg int64) {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Dev), s.Ino, s.Ctimespec.Nano()
	}
	return 0, 0, 0
}
