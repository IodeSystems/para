package mcpserver

import (
	"context"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/ops"
)

// settled waits for the goroutine count to fall to want or less.
func settled(want int) int {
	n := runtime.NumGoroutine()
	for i := 0; i < 100 && n > want; i++ {
		time.Sleep(20 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	return n
}

func TestServeStopsItsWatcher(t *testing.T) {
	s, root := newVault(t)
	before := runtime.NumGoroutine()
	srv, err := New(ops.Deps{Store: s}, Options{EventsDir: filepath.Join(root, "events")})
	if err != nil {
		t.Fatal(err)
	}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"request_wait","arguments":{"timeout_seconds":1}}}`,
	}, "\n") + "\n"
	if err := srv.Serve(context.Background(), strings.NewReader(in), io.Discard); err != nil {
		t.Fatal(err)
	}
	if n := settled(before); n > before {
		t.Errorf("goroutines: %d before, %d after Serve returned", before, n)
	}
}

func TestStartStopIsIdempotent(t *testing.T) {
	s, root := newVault(t)
	before := runtime.NumGoroutine()
	srv, err := New(ops.Deps{Store: s}, Options{EventsDir: filepath.Join(root, "events")})
	if err != nil {
		t.Fatal(err)
	}
	stop := srv.Start(context.Background())
	stop()
	stop()
	if n := settled(before); n > before {
		t.Errorf("goroutines: %d before, %d after stop", before, n)
	}
}
