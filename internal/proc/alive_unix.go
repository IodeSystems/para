//go:build unix

// Package proc answers questions about processes on this machine.
package proc

import (
	"errors"
	"syscall"
)

// Alive reports whether a process with this id exists. A process that
// belongs to another user exists too.
func Alive(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
