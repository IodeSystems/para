package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// An upgrade from a release: fetch the newest manifest and its signature,
// verify them with a key this build embeds, refuse one older than a manifest
// seen before (a replay: a signed manifest stays valid for ever), refuse a
// version older than this build (a downgrade) unless forced, and install the
// platform's archive over Exe.

// State is what is kept between upgrades, in StateFile: the newest verified
// manifest seen. Nothing from a manifest that failed verification is kept.
type State struct {
	Version   string `json:"version,omitempty"`
	Published string `json:"published,omitempty"`
}

// StateFile is the state's path in an install home.
func StateFile(home string) string { return filepath.Join(home, "update.json") }

// LoadState reads the state; a missing or unreadable file is the zero state.
func LoadState(path string) State {
	var s State
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveState(path string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Upgrader is one upgrade from a release.
type Upgrader struct {
	Product     string // "para": the manifest's product and the binary's name in the archive
	ManifestURL string
	Keys        []PublicKey
	Current     string // this build's release version (vX.Y.Z)
	Exe         string // the binary the new build replaces
	StatePath   string
	Platform    string // Platform(runtime.GOOS, runtime.GOARCH)
	Force       bool   // install even when the release is not newer than Current
	Fetch       *Fetcher
}

// Result is what an upgrade did.
type Result struct {
	From, To  string
	Summary   string
	Installed bool
}

// Run fetches, verifies, and installs when the release is newer (or Force).
func (u *Upgrader) Run(ctx context.Context) (Result, error) {
	res := Result{From: u.Current}
	cur, err := ParseVersion(u.Current)
	if err != nil {
		return res, fmt.Errorf("update: this build: %w", err)
	}
	m, err := u.Latest(ctx)
	if err != nil {
		return res, err
	}
	res.To, res.Summary = m.Version, m.Summary
	mv, _ := ParseVersion(m.Version)
	switch c := mv.Compare(cur); {
	case c < 0 && !u.Force:
		return res, &VerifyError{fmt.Sprintf("the newest release is %s, older than this %s: a downgrade is refused (--force installs it)", m.Version, u.Current)}
	case c == 0 && !u.Force:
		return res, nil
	}
	file, ok := m.Platforms[u.Platform]
	if !ok {
		return res, fmt.Errorf("update: release %s has no build for %s", m.Version, u.Platform)
	}
	if err := os.MkdirAll(filepath.Dir(u.Exe), 0o755); err != nil {
		return res, err
	}
	if _, err := u.Fetch.Install(ctx, file, u.Product, u.Exe); err != nil {
		return res, err
	}
	res.Installed = true
	return res, nil
}

// Latest fetches and verifies the newest manifest, refuses a replay, and
// records it as seen. Nothing is recorded unless every check passed.
func (u *Upgrader) Latest(ctx context.Context) (*Manifest, error) {
	body, err := u.Fetch.Small(ctx, u.ManifestURL, MaxManifest)
	if err != nil {
		return nil, err
	}
	sig, err := u.Fetch.Small(ctx, u.ManifestURL+".minisig", MaxSig)
	if err != nil {
		return nil, err
	}
	m, err := VerifyManifest(body, string(sig), u.Keys, u.Product)
	if err != nil {
		return nil, err
	}
	st := LoadState(u.StatePath)
	mv, _ := ParseVersion(m.Version)
	if prev, err := ParseVersion(st.Version); err == nil && mv.Compare(prev) < 0 {
		return nil, &VerifyError{fmt.Sprintf("the manifest is %s, older than %s seen before (a replay?)", m.Version, st.Version)}
	}
	if p, err := time.Parse(time.RFC3339, st.Published); err == nil && m.PublishedAt().Before(p) {
		return nil, &VerifyError{fmt.Sprintf("the manifest was published %s, before one seen %s (a replay?)", m.Published, st.Published)}
	}
	if err := saveState(u.StatePath, State{Version: m.Version, Published: m.Published}); err != nil {
		return nil, err
	}
	return m, nil
}
