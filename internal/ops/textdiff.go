package ops

import (
	"strconv"
	"strings"
	"unicode"
)

// A diff of two versions of a tracked document, by line or by paragraph,
// cut to the changes and a few units of context around them. The common
// start and end are cut first, so a small edit to a long document stays
// cheap; what is left is compared with a longest common subsequence, up to
// diffCellCap cells, and past that shown as removed then added.

// Diff units.
const (
	UnitLine      = "line"
	UnitParagraph = "paragraph"
)

// DiffUnits lists the units doc_diff takes.
var DiffUnits = []string{UnitLine, UnitParagraph}

// Diff part ops.
const (
	DiffSame = "same"
	DiffDel  = "del"
	DiffAdd  = "add"
	// DiffGap stands for unchanged units left out between two hunks.
	DiffGap = "gap"
)

const diffCellCap = 4_000_000

// DiffPart is one unit of a diff: a line, or a paragraph of lines.
type DiffPart struct {
	Op   string `json:"op"` // same, del, add, or gap
	Text string `json:"text"`
	// A and B are the 1-based unit numbers in the old and the new text; 0 for
	// a part that is not in that text. A gap counts the units it leaves out
	// in A.
	A int `json:"a,omitempty"`
	B int `json:"b,omitempty"`
}

// splitUnits cuts text into lines, or into paragraphs (runs of lines between
// blank lines).
func splitUnits(text, unit string) []string {
	if text == "" {
		return nil
	}
	if unit != UnitParagraph {
		return strings.Split(text, "\n")
	}
	var out []string
	var cur []string
	for _, ln := range strings.Split(text, "\n") {
		if strings.TrimSpace(ln) == "" {
			if len(cur) > 0 {
				out = append(out, strings.Join(cur, "\n"))
				cur = nil
			}
			continue
		}
		cur = append(cur, ln)
	}
	if len(cur) > 0 {
		out = append(out, strings.Join(cur, "\n"))
	}
	return out
}

// diffUnits is every unit of x and y as same, del, or add, in order.
func diffUnits(x, y []string) []DiffPart {
	pre := 0
	for pre < len(x) && pre < len(y) && x[pre] == y[pre] {
		pre++
	}
	suf := 0
	for suf < len(x)-pre && suf < len(y)-pre && x[len(x)-1-suf] == y[len(y)-1-suf] {
		suf++
	}
	out := make([]DiffPart, 0, len(x)+len(y))
	for i := range pre {
		out = append(out, DiffPart{Op: DiffSame, Text: x[i], A: i + 1, B: i + 1})
	}
	mx, my := x[pre:len(x)-suf], y[pre:len(y)-suf]
	if len(mx)*len(my) > diffCellCap {
		for i, s := range mx {
			out = append(out, DiffPart{Op: DiffDel, Text: s, A: pre + i + 1})
		}
		for j, s := range my {
			out = append(out, DiffPart{Op: DiffAdd, Text: s, B: pre + j + 1})
		}
	} else {
		lcs := make([][]int32, len(mx)+1)
		for i := range lcs {
			lcs[i] = make([]int32, len(my)+1)
		}
		for i := len(mx) - 1; i >= 0; i-- {
			for j := len(my) - 1; j >= 0; j-- {
				if mx[i] == my[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(mx) || j < len(my) {
			switch {
			case i < len(mx) && j < len(my) && mx[i] == my[j]:
				out = append(out, DiffPart{Op: DiffSame, Text: mx[i], A: pre + i + 1, B: pre + j + 1})
				i++
				j++
			case j >= len(my) || (i < len(mx) && lcs[i+1][j] >= lcs[i][j+1]):
				out = append(out, DiffPart{Op: DiffDel, Text: mx[i], A: pre + i + 1})
				i++
			default:
				out = append(out, DiffPart{Op: DiffAdd, Text: my[j], B: pre + j + 1})
				j++
			}
		}
	}
	for k := range suf {
		a, b := len(x)-suf+k, len(y)-suf+k
		out = append(out, DiffPart{Op: DiffSame, Text: x[a], A: a + 1, B: b + 1})
	}
	return out
}

// compactDiff keeps the changed parts and n same parts around each, and puts
// a gap part where same parts are left out.
func compactDiff(parts []DiffPart, n int) []DiffPart {
	keep := make([]bool, len(parts))
	for i, p := range parts {
		if p.Op == DiffSame {
			continue
		}
		for k := max(0, i-n); k <= min(len(parts)-1, i+n); k++ {
			keep[k] = true
		}
	}
	out := []DiffPart{}
	skipped, at := 0, 0
	for i, p := range parts {
		if !keep[i] {
			if skipped == 0 {
				at = p.A
			}
			skipped++
			continue
		}
		if skipped > 0 {
			out = append(out, DiffPart{Op: DiffGap, A: at, Text: gapText(skipped)})
			skipped = 0
		}
		out = append(out, p)
	}
	if skipped > 0 && len(out) > 0 {
		out = append(out, DiffPart{Op: DiffGap, A: at, Text: gapText(skipped)})
	}
	return out
}

func gapText(n int) string {
	if n == 1 {
		return "1 unchanged"
	}
	return strconv.Itoa(n) + " unchanged"
}

// renderDiff writes parts as text: two-character prefixes per line, and a
// "@@" line for a gap.
func renderDiff(parts []DiffPart) string {
	var b strings.Builder
	for _, p := range parts {
		prefix := "  "
		switch p.Op {
		case DiffGap:
			b.WriteString("@@ " + p.Text + " @@\n")
			continue
		case DiffDel:
			prefix = "- "
		case DiffAdd:
			prefix = "+ "
		}
		for _, ln := range strings.Split(p.Text, "\n") {
			b.WriteString(strings.TrimRightFunc(prefix+ln, unicode.IsSpace) + "\n")
		}
	}
	return b.String()
}

// locateQuote finds quote in text and returns its start and end in code
// points. Of several exact matches it takes the one that starts nearest
// hint. With no exact match it compares with runs of white space as one
// space. ok is false when the quote is not there.
func locateQuote(text, quote string, hint int) (start, end int, ok bool) {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return 0, 0, false
	}
	t, q := []rune(text), []rune(quote)
	best, bestDist := -1, 0
	for i := 0; i+len(q) <= len(t); i++ {
		if !runesAt(t, q, i) {
			continue
		}
		d := abs(i - hint)
		if best < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	if best >= 0 {
		return best, best + len(q), true
	}
	// White space as one space: fold both, keep where each folded rune came from.
	ft, from := foldSpace(t)
	fq, _ := foldSpace(q)
	for i := 0; i+len(fq) <= len(ft); i++ {
		if !runesAt(ft, fq, i) {
			continue
		}
		s, e := from[i], from[i+len(fq)-1]+1
		d := abs(s - hint)
		if best < 0 || d < bestDist {
			best, bestDist, end = s, d, e
		}
	}
	if best >= 0 {
		return best, end, true
	}
	return 0, 0, false
}

func runesAt(t, q []rune, i int) bool {
	for k := range q {
		if t[i+k] != q[k] {
			return false
		}
	}
	return true
}

// foldSpace turns each run of white space into one space and trims both
// ends. from[i] is the index in r of folded rune i.
func foldSpace(r []rune) ([]rune, []int) {
	out := make([]rune, 0, len(r))
	from := make([]int, 0, len(r))
	space := false
	for i, c := range r {
		if unicode.IsSpace(c) {
			space = true
			continue
		}
		if space && len(out) > 0 {
			out = append(out, ' ')
			from = append(from, i-1)
		}
		space = false
		out = append(out, c)
		from = append(from, i)
	}
	return out, from
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
