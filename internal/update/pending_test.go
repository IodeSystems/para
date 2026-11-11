package update

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestPendingHoldsClaimsThenInstallsAfterGrace: a decided install is
// announced at once; claims are refused while it waits for claimed agent
// work; after Grace it installs anyway.
func TestPendingHoldsClaimsThenInstallsAfterGrace(t *testing.T) {
	r := newAutoRig(t, "v0.3.0", true)
	agents := "vault v: request request-1 is claimed"
	r.a.Agents = func(context.Context, time.Time) string { return agents }
	start := r.now
	if got := r.a.Pass(t.Context()); got != OutcomeWaiting || r.installs != 0 {
		t.Fatalf("pass = %s, installs %d", got, r.installs)
	}
	st := LoadState(r.a.StatePath)
	v, since, ok := st.Pending("v0.3.0")
	if !ok || v != "v0.4.0" || !since.Equal(start) {
		t.Fatalf("pending = %s %s %v", v, since, ok)
	}
	notice, claim := Hold(st, "v0.3.0", start.Add(time.Minute))
	if notice != "para upgrades to v0.4.0 when idle" || claim == nil {
		t.Fatalf("hold = %q, %v", notice, claim)
	}
	if !strings.Contains(claim.Error(), "03:10 UTC") || !strings.Contains(claim.Error(), "no new agent work is claimed") {
		t.Errorf("claim refusal = %s: want the time limit", claim)
	}
	if !strings.Contains(Notice(st, "v0.3.0", true, r.set), "installs when idle") {
		t.Errorf("notice = %s", Notice(st, "v0.3.0", true, r.set))
	}
	// The new binary counts the pending release as current: no hold.
	if n, c := Hold(st, "v0.4.0", start.Add(time.Minute)); n != "" || c != nil {
		t.Errorf("hold on the new binary = %q, %v", n, c)
	}

	r.now = start.Add(Grace - time.Second)
	if got := r.a.Pass(t.Context()); got != OutcomeWaiting || r.installs != 0 {
		t.Fatalf("in grace: pass = %s, installs %d", got, r.installs)
	}
	// The write rule stays after the grace.
	r.now, r.quiet = start.Add(Grace), false
	if got := r.a.Pass(t.Context()); got != OutcomeWaiting || r.installs != 0 {
		t.Fatalf("a write: pass = %s, installs %d", got, r.installs)
	}
	r.quiet = true
	if got := r.a.Pass(t.Context()); got != OutcomeInstalled || r.installs != 1 {
		t.Fatalf("grace over: pass = %s, installs %d", got, r.installs)
	}
	// Pending again from the same start: the start is kept.
	if _, s2, _ := LoadState(r.a.StatePath).Pending("v0.3.0"); !s2.Equal(start) {
		t.Errorf("pending since moved to %s", s2)
	}
	if err := MarkInstalled(r.a.StatePath, "v0.3.0", "v0.4.0", r.now); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := LoadState(r.a.StatePath).Pending("v0.3.0"); ok {
		t.Error("an install leaves the hold")
	}
}

func TestPendingHoldEnds(t *testing.T) {
	r := newAutoRig(t, "v0.3.0", true)
	r.a.Agents = func(context.Context, time.Time) string { return "busy" }
	start := r.now
	r.a.Pass(t.Context())
	st := LoadState(r.a.StatePath)
	if _, c := Hold(st, "v0.3.0", start.Add(HoldMax)); c != nil {
		t.Errorf("a hold past HoldMax refuses: %v", c)
	}
	// A failed install ends the hold and holds no claim for a day.
	r.a.Agents = nil
	r.a.Install = func(context.Context) error { return errors.New("disk full") }
	if got := r.a.Pass(t.Context()); got != OutcomeInstallErr {
		t.Fatalf("pass = %s", got)
	}
	if _, _, ok := LoadState(r.a.StatePath).Pending("v0.3.0"); ok {
		t.Error("a failed install keeps the hold")
	}
	r.quiet = false
	r.now = start.Add(time.Hour)
	r.a.Pass(t.Context())
	if _, _, ok := LoadState(r.a.StatePath).Pending("v0.3.0"); ok {
		t.Error("a release that failed holds claims again within a day")
	}
	// Auto off: no hold.
	r.now = start.Add(CheckEvery + time.Hour)
	r.a.Pass(t.Context())
	if _, _, ok := LoadState(r.a.StatePath).Pending("v0.3.0"); !ok {
		t.Fatal("a day after the failure: want the hold again")
	}
	r.set.Auto = false
	r.a.Pass(t.Context())
	if _, _, ok := LoadState(r.a.StatePath).Pending("v0.3.0"); ok {
		t.Error("upgrade.auto=false keeps the hold")
	}
}
