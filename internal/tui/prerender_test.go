package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const sampleDoc = `---
type: policy
created: 2026-11-01
project: "[[acme-portal]]"
---

# Logging policy

See [[policy-logging#^R4]] and [[acme-portal|the portal]] and [docs](https://wiki.acme.example/logging).
Again [[acme-portal]] and ` + "`[[not-a-link]]`" + `.

R4 policy-logging: Services log every authentication failure. ^R4

%% para:begin open-tasks %%
Generated.
%% para:end open-tasks %%

` + "```" + `bash
echo "[[inside-code]] https://x.example"
` + "```" + `

| Service | Level |
|---|---|
| portal.acme.example | info |
`

func TestPreRenderStripsBlockIdsAndMarkers(t *testing.T) {
	p := PreRender(sampleDoc, PreOpts{Width: 100})
	if strings.Contains(p.Markdown, "^R4") || strings.Contains(p.Markdown, "%%") {
		t.Fatalf("not stripped:\n%s", p.Markdown)
	}
	if !strings.Contains(p.Markdown, "---") {
		t.Fatal("marker rule missing")
	}
}

func TestPreRenderLinksOnePerTarget(t *testing.T) {
	p := PreRender(sampleDoc, PreOpts{Width: 100})
	var ids []string
	for _, l := range p.Links {
		ids = append(ids, l.ID)
	}
	want := []string{"wiki:acme-portal", "wiki:policy-logging", "url:https://wiki.acme.example/logging", "code:0"}
	if strings.Join(ids, " ") != strings.Join(want, " ") {
		t.Fatalf("links %v want %v", ids, want)
	}
	seen := map[string]string{}
	for id, k := range p.Keys {
		if prev, dup := seen[k.Code]; dup {
			t.Fatalf("key %q shared by %s and %s", k.Code, prev, id)
		}
		seen[k.Code] = id
	}
}

func TestPreRenderCodeIsUntouched(t *testing.T) {
	p := PreRender(sampleDoc, PreOpts{Width: 100})
	if !strings.Contains(p.Markdown, `echo "[[inside-code]] https://x.example"`) {
		t.Fatalf("code changed:\n%s", p.Markdown)
	}
	if !strings.Contains(p.Markdown, "`[[not-a-link]]`") {
		t.Fatal("inline code changed")
	}
	if len(p.Blocks) != 1 || !strings.Contains(p.Blocks[0], "inside-code") {
		t.Fatalf("blocks %q", p.Blocks)
	}
	// The copy line sits before the fence.
	fi := strings.Index(p.Markdown, "```bash")
	ci := strings.LastIndex(p.Markdown[:fi], "\n*")
	if ci < 0 || fi < 0 || ci > fi {
		t.Fatalf("copy line at %d, fence at %d", ci, fi)
	}
}

func TestPreRenderStandInHasLabelWidth(t *testing.T) {
	plain := transform(sampleDoc, PreOpts{Width: 100}, nil)
	keyed := PreRender(sampleDoc, PreOpts{Width: 100})
	pl, kl := strings.Split(plain.Markdown, "\n"), strings.Split(keyed.Markdown, "\n")
	if len(pl) != len(kl) {
		t.Fatalf("line counts %d %d", len(pl), len(kl))
	}
	for i := range pl {
		if _, extra := keyed.Keys, 0; extra == 0 {
			// A label with no letter gets " k" appended; those lines are 2 wider.
			d := ansi.StringWidth(kl[i]) - ansi.StringWidth(pl[i])
			if d != 0 && d != 2 && d != 3 && d != 4 {
				t.Errorf("line %d width differs by %d:\n%q\n%q", i, d, pl[i], kl[i])
			}
		}
	}
}

