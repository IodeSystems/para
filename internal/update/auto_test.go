package update

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"
)

// autoRig is an Auto over a test release server. installs counts Install
// calls; dests records each egress destination in order.
type autoRig struct {
	a        *Auto
	srv      *server
	set      Settings
	quiet    bool
	installs int
	dests    []string
	now      time.Time
}

func newAutoRig(t *testing.T, current string, release bool) *autoRig {
	t.Helper()
	srv, k, dir := newServer(t), newKey(t), t.TempDir()
	rel(t, srv, k, "para", "v0.4.0", "2026-11-10T00:00:00Z", "para v0.4.0 manifest")
	u := upgrader(srv, k, dir, current)
	r := &autoRig{srv: srv, set: Settings{Check: true, Auto: true}, quiet: true, now: time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)}
	r.a = &Auto{
		StatePath: u.StatePath, Current: current, Release: release,
		Settings: func() Settings { return r.set },
		Latest:   u.Latest,
		Egress:   func(_ context.Context, d string) error { r.dests = append(r.dests, d); return nil },
		Quiet: func(time.Time) (bool, string) {
			if r.quiet {
				return true, ""
			}
			return false, "a write 2m ago"
		},
		Install: func(context.Context) error { r.installs++; return nil },
		Now:     func() time.Time { return r.now },
	}
	return r
}

func (r *autoRig) manifestHits() int {
	r.srv.mu.Lock()
	defer r.srv.mu.Unlock()
	return r.srv.hits["/latest/para-manifest.json"]
}

func TestAutoChecksAtMostOnceADay(t *testing.T) {
	r := newAutoRig(t, "v0.4.0", true)
	for range 3 {
		if got := r.a.Pass(t.Context()); got != OutcomeCurrent {
			t.Fatalf("pass = %s", got)
		}
		r.now = r.now.Add(6 * time.Hour)
	}
	if n := r.manifestHits(); n != 1 {
		t.Fatalf("%d fetches in 12 hours, want 1", n)
	}
	r.now = r.now.Add(7 * time.Hour) // 25 hours after the first
	r.a.Pass(t.Context())
	if n := r.manifestHits(); n != 2 {
		t.Fatalf("%d fetches after a day, want 2", n)
	}
	if len(r.dests) != 2 || r.dests[0] != DestManifest {
		t.Fatalf("egress %v", r.dests)
	}
	// A failed check waits a day too.
	r.srv.mu.Lock()
	delete(r.srv.files, "/latest/para-manifest.json")
	r.srv.mu.Unlock()
	r.now = r.now.Add(25 * time.Hour)
	if got := r.a.Pass(t.Context()); got != OutcomeFailed {
		t.Fatalf("pass = %s", got)
	}
	if st := LoadState(r.a.StatePath); st.CheckError == "" || st.Version != "v0.4.0" {
		t.Fatalf("state after a failure %+v", st)
	}
	r.now = r.now.Add(time.Hour)
	r.a.Pass(t.Context())
	if n := r.manifestHits(); n != 3 {
		t.Fatalf("a failed check was retried within the day: %d fetches", n)
	}
}

func TestAutoNeverReplacesADevBuild(t *testing.T) {
	r := newAutoRig(t, "v0.3.0", false)
	if got := r.a.Pass(t.Context()); got != OutcomeNotifyDev || r.installs != 0 {
		t.Fatalf("pass = %s, installs %d", got, r.installs)
	}
	if n := Notice(LoadState(r.a.StatePath), "v0.3.0", false, r.set); n == "" {
		t.Fatal("no notice for a dev build")
	}
	if IsRelease("v0.3.0", "/src/para") || IsRelease("v0.3.0-4-gabc", "") || IsRelease("dev", "") || !IsRelease("v0.3.0", "") {
		t.Fatal("IsRelease")
	}
}

func TestAutoOffOnlyNotifies(t *testing.T) {
	r := newAutoRig(t, "v0.3.0", true)
	r.set.Auto = false
	if got := r.a.Pass(t.Context()); got != OutcomeNotifyOff || r.installs != 0 {
		t.Fatalf("pass = %s, installs %d", got, r.installs)
	}
	r.set.Check = false
	r.now = r.now.Add(48 * time.Hour)
	if got := r.a.Pass(t.Context()); got != OutcomeOff || r.manifestHits() != 1 {
		t.Fatalf("check=false: pass = %s, fetches %d", got, r.manifestHits())
	}
	if s := SettingsFrom(map[string]string{KeyCheck: "false", KeyAuto: " OFF "}); s.Check || s.Auto {
		t.Fatalf("settings %+v", s)
	}
	if s := SettingsFrom(nil); !s.Check || !s.Auto {
		t.Fatalf("defaults %+v", s)
	}
}

