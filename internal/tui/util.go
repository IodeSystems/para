package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func stripSGR(s string) string { return ansi.Strip(s) }

// cells is the display width of s, ignoring SGR codes. It counts CJK and
// emoji as two cells.
func cells(s string) int { return ansi.StringWidth(s) }

// truncate cuts s to w display cells, ending with an ellipsis when it cuts.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if cells(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// pad fills s with spaces to w display cells.
func pad(s string, w int) string {
	if n := w - cells(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// firstNonEmpty is the first string that is not empty.
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
