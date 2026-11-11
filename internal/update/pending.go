package update

import (
	"fmt"
	"time"
)

// Announce, then hold. When an auto-install is decided (a newer release, a
// release build, upgrade.auto on) and waits for a quiet moment, the pass
// records it as pending in the machine's update state ($PARA_HOME, never a
// vault). While it is pending, every MCP answer says "para upgrades to vX
// when idle", and claiming new agent work is refused (Hold). The install
// waits for claimed agent work at most Grace from the pending start; then it
// installs anyway, as para cannot interrupt an agent. The rule of no write
// for QuietFor stays.

// Grace is how long a pending install waits for claimed agent work.
const Grace = 10 * time.Minute

// HoldMax ends a hold that no install ended, for a state no para serve
// clears any more (the service stopped): claims are refused at most this long.
const HoldMax = 30 * time.Minute

// Pending returns the release a pending auto-install installs and when it
// started, for a build that counts as current. ok is false when nothing is
// pending: none recorded, or the recorded release is not newer than current
// (it was installed).
func (s State) Pending(current string) (version string, since time.Time, ok bool) {
	if s.PendingVersion == "" {
		return "", time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s.PendingSince)
	if err != nil {
		return "", time.Time{}, false
	}
	pv, err := ParseVersion(s.PendingVersion)
	if err != nil {
		return "", time.Time{}, false
	}
	cur, err := ParseVersion(current)
	if err != nil || pv.Compare(cur) <= 0 {
		return "", time.Time{}, false
	}
	return s.PendingVersion, t, true
}

// MarkPending records that version installs at the next quiet moment. A
// pending record of the same version keeps its start.
func MarkPending(path, version string, now time.Time) error {
	st := LoadState(path)
	if st.PendingVersion == version && st.PendingSince != "" {
		return nil
	}
	st.PendingVersion, st.PendingSince = version, now.UTC().Format(time.RFC3339)
	return saveState(path, st)
}

// ClearPending ends the hold. It writes nothing when none is recorded.
func ClearPending(path string) error {
	st := LoadState(path)
	if st.PendingVersion == "" && st.PendingSince == "" {
		return nil
	}
	st.PendingVersion, st.PendingSince = "", ""
	return saveState(path, st)
}

// markFailed records an auto-install that failed, and ends its hold: a
// release that fails to install does not keep agent work from being claimed.
func markFailed(path, version string, now time.Time) error {
	st := LoadState(path)
	st.PendingVersion, st.PendingSince = "", ""
	st.FailedVersion, st.FailedAt = version, now.UTC().Format(time.RFC3339)
	return saveState(path, st)
}

// failedRecently reports whether version failed to install less than
// CheckEvery ago. Such a release is retried at quiet moments, as before, but
// holds no claim.
func (s State) failedRecently(version string, now time.Time) bool {
	if s.FailedVersion != version {
		return false
	}
	t, err := time.Parse(time.RFC3339, s.FailedAt)
	return err == nil && now.Sub(t) < CheckEvery
}

// PendingNotice is the short line every MCP answer carries while an install
// is pending.
func PendingNotice(version string) string {
	return "para upgrades to " + version + " when idle"
}

// Hold returns the notice for every MCP answer and the refusal of a claim
// of new agent work, both empty while nothing is pending for a build that
// counts as current. A hold older than HoldMax refuses nothing.
func Hold(st State, current string, now time.Time) (notice string, claim error) {
	v, since, ok := st.Pending(current)
	if !ok {
		return "", nil
	}
	notice = PendingNotice(v)
	if now.Sub(since) >= HoldMax {
		return notice, nil
	}
	return notice, fmt.Errorf("para upgrades to %s when idle, so no new agent work is claimed until then. The upgrade waits for claimed work until %s (%s after it was announced), then installs anyway. Finish or note the work you hold (work {action: note}); claim again after the upgrade, then call changes",
		v, since.Add(Grace).UTC().Format("15:04 UTC"), Grace)
}
