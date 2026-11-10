package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/registry"
)

// A vault whose domain the machine does not hold is not served.
func TestHubRefusesForeignDomain(t *testing.T) {
	src := seededHome(t)
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dst, "home")
	root := filepath.Join(dst, "v-acme")
	if err := os.WriteFile(filepath.Join(home, "vaults", "acme", "vault.ref"), []byte("path="+root+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pp := filepath.Join(root, ".para", "vault.properties")
	raw, err := os.ReadFile(pp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pp, append(raw, []byte("vault.domain=corp\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	in := registry.Install{Home: home}
	if err := in.SetDomains([]string{"life"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, "vaults", "beta", "vault.ref")); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(in, nil)
	t.Cleanup(hub.Close)
	_, err = hub.Prepare()
	if err == nil || !strings.Contains(err.Error(), "vault acme is domain corp, which this machine does not hold (machine.domains: life)") {
		t.Fatalf("Prepare: %v", err)
	}
}
