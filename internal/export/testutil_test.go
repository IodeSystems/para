package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTools puts shell scripts on a temp PATH, replacing the real PATH so no
// real tool runs. Each script is name -> body. It returns the bin dir.
func fakeTools(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range scripts {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Only the fakes and a few coreutils are on PATH, so no real tmux, ssh,
	// wormhole, or clipboard tool can run or be detected.
	util := t.TempDir()
	for _, name := range []string{"cat", "cp", "stat", "touch", "sh"} {
		for _, d := range []string{"/usr/bin", "/bin"} {
			if _, err := os.Stat(filepath.Join(d, name)); err == nil {
				if err := os.Symlink(filepath.Join(d, name), filepath.Join(util, name)); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+util)
	return dir
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func contains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("lacks %q in:\n%s", w, got)
		}
	}
}
