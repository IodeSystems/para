package ops

import (
	"regexp"
	"sort"
	"strings"

	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/model"
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
	h.Snippet, h.Marks = "A legacy proposal. Read only.", nil
}

// readableHits fills the snippets of hits, and puts journal hits after the
// rest: a journal is a log of everything else.
func readableHits(hits []index.Hit, name func(string) string) {
	for i := range hits {
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
