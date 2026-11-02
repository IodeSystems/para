//go:build !unix

package dispatch

import "os"

// pidAlive reports true: with no way to ask, a recorded run counts as alive
// until it ages out, which keeps the concurrency limit safe.
func pidAlive(int) bool { return true }

// terminate stops the command; without SIGTERM here, it is the interrupt.
func terminate(p *os.Process) error { return p.Signal(os.Interrupt) }
