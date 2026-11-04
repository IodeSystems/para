// Package keys holds the credentials of one vault on this machine, under
// $PARA_HOME/vaults/<id>/server/. It stores SHA-256 hashes and compares them in
// constant time (AUTH-2). The one secret kept in the clear is admin.token, at
// mode 0600, because login-admin must read it back.
//
// Each key and each session is its own file, so `para key` and a running
// server share the directory with no lock, and a revoke applies to the next
// request.
package keys

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/atomicfile"
)

// AdminName is the actor of the admin token. No key can take this name.
const AdminName = "admin"

// SessionTTL is the life of a browser session, cut short by its key's expiry.
const SessionTTL = 365 * 24 * time.Hour

const (
	tokenPrefix = "para_"
	secretBytes = 32
)

var nameRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// Errors.
var (
	// ErrInvalid is every failed credential: unknown, wrong, expired, or
	// revoked. The caller cannot tell them apart.
	ErrInvalid = errors.New("keys: invalid credential")
	ErrExists  = errors.New("keys: name already used")
	ErrUnknown = errors.New("keys: no such key")
)

// Key is one named key. Hash is never returned by the API.
type Key struct {
	Name    string     `json:"name"`
	Scopes  []string   `json:"scopes"`
	AllowIP []string   `json:"allow_ip,omitempty"`
	Created time.Time  `json:"created"`
	Expires *time.Time `json:"expires,omitempty"`
	Revoked *time.Time `json:"revoked,omitempty"`
	// Login marks a browser sign-in key made by `para serve login` or `para
	// open`. Only a login key can renew, and logout revokes only these.
	Login bool   `json:"login,omitempty"`
	Hash  string `json:"hash"`
}

