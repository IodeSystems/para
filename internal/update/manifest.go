package update

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A manifest is what one release publishes beside its archives
// (para-manifest.json, signed as a whole by para-manifest.json.minisig):
//
//	{"product": "para", "version": "v0.3.0", "published": "2026-11-10T10:00:00Z",
//	 "summary": "one line on what changed",
//	 "platforms": {"linux_amd64": {"url": "https://…/para_linux_amd64.tar.gz",
//	   "sha256": "…", "size": 123}}}
//
// The signing key is shared with dun. The minisign trusted comment, which the
// signature covers, must be "<product> <version> manifest" (make release
// signs with -t "para $(VERSION) manifest"), and the product field must be
// the product asked for. A dun manifest fails both.
//
// para never trusts a key from a manifest: a key_next field is ignored. A
// key rotation ships a release, signed with the old key, that embeds the new
// one in release/minisign.pub.

// Limits on what is read from the network.
const (
	MaxManifest = 1 << 20
	MaxSig      = 4 << 10
	MaxArchive  = 512 << 20
)

type Manifest struct {
	Product   string          `json:"product"`
	Version   string          `json:"version"`
	Published string          `json:"published"`
	Summary   string          `json:"summary,omitempty"`
	Platforms map[string]File `json:"platforms"`
}

type File struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Platform is a platforms key: "linux_amd64".
func Platform(goos, goarch string) string { return goos + "_" + goarch }

// TrustedComment is the trusted comment a product's manifest is signed with.
func TrustedComment(product, version string) string {
	return product + " " + version + " manifest"
}

// ParseManifest reads and checks a manifest's shape. It does not verify a
// signature: VerifyManifest does.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if m.Product == "" {
		return nil, errors.New("manifest: no product")
	}
	if _, err := ParseVersion(m.Version); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if m.PublishedAt().IsZero() {
		return nil, errors.New("manifest: no publication time")
	}
	if len(m.Platforms) == 0 {
		return nil, errors.New("manifest: no platforms")
	}
	for plat, f := range m.Platforms {
		if err := f.check(); err != nil {
			return nil, fmt.Errorf("manifest: %s: %w", plat, err)
		}
	}
	return &m, nil
}

func (f File) check() error {
	u, err := url.Parse(f.URL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("bad url %q", f.URL)
	}
	if h, err := hex.DecodeString(f.SHA256); err != nil || len(h) != 32 {
		return fmt.Errorf("bad sha256 %q", f.SHA256)
	}
	if f.Size <= 0 || f.Size > MaxArchive {
		return fmt.Errorf("bad size %d", f.Size)
	}
	return nil
}

// PublishedAt is the manifest's publication time (zero when unset).
func (m *Manifest) PublishedAt() time.Time {
	t, _ := time.Parse(time.RFC3339, m.Published)
	return t
}

// VerifyManifest checks body's signature with one of keys (the signature and
// the global signature over the trusted comment), then that the manifest is
// product's: its product field, and a trusted comment of exactly
// TrustedComment(product, version). Only then is it returned; the caller
// records nothing from a manifest that fails.
func VerifyManifest(body []byte, sigText string, keys []PublicKey, product string) (*Manifest, error) {
	if len(keys) == 0 {
		return nil, ErrNoKey
	}
	sig, err := ParseSignature(sigText)
	if err != nil {
		return nil, &VerifyError{"the manifest's signature: " + err.Error()}
	}
	if _, err := VerifyAny(body, sigText, keys); err != nil {
		return nil, &VerifyError{"the manifest's signature: " + err.Error()}
	}
	m, err := ParseManifest(body)
	if err != nil {
		return nil, &VerifyError{err.Error()}
	}
	if m.Product != product {
		return nil, &VerifyError{fmt.Sprintf("the manifest is for %q, not %q", m.Product, product)}
	}
	if want := TrustedComment(product, m.Version); sig.Trusted != want {
		return nil, &VerifyError{fmt.Sprintf("the manifest's trusted comment is %q, want %q", sig.Trusted, want)}
	}
	return m, nil
}

// VerifyError is a check that failed on what was downloaded — a signature,
// a checksum, an archive, a version — as opposed to a network that did not
// answer.
type VerifyError struct{ why string }

func (e *VerifyError) Error() string { return e.why }

// IsVerify reports whether err is a failed check.
func IsVerify(err error) bool {
	var v *VerifyError
	return errors.As(err, &v)
}

// Version is a parsed vMAJOR.MINOR.PATCH[-pre][+build].
type Version struct {
	n   [3]int
	pre string
}

// ParseVersion reads a release version; anything else ("dev", a git
// describe with a commit count) is an error.
func ParseVersion(s string) (Version, error) {
	rest, ok := strings.CutPrefix(s, "v")
	if !ok {
		return Version{}, fmt.Errorf("not a release version: %q", s)
	}
	rest, _, _ = strings.Cut(rest, "+")
	core, pre, _ := strings.Cut(rest, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("not a release version: %q", s)
	}
	var v Version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return Version{}, fmt.Errorf("not a release version: %q", s)
		}
		v.n[i] = n
	}
	v.pre = pre
	return v, nil
}

// Compare is -1, 0 or 1. A pre-release is before its release; two
// pre-releases compare as strings (a simplification of semver's rule).
func (a Version) Compare(b Version) int {
	for i := range a.n {
		if a.n[i] != b.n[i] {
			if a.n[i] < b.n[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}
