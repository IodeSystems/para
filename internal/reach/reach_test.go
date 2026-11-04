package reach

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	if got, err := Read(dir); err != nil || len(got) != 0 {
		t.Fatalf("empty = %v %v", got, err)
	}
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	for _, a := range []string{"claude", "../evil one"} {
		if err := Write(dir, Declaration{Actor: a, Install: "m1", At: at.Format(time.RFC3339), Sources: []string{"mail"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, Dir, "claude.json")); err != nil {
		t.Fatal(err)
	}
	if got := File(dir, "../evil one"); filepath.Dir(got) != filepath.Join(dir, Dir) {
		t.Fatalf("escaped: %s", got)
	}
	got, err := Read(dir)
	if err != nil || len(got) != 2 || got[0].Actor != "../evil one" || !got[1].Fresh(at) || got[1].Fresh(at.Add(time.Second)) {
		t.Fatalf("read = %+v %v", got, err)
	}
	if err := Write("", Declaration{Actor: "x"}); err == nil {
		t.Fatal("no state dir must fail")
	}
}
