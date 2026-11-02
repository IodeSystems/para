package cli

import (
	"context"
	"path/filepath"
	"time"

	"github.com/iodesystems/parasolve/internal/events"
	"github.com/iodesystems/parasolve/internal/model"
)

// waitFallback is the default time between checks when no event arrives. A
// sync folder that raises no file events, a lease that runs out, and a hand
// edit that no event logged yet are all seen by this check.
const waitFallback = 30 * time.Second

// waitFor blocks until check reports done. It checks once at the start, then
// on each batch of vault events, and at least every fallback. It returns
// timeoutErr when timeout (when positive) passes first, and ctx.Err() when ctx
// ends. The watcher starts before the first check, so a change between the two
// is not missed. It stops the watcher before it returns.
func (l *loader) waitFor(ctx context.Context, fallback, timeout time.Duration, timeoutErr error, check func(context.Context) (bool, error)) error {
	sel, err := l.selectVault()
	if err != nil {
		return err
	}
	kick := make(chan struct{}, 1)
	stop := events.Watch(ctx, filepath.Join(sel.Path, "events"), 0, func([]model.Event) {
		select {
		case kick <- struct{}{}:
		default:
		}
	}, nil)
	defer stop()
	var deadline <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		deadline = t.C
	}
	tick := time.NewTicker(fallback)
	defer tick.Stop()
	for {
		done, err := check(ctx)
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return timeoutErr
		case <-tick.C:
		case <-kick:
		}
	}
}