type session struct {
	Key     string    `json:"key"`
	Hash    string    `json:"hash"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
}

// Store is the credential directory of one vault.
type Store struct {
	dir string
	Now func() time.Time
}

// New returns the Store at dir, $PARA_HOME/vaults/<id>/server.
func New(dir string) *Store { return &Store{dir: dir, Now: time.Now} }

// Dir returns the credential directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) keyPath(name string) string { return filepath.Join(s.dir, "keys", name+".json") }

func (s *Store) sessionPath(hash string) string {
	return filepath.Join(s.dir, "sessions", hash+".json")
}

// AdminPath is the admin token file.
func (s *Store) AdminPath() string { return filepath.Join(s.dir, "admin.token") }

// Ensure creates the directory and the admin token. It reports whether it made
// a new token.
func (s *Store) Ensure() (bool, error) {
	for _, d := range []string{s.dir, filepath.Join(s.dir, "keys"), filepath.Join(s.dir, "sessions")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return false, fmt.Errorf("keys: ensure: %w", err)
		}
	}
	if _, err := os.Stat(s.AdminPath()); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("keys: ensure: %w", err)
	}
	tok, err := secret()
	if err != nil {
		return false, err
	}
	if err := atomicfile.Write(s.AdminPath(), []byte(tok+"\n"), 0o600); err != nil {
		return false, fmt.Errorf("keys: ensure: %w", err)
	}
	return true, nil
}

// AdminToken reads the admin token. A file another user can read is refused.
func (s *Store) AdminToken() (string, error) {
	info, err := os.Stat(s.AdminPath())
	if err != nil {
		return "", fmt.Errorf("keys: admin token: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("keys: admin token: %s has mode %o, want 600", s.AdminPath(), info.Mode().Perm())
	}
	b, err := os.ReadFile(s.AdminPath())
	if err != nil {
		return "", fmt.Errorf("keys: admin token: %w", err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("keys: admin token: %s is empty", s.AdminPath())
	}
	return tok, nil
}

// ValidName reports whether name can name a key. A key name is the actor in
// events.
func ValidName(name string) bool { return nameRe.MatchString(name) && name != AdminName }

// ParseAllowIP checks and normalizes an --allow-ip list: addresses and CIDRs.
func ParseAllowIP(list []string) ([]string, error) {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if _, err := parseBlock(v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func parseBlock(v string) (netip.Prefix, error) {
	if a, err := netip.ParseAddr(v); err == nil {
		a = a.Unmap().WithZone("")
		return netip.PrefixFrom(a, a.BitLen()), nil
	}
	p, err := netip.ParsePrefix(v)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("keys: bad address %q", v)
	}
	if p.Addr().Is4In6() && p.Bits() >= 96 {
		return netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96).Masked(), nil
	}
	return p.Masked(), nil
}

// Create makes a key and returns its token. The token is shown once. A name is
// never reused, so an event's actor keeps one meaning.
func (s *Store) Create(name string, scopes, allowIP []string, ttl time.Duration) (Key, string, error) {
	return s.create(name, scopes, allowIP, ttl, false)
}

func (s *Store) create(name string, scopes, allowIP []string, ttl time.Duration, login bool) (Key, string, error) {
	if !ValidName(name) {
		return Key{}, "", fmt.Errorf("keys: create: bad name %q: use lowercase letters, digits, and hyphens", name)
	}
	if len(scopes) == 0 {
		return Key{}, "", errors.New("keys: create: at least one scope is required")
	}
	if ttl < 0 {
		return Key{}, "", errors.New("keys: create: ttl must be positive")
	}
	if _, err := s.Ensure(); err != nil {
		return Key{}, "", err
	}
	if _, err := ParseAllowIP(allowIP); err != nil {
		return Key{}, "", fmt.Errorf("keys: create: %w", err)
	}
	sec, err := secret()
	if err != nil {
		return Key{}, "", err
	}
	now := s.Now().UTC()
	k := Key{Name: name, Scopes: scopes, AllowIP: allowIP, Created: now, Login: login, Hash: hash(sec)}
	if ttl > 0 {
		e := now.Add(ttl)
		k.Expires = &e
	}
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return Key{}, "", fmt.Errorf("keys: create: %w", err)
	}
	// Link fails on an existing name, so the name is claimed and the file
	// appears whole, in one step.
	tmp := filepath.Join(s.dir, "keys", "."+name+"."+sec[:8])
	if err := atomicfile.Write(tmp, data, 0o600); err != nil {
		return Key{}, "", fmt.Errorf("keys: create: %w", err)
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := os.Link(tmp, s.keyPath(name)); errors.Is(err, fs.ErrExist) {
		return Key{}, "", fmt.Errorf("%w: %s", ErrExists, name)
	} else if err != nil {
		return Key{}, "", fmt.Errorf("keys: create: %w", err)
	}
	return k, tokenPrefix + name + "_" + sec, nil
}

// List returns every key, revoked keys included, sorted by name.
func (s *Store) List() ([]Key, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "keys"))
	if errors.Is(err, fs.ErrNotExist) {
		return []Key{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("keys: list: %w", err)
	}
	out := []Key{}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || strings.HasPrefix(name, ".") {
			continue
		}
		k, err := s.load(name)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func (s *Store) load(name string) (Key, error) {
	var k Key
	b, err := os.ReadFile(s.keyPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return k, fmt.Errorf("%w: %s", ErrUnknown, name)
	}
	if err != nil {
		return k, fmt.Errorf("keys: read %s: %w", name, err)
	}
	if err := json.Unmarshal(b, &k); err != nil {
		return k, fmt.Errorf("keys: read %s: %w", name, err)
	}
	return k, nil
}

// Revoke marks a key revoked. Its sessions die with it, because a session
// resolves through its key on each request. Revoking a revoked key is a no-op.
func (s *Store) Revoke(name string) (Key, error) {
	if !nameRe.MatchString(name) {
		return Key{}, fmt.Errorf("%w: %s", ErrUnknown, name)
	}
	k, err := s.load(name)
	if err != nil {
		return k, err
	}
	if k.Revoked != nil {
		return k, nil
	}
	now := s.Now().UTC()
	k.Revoked = &now
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return k, fmt.Errorf("keys: revoke: %w", err)
	}
	if err := atomicfile.Write(s.keyPath(name), data, 0o600); err != nil {
		return k, fmt.Errorf("keys: revoke: %w", err)
	}
	return k, nil
}

// Identity is what a valid credential resolves to.
type Identity struct {
	Name    string
	Admin   bool
	Scopes  []string
	AllowIP []string
	Session bool
	Expires *time.Time
	Login   bool // a login key: it can renew
}

// AllowsIP reports whether addr may use this identity. An empty list allows
// every address that the IP rules let through.
func (id Identity) AllowsIP(addr netip.Addr) bool {
	if len(id.AllowIP) == 0 {
		return true
	}
	if !addr.IsValid() {
		return false
	}
	return slices.ContainsFunc(id.AllowIP, func(v string) bool {
		b, err := parseBlock(v)
		return err == nil && b.Contains(addr)
	})
}

// Bearer resolves a token from an Authorization header: a key or the admin
// token.
func (s *Store) Bearer(token string) (Identity, error) {
	if rest, ok := strings.CutPrefix(token, tokenPrefix); ok {
		name, sec, ok := strings.Cut(rest, "_")
		if !ok || !nameRe.MatchString(name) {
			return Identity{}, ErrInvalid
		}
		return s.byKey(name, hash(sec), s.Now())
	}
	want, err := s.AdminToken()
	if err != nil {
		return Identity{}, ErrInvalid
	}
	if !equal(hash(token), hash(want)) {
		return Identity{}, ErrInvalid
	}
	return Identity{Name: AdminName, Admin: true}, nil
}

func (s *Store) byKey(name, h string, now time.Time) (Identity, error) {
	k, err := s.load(name)
	if err != nil {
		return Identity{}, ErrInvalid
	}
	if !equal(h, k.Hash) {
		return Identity{}, ErrInvalid
	}
	id, err := k.identity(now)
	if err == nil {
		s.markUsed(k, now)
	}
	return id, err
}

func (k Key) identity(now time.Time) (Identity, error) {
	if k.Revoked != nil || (k.Expires != nil && !now.Before(*k.Expires)) {
		return Identity{}, ErrInvalid
	}
	return Identity{Name: k.Name, Scopes: k.Scopes, AllowIP: k.AllowIP, Expires: k.Expires, Login: k.Login}, nil
}

// Session resolves the token of a session cookie.
func (s *Store) Session(token string) (Identity, error) {
	h := hash(token)
	b, err := os.ReadFile(s.sessionPath(h))
	if err != nil {
		return Identity{}, ErrInvalid
	}
	var se session
	if err := json.Unmarshal(b, &se); err != nil || !equal(h, se.Hash) {
		return Identity{}, ErrInvalid
	}
	now := s.Now()
	if !now.Before(se.Expires) {
		_ = os.Remove(s.sessionPath(h))
		return Identity{}, ErrInvalid
	}
	if se.Key == AdminName {
		return Identity{Name: AdminName, Admin: true, Session: true}, nil
	}
	k, err := s.load(se.Key)
	if err != nil {
		return Identity{}, ErrInvalid
	}
	id, err := k.identity(now)
	id.Session = true
	if err == nil {
		s.markUsed(k, now)
	}
	return id, err
}

// NewSession makes a session for an identity and returns its token and expiry.
func (s *Store) NewSession(id Identity) (string, time.Time, error) {
	tok, err := secret()
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.Now().UTC()
	exp := now.Add(SessionTTL)
	if id.Expires != nil && id.Expires.Before(exp) {
		exp = *id.Expires
	}
	h := hash(tok)
	data, err := json.Marshal(session{Key: id.Name, Hash: h, Created: now, Expires: exp})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("keys: session: %w", err)
	}
	if err := atomicfile.Write(s.sessionPath(h), data, 0o600); err != nil {
		return "", time.Time{}, fmt.Errorf("keys: session: %w", err)
	}
	return tok, exp, nil
}

// EndSession removes a session. An unknown token is a no-op.
func (s *Store) EndSession(token string) error {
	err := os.Remove(s.sessionPath(hash(token)))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("keys: end session: %w", err)
	}
	return nil
}

func hash(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// equal compares two hashes in constant time.
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func secret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("keys: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login keys: the browser sign-in of `para serve login`.
const (
	// LoginTTL is the default life of a login key.
	LoginTTL = 30 * 24 * time.Hour
	// LoginMaxTTL is the longest life a login key can have.
	LoginMaxTTL = 90 * 24 * time.Hour
	// RenewWindow is how close to its expiry a login key can renew.
	RenewWindow = 7 * 24 * time.Hour
	// loginPrefix starts the name of every login key.
	loginPrefix = "viewer-"
	// usedEvery is how often the last use of a login key is written.
	usedEvery = time.Minute
)

// ErrNotRenewable is a renew of a key that is not a login key with an expiry,
// or one not yet within RenewWindow of it.
var ErrNotRenewable = errors.New("keys: not renewable")

// CreateLogin makes a login key: viewer-<time>-<random>.
func (s *Store) CreateLogin(scopes []string, ttl time.Duration) (Key, string, error) {
	return s.createLogin(scopes, nil, ttl)
}

func (s *Store) createLogin(scopes, allowIP []string, ttl time.Duration) (Key, string, error) {
	if ttl <= 0 || ttl > LoginMaxTTL {
		return Key{}, "", fmt.Errorf("keys: create login: life must be more than 0 and at most %d days", int(LoginMaxTTL/(24*time.Hour)))
	}
	sfx, err := secret()
	if err != nil {
		return Key{}, "", err
	}
	tag := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(sfx))
	if len(tag) > 6 {
		tag = tag[:6]
	}
	name := loginPrefix + s.Now().UTC().Format("20060102-150405") + "-" + tag
	return s.create(name, scopes, allowIP, ttl, true)
}

// ActiveLogins returns the login keys that are not revoked or expired.
func (s *Store) ActiveLogins() ([]Key, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	now := s.Now()
	out := []Key{}
	for _, k := range list {
		if k.Login && k.Revoked == nil && (k.Expires == nil || now.Before(*k.Expires)) {
			out = append(out, k)
		}
	}
	return out, nil
}

// Renew replaces a valid login key within RenewWindow of its expiry: a new
// login key with the same scopes and address list, and the old one revoked.
// The new key lives as long as the old one did, or LoginTTL when that was no
// longer than RenewWindow. An expired or revoked key cannot renew.
func (s *Store) Renew(name string) (Key, string, error) {
	if !nameRe.MatchString(name) {
		return Key{}, "", ErrInvalid
	}
	old, err := s.load(name)
	if err != nil {
		return Key{}, "", ErrInvalid
	}
	now := s.Now()
	if _, err := old.identity(now); err != nil {
		return Key{}, "", ErrInvalid
	}
	if !old.Login || old.Expires == nil {
		return Key{}, "", fmt.Errorf("%w: %s is not a login key", ErrNotRenewable, name)
	}
	if old.Expires.Sub(now) > RenewWindow {
		return Key{}, "", fmt.Errorf("%w: %s is not within %d days of its expiry", ErrNotRenewable, name, int(RenewWindow/(24*time.Hour)))
	}
	ttl := old.Expires.Sub(old.Created)
	if ttl <= RenewWindow {
		ttl = LoginTTL
	}
	ttl = min(ttl, LoginMaxTTL)
	k, tok, err := s.createLogin(old.Scopes, old.AllowIP, ttl)
	if err != nil {
		return Key{}, "", err
	}
	if _, err := s.Revoke(name); err != nil {
		_, _ = s.Revoke(k.Name)
		return Key{}, "", err
	}
	return k, tok, nil
}

func (s *Store) usedPath(name string) string { return filepath.Join(s.dir, "used", name) }

// markUsed notes the use of a login key, at most once a minute. The time is the
// mtime of an empty file, so the key file is never rewritten.
func (s *Store) markUsed(k Key, now time.Time) {
	if !k.Login {
		return
	}
	p := s.usedPath(k.Name)
	if info, err := os.Stat(p); err == nil {
		if now.Sub(info.ModTime()) < usedEvery {
			return
		}
	} else if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	} else if err := os.WriteFile(p, nil, 0o600); err != nil {
		return
	}
	_ = os.Chtimes(p, now, now)
}

// LastUsed is when a login key was last used, or the zero time.
func (s *Store) LastUsed(name string) time.Time {
	if !nameRe.MatchString(name) {
		return time.Time{}
	}
	info, err := os.Stat(s.usedPath(name))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
