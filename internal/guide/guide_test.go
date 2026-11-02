package guide

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/iodesystems/parasolve/internal/ops"
)

func texts() map[string]string {
	return map[string]string{"intro": Intro(), "rule": Rule(), "orient": Orient(), "bootstrap": Bootstrap(), "extended": Extended(), "review_cycle": ReviewCycle(), "process_meeting": ProcessMeeting()}
}

var backticked = regexp.MustCompile("`([^`\n]+)`")

// Every backticked word in the guide is an op name, so a renamed or removed
// tool breaks this test. Prose that is not a tool name goes without backticks.
func TestBackticksAreTools(t *testing.T) {
	names := map[string]bool{}
	for _, e := range ops.All() {
		names[e.Name()] = true
	}
	used := map[string]bool{}
	for name, text := range texts() {
		for _, m := range backticked.FindAllStringSubmatch(text, -1) {
			if !names[m[1]] {
				t.Errorf("%s: `%s` is not in ops.All", name, m[1])
			}
			used[m[1]] = true
		}
	}
	// The tools the owner's workflow depends on must be named somewhere.
	for _, want := range []string{"area_add", "area_link_add", "project_add", "project_link_add", "task_add", "task_due",
		"task_depends_add", "org_person_add", "inbox_add", "inbox_triage", "rfx_add", "rfx_reply", "lint", "next", "where"} {
		if !used[want] {
			t.Errorf("no guide text names %s", want)
		}
	}
}

func TestIntroIsShort(t *testing.T) {
	if n := len(strings.Split(Intro(), "\n")); n >= 12 {
		t.Errorf("intro is %d lines, want under 12", n)
	}
	for _, want := range []string{"what you owe and who you wait on", "is not a documentation system", "pager", "scheduler", "calendar", "sync tool", "para links to them"} {
		if !strings.Contains(strings.Join(strings.Fields(Intro()), " "), want) {
			t.Errorf("intro lacks %q", want)
		}
	}
}

func TestRuleReachesEverySurface(t *testing.T) {
	for _, name := range []string{"orient", "bootstrap"} {
		if !strings.Contains(texts()[name], Rule()) {
			t.Errorf("%s lacks the rule", name)
		}
	}
	for _, want := range []string{"one question at a time", "Never invent a fact", "area_add", "lint"} {
		if !strings.Contains(strings.Join(strings.Fields(Bootstrap()), " "), want) {
			t.Errorf("bootstrap lacks %q", want)
		}
	}
	// The bootstrap order: areas, projects, tasks, people, then lint.
	order := []string{"area_add", "project_add", "task_add", "org_person_add", "lint"}
	at := -1
	for _, tool := range order {
		i := strings.Index(Bootstrap(), tool)
		if i < at {
			t.Errorf("bootstrap names %s out of order", tool)
		}
		at = i
	}
}

func TestSkillFrontmatterParses(t *testing.T) {
	parts := strings.SplitN(Skill(), "---\n", 3)
	if len(parts) != 3 {
		t.Fatalf("no frontmatter: %q", Skill()[:40])
	}
	var fm map[string]string
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		t.Fatal(err)
	}
	if fm["name"] != "para" || fm["description"] == "" {
		t.Fatalf("frontmatter = %v", fm)
	}
}
