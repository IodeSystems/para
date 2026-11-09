package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/iodesystems/para/internal/model"
)

// tone is the severity of a line of text output: it picks the colour of a
// severity word and of a verdict.
type tone int

const (
	toneOK tone = iota
	toneWarn
	toneErr
)

var toneColor = map[tone]string{toneOK: "\x1b[32m", toneWarn: "\x1b[33m", toneErr: "\x1b[31m"}

// terminal reports whether w is a terminal. Tests replace it.
var terminal = isTerminal

// colorOn reports whether text to w is coloured: w is a terminal and NO_COLOR
// is unset or empty. --json output never goes through paint.
func colorOn(w io.Writer) bool {
	return os.Getenv("NO_COLOR") == "" && terminal(w)
}

// paint colours s by t when w takes colour; otherwise s as it is.
func paint(w io.Writer, t tone, s string) string {
	if !colorOn(w) {
		return s
	}
	return toneColor[t] + s + "\x1b[0m"
}

// severityTone is the tone of a severity word: error and fail are red, warn
// and warning yellow, anything else green.
func severityTone(sev string) tone {
	switch sev {
	case model.SeverityError, "fail":
		return toneErr
	case "warn", "warning":
		return toneWarn
	}
	return toneOK
}

// findingLine is a lint finding as model.Finding.String prints it, with the
// severity coloured.
func findingLine(w io.Writer, f model.Finding) string {
	loc := f.Path
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.Path, f.Line)
	}
	return fmt.Sprintf("%s: %s [%s] %s", loc, paint(w, severityTone(f.Severity), f.Severity), f.Code, f.Message)
}

// verdict prints the one last line of a check: ✓ when the command exits 0, ✗
// when it fails (toneErr). The colour says the worst severity.
func verdict(w io.Writer, t tone, format string, a ...any) error {
	mark := "✓"
	if t == toneErr {
		mark = "✗"
	}
	_, err := fmt.Fprintln(w, paint(w, t, mark+" "+fmt.Sprintf(format, a...)))
	return err
}

// countTone is toneErr with errors, toneWarn with warnings, else toneOK.
func countTone(errors, warnings int) tone {
	switch {
	case errors > 0:
		return toneErr
	case warnings > 0:
		return toneWarn
	}
	return toneOK
}

// filedNote is the end of a verdict that filed agent work.
func filedNote(id string, updated bool) string {
	if id == "" {
		return ""
	}
	how := "filed as"
	if updated {
		how = "updated"
	}
	return fmt.Sprintf(" — %s agent work %s; run /para loop", how, id)
}
