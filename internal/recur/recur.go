// Package recur parses the `recur` value of a task and computes the next due
// date. Para has no scheduler: a recurring task makes its next occurrence when
// it is marked done (or skipped), and nothing else.
package recur

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Layout is the date form of a due date.
const Layout = "2006-01-02"

// MaxDays is the longest `every <n> days` interval.
const MaxDays = 3660

// Rule is a parsed recurrence: a step of Days, or of Months.
type Rule struct {
	Days   int
	Months int
}

var everyRe = regexp.MustCompile(`^every (\d+) days?$`)

// Forms lists the accepted values, for messages.
const Forms = "daily, weekly, monthly, quarterly, yearly, every <n> days"

// Parse reads a recur value: daily, weekly, monthly, quarterly, yearly, or
// every <n> days.
// Case and spacing are ignored.
func Parse(s string) (Rule, error) {
	v := strings.ToLower(strings.Join(strings.Fields(s), " "))
	switch v {
	case "daily":
		return Rule{Days: 1}, nil
	case "weekly":
		return Rule{Days: 7}, nil
	case "monthly":
		return Rule{Months: 1}, nil
	case "quarterly":
		return Rule{Months: 3}, nil
	case "yearly":
		return Rule{Months: 12}, nil
	}
	if m := everyRe.FindStringSubmatch(v); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > MaxDays {
			return Rule{}, fmt.Errorf("recur %q: n must be 1 to %d", s, MaxDays)
		}
		return Rule{Days: n}, nil
	}
	return Rule{}, fmt.Errorf("recur %q: want %s", s, Forms)
}

// Next returns the first occurrence after from that is not before today:
// from plus one step, or more steps when the task was finished late. A missed
// cycle is skipped, never queued. Months keep the day of month of from, and
// clamp to the end of a short month (Jan 31 gives Feb 28, then Mar 31).
func (r Rule) Next(from, today time.Time) time.Time {
	from = day(from)
	today = day(today)
	for k := 1; ; k++ {
		next := r.step(from, k)
		if !next.Before(today) {
			return next
		}
	}
}

func (r Rule) step(from time.Time, k int) time.Time {
	if r.Months == 0 {
		return from.AddDate(0, 0, r.Days*k)
	}
	y, m, d := from.Date()
	first := time.Date(y, m+time.Month(r.Months*k), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, time.UTC)
}

func day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
