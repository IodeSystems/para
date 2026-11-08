//go:build unix

package dispatch

import (
	"os"
	"syscall"
)

// terminate asks a command to stop with SIGTERM. Only that process is
// signalled, never a group.
func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