func TestPreRenderDeterministic(t *testing.T) {
	a := PreRender(sampleDoc, PreOpts{Width: 100, Reserved: "zxycv"})
	for _, w := range []int{40, 80, 120} {
		b := PreRender(sampleDoc, PreOpts{Width: w, Reserved: "zxycv"})
		for id, k := range a.Keys {
			if b.Keys[id] != k {
				t.Fatalf("width %d: %s key %+v vs %+v", w, id, b.Keys[id], k)
			}
		}
	}
}

func TestFinishStyleLetters(t *testing.T) {
	p := PreRender("# T\n\n[[acme-portal]] here\n", PreOpts{Width: 80})
	var r renderers
	d := r.render(p, 80)
	joined := strings.Join(d.Lines, "\n")
	if !strings.Contains(joined, KeyStyle.On+"a"+KeyStyle.Off) {
		t.Fatalf("no styled key in %q", joined)
	}
	if strings.ContainsFunc(joined, func(c rune) bool { return c >= standInBase && c < standInBase+0x100 }) {
		t.Fatal("stand-in left behind")
	}
}

func TestCardsBelowWidth(t *testing.T) {
	src := "# T\n\n| Service | Level | Owner |\n|---|---|---|\n| portal | info | 日本語の担当者 |\n| 🚀 launch | warn | ops |\n"
	n := PreRender(src, PreOpts{Width: 40})
	if strings.Contains(n.Markdown, "|---") || !strings.Contains(n.Markdown, "- **portal**") || !strings.Contains(n.Markdown, "  - Owner: 日本語の担当者") {
		t.Fatalf("not cards:\n%s", n.Markdown)
	}
	w := PreRender(src, PreOpts{Width: 120})
	if !strings.Contains(w.Markdown, "|---") {
		t.Fatalf("table lost at 120:\n%s", w.Markdown)
	}
}

func TestCardsRenderWithinWidthCJKAndEmoji(t *testing.T) {
	src := "# T\n\n| Service | Level | Owner |\n|---|---|---|\n| portal | info | 日本語の担当者の名前が長い人 |\n| 🚀 launch | warn | ops |\n"
	var r renderers
	for _, w := range []int{40, 30} {
		d := r.render(PreRender(src, PreOpts{Width: w}), w)
		for _, l := range d.Lines {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("width %d: line is %d wide: %q", w, got, ansi.Strip(l))
			}
		}
	}
}

// A wide table with key stand-ins in its cells keeps its columns aligned: the
// stand-in has the width of the letter it replaced.
func TestWideTableWithKeysAligns(t *testing.T) {
	src := `# T

| Policy | Owner | Notes |
|---|---|---|
| [[policy-logging]] | [[jordan-example]] | Services log every authentication failure |
| [[policy-retention]] | [[sam-example]] | Retain for 90 days |
`
	var r renderers
	withKeys := r.render(PreRender(src, PreOpts{Width: 120}), 120)
	plain := glamourPlain(t, strings.NewReplacer("[[", "", "]]", "").Replace(src), 120)
	var a, b []int
	for _, l := range withKeys.Lines {
		a = append(a, ansi.StringWidth(l))
	}
	for _, l := range plain {
		b = append(b, ansi.StringWidth(l))
	}
	if len(a) != len(b) {
		t.Fatalf("line count %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("line %d: %d vs %d\n%q\n%q", i, a[i], b[i], ansi.Strip(withKeys.Lines[i]), ansi.Strip(plain[i]))
		}
	}
	var sawBar bool
	for _, l := range withKeys.Lines {
		if strings.Contains(l, "│") {
			sawBar = true
		}
		if strings.Contains(ansi.Strip(l), "policy-logging") && !strings.Contains(l, KeyStyle.On) {
			t.Errorf("row without a styled key: %q", l)
		}
	}
	if !sawBar {
		t.Fatal("expected a table")
	}
}

func glamourPlain(t *testing.T, src string, w int) []string {
	t.Helper()
	var r renderers
	d := r.render(Prepared{Markdown: src}, w)
	return d.Lines
}
