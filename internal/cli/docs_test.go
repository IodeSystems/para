package cli

import (
	"os"
	"strings"
	"testing"
)

// docCommands returns the para command lines of a markdown file: lines in a
// fenced or four-space-indented block that start with "para ", without a
// trailing "# comment". Lines holding a "<command>" placeholder are skipped.
func docCommands(text string) [][]string {
	var out [][]string
	fence := false
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		if !fence && !strings.HasPrefix(line, "    ") {
			continue
		}
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "  #"); i >= 0 {
			line = line[:i]
		}
		if !strings.HasPrefix(line, "para ") && line != "para" || strings.Contains(line, "<command>") {
			continue
		}
		out = append(out, splitWords(line))
	}
	return out
}

// Every para command in README.md and AGENTS.md exists and takes its flags.
func TestDocsCommandsParse(t *testing.T) {
	root := newRoot(&loader{})
	seen := 0
	for _, f := range []string{"../../README.md", "../../AGENTS.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, words := range docCommands(string(b)) {
			seen++
			line := strings.Join(words, " ")
			if len(words) == 1 || words[1] == "help" {
				continue // bare "para" runs the agenda; help is built in
			}
			cmd, rest, err := root.Find(words[1:])
			if err != nil || cmd == root {
				t.Errorf("%s: %s: no such command (%v)", f, line, err)
				continue
			}
			parses(t, cmd, rest, line)
		}
	}
	if seen < 8 {
		t.Errorf("only %d commands found: is the block convention broken?", seen)
	}
}
