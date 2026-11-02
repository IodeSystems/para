//go:build unix

package server

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// LockPID takes the lock-file gate (CLI-8). A second server on the same home is
// refused. The kernel drops the flock when the process dies, so a crash leaves
// no stale lock. The file holds no pid; the record does.
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
	return func() {
		_ = os.Remove(path)
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// PIDAlive reports whether a process with this pid exists.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
