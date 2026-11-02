// Command livegen writes the typed live-event client from the Go event registry
// through gwag's gw/relay generator: payload types plus the static runtime, in
// ui/src/gen. Offline: no vault, no server. Run `go run ./cmd/livegen`.
package main

import (
	"fmt"
	"os"

	"github.com/iodesystems/gwag/v2/gw/relay"

	"github.com/iodesystems/parasolve/internal/server"
)

func main() {
	out := "ui/src/gen"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := relay.GenClient(server.GenChannels, server.GenEnums, out); err != nil {
		fmt.Fprintln(os.Stderr, "livegen:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s/{%s,%s}\n", out, relay.TypesFile, relay.RuntimeFile)
}
