package model

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

// A type: project file is the ongoing project. Below format 11 the vault
// reads it as a goal through LegacyRead.
func TestProjectDecodes(t *testing.T) {
	fm := "type: project\ncreated: 2026-11-11\nstatus: active\nowner: self\nstate_line: on track\n"
	d, err := Decode("project-x", FolderProjects, []byte(fm), "X", "")
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := d.Meta.(*Project); !ok || p.StateLine != "on track" || !p.Active() {
		t.Fatalf("meta %T %+v", d.Meta, d.Meta)
	}
	if FolderOf(TypeProject) != FolderProjects || FolderOf(TypeTask) != FolderGoals {
		t.Errorf("folders %s %s", FolderOf(TypeProject), FolderOf(TypeTask))
	}
}

// Below format 11 a goal is type project and a task links it with project:.
// LegacyRead reads both in the format-11 form; LegacyWrite writes them back.
func TestLegacyGoalRoundTrip(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("type: project\ncreated: 2026-11-11\nstatus: done\nowner: self\ndone_when: shipped\n"), &doc); err != nil {
		t.Fatal(err)
	}
	root := doc.Content[0]
	d, err := DecodeNode("project-x", FolderProjects, LegacyRead(root), "X", "")
	if err != nil {
		t.Fatal(err)
	}
	g, ok := d.Meta.(*Goal)
	if !ok || g.DoneWhen != "shipped" || g.Status != GoalDone || g.Type != TypeGoal {
		t.Fatalf("meta %T %+v", d.Meta, d.Meta)
	}
	if root.Content[1].Value != "project" {
		t.Errorf("LegacyRead changed its input: %s", root.Content[1].Value)
	}
	out, err := Render(LegacyWrite(d))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "type: project\n") || g.Type != TypeGoal {
		t.Errorf("legacy write:\n%s", out)
	}
	if err := yaml.Unmarshal([]byte("type: task\ncreated: 2026-11-11\nstatus: todo\nproject: \"[[project-x]]\"\n"), &doc); err != nil {
		t.Fatal(err)
	}
	td, err := DecodeNode("task-a", FolderProjects, LegacyRead(doc.Content[0]), "A", "")
	if err != nil {
		t.Fatal(err)
	}
	if tk := td.Meta.(*Task); tk.Goal.Target() != "project-x" || tk.Project != "" {
		t.Fatalf("task %+v", tk)
	}
	out, err = Render(LegacyWrite(td))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "project: '[[project-x]]'") && !strings.Contains(string(out), `project: "[[project-x]]"`) || strings.Contains(string(out), "goal:") {
		t.Errorf("legacy task write:\n%s", out)
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
