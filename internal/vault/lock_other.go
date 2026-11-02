//go:build !unix

package vault

import (
	"context"
	"errors"
)

// acquire refuses: writes without a lock can lose data.
func acquire(context.Context, string) (func(), error) {
	return nil, errors.New("vault: lock: not supported on this platform")
}
