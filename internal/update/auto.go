package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The check and the auto-install. `para serve` runs Auto.Pass on a timer. A
// pass fetches the signed manifest at most once a day (CheckEvery, failures
// included), and when a newer release is there it installs it, at a quiet
// moment, through the same code path as `para upgrade --release`. Only a
// release build is ever replaced: a build from a checkout only notifies.
// Nothing from a vault goes out: the fetch is the manifest and its signature.

// Keys in $PARA_HOME/install.properties: per machine, never in a vault.
const (
	KeyCheck      = "upgrade.check"       // false: no check at all
	KeyAuto       = "upgrade.auto"        // false: notify only, never install
	KeyVaultSteps = "upgrade.vault_steps" // auto: run automated vault steps after an auto-install
)

// CheckEvery is the shortest time between two checks.
const CheckEvery = 24 * time.Hour

// QuietFor is how long the machine must see no write before an auto-install.
const QuietFor = 15 * time.Minute

// Settings are the machine's upgrade switches. Check and Auto default on.
// A vault this machine serves with egress.update=deny stops both.
type Settings struct {
	Check      bool
	Auto       bool
	VaultSteps string // "auto" runs automated vault steps; anything else files them as agent work
}

// SettingsFrom reads the switches from install.properties values. Only an
// explicit false (or off, no, 0) turns a switch off.
func SettingsFrom(cfg map[string]string) Settings {
	return Settings{
		Check:      !isOff(cfg[KeyCheck]),
		Auto:       !isOff(cfg[KeyAuto]),
		VaultSteps: strings.ToLower(strings.TrimSpace(cfg[KeyVaultSteps])),
	}
}

func isOff(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "off", "no", "0":
		return true
	}
	return false
}

var releaseRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// IsRelease reports whether a build is a release build: a vX.Y.Z version and
// no checkout recorded (make release sets no srcDir; make install does). A
// checkout at a tag also prints vX.Y.Z, so the checkout decides.
func IsRelease(version, srcDir string) bool {
	return srcDir == "" && releaseRe.MatchString(version)
}

// Due reports whether a check is due at now: none recorded, or the last one
// CheckEvery ago or more. A time in the future (a clock set back) is due.
func (s State) Due(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, s.Checked)
	if err != nil {
		return true
	}
	return now.Sub(t) >= CheckEvery || t.After(now.Add(time.Minute))
}

// Newer reports whether the newest manifest seen is newer than current.
func (s State) Newer(current string) bool {
	seen, err := ParseVersion(s.Version)
	if err != nil {
		return false
	}
	cur, err := ParseVersion(current)
	if err != nil {
		return false
	}
	return seen.Compare(cur) > 0
}

// MarkChecked records a check at now and its error ("" for none), keeping
// the rest of the state.
func MarkChecked(path string, now time.Time, err error) error {
	st := LoadState(path)
	st.Checked = now.UTC().Format(time.RFC3339)
	st.CheckError, st.CheckKind = "", ""
	if err != nil {
		st.CheckError, st.CheckKind = oneLine(err.Error(), 200), ErrorKind(err)
	}
	return saveState(path, st)
}

// ErrEgress marks a check that egress refused.
var ErrEgress = errors.New("egress refused")

// errStatus marks an HTTP answer other than 200.
var errStatus = errors.New("unexpected status")

// ErrorKind is the short category of a failed check, with no URL or path:
// egress, signature (a failed verification, a replay, an oversized or
// non-https download), network (the request or its status), or other.
func ErrorKind(err error) string {
	var ne net.Error
	var ue *url.Error
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrEgress):
		return "egress"
	case IsVerify(err):
		return "signature"
	case errors.Is(err, errStatus) || errors.As(err, &ne) || errors.As(err, &ue):
		return "network"
	}
	return "other"
}

// MarkInstalled records an auto-install, for the "updated" banner and doctor.
func MarkInstalled(path, from, to string, now time.Time) error {
	st := LoadState(path)
	st.InstalledFrom, st.InstalledTo, st.InstalledAt = from, to, now.UTC().Format(time.RFC3339)
	return saveState(path, st)
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		s = s[:max]
	}
	return s
}

// Outcome is what one pass did.
type Outcome string

