//go:build !unix

package dispatch

import "os"

// terminate stops the command; without SIGTERM here, it is the interrupt.
func terminate(p *os.Process) error { return p.Signal(os.Interrupt) }