func TestAutoInstallsOnlyWhenQuiet(t *testing.T) {
	r := newAutoRig(t, "v0.3.0", true)
	r.quiet = false
	if got := r.a.Pass(t.Context()); got != OutcomeWaiting || r.installs != 0 {
		t.Fatalf("busy: pass = %s, installs %d", got, r.installs)
	}
	r.quiet = true
	r.now = r.now.Add(10 * time.Minute)
	if got := r.a.Pass(t.Context()); got != OutcomeInstalled || r.installs != 1 {
		t.Fatalf("quiet: pass = %s, installs %d", got, r.installs)
	}
	if r.manifestHits() != 1 {
		t.Fatalf("the install pass checked again: %d fetches", r.manifestHits())
	}
	if r.dests[len(r.dests)-1] != DestRelease {
		t.Fatalf("egress %v", r.dests)
	}
}

func TestAutoRefusesADowngrade(t *testing.T) {
	r := newAutoRig(t, "v0.5.0", true)
	if got := r.a.Pass(t.Context()); got != OutcomeCurrent || r.installs != 0 {
		t.Fatalf("pass = %s, installs %d", got, r.installs)
	}
}

// Latest keeps the check time and the last install it did not write.
func TestLatestKeepsTheRestOfTheState(t *testing.T) {
	r := newAutoRig(t, "v0.3.0", true)
	if err := MarkInstalled(r.a.StatePath, "v0.2.0", "v0.3.0", r.now); err != nil {
		t.Fatal(err)
	}
	r.a.Pass(t.Context())
	st := LoadState(r.a.StatePath)
	if st.InstalledTo != "v0.3.0" || st.Checked == "" || st.Version != "v0.4.0" || st.Summary != "s" {
		t.Fatalf("state %+v", st)
	}
}

// A served vault with egress.update=deny stops all update traffic: no fetch
// and no egress record. Without one, the check runs and is recorded first.
func TestAutoUnderAnUpdateDenyVault(t *testing.T) {
	r := newAutoRig(t, "v0.4.0", true)
	r.set = SettingsFrom(nil)
	r.a.UpdateDenied = func(context.Context) (string, error) { return "corp", nil }
	if got := r.a.Pass(t.Context()); got != OutcomeDenied || r.manifestHits() != 0 || len(r.dests) != 0 {
		t.Fatalf("pass = %s, %d fetches, egress %v", got, r.manifestHits(), r.dests)
	}
	// A vault that cannot be read counts as a deny.
	r.a.UpdateDenied = func(context.Context) (string, error) { return "", errors.New("unreadable") }
	if got := r.a.Pass(t.Context()); got != OutcomeDenied || r.manifestHits() != 0 {
		t.Fatalf("pass = %s, %d fetches", got, r.manifestHits())
	}
	// An explicit upgrade.check=true does not override the vault.
	r.a.UpdateDenied = func(context.Context) (string, error) { return "corp", nil }
	r.set = SettingsFrom(map[string]string{KeyCheck: "true"})
	if got := r.a.Pass(t.Context()); got != OutcomeDenied || r.manifestHits() != 0 {
		t.Fatalf("pass = %s, %d fetches", got, r.manifestHits())
	}
	r.a.UpdateDenied = func(context.Context) (string, error) { return "", nil }
	if got := r.a.Pass(t.Context()); got != OutcomeCurrent || r.manifestHits() != 1 || len(r.dests) != 1 || r.dests[0] != DestManifest {
		t.Fatalf("pass = %s, %d fetches, egress %v", got, r.manifestHits(), r.dests)
	}
}

// A refused egress decision stops the pass before anything goes out.
func TestAutoStopsWhenEgressRefuses(t *testing.T) {
	r := newAutoRig(t, "v0.3.0", true)
	r.a.Egress = func(context.Context, string) error { return errors.New("vault corp does not allow update:manifest") }
	if got := r.a.Pass(t.Context()); got != OutcomeDenied || r.manifestHits() != 0 || r.installs != 0 {
		t.Fatalf("pass = %s, %d fetches, %d installs", got, r.manifestHits(), r.installs)
	}
	// The check is recorded, so the next try is in a day.
	if st := LoadState(r.a.StatePath); st.Checked == "" || st.CheckError == "" {
		t.Fatalf("state %+v", st)
	}
}

func TestErrorKind(t *testing.T) {
	for err, want := range map[error]string{
		nil: "", &VerifyError{"bad signature"}: "signature",
		fmt.Errorf("%w: vault corp", ErrEgress):                          "egress",
		fmt.Errorf("https://x/m: 404 Not Found: %w", errStatus):          "network",
		&url.Error{Op: "Get", URL: "https://x", Err: errors.New("dial")}: "network",
		errors.New("disk full"):                                          "other",
	} {
		if got := ErrorKind(err); got != want {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
}
