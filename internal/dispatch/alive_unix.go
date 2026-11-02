//go:build unix

package dispatch

import (
	"errors"
	"os"
	"syscall"
)

// pidAlive reports whether a process with this id exists. A process that
// belongs to another user exists too.
func pidAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// terminate asks a command to stop with SIGTERM. Only that process is
// signalled, never a group.
func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
