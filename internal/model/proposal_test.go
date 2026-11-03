package model

import (
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/paramd"
)

// The block must give back exactly what went in, whatever the text holds. A
// hash binds to this text, so one changed byte is a broken approval.
func TestProposalBodyRoundTrip(t *testing.T) {
	hostile := []string{
		"plain",
		"two\nlines",
		"trailing newline\n",
		"two trailing newlines\n\n",
		"\nleading newline",
		"  leading and trailing spaces  ",
		"tab\there",
		"carriage\r\nreturn",
		"````\nfour backticks\n````",
		"```\nthree\n```",
		"`````yaml para-proposal\nitems: []\n`````",
		"---\nfrontmatter: fake\n---",
		"# Second title\n## Page 3",
		"<script>alert(1)</script>",
		"%% para:begin x %%",
		"[[missing-link]] and ![[embed]]",
		"R4 policy-x: looks like a rule ^R4",
		"| a | b |\n|---|\n| 1 |",
		"quote: \"double\" and 'single'",
		"key: value\n- item",
		"null",
		"true",
		"123",
		"2026-11-01",
		"é ü 日本語 🙂",
		"  line separator",
	}
	for _, text := range hostile {
		c := ProposalContent{
			Items: []ProposalItem{{ID: "i1", Kind: ItemAction, Subtype: "email", Target: "x@acme.example", Body: text,
				Evidence: Evidence{Quote: text, Locator: text}, State: ItemPending, Reason: text,
				Thread: []Turn{{ID: "a1", Kind: TurnClarify, Quote: text, Text: text, By: "user", Revision: 1, State: TurnOpen}}},
				{ID: "i2", Kind: ItemPara, Op: "task_add", Input: map[string]any{"title": text, "n": 3, "nested": map[string]any{"list": []any{text}}},
					Evidence: Evidence{Quote: "q", Locator: "l"}, State: ItemPending}},
		}
		body, err := RenderProposalBody(c)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		got, err := ParseProposalBody(strings.Trim(body, "\n"))
		if err != nil {
			t.Fatalf("%q: %v\n%s", text, err, body)
		}
		if !reflect.DeepEqual(got, c) {
			t.Fatalf("%q: round trip\n got %#v\nwant %#v", text, got, c)
		}
		// A person's vault must lint clean, whatever an agent quoted.
		d := Document{ID: "proposal-x", Folder: FolderProposals, Title: "T", Body: body,
			Meta: &Proposal{Common: Common{Type: TypeProposal, Created: "2026-11-01"}, Source: LinkTo("note-x"), Status: ProposalOpen, Revision: 1, ProposedBy: "agent"}}
		raw, err := Render(d)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range paramd.Lint(paramd.Parse("proposals/proposal-x.md", raw)) {
			if f.Severity == paramd.Error {
				t.Errorf("%q: lint: %s", text, f)
			}
		}
	}
}

func TestParseProposalBodyRefuses(t *testing.T) {
	for name, body := range map[string]string{
		"no block":      "Text only.",
		"not closed":    "````yaml para-proposal\nitems: []\n",
		"unknown field": "````yaml para-proposal\nitems: []\nextra: 1\n````",
		"bad yaml":      "````yaml para-proposal\nitems: [\n````",
	} {
		if _, err := ParseProposalBody(body); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestHashes(t *testing.T) {
	if ExternalHash("a", "b") == ExternalHash("a", "b ") || ExternalHash("a", "b") == ExternalHash("ab", "") {
		t.Error("external hash does not bind target and draft")
	}
	if got := ExternalHash("t", "d"); !strings.HasPrefix(got, "sha256:") || len(got) != 71 {
		t.Errorf("hash form %q", got)
	}
	a, err := OpHash("task_add", map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := OpHash("task_add", map[string]any{"b": 2, "a": 1})
	c, _ := OpHash("task_add", map[string]any{"a": 1, "b": 3})
	if a != b || a == c {
		t.Errorf("op hash: %s %s %s", a, b, c)
	}
}
