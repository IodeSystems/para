package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/model"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name, in, want string
		wantErr        bool
	}{
		{"crlf and trailing blanks", "a  \r\nb\t\r\n", "a\nb", false},
		{"cr only", "a\rb", "a\nb", false},
		{"bom", "\xef\xbb\xbfa", "a", false},
		{"blank runs", "a\n\n\n\n\nb", "a\n\nb", false},
		{"h1 demoted", "# One\ntext\n## Two", "## One\ntext\n## Two", false},
		{"h1 in fence kept", "```\n# not a heading  \n```", "```\n# not a heading  \n```", false},
		{"page marker kept", "## Page 1\n\ntext", "## Page 1\n\ntext", false},
		{"empty", " \n\n", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Normalize(c.in)
			if (err != nil) != c.wantErr || got != c.want {
				t.Fatalf("got %q err %v", got, err)
			}
		})
	}
	a, _ := Normalize("x \r\n\r\n\r\ny")
	b, _ := Normalize("x\n\ny\n")
	if hashText(a) != hashText(b) {
		t.Error("equal texts hash differently")
	}
}

func TestParseRules(t *testing.T) {
	body := strings.Join([]string{
		"intro",
		"- R1 policy-x: First. ^R1",
		"R2 policy-x: Second, with colon: here. ^R2",
		"- R3 policy-x: mismatched ^R4",
		"- R5 policy-x: no block id",
		"```",
		"- R6 policy-x: in code ^R6",
		"```",
	}, "\n")
	got := parseRules("policy-x", body)
	if len(got) != 2 || got[0].ID != "R1" || got[0].Text != "First." || got[1].Text != "Second, with colon: here." {
		t.Fatalf("rules = %+v", got)
	}
	if got[0].Link != model.RuleLink("policy-x", "R1") {
		t.Errorf("link = %s", got[0].Link)
	}
	cases := []struct {
		name  string
		rules []Rule
		want  int
	}{
		{"none", nil, 1},
		{"sequence", []Rule{{ID: "R1"}, {ID: "R2"}}, 3},
		{"gap is not reused", []Rule{{ID: "R1"}, {ID: "R7"}}, 8},
	}
	for _, c := range cases {
		if n := nextRuleNumber(c.rules); n != c.want {
			t.Errorf("%s: next = %d", c.name, n)
		}
	}
}

func TestAddRuleLine(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"empty body", "", "## Rules\n\n- R1"},
		{"no section", "intro", "intro\n\n## Rules\n\n- R1"},
		{"empty section", "intro\n\n## Rules", "intro\n\n## Rules\n\n- R1"},
		{"after last rule", "## Rules\n\n- A", "## Rules\n\n- A\n- R1"},
		{"before next heading", "## Rules\n\n- A\n\n## Notes\n\ntext", "## Rules\n\n- A\n- R1\n\n## Notes\n\ntext"},
		{"heading in fence ignored", "```\n## Rules\n```", "```\n## Rules\n```\n\n## Rules\n\n- R1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := addRuleLine(c.body, "- R1"); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestRenderCopy(t *testing.T) {
	body := strings.Join([]string{
		"## Scope",
		"Applies to **all** services, see [wiki](https://wiki.acme.example/x) and [[policy-y#^R2]] and [[policy-z|Z policy]].",
		"- item with `code`",
		"1. step",
		"",
		"| Level | Use \\| note |",
		"|---|---|",
		"| info | normal |",
		"",
		"```go",
		"x := 1",
		"```",
		"R1 policy-x: Rule text. ^R1",
	}, "\n")
	doc := model.Document{Title: "Logging", Body: body, Meta: &model.Policy{Source: model.Source{URL: "https://wiki.acme.example/p"}}}
	cases := []struct {
		f     CopyFormat
		wants []string
		nots  []string
	}{
		{CopyPlain, []string{"[ACME]\n\nLogging\n", "Scope\n", "Applies to all services, see wiki (https://wiki.acme.example/x) and policy-y R2 and Z policy.", "- item with code", "Level | Use | note", "info | normal", "```\nx := 1\n```", "R1 policy-x: Rule text.\n", "Source: https://wiki.acme.example/p\n\n[ACME]\n"}, []string{"---|", "^R1", "**", "```go"}},
		{CopyEmail, []string{"Subject: [ACME] Logging\n"}, []string{"^R1"}},
		{CopySlack, []string{"*[ACME]*\n\n*Logging*", "*Scope*", "*all*", "<https://wiki.acme.example/x|wiki>", "• item with `code`", "```\nLevel | Use | note\ninfo | normal\n```"}, []string{"**"}},
		{CopyJira, []string{"h1. Logging", "h2. Scope", "*all*", "[wiki|https://wiki.acme.example/x]", "* item with {{code}}", "# step", "||Level||Use | note||\n|info|normal|", "{code}\nx := 1\n{code}"}, []string{"**", "---|"}},
	}
	for _, c := range cases {
		t.Run(string(c.f), func(t *testing.T) {
			got := renderCopy(c.f, doc, "ACME")
			for _, w := range c.wants {
				if !strings.Contains(got, w) {
					t.Errorf("lacks %q in:\n%s", w, got)
				}
			}
			for _, n := range c.nots {
				if strings.Contains(got, n) {
					t.Errorf("has %q in:\n%s", n, got)
				}
			}
			if strings.Contains(got, "type:") {
				t.Errorf("frontmatter leaked:\n%s", got)
			}
		})
	}
}

func TestCheckHelpers(t *testing.T) {
	for _, c := range []struct {
		in      string
		wantErr bool
	}{{"2026-11-01", false}, {"2026-13-01", true}, {"26-11-01", true}, {"", true}} {
		if err := checkDate("d", c.in); (err != nil) != c.wantErr {
			t.Errorf("%q: %v", c.in, err)
		}
	}
	got := cleanList([]string{"a, b", "", " c ", "a"})
	if strings.Join(got, "|") != "a|b|c|a" {
		t.Errorf("cleanList = %v", got)
	}
	for _, in := range []string{"https://wiki.acme.example/p", "http://x.example/a?page=2"} {
		if _, err := checkURL(in); err != nil {
			t.Errorf("%s: %v", in, err)
		}
	}
	for _, in := range []string{"", "x", "ftp://x.example/a", "https://x.example/a?X-Amz-Signature=1", "https://u:p@x.example/a", "https://x.example/a?API_KEY=1"} {
		if _, err := checkURL(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
	if got := defaultTitle("text\n## Heading\nmore", "https://h.example/a/b/"); got != "Heading" {
		t.Errorf("title = %q", got)
	}
	if got := defaultTitle("plain", "https://h.example/a/b/"); got != "h.example/a/b" {
		t.Errorf("title = %q", got)
	}
}
