package ops

import (
	"regexp"
	"sort"
	"strings"

	"github.com/iodesystems/para/internal/index"
	"github.com/iodesystems/para/internal/model"
)

// noise finds what makes an excerpt hard to read: a wiki link (groups 1 and 2),
// an RFC 3339 time stamp (group 3 is its day), a table separator row (group 4),
// and a run of table pipes.
var noise = regexp.MustCompile(`\[\[([^\]|#]*)(?:#[^\]|]*)?(?:\|([^\]]*))?\]\]` +
	`|(\d{4}-\d\d-\d\d)T\d\d:\d\d(?::\d\d)?(?:\.\d+)?(?:Z|[+-]\d\d:?\d\d)?` +
	`|((?:\s*\|)+(?:\s*:?-{3,}:?\s*\|)+)` +
	`|(?:\s*\|)+\s*`)

// readable fills Snippet and Marks of a hit from its Passage: the match markers
// come out and are kept as spans, and a link [[id]] becomes the title of what
// it points at. A span that touches a link covers the whole title. name gives
// a title for an id, "" when unknown.
func readable(h *index.Hit, name func(string) string) {
	h.Marks = nil
	// The index folds white space itself; a meaning hit's text is folded here.
	if !strings.Contains(h.Passage, index.MarkOpen) {
		h.Passage = strings.Join(strings.Fields(h.Passage), " ")
	}
	// 1. Remove the markers. Offsets are in runes of the clean text.
	var clean []rune
	var spans []index.Span
	rest, open := h.Passage, -1
	for rest != "" {
		tok, i := index.MarkOpen, strings.Index(rest, index.MarkOpen)
		if open >= 0 {
			tok, i = index.MarkClose, strings.Index(rest, index.MarkClose)
		}
		if i < 0 {
			clean = append(clean, []rune(rest)...)
			break
		}
		clean = append(clean, []rune(rest[:i])...)
		rest = rest[i+len(tok):]
		if open < 0 {
			open = len(clean)
		} else {
			spans = append(spans, index.Span{Start: open, End: len(clean)})
			open = -1
		}
	}
	text := string(clean)
	// 2. Write links as titles, moving the spans with the text.
	type swap struct{ from, to, newFrom, newTo int }
	var swaps []swap
	var out strings.Builder
	last, outRunes := 0, 0
	runeAt := func(byteOff int) int { return len([]rune(text[:byteOff])) }
	for _, m := range noise.FindAllStringSubmatchIndex(text, -1) {
		before := text[last:m[0]]
		out.WriteString(before)
		outRunes += len([]rune(before))
		var label string
		switch {
		case m[2] >= 0: // [[id]] or [[id|label]]: the title
			if m[4] >= 0 {
				label = strings.TrimSpace(text[m[4]:m[5]])
			}
			id := strings.TrimSpace(text[m[2]:m[3]])
			if label == "" && name != nil {
				label = name(id)
			}
			if label == "" {
				label = id
			}
		case m[6] >= 0: // a time stamp: the day it names
			label = text[m[6]:m[7]]
		case m[8] >= 0: // a table separator row: nothing
		default: // table pipes: one dot between cells, none at an edge
			if m[0] > 0 && m[1] < len(text) {
				label = " \u00b7 "
			}
		}
		sw := swap{from: runeAt(m[0]), to: runeAt(m[1]), newFrom: outRunes, newTo: outRunes + len([]rune(label))}
		swaps = append(swaps, sw)
		out.WriteString(label)
		outRunes = sw.newTo
		last = m[1]
	}
	out.WriteString(text[last:])
	snippet := out.String()
	// A link cut by the snippet's edge leaves its brackets.
	cleaned := strings.NewReplacer("[[", "", "]]", "").Replace(snippet)
	if cleaned != snippet {
		// Brackets only remain at an edge, away from the spans in practice; the
		// spans are dropped rather than shifted wrongly when they could move.
		spans, swaps = nil, nil
		snippet = cleaned
	}
	moveStart := func(p int) int {
		delta := 0
		for _, s := range swaps {
			switch {
			case p >= s.to:
				delta = s.newTo - s.to
			case p >= s.from:
				return s.newFrom
			}
		}
		return p + delta
	}
	moveEnd := func(p int) int {
		delta := 0
		for _, s := range swaps {
			switch {
			case p >= s.to:
				delta = s.newTo - s.to
			case p > s.from:
				return s.newTo
			}
		}
		return p + delta
	}
	for _, sp := range spans {
		h.Marks = append(h.Marks, index.Span{Start: moveStart(sp.Start), End: moveEnd(sp.End)})
	}
	sort.SliceStable(h.Marks, func(i, j int) bool { return h.Marks[i].Start < h.Marks[j].Start })
	h.Snippet = snippet
}

