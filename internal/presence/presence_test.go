package presence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRemove(t *testing.T) {
	dir := t.TempDir()
	if got, err := Read(dir); err != nil || len(got) != 0 {
		t.Fatalf("empty = %v %v", got, err)
	}
	for _, r := range []Record{{PID: 42, Actor: "dun", Push: []string{PushMessage}}, {PID: 7, Actor: "claude", Push: []string{PushMessage, PushChannel}}} {
		if err := Write(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(File(dir, 42))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file = %v %v", info, err)
	}
	if d, err := os.Stat(filepath.Join(dir, Dir)); err != nil || d.Mode().Perm() != 0o700 {
		t.Fatalf("dir = %v %v", d, err)
	}
	// A file whose name is not its pid declares nothing.
	if err := os.WriteFile(filepath.Join(dir, Dir, "9.json"), []byte(`{"pid":10,"actor":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if err != nil || len(got) != 2 || got[0].Actor != "claude" || got[1].Actor != "dun" {
		t.Fatalf("read = %+v %v", got, err)
	}
	if err := Remove(dir, 42); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir, 42); err != nil {
		t.Fatalf("second remove: %v", err)
	}
	if got, _ := Read(dir); len(got) != 1 {
		t.Fatalf("after remove = %+v", got)
	}
	if err := Write("", Record{PID: 1}); err == nil {
		t.Error("write with no state directory")
	}
}
