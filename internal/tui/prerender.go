package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Link is one followable target in a document, in document order. ID is the
// target: "wiki:<stem>", "url:<url>", or "code:<n>".
type Link struct {
	ID    string
	Label string
}

// Prepared is a para-md document after the pre-render pass: markdown that
// glamour renders, the links with their keys, and the code blocks.
type Prepared struct {
	Markdown string
	Links    []Link
	Keys     map[string]Key
	Blocks   []string
}

// PreOpts configures PreRender.
type PreOpts struct {
	// Width is the render width. Below cardWidth, tables become cards.
	Width int
	// Reserved holds letters link keys must not use, such as the letters of
	// the actions on the screen.
	Reserved string
}

// cardWidth is the width below which a table turns into cards.
const cardWidth = 60

// standIn bytes are mapped into the private use area. A stand-in has the
// width of the letter it replaces, so glamour wraps and sizes tables as if
// the letter were there. Finish swaps it for the styled letter.
const standInBase = 0xE000

func standIn(r rune) rune { return standInBase + r }

var (
	reBlockEOL  = regexp.MustCompile(` \^[A-Za-z0-9-]+\s*$`)
	reBlockOwn  = regexp.MustCompile(`^\^[A-Za-z0-9-]+\s*$`)
	reMarker    = regexp.MustCompile(`^%% para:(begin|end) ([a-z0-9-]+) %%\s*$`)
	reWiki      = regexp.MustCompile(`^\[\[([^\]|#]+)(?:#(\^?)([^\]|]*))?(?:\|([^\]]*))?\]\]$`)
	reMDLink    = regexp.MustCompile(`^\[([^\]]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)$`)
	reFenceLine = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})\\s*([^`\\s]*)")
	reLinkish   = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)|\[\[[^\]]+\]\]|\[[^\]]+\]\([^)\s]+(?:\s+"[^"]*")?\)|<https?://[^>\s]+>|https?://[^\s<>)\]]+`)
	reTableSep  = regexp.MustCompile(`^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$`)
)

// PreRender turns para-md into markdown glamour renders well, and assigns link
// keys. Frontmatter becomes a quoted header, block ids and journal markers
// disappear, wikilinks and URLs become plain labels, each fenced block gets a
// copy line, and a table below cardWidth becomes cards. Text in code is never
// touched. The result depends only on src and opts.
func PreRender(src string, o PreOpts) Prepared {
	first := transform(src, o, nil)
	targets := make([]Target, len(first.Links))
	for n, l := range first.Links {
		targets[n] = Target{ID: l.ID, Label: l.Label}
	}
	keys := Assign(nil, targets, o.Reserved)
	p := transform(src, o, keys)
	p.Keys = keys
	return p
}

// transform runs one pass. With keys nil it only records links.
func transform(src string, o PreOpts, keys map[string]Key) Prepared {
	var p Prepared
	seen := map[string]bool{}
	note := func(l Link) {
		if !seen[l.ID] {
			seen[l.ID] = true
			p.Links = append(p.Links, l)
		}
	}
	mark := func(l Link) string {
		note(l)
		if keys == nil {
			return l.Label
		}
		return markStandIn(l.Label, keys[l.ID])
	}

	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var out []string
	i := 0
	if len(lines) > 0 && lines[0] == "---" {
		for j := 1; j < len(lines); j++ {
			if lines[j] != "---" {
				continue
			}
			var meta []string
			for _, l := range lines[1:j] {
				if l == "" || strings.HasPrefix(l, " ") || strings.HasPrefix(l, "-") {
					continue // nested yaml stays out of the header
				}
				k, v, ok := strings.Cut(l, ":")
				if !ok {
					continue
				}
				v = strings.TrimSpace(v)
				if len(v) >= 2 && (v[0] == '\'' || v[0] == '"') && v[len(v)-1] == v[0] {
					v = v[1 : len(v)-1]
				}
				if v == "" {
					continue
				}
				meta = append(meta, "**"+strings.TrimSpace(k)+"** "+linkify(v, mark))
			}
			if len(meta) > 0 {
				out = append(out, "> "+strings.Join(meta, " · "), "")
			}
			i = j + 1
			break
		}
	}

	var fence string // fence characters while inside a fenced block
	var block []string
	openAt := 0
	finishBlock := func() {
		idx := len(p.Blocks)
		p.Blocks = append(p.Blocks, strings.Join(block, "\n"))
		lab := Link{ID: fmt.Sprintf("code:%d", idx), Label: "copy code"}
		if idx > 0 {
			lab.Label = fmt.Sprintf("copy code %d", idx+1)
		}
		copyLine := []string{"", "*" + mark(lab) + "*", ""}
		out = append(out[:openAt], append(copyLine, out[openAt:]...)...)
		fence, block = "", nil
	}
	for ; i < len(lines); i++ {
		l := lines[i]
		if fence != "" {
			out = append(out, l)
			if t := strings.TrimSpace(l); strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				finishBlock()
				continue
			}
			block = append(block, l)
			continue
		}
		if m := reFenceLine.FindStringSubmatch(l); m != nil {
			fence, block, openAt = m[1], nil, len(out)
			out = append(out, l)
			continue
		}
		if reMarker.MatchString(l) {
			out = append(out, "", "---", "")
			continue
		}
		if reBlockOwn.MatchString(l) {
			continue
		}
		l = reBlockEOL.ReplaceAllString(l, "")
		out = append(out, linkify(l, mark))
	}
	if fence != "" { // unclosed fence: the block runs to the end
		finishBlock()
	}
	out = tablesToCards(out, o.Width)
	p.Markdown = strings.Join(out, "\n")
	return p
}

