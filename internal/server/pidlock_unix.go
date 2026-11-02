//go:build unix

package server

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// LockPID takes the pidfile gate (CLI-8). A second server on the same home is
// refused. The kernel drops the flock when the process dies, so a crash leaves
// no stale lock.
func LockPID(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("server: pidfile: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("server: another para serve holds %s", path)
		}
		return nil, fmt.Errorf("server: pidfile: %w", err)
	}
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	}
	return func() {
		_ = os.Remove(path)
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
