package ical

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestEscape(t *testing.T) {
	cases := map[string]string{
		`a,b;c\d`:           `a\,b\;c\\d`,
		"two\nlines":        `two\nlines`,
		"crlf\r\nline":      `crlf\nline`,
		"nul\x00bell\x07":   "nulbell",
		"tab\tkept":         "tab\tkept",
		"plain \u00e9 text": "plain \u00e9 text",
	}
	for in, want := range cases {
		if got := Escape(in); got != want {
			t.Errorf("Escape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFoldKeepsCharactersWhole(t *testing.T) {
	long := strings.Repeat("\u00e9", 100) // two octets each
	got := fold("SUMMARY:" + long)
	lines := strings.Split(got, "\r\n")
	if len(lines) < 3 {
		t.Fatalf("not folded: %q", got)
	}
	for i, l := range lines {
		if len(l) > lineMax {
			t.Errorf("line %d is %d octets", i, len(l))
		}
		if !utf8.ValidString(l) {
			t.Errorf("line %d splits a character: %q", i, l)
		}
		if i > 0 && !strings.HasPrefix(l, " ") {
			t.Errorf("continuation %d has no leading space", i)
		}
	}
	if back := strings.ReplaceAll(got, "\r\n ", ""); back != "SUMMARY:"+long {
		t.Error("unfolding does not give the original line")
	}
	if fold("short") != "short" {
		t.Error("a short line must not change")
	}
}

func TestRenderCannotInjectProperties(t *testing.T) {
	c := Calendar{ProdID: "-//para//EN", Marking: "SECRET\r\nEND:VCALENDAR", Stamp: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
		Events: []Event{{UID: "x@y\r\nEND:VEVENT", Summary: "Hi\r\nATTENDEE:mailto:evil@example.com", Description: "d\r\nURL:http://evil.example",
			Start: time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 4, 11, 0, 0, 0, time.UTC)}}}
	lines := strings.Split(strings.TrimSuffix(c.Render(), "\r\n"), "\r\n")
	count := map[string]int{}
	for _, l := range lines {
		count[l]++
		for _, bad := range []string{"ATTENDEE", "URL:"} {
			if strings.HasPrefix(l, bad) {
				t.Errorf("an injected property starts a line: %q", l)
			}
		}
	}
	// Each structural line stands alone exactly once.
	for _, l := range []string{"BEGIN:VCALENDAR", "END:VCALENDAR", "BEGIN:VEVENT", "END:VEVENT"} {
		if count[l] != 1 {
			t.Errorf("%q appears %d times:\n%s", l, count[l], strings.Join(lines, "\n"))
		}
	}
}

func TestAllDayEventEndsTheNextDay(t *testing.T) {
	out := Calendar{ProdID: "p", Stamp: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), Events: []Event{{UID: "u", Summary: "s", Day: "2026-12-31"}}}.Render()
	for _, want := range []string{"DTSTART;VALUE=DATE:20261231\r\n", "DTEND;VALUE=DATE:20270101\r\n", "TRANSP:TRANSPARENT\r\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}
