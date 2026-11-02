//go:build unix

package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

const lockPoll = 5 * time.Millisecond

// acquire takes an exclusive flock on path, waiting until ctx ends. Each call
// opens its own descriptor, so goroutines exclude each other as processes do.
func acquire(ctx context.Context, path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("vault: lock: %w", err)
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("vault: lock: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("vault: lock: %w", ctx.Err())
		case <-time.After(lockPoll):
		}
	}
}
