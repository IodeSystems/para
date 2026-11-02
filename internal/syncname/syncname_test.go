package syncname

import "testing"

func TestIsConflict(t *testing.T) {
	for name, want := range map[string]bool{
		"note (1).md": true, "note (12).md": true, "note (Ann's conflicted copy 2026-11-01).md": true,
		"note (conflicted copy).md": true, "note.conflict1.md": true, "note.md.conflict2": true,
		"note.sync-conflict-20261101-101500-ABC.md": true, "note.md.orig": true, "note.orig.md": true,
		"note.md": false, "note-1.md": false, "original.md": false, "conflict.md": false,
		"policy-x.source.md": false, "task (draft).md": false, "a.conflicts.md": false,
	} {
		if got := IsConflict(name); got != want {
			t.Errorf("IsConflict(%q) = %v, want %v", name, got, want)
		}
	}
}