// journalHit rewrites a hit on a journal: its text is a log line, which says
// nothing to a reader, so the hit shows the date it belongs to instead.
func journalHit(h *index.Hit) {
	h.Snippet, h.Marks = "Activity in the journal of "+h.ID, nil
}

// proposalHit rewrites a hit on a proposal: its file is a block of YAML with ids
// in it, so the hit says what it is and where to read it.
func proposalHit(h *index.Hit) {
	h.Snippet, h.Marks = "A proposal from an agent. Open it to read its items.", nil
}

// edgeEllipsis is what the index puts where it cut a passage.
const edgeEllipsis = "..."

// completeEdgeLinks closes a link the passage's edge cut, so it reads as the
// whole link and not as part of an id. cut gives the one id that starts
// (prefix) or ends with a part, "" when it is not known. A part it cannot
// place is left as it is.
func completeEdgeLinks(p string, cut func(frag string, prefix bool) string) string {
	hasMark := func(s string) bool {
		return strings.Contains(s, index.MarkOpen) || strings.Contains(s, index.MarkClose)
	}
	// The end: "[[id-st ..."
	if i := strings.LastIndex(p, "[["); i >= 0 && !strings.Contains(p[i:], "]]") {
		rest, tail := p[i+2:], ""
		if k := strings.LastIndex(rest, " "+edgeEllipsis); k >= 0 && strings.TrimSpace(rest[k:]) == edgeEllipsis {
			rest, tail = rest[:k], rest[k:]
		}
		id := ""
		if j := strings.IndexAny(rest, "|#"); j >= 0 {
			id = rest[:j] // the id is whole; the label or anchor was cut
		} else if !strings.ContainsAny(rest, " \t\n") {
			id = cut(rest, true)
		}
		if id != "" && !hasMark(rest) && !strings.ContainsAny(id, " \t\n") {
			p = p[:i] + "[[" + id + "]]" + tail
		}
	}
	// The start: "... nd-id]]"
	if j := strings.Index(p, "]]"); j >= 0 && !strings.Contains(p[:j], "[[") {
		head, lead := p[:j], ""
		if t := strings.TrimLeft(head, " "); strings.HasPrefix(t, edgeEllipsis+" ") {
			n := len(head) - len(t) + len(edgeEllipsis) + 1
			head, lead = head[n:], head[:n]
		}
		if hasMark(head) {
			return p
		}
		if pipe := strings.LastIndex(head, "|"); pipe >= 0 {
			if !strings.ContainsAny(head[:pipe], " \t\n") {
				p = lead + head[pipe+1:] + p[j+2:] // the label is whole
			}
		} else if !strings.ContainsAny(head, " \t\n") {
			if id := cut(head, false); id != "" {
				p = lead + "[[" + id + "]]" + p[j+2:]
			}
		}
	}
	return p
}

// readableHits fills the snippets of hits, and puts journal hits after the
// rest: a journal is a log of everything else. cut, when set, completes a
// link the passage's edge cut (completeEdgeLinks).
func readableHits(hits []index.Hit, name func(string) string, cut func(string, bool) string) {
	for i := range hits {
		if cut != nil {
			hits[i].Passage = completeEdgeLinks(hits[i].Passage, cut)
		}
		if hits[i].Kind == string(model.TypeJournal) {
			journalHit(&hits[i])
			continue
		}
		if hits[i].Kind == string(model.TypeProposal) {
			proposalHit(&hits[i])
			continue
		}
		readable(&hits[i], name)
	}
	sort.SliceStable(hits, func(i, j int) bool { return hitClass(hits[i]) < hitClass(hits[j]) })
}

// hitClass ranks a hit by what the owner looks for: the work itself first
// (tasks, questions, RFX, policies and their summaries), then other documents,
// and last the documents that only record that something happened (domains,
// journal days). Within a class the order of the index stands.
func hitClass(h index.Hit) int {
	switch h.Kind {
	case string(model.TypeTask), string(model.TypeQuestion), string(model.TypeRFX), string(model.TypePolicy), index.KindSummary:
		return 0
	case string(model.TypeDomain), string(model.TypeJournal):
		return 2
	}
	return 1
}