const (
	OutcomeOff        Outcome = "off"        // upgrade.check=false
	OutcomeDenied     Outcome = "denied"     // a served vault has egress.update=deny, or egress refused
	OutcomeCurrent    Outcome = "current"    // nothing newer
	OutcomeFailed     Outcome = "failed"     // the check failed; the next is in a day
	OutcomeNotifyDev  Outcome = "notify_dev" // newer, but this build is from a checkout
	OutcomeNotifyOff  Outcome = "notify_off" // newer, upgrade.auto=false
	OutcomeWaiting    Outcome = "waiting"    // newer, the machine is not quiet
	OutcomeInstalled  Outcome = "installed"
	OutcomeInstallErr Outcome = "install_failed"
)

// Auto is the timer's work. Every func is required but Log and Now.
type Auto struct {
	StatePath string
	// Current is the release this build counts as: its version, or for a
	// checkout build v<newest release notes>.
	Current string
	Release bool // IsRelease of this build
	// Settings reads install.properties on each pass, so a change needs no restart.
	Settings func() Settings
	// Latest fetches and verifies the manifest (Upgrader.Latest).
	Latest func(ctx context.Context) (*Manifest, error)
	// Egress logs one egress record for dest in every vault this machine
	// serves, before anything goes out. An error, a denied decision
	// included, stops the pass: nothing goes out.
	Egress func(ctx context.Context, dest string) error
	// UpdateDenied names a vault this machine serves that has
	// egress.update=deny, or "". Such a vault stops all update traffic. An
	// error counts as a deny. Nil means no vault denies.
	UpdateDenied func(ctx context.Context) (string, error)
	// Quiet reports whether the machine is quiet now, and why not.
	Quiet func(now time.Time) (bool, string)
	// Install installs the newest release, brings the vaults along, and
	// restarts the service. It is called at most once per pass.
	Install func(ctx context.Context) error
	Now     func() time.Time
	Log     *slog.Logger
}

// Egress destinations of the update check and the download.
const (
	DestManifest = "update:manifest"
	DestRelease  = "update:release"
)

// Pass checks when due and installs when it should.
func (a *Auto) Pass(ctx context.Context) Outcome {
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	log := a.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	set := a.Settings()
	if !set.Check {
		return OutcomeOff
	}
	if a.UpdateDenied != nil {
		id, err := a.UpdateDenied(ctx)
		if err != nil || id != "" {
			log.Debug("update check off: a vault has egress.update=deny", "vault", id, "error", err)
			return OutcomeDenied
		}
	}
	st := LoadState(a.StatePath)
	if st.Due(now) {
		if err := a.Egress(ctx, DestManifest); err != nil {
			// Recorded as a check, so the next try is in a day.
			if merr := MarkChecked(a.StatePath, now, fmt.Errorf("%w: %w", ErrEgress, err)); merr != nil {
				log.Warn("update check: state", "error", merr)
			}
			log.Warn("update check: egress refused", "error", err)
			return OutcomeDenied
		}
		_, err := a.Latest(ctx)
		if merr := MarkChecked(a.StatePath, now, err); merr != nil {
			log.Warn("update check: state", "error", merr)
		}
		if err != nil {
			log.Warn("update check failed; next in a day", "error", err)
			return OutcomeFailed
		}
		st = LoadState(a.StatePath)
		log.Info("update check", "newest", st.Version, "running", a.Current)
	}
	if !st.Newer(a.Current) {
		return OutcomeCurrent
	}
	switch {
	case !a.Release:
		return OutcomeNotifyDev
	case !set.Auto:
		return OutcomeNotifyOff
	}
	if ok, why := a.Quiet(now); !ok {
		log.Debug("update waits for a quiet moment", "version", st.Version, "busy", why)
		return OutcomeWaiting
	}
	if err := a.Egress(ctx, DestRelease); err != nil {
		log.Warn("update install: egress refused", "error", err)
		return OutcomeDenied
	}
	log.Info("update: installing at a quiet moment", "from", a.Current, "to", st.Version)
	if err := a.Install(ctx); err != nil {
		log.Warn("update install failed", "to", st.Version, "error", err)
		return OutcomeInstallErr
	}
	return OutcomeInstalled
}

// Notice is the one line that says where this machine stands, for doctor,
// the web banner, and the MCP server: "" when nothing is newer.
func Notice(st State, current string, release bool, set Settings) string {
	if !st.Newer(current) {
		return ""
	}
	switch {
	case !release:
		return fmt.Sprintf("para %s is available (this build %s is from a checkout: para upgrade pulls and installs it)", st.Version, current)
	case !set.Auto:
		return fmt.Sprintf("para %s is available (this is %s; upgrade.auto=false: the owner runs para upgrade)", st.Version, current)
	}
	return fmt.Sprintf("para %s is available (this is %s): it installs automatically at a quiet moment", st.Version, current)
}
