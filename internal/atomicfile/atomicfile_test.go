package atomicfile

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWrite(t *testing.T) {
	cases := []struct {
		name   string
		before []byte
		data   []byte
	}{
		{"new file", nil, []byte("new\n")},
		{"replace", []byte("old\n"), []byte("new\n")},
		{"empty", []byte("old\n"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "f")
			if c.before != nil {
				os.WriteFile(p, c.before, 0o600)
			}
			if err := Write(p, c.data, 0o640); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(p)
			if !bytes.Equal(got, c.data) {
				t.Fatalf("got %q", got)
			}
			st, _ := os.Stat(p)
			if st.Mode().Perm() != 0o640 {
				t.Errorf("mode %v", st.Mode())
			}
			if left, _ := os.ReadDir(dir); len(left) != 1 {
				t.Errorf("dir holds %v, want only the target", left)
			}
		})
	}
}

func TestWriteFailureKeepsNothing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "is-a-dir")
	os.MkdirAll(filepath.Join(target, "child"), 0o700)
	if err := Write(target, []byte("x"), 0o600); err == nil {
		t.Fatal("rename over a non-empty directory must fail")
	}
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Errorf("temp files left: %v", left)
	}
}

// A reader during replacement sees one whole version, never a mix.
func TestWriteReaderNeverSeesPartial(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	a, b := bytes.Repeat([]byte("a"), 1<<16), bytes.Repeat([]byte("b"), 1<<16)
	if err := Write(p, a, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			got, err := os.ReadFile(p)
			if err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(got, a) && !bytes.Equal(got, b) {
				t.Errorf("partial read: %d bytes", len(got))
				return
			}
		}
	}()
	for i := range 50 {
		data := a
		if i%2 == 0 {
			data = b
		}
		if err := Write(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	close(done)
	wg.Wait()
}
