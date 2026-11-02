//go:build !linux

package vault

func syncBatch(_ string, files, dirs []string) error { return syncEach(files, dirs) }
