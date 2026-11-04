package keys

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s := New(filepath.Join(t.TempDir(), "server"))
	if _, err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAdminTokenIsStableAndPrivate(t *testing.T) {
	s := newStore(t)
	info, err := os.Stat(s.AdminPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %o", info.Mode().Perm())
	}
	a, err := s.AdminToken()
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.Ensure()
	if err != nil || created {
		t.Fatalf("second Ensure: %v %v", created, err)
	}
	if b, _ := s.AdminToken(); a != b {
		t.Error("token changed")
	}
	id, err := s.Bearer(a)
	if err != nil || !id.Admin || id.Name != AdminName {
		t.Fatalf("%+v %v", id, err)
	}
	if err := os.Chmod(s.AdminPath(), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminToken(); err == nil {
		t.Error("a group-readable token must be refused")
	}
	if _, err := s.Bearer(a); !errors.Is(err, ErrInvalid) {
		t.Errorf("%v", err)
	}
}

func TestKeyFileHoldsOnlyAHash(t *testing.T) {
	s := newStore(t)
	k, tok, err := s.Create("agent", []string{"vault:read"}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.keyPath("agent"))
	if err != nil {
		t.Fatal(err)
	}
	sec := tok[strings.LastIndex(tok, "agent_")+len("agent_"):]
	if strings.Contains(string(b), sec) {
		t.Error("the secret is on disk")
	}
	if !strings.Contains(string(b), k.Hash) {
		t.Error("no hash")
	}
	id, err := s.Bearer(tok)
	if err != nil || id.Name != "agent" || id.Scopes[0] != "vault:read" {
		t.Fatalf("%+v %v", id, err)
	}
	for _, bad := range []string{tok + "x", "para_agent_", "para_agent", "para_missing_abc", "para_../x_abc", "other", ""} {
		if _, err := s.Bearer(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestCreateRefusals(t *testing.T) {
	s := newStore(t)
	for _, tc := range []struct {
		name   string
		scopes []string
		ips    []string
		ttl    time.Duration
	}{
		{"admin", []string{"vault:read"}, nil, 0},
		{"Bad_Name", []string{"vault:read"}, nil, 0},
		{"../x", []string{"vault:read"}, nil, 0},
		{"ok", nil, nil, 0},
		{"ok", []string{"vault:read"}, []string{"nope"}, 0},
		{"ok", []string{"vault:read"}, nil, -time.Hour},
	} {
		if _, _, err := s.Create(tc.name, tc.scopes, tc.ips, tc.ttl); err == nil {
			t.Errorf("%+v was accepted", tc)
		}
	}
	if _, _, err := s.Create("once", []string{"vault:read"}, nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create("once", []string{"vault:read"}, nil, 0); !errors.Is(err, ErrExists) {
		t.Errorf("reuse: %v", err)
	}
	// A revoked name stays taken, so an event's actor keeps one meaning.
	if _, err := s.Revoke("once"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create("once", []string{"vault:read"}, nil, 0); !errors.Is(err, ErrExists) {
		t.Errorf("reuse after revoke: %v", err)
	}
}

func TestExpiryAndRevocation(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	s.Now = func() time.Time { return now }
	_, tok, err := s.Create("k", []string{"vault:read"}, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Bearer(tok); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return now.Add(time.Hour) }
	if _, err := s.Bearer(tok); !errors.Is(err, ErrInvalid) {
		t.Errorf("expired: %v", err)
	}
	s.Now = func() time.Time { return now }
	if _, err := s.Revoke("k"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Bearer(tok); !errors.Is(err, ErrInvalid) {
		t.Errorf("revoked: %v", err)
	}
	if _, err := s.Revoke("missing"); !errors.Is(err, ErrUnknown) {
		t.Errorf("%v", err)
	}
	if _, err := s.Revoke("../x"); !errors.Is(err, ErrUnknown) {
		t.Errorf("%v", err)
	}
}

func TestSessionFollowsItsKey(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	s.Now = func() time.Time { return now }
	_, tok, err := s.Create("k", []string{"vault:read"}, []string{"192.0.2.0/24"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Bearer(tok)
	if err != nil {
		t.Fatal(err)
	}
	sess, exp, err := s.NewSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if exp.After(now.Add(time.Hour)) {
		t.Errorf("session outlives its key: %v", exp)
	}
	got, err := s.Session(sess)
	if err != nil || got.Name != "k" || !got.Session || got.AllowsIP(netip.MustParseAddr("198.51.100.1")) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.Session(tok); !errors.Is(err, ErrInvalid) {
		t.Error("a key is not a session token")
	}
	if _, err := s.Bearer(sess); !errors.Is(err, ErrInvalid) {
		t.Error("a session token is not a bearer token")
	}
	if err := s.EndSession(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session(sess); !errors.Is(err, ErrInvalid) {
		t.Error("ended session works")
	}
	sess, _, err = s.NewSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revoke("k"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session(sess); !errors.Is(err, ErrInvalid) {
		t.Error("session outlived a revoke")
	}
}

func TestAllowsIP(t *testing.T) {
	id := Identity{AllowIP: []string{"192.0.2.5", "198.51.100.0/24", "::ffff:203.0.113.0/120"}}
	for ip, want := range map[string]bool{"192.0.2.5": true, "192.0.2.6": false, "198.51.100.200": true, "203.0.113.9": true, "2001:db8::1": false} {
		if got := id.AllowsIP(netip.MustParseAddr(ip)); got != want {
			t.Errorf("%s: %v", ip, got)
		}
	}
	if id.AllowsIP(netip.Addr{}) {
		t.Error("an unknown address must fail a pin")
	}
	if !(Identity{}).AllowsIP(netip.Addr{}) {
		t.Error("no pin allows")
	}
}

func TestListHidesNothingButIsSorted(t *testing.T) {
	s := newStore(t)
	for _, n := range []string{"b", "a"} {
		if _, _, err := s.Create(n, []string{"vault:read"}, nil, 0); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List()
	if err != nil || len(list) != 2 || list[0].Name != "a" {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestCreateLoginLife(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.CreateLogin([]string{"vault:read"}, LoginMaxTTL+time.Hour); err == nil {
		t.Error("a login key over 90 days was made")
	}
	if _, _, err := s.CreateLogin([]string{"vault:read"}, 0); err == nil {
		t.Error("a login key with no expiry was made")
	}
	k, tok, err := s.CreateLogin([]string{"vault:read", "review:decide"}, LoginTTL)
	if err != nil {
		t.Fatal(err)
	}
	if !k.Login || !strings.HasPrefix(k.Name, "viewer-") || k.Expires == nil || k.Expires.Sub(k.Created) != LoginTTL {
		t.Errorf("login key %+v", k)
	}
	id, err := s.Bearer(tok)
	if err != nil || !id.Login {
		t.Errorf("bearer %+v %v", id, err)
	}
	if got := s.LastUsed(k.Name); got.IsZero() {
		t.Error("use not noted")
	}
	plain, _, _ := s.Create("api", []string{"vault:read"}, nil, time.Hour)
	if plain.Login {
		t.Error("a plain key is a login key")
	}
	act, err := s.ActiveLogins()
	if err != nil || len(act) != 1 || act[0].Name != k.Name {
		t.Errorf("active logins %+v %v", act, err)
	}
}

func TestRenew(t *testing.T) {
	s := newStore(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	scopes := []string{"vault:read", "review:decide"}
	old, oldTok, err := s.CreateLogin(scopes, LoginTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Renew(old.Name); !errors.Is(err, ErrNotRenewable) {
		t.Errorf("renew 30 days out: %v", err)
	}
	now = now.Add(LoginTTL - 3*24*time.Hour)
	k, tok, err := s.Renew(old.Name)
	if err != nil {
		t.Fatal(err)
	}
	if k.Name == old.Name || !k.Login || strings.Join(k.Scopes, ",") != strings.Join(scopes, ",") || k.Expires.Sub(now) != LoginTTL {
		t.Errorf("renewed %+v", k)
	}
	if _, err := s.Bearer(oldTok); !errors.Is(err, ErrInvalid) {
		t.Error("the old key still works")
	}
	if _, err := s.Bearer(tok); err != nil {
		t.Errorf("the new key: %v", err)
	}
	if _, _, err := s.Renew(old.Name); !errors.Is(err, ErrInvalid) {
		t.Errorf("a revoked key renewed: %v", err)
	}
	// Expired: refused.
	now = now.Add(LoginTTL + time.Second)
	if _, _, err := s.Renew(k.Name); !errors.Is(err, ErrInvalid) {
		t.Errorf("an expired key renewed: %v", err)
	}
	// A plain key cannot renew, even near its expiry.
	plain, _, _ := s.Create("api", scopes, nil, time.Hour)
	if _, _, err := s.Renew(plain.Name); !errors.Is(err, ErrNotRenewable) {
		t.Errorf("a plain key renewed: %v", err)
	}
	// A short login key renews to the default life.
	short, _, _ := s.CreateLogin(scopes, 24*time.Hour)
	k2, _, err := s.Renew(short.Name)
	if err != nil || k2.Expires.Sub(now) != LoginTTL {
		t.Errorf("short renew %+v %v", k2, err)
	}
}
