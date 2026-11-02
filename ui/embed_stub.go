//go:build !embedui

// Package ui holds the built PWA for the para binary. Build with -tags embedui
// after `pnpm build` to embed ui/dist. Without the tag the binary has no UI and
// `go build` needs no ui/dist; pass --web-root to serve one from disk.
package ui

import "io/fs"

// Dist returns nil: this build has no UI.
func Dist() fs.FS { return nil }
