// Command para is the parasolve CLI.
package main

import (
	"fmt"
	"os"

	"github.com/iodesystems/parasolve/internal/cli"
)

// Stamped by the Makefile via -ldflags -X (CLI-12).
var (
	version = "dev"
	srcDir  = ""
)

func main() {
	if err := cli.Execute(version, srcDir); err != nil {
		fmt.Fprintln(os.Stderr, "para:", err)
		os.Exit(1)
	}
}
