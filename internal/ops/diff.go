package ops

import (
	"maps"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// diffLines is a longest-common-subsequence line diff.
func diffLines(a, b string) []string {
	x, y := splitLines(a), splitLines(b)
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	out := []string{}
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			out = append(out, "  "+x[i])
			i++
			j++
		case j >= len(y) || (i < len(x) && lcs[i+1][j] >= lcs[i][j+1]):
			out = append(out, "- "+x[i])
			i++
		default:
			out = append(out, "+ "+y[j])
			j++
		}
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// trimContext keeps changed lines and n lines around them.
func trimContext(lines []string, n int) []string {
	keep := map[int]bool{}
	for i, l := range lines {
		if strings.HasPrefix(l, "  ") {
			continue
		}
		for j := max(0, i-n); j <= min(len(lines)-1, i+n); j++ {
			keep[j] = true
		}
	}
	out := make([]string, 0, len(keep))
	for _, i := range slices.Sorted(maps.Keys(keep)) {
		out = append(out, lines[i])
	}
	return out
}

func renderText(d model.Document) string {
	b, err := model.Render(d)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\n")
}

// closesDoc reports whether a document is in a final state, so what waits on it
// is free.
func closesDoc(d model.Document) bool {
	switch m := d.Meta.(type) {
	case *model.Task:
		return taskTerminal(m.Status)
	case *model.RFX:
		return m.Status != model.RFXOpen
	case *model.Question:
		return m.Status != model.QuestionOpen
	case *model.Request:
		return slices.Contains([]model.RequestStatus{model.RequestDone, model.RequestFailed, model.RequestCancelled}, m.Status)
	}
	return false
}

// sameText compares two rendered documents without the updated: line, which
// every write refreshes, so a document undone to an older text still matches
// the receipt that wrote it.
func sameText(a, b string) bool { return withoutUpdated(a) == withoutUpdated(b) }

func withoutUpdated(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return s
	}
	for i := 1; i < len(lines) && lines[i] != "---"; i++ {
		if strings.HasPrefix(lines[i], "updated:") {
			return strings.Join(slices.Delete(slices.Clone(lines), i, i+1), "\n")
		}
	}
	return s
}
