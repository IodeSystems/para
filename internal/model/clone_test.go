package model

import (
	"reflect"
	"testing"
)

func TestCloneIsDeep(t *testing.T) {
	fm := []byte("type: task\ncreated: 2026-11-01\nstatus: todo\ntags: [a, b]\nblocked_by: [x]\nimported_from:\n  path: old.md\nkept:\n  - \"[[task-other]]\"\n")
	d, err := Decode("task-a", FolderGoals, fm, "Title", "Body.")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := Render(d)
	c := d.Clone()
	if !reflect.DeepEqual(c, d) {
		t.Fatal("clone differs")
	}
	ct := c.Meta.(*Task)
	ct.Tags[0], ct.BlockedBy[0], ct.ImportedFrom.Path, ct.Status = "z", "y", "new.md", TaskDone
	c.extra["kept"].Content[0].Value = "changed"
	if got, _ := Render(d); string(got) != string(want) {
		t.Fatalf("original changed:\n%s", got)
	}
}

// Every type clones without a panic and equal to itself.
func TestCloneEveryType(t *testing.T) {
	for _, typ := range Types {
		m, ok := NewMeta(typ)
		if !ok {
			continue
		}
		d := Document{ID: "x", Meta: m}
		if c := d.Clone(); !reflect.DeepEqual(c, d) || c.Meta == d.Meta {
			t.Errorf("%s: clone not a deep copy", typ)
		}
	}
}
