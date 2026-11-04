// Package ical writes an iCalendar (RFC 5545) feed of events, and reads
// calendar text a caller hands it (parse.go). It opens nothing: para does not
// fetch or change an external calendar.
package ical

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Event is one VEVENT. A timed event has Start and End; an all-day event has
// Day (YYYY-MM-DD) and no times.
type Event struct {
	UID         string
	Summary     string
	Description string
	Start, End  time.Time
	Day         string
}

// Calendar is a feed.
type Calendar struct {
	ProdID  string
	Marking string // classification text, written as X-PARA-MARKING
	Stamp   time.Time
	Events  []Event
}

const (
	lineMax  = 75 // octets per line before folding
	timeForm = "20060102T150405Z"
	dayForm  = "20060102"
)

// Escape quotes a TEXT value: backslash, semicolon, comma, and newline.
func Escape(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`, "\r", `\n`)
	return r.Replace(s)
}

// fold breaks a line at 75 octets with CRLF and a space, never inside a
// multi-byte character.
func fold(line string) string {
	if len(line) <= lineMax {
		return line
	}
	var b strings.Builder
	n := 0
	limit := lineMax
	for _, r := range line {
		size := utf8.RuneLen(r)
		if n+size > limit {
			b.WriteString("\r\n ")
			n, limit = 0, lineMax-1 // a continuation starts with a space
		}
		b.WriteRune(r)
		n += size
	}
	return b.String()
}

// Render writes the feed with CRLF line ends. A control character other than a
// tab in any text is dropped, so no value can start a new property.
func (c Calendar) Render() string {
	var b strings.Builder
	line := func(s string) { b.WriteString(fold(s) + "\r\n") }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:" + Escape(c.ProdID))
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	if c.Marking != "" {
		line("X-PARA-MARKING:" + Escape(clean(c.Marking)))
	}
	stamp := c.Stamp.UTC().Format(timeForm)
	for _, e := range c.Events {
		line("BEGIN:VEVENT")
		line("UID:" + clean(e.UID))
		line("DTSTAMP:" + stamp)
		if e.Day != "" {
			start, err := time.Parse("2006-01-02", e.Day)
			if err == nil {
				line("DTSTART;VALUE=DATE:" + start.Format(dayForm))
				line("DTEND;VALUE=DATE:" + start.AddDate(0, 0, 1).Format(dayForm))
			}
			line("TRANSP:TRANSPARENT")
		} else {
			line("DTSTART:" + e.Start.UTC().Format(timeForm))
			line("DTEND:" + e.End.UTC().Format(timeForm))
		}
		line("SUMMARY:" + Escape(clean(e.Summary)))
		if e.Description != "" {
			line("DESCRIPTION:" + Escape(e.Description))
		}
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return b.String()
}

// clean drops control characters from a one-line value.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
