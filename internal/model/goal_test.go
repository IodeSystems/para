package model

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGoalDecodes(t *testing.T) {
	d, err := Decode("goal-x", FolderGoals, []byte("type: goal\ncreated: 2026-11-11\nstatus: parked\nowner: self\nproject: \"[[project-y]]\"\nresume_when: the vendor ships 2.0\nformer_ids: [project-x]\n"), "Ship it", "")
	if err != nil {
		t.Fatal(err)
	}
	g, ok := d.Meta.(*Goal)
	if !ok {
		t.Fatalf("meta %T, want *Goal", d.Meta)
	}
	if g.Status != GoalParked || g.ResumeWhen != "the vendor ships 2.0" || g.Project.Target() != "project-y" {
		t.Errorf("goal %+v", g)
	}
	if !g.WasID("project-x") || g.WasID("goal-x") {
		t.Errorf("former ids %v", g.FormerIDs)
	}
	if FolderOf(TypeGoal) != FolderGoals {
		t.Errorf("folder %s", FolderOf(TypeGoal))
	}
}

// A type: project file stays the legacy goal until the flip (plan/paralog.md).
func TestProjectIsLegacyGoalUntilFlip(t *testing.T) {
	d, err := Decode("project-x", FolderProjects, []byte("type: project\ncreated: 2026-11-11\nstatus: active\nowner: self\ndone_when: shipped\n"), "X", "")
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := d.Meta.(*Project); !ok || p.DoneWhen != "shipped" {
		t.Fatalf("meta %T %+v", d.Meta, d.Meta)
	}
}

// The id prefix is not the type: a goal keeps project-* in old events and
// receipts, and a project made from an area keeps area-*. No code may read the
// type of a goal or project from its id.
func TestNoTypeFromGoalOrProjectPrefix(t *testing.T) {
	bad := regexp.MustCompile(`(HasPrefix|TrimPrefix|CutPrefix|startsWith)\([^)]*["'\x60](project|goal|prj)-["'\x60]`)
	for _, root := range []string{"../../internal", "../../ui/src"} {
		err := filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if de.IsDir() {
				if de.Name() == "node_modules" || de.Name() == "gen" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(p)
			if (ext != ".go" && ext != ".ts" && ext != ".tsx") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, ".test.") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for i, l := range strings.Split(string(b), "\n") {
				if bad.MatchString(l) {
					t.Errorf("%s:%d reads a type from an id prefix: %s", p, i+1, strings.TrimSpace(l))
				}
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}
