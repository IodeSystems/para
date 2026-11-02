package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/guide"
)

// splitWords splits a command line on spaces, honouring single and double quotes.
func splitWords(line string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	inWord := false
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

// guideCommands returns the "$ para ..." lines of a text, as argument lists
// without the leading "para". A trailing "< file" is dropped.
func guideCommands(text string) [][]string {
	var out [][]string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "$ ")
		if !ok {
			continue
		}
		words := splitWords(rest)
		if n := len(words); n >= 2 && words[n-2] == "<" {
			words = words[:n-2]
		}
		out = append(out, words)
	}
	return out
}

// Every command in the guide text exists, takes its flags, and accepts its
// arguments, so the text cannot drift from the command tree.
func TestGuideCommandsParse(t *testing.T) {
	root := newRoot(&loader{})
	seen := 0
	for _, text := range []string{guide.Extended(), guide.Orient(), guide.Bootstrap(), guide.ReviewCycle(), guide.Harness("claude", ""), guide.Harness("dun", "")} {
		for _, words := range guideCommands(text) {
			seen++
			if len(words) == 0 || words[0] != "para" {
				t.Errorf("not a para command: %q", words)
				continue
			}
			cmd, rest, err := root.Find(words[1:])
			if err != nil || cmd == root {
				t.Errorf("para %s: no such command (%v)", strings.Join(words[1:], " "), err)
				continue
			}
			parses(t, cmd, rest, strings.Join(words, " "))
		}
	}
	if seen < 30 {
		t.Errorf("only %d commands found in the guide: is the $ convention broken?", seen)
	}
}

func parses(t *testing.T, cmd *cobra.Command, rest []string, line string) {
	t.Helper()
	if !cmd.Runnable() {
		t.Errorf("%s: %s has subcommands and does not run alone", line, cmd.CommandPath())
		return
	}
	// ParseFlags validates every flag name and value; ValidateArgs the arguments.
	if err := cmd.ParseFlags(rest); err != nil {
		t.Errorf("%s: %v", line, err)
		return
	}
	if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
		t.Errorf("%s: %v", line, err)
	}
}

func TestHelpOpensWithIntro(t *testing.T) {
	var out bytes.Buffer
	root := newRoot(&loader{})
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, guide.Intro()+"\n") {
		t.Errorf("help does not open with the intro:\n%s", got)
	}
	if !strings.Contains(got, "Available Commands:") || strings.Index(got, "Available Commands:") < len(guide.Intro()) {
		t.Errorf("the command list must follow the intro:\n%s", got)
	}
}

func TestHelpExtendedAndBootstrapCommands(t *testing.T) {
	e := newEnv(t) // no vault: neither command reads one
	if out := e.ok("guide"); out != guide.Extended()+"\n" {
		t.Errorf("guide differs from the guide text")
	}
	if out := e.ok("--help-extended"); out != guide.Extended()+"\n" {
		t.Errorf("--help-extended differs from the guide text")
	}
	if out := e.ok("bootstrap"); out != guide.Bootstrap()+"\n" {
		t.Errorf("bootstrap differs from the guide text")
	}
	if out := e.ok("--help-extended"); !strings.Contains(out, "Where does this go?") {
		t.Error("extended help lacks the table")
	}
	if _, _, err := e.run("bootstrap", "extra"); err == nil {
		t.Error("bootstrap takes no arguments")
	}
}

func TestMCPHarnessHelp(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	for flag, want := range map[string]string{
		"--help-claude": "claude mcp add para-acme",
		"--help-dun":    `"id": "para-acme"`,
	} {
		out := e.ok("mcp", flag)
		if !strings.Contains(out, want) || strings.Contains(out, "{{config}}") {
			t.Errorf("%s:\n%s", flag, out)
		}
	}
	if _, _, err := e.run("mcp", "--help-claude", "--help-dun"); err == nil {
		t.Error("both flags must be refused")
	}
}

func TestMCPHarnessHelpNoVault(t *testing.T) {
	e := newEnv(t)
	if out := e.ok("mcp", "--help-dun"); !strings.Contains(out, "para init") {
		t.Errorf("no vault:\n%s", out)
	}
}

func TestDefaultVault(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.init("globex")
	if _, _, err := e.run("inbox", "add", "x"); err == nil {
		t.Fatal("two vaults and no default must fail")
	}
	if out := e.ok("vault", "set-default", "globex"); !strings.Contains(out, "globex") {
		t.Fatal(out)
	}
	e.ok("inbox", "add", "x")
	if out := e.ok("vault", "list"); !strings.Contains(out, "default") {
		t.Fatalf("list: %s", out)
	}
	if out := e.ok("--vault", "acme", "vault", "use-check"); !strings.Contains(out, "acme") {
		t.Fatalf("flag must beat the default: %s", out)
	}
	if _, _, err := e.run("mcp"); err == nil || !strings.Contains(err.Error(), "--vault") {
		t.Fatalf("mcp must refuse the default: %v", err)
	}
	if _, _, err := e.run("vault", "set-default", "nope"); err == nil {
		t.Fatal("unknown vault must fail")
	}
	e.ok("vault", "set-default", "--clear")
	if _, _, err := e.run("inbox", "add", "x"); err == nil {
		t.Fatal("cleared default must fail")
	}
	if _, _, err := e.run("tui", "acme", "--vault", "globex"); err == nil {
		t.Fatal("disagreeing vaults must fail")
	}
}
