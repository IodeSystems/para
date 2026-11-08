//go:build !unix

// Package proc answers questions about processes on this machine.
package proc

// Alive reports true: with no way to ask, a recorded process counts as
// alive. Callers that keep records age them out or wait for a clean exit.
func Alive(int) bool { return true }
