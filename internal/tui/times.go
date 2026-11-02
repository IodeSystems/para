package tui

import (
	"fmt"
	"time"
)

// whenWords says a time for a person, in local time: "2 hours ago" for the
// last day, "Nov 2, 07:09" after it, with the year when it is not this year.
// A zero now means the present.
func whenWords(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	if now.IsZero() {
		now = time.Now()
	}
	t, now = t.Local(), now.Local()
	d := now.Sub(t)
	switch {
	case d >= 0 && d < time.Minute:
		return "just now"
	case d >= time.Minute && d < time.Hour:
		return agoWords(int(d/time.Minute), "minute")
	case d >= time.Hour && d < 24*time.Hour:
		return agoWords(int(d/time.Hour), "hour")
	}
	if t.Year() != now.Year() {
		return t.Format("Jan 2, 2006, 15:04")
	}
	return t.Format("Jan 2, 15:04")
}

func agoWords(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s ago", unit)
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

// dateWords says a YYYY-MM-DD date as "Nov 3", with the year when it is not
// this year. Text that is not a date is returned as it is.
func dateWords(s string, now time.Time) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	if now.IsZero() {
		now = time.Now()
	}
	if t.Year() != now.Year() {
		return t.Format("Jan 2, 2006")
	}
	return t.Format("Jan 2")
}
