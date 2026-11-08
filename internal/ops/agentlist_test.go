package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/presence"
)

// deadPID is the pid of a process that has exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// agent_list shows live sessions with the requests their actor holds, drops
// the file of a process that is gone, and says when queued work has no
// listener.
func TestAgentList(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	d, root := sourceClockDeps(t, &now)
	state := filepath.Join(filepath.Dir(root), "state")

	out, err := AgentList.Run(ctx, d, AgentListInput{})
	if err != nil || len(out.Agents) != 0 || out.QueuedCount != 0 || out.NoListener {
		t.Fatalf("empty = %+v %v", out, err)
	}

	for _, ask := range []string{"Check the backups", "Read the release notes"} {
		if _, err := RequestAdd.Run(ctx, d, RequestAddInput{Ask: ask}); err != nil {
			t.Fatal(err)
		}
	}
	out, err = AgentList.Run(ctx, d, AgentListInput{})
	if err != nil || out.QueuedCount != 2 || !out.NoListener {
		t.Fatalf("queued, nobody connected = %+v %v", out, err)
	}

	next, err := RequestNext.Run(ctx, agentDeps(d, "claude"), RequestNextInput{})
	if err != nil || next.Request == nil {
		t.Fatalf("claim = %+v %v", next, err)
	}
	dead := deadPID(t)
	for _, r := range []presence.Record{
		{PID: os.Getpid(), Actor: "claude", Client: "claude-code", ClientVersion: "2.1.0", StartedAt: "2026-11-04T09:00:00Z", Push: []string{"message"}},
		{PID: dead, Actor: "dun", Client: "dun", StartedAt: "2026-11-04T08:00:00Z", Push: []string{"message", "channel"}},
	} {
		if err := presence.Write(state, r); err != nil {
			t.Fatal(err)
		}
	}
	out, err = AgentList.Run(ctx, d, AgentListInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Agents) != 1 {
		t.Fatalf("agents = %+v", out.Agents)
	}
	a := out.Agents[0]
	if a.Actor != "claude" || a.Client != "claude-code" || a.Version != "2.1.0" || a.PID != os.Getpid() {
		t.Errorf("row = %+v", a)
	}
	if len(a.Claims) != 1 || a.Claims[0].ID != next.Request.ID || a.Claims[0].Title == "" || a.Claims[0].ClaimedAt == "" {
		t.Errorf("claims = %+v, want %s", a.Claims, next.Request.ID)
	}
	if out.QueuedCount != 1 || out.NoListener {
		t.Errorf("queued = %d, no_listener = %v", out.QueuedCount, out.NoListener)
	}
	if _, err := os.Stat(presence.File(state, dead)); !os.IsNotExist(err) {
		t.Errorf("the dead session's file stays: %v", err)
	}

	// Claims join by actor: another actor's session holds nothing.
	if err := presence.Remove(state, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := presence.Write(state, presence.Record{PID: os.Getpid(), Actor: "codex", StartedAt: "2026-11-04T09:00:00Z", Push: []string{"message"}}); err != nil {
		t.Fatal(err)
	}
	out, err = AgentList.Run(ctx, d, AgentListInput{})
	if err != nil || len(out.Agents) != 1 || out.Agents[0].Actor != "codex" || len(out.Agents[0].Claims) != 0 || out.NoListener {
		t.Fatalf("other actor = %+v %v", out, err)
	}
}
