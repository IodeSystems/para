// Command para is the para CLI.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/iodesystems/para/internal/cli"
)

// Stamped by the Makefile via -ldflags -X (CLI-12).
var (
	version = "dev"
	srcDir  = ""
)

// exitCode is 1, or what the error asks for: a blocked hook exits 2.
func exitCode(err error) int {
	var c interface{ ExitCode() int }
	if errors.As(err, &c) {
		return c.ExitCode()
	}
	return 1
}

func main() {
	if err := cli.Execute(version, srcDir); err != nil {
		fmt.Fprintln(os.Stderr, "para:", err)
		os.Exit(exitCode(err))
	}
}
