package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/dispatch"
	"github.com/iodesystems/para/internal/ops"
)

// No real agent: the command is a script that records what it got.
func TestRunDispatchStartsTheCommandForAClaimableRequest(t *testing.T) {
	f := newFixture(t)
	out := t.TempDir()
	t.Setenv("OUT", out)
	script := filepath.Join(t.TempDir(), "agent.sh")
	body := "#!/bin/sh\necho \"$PARA_REQUEST $PARA_VAULT\" >> \"$OUT/started\"\nwhile [ ! -f \"$OUT/release\" ]; do sleep 0.02; done\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(f.home, "vaults", "acme")

	// No agent.json: nothing starts, nothing is watched, no file appears.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.hub.RunDispatch(ctx, f.home); close(done) }()
	st := f.store(t)
	install := st.Info().Install
	add, err := ops.RequestAdd.Run(ctx, ops.Deps{Store: st.As("owner", "cli")}, ops.RequestAddInput{Ask: "do it", Runner: install})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(out, "started")); err == nil {
		t.Fatal("a command started with no agent.json")
	}
	if _, err := os.Stat(filepath.Join(state, dispatch.StateFile)); err == nil {
		t.Fatal("a state file exists with no agent.json")
	}

	// The file appears; the next refresh starts a dispatcher for the vault.
	if err := os.WriteFile(dispatch.ConfigPath(state), []byte(`{"command":["`+script+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var started string
	for range 40 {
		time.Sleep(50 * time.Millisecond)
		if b, err := os.ReadFile(filepath.Join(out, "started")); err == nil && len(b) > 0 {
			started = strings.TrimSpace(string(b))
			break
		}
	}
	// The refresh period is 15 s; do not wait for it in a unit test. Kick the
	// refresh by restarting with the file already there.
	if started == "" {
		cancel()
		<-done
		ctx, cancel = context.WithCancel(context.Background())
		done = make(chan struct{})
		go func() { f.hub.RunDispatch(ctx, f.home); close(done) }()
		for range 100 {
			time.Sleep(50 * time.Millisecond)
			if b, err := os.ReadFile(filepath.Join(out, "started")); err == nil && len(b) > 0 {
				started = strings.TrimSpace(string(b))
				break
			}
		}
	}
	if started != add.ID+" acme" {
		t.Fatalf("started = %q, want %q", started, add.ID+" acme")
	}
	// Stopping the server ends the command and RunDispatch returns.
	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("RunDispatch did not return")
	}
	if err := os.WriteFile(filepath.Join(out, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDispatchRefusesABadConfigAndNotesAMissingCommand(t *testing.T) {
	f := newFixture(t)
	state := filepath.Join(f.home, "vaults", "acme")
	if notes, err := f.hub.CheckDispatch(); err != nil || len(notes) != 0 {
		t.Fatalf("no config: %v, %v", notes, err)
	}
	if err := os.WriteFile(dispatch.ConfigPath(state), []byte(`{"command":"claude -p"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.hub.CheckDispatch(); err == nil || !strings.Contains(err.Error(), "list of arguments") {
		t.Fatalf("err = %v", err)
	}
	if err := os.WriteFile(dispatch.ConfigPath(state), []byte(`{"command":["/nonexistent/agent"],"cwd":"/nonexistent/work"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	notes, err := f.hub.CheckDispatch()
	if err != nil || len(notes) != 2 {
		t.Fatalf("notes %v, err %v", notes, err)
	}
	if err := os.Chmod(dispatch.ConfigPath(state), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := f.hub.CheckDispatch(); err == nil || !strings.Contains(err.Error(), "writable by others") {
		t.Fatalf("err = %v", err)
	}
}