// linkify replaces links and URLs in a line, outside inline code, with marked
// labels.
func linkify(line string, mark func(Link) string) string {
	spans := codeSpans(line)
	var b strings.Builder
	pos := 0
	do := func(seg string) {
		b.WriteString(reLinkish.ReplaceAllStringFunc(seg, func(s string) string {
			switch {
			case strings.HasPrefix(s, "!"):
				return s
			case strings.HasPrefix(s, "[["):
				m := reWiki.FindStringSubmatch(s)
				if m == nil {
					return s
				}
				label := m[1]
				switch {
				case m[4] != "":
					label = m[4]
				case m[3] != "":
					label = m[1] + " §" + m[3]
				}
				return mark(Link{ID: "wiki:" + strings.TrimSpace(m[1]), Label: label})
			case strings.HasPrefix(s, "["):
				m := reMDLink.FindStringSubmatch(s)
				if m == nil {
					return s
				}
				return mark(Link{ID: "url:" + m[2], Label: m[1]})
			case strings.HasPrefix(s, "<"):
				u := s[1 : len(s)-1]
				return mark(Link{ID: "url:" + u, Label: u})
			}
			u := strings.TrimRight(s, ".,;:!?'\"")
			return mark(Link{ID: "url:" + u, Label: u}) + s[len(u):]
		}))
	}
	for _, sp := range spans {
		do(line[pos:sp[0]])
		b.WriteString(line[sp[0]:sp[1]])
		pos = sp[1]
	}
	do(line[pos:])
	return b.String()
}

// markStandIn is Mark with a stand-in for the key letter.
func markStandIn(label string, k Key) string {
	lab := []rune(label)
	if k.Code == "" {
		return label
	}
	if k.At >= 0 && k.At < len(lab) {
		lab[k.At] = standIn(lab[k.At])
		return string(lab)
	}
	var b strings.Builder
	b.WriteString(label)
	b.WriteByte(' ')
	for _, r := range k.Code {
		b.WriteRune(standIn(r))
	}
	return b.String()
}

// Finish swaps stand-ins in rendered text for the styled key letters.
func Finish(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r >= standInBase && r < standInBase+0x100 }) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r >= standInBase && r < standInBase+0x100 {
			b.WriteString(keyed(string(r - standInBase)))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// splitRow splits a GFM table row into trimmed cells.
func splitRow(l string) []string {
	l = strings.TrimSpace(l)
	l = strings.TrimPrefix(l, "|")
	l = strings.TrimSuffix(l, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(l); i++ {
		if l[i] == '\\' && i+1 < len(l) && l[i+1] == '|' {
			cur.WriteString(`\|`)
			i++
			continue
		}
		if l[i] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(l[i])
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// tablesToCards rewrites each table that would not fit as cards: one bullet
// per row, titled by the first cell, with a sub-bullet per column. Glamour
// does the wrapping, so wide and emoji text stays correct. A table stays a
// table when width allows it to fit.
func tablesToCards(in []string, width int) []string {
	var out []string
	inCode := false
	for i := 0; i < len(in); i++ {
		l := in[i]
		if reFenceLine.MatchString(l) {
			inCode = !inCode
		}
		if inCode || i+1 >= len(in) || !strings.Contains(l, "|") || !reTableSep.MatchString(in[i+1]) || !strings.Contains(in[i+1], "-") {
			out = append(out, l)
			continue
		}
		head := splitRow(l)
		j := i + 2
		var rows [][]string
		for j < len(in) && strings.Contains(in[j], "|") && strings.TrimSpace(in[j]) != "" {
			rows = append(rows, splitRow(in[j]))
			j++
		}
		if !needsCards(head, rows, width) {
			out = append(out, in[i:j]...)
			i = j - 1
			continue
		}
		out = append(out, "")
		for n, r := range rows {
			title := ""
			if len(r) > 0 {
				title = r[0]
			}
			if title == "" {
				title = fmt.Sprintf("row %d", n+1)
			}
			out = append(out, "- **"+title+"**")
			for c := 1; c < len(head) && c < len(r); c++ {
				if r[c] == "" {
					continue
				}
				out = append(out, "  - "+head[c]+": "+r[c])
			}
		}
		out = append(out, "")
		i = j - 1
	}
	return out
}

// needsCards reports whether a table should be shown as cards: the view is
// narrower than cardWidth, or the table is wider than the view.
func needsCards(head []string, rows [][]string, width int) bool {
	if width <= 0 {
		return false
	}
	if width < cardWidth {
		return true
	}
	w := make([]int, len(head))
	for c, h := range head {
		w[c] = ansi.StringWidth(h)
	}
	for _, r := range rows {
		for c := 0; c < len(r) && c < len(w); c++ {
			w[c] = max(w[c], ansi.StringWidth(r[c]))
		}
	}
	total := 1
	for _, x := range w {
		total += x + 3
	}
	return total > width
}

// codeSpans returns the byte ranges of inline code spans: a run of backticks
// up to the next run of the same length.
func codeSpans(line string) [][]int {
	var spans [][]int
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		run := j - i
		k := j
		end := -1
		for k < len(line) {
			if line[k] != '`' {
				k++
				continue
			}
			m := k
			for m < len(line) && line[m] == '`' {
				m++
			}
			if m-k == run {
				end = m
				break
			}
			k = m
		}
		if end < 0 {
			i = j
			continue
		}
		spans = append(spans, []int{i, end})
		i = end
	}
	return spans
}
