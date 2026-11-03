package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSetConfigKeepsOtherLinesAndComments(t *testing.T) {
	v, root := newVault(t)
	p := filepath.Join(root, ".para", "vault.properties")
	cur, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append([]byte("# mine\n"), cur...), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := v.SetConfig(ctx, "format_version", "1"); err != nil {
		t.Fatal(err)
	}
	if err := v.SetConfig(ctx, "format_version", "2"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "# mine\n") || strings.Count(string(b), "format_version=") != 1 || !strings.Contains(string(b), "format_version=2\n") {
		t.Fatalf("vault.properties = %q", b)
	}
	if got, _ := v.Config("vault.id"); got == "" {
		t.Fatal("lost vault.id")
	}
	if err := v.SetConfig(ctx, "bad key", "x"); err == nil {
		t.Fatal("accepted a bad key")
	}
}

func TestInitRecordsTheFormatVersion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "v")
	if err := Init(root, "acme", fstest.MapFS{}, InitOptions{NoGit: true, FormatVersion: 3}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, ".para", "vault.properties"))
	if !strings.Contains(string(b), "format_version=3\n") {
		t.Fatalf("vault.properties = %q", b)
	}
}
