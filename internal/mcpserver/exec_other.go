//go:build !linux && !darwin

package mcpserver

import (
	"errors"
	"os"
)

// fdSource: only Linux and macOS exec into a replacing binary.
func fdSource(*os.File) source { return nil }

func execBinary(string, string, Handoff) error {
	return errors.New("mcpserver: exec: not on this platform")
}
