package recur

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	ok := map[string]Rule{
		"daily": {Days: 1}, "Weekly": {Days: 7}, " monthly ": {Months: 1},
		"every 3 days": {Days: 3}, "quarterly": {Months: 3}, "Yearly": {Months: 12}, "Every  1  day": {Days: 1}, "every 3660 days": {Days: 3660},
	}
	for in, want := range ok {
		if got, err := Parse(in); err != nil || got != want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "annually", "every 0 days", "every 3661 days", "every x days", "every 2 weeks", "weekly on monday"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): want an error", in)
		}
	}
}

func TestNext(t *testing.T) {
	d := func(s string) time.Time {
		v, err := time.Parse(Layout, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		rule        string
		from, today string
		want        string
	}{
		{"daily", "2026-11-01", "2026-11-01", "2026-11-02"},
		{"weekly", "2026-11-01", "2026-10-01", "2026-11-08"},
		{"weekly", "2026-11-01", "2026-11-30", "2026-12-06"}, // late: first week after today
		{"daily", "2026-10-01", "2026-11-05", "2026-11-06"},  // finished late today: not due again today
		{"weekly", "2026-10-29", "2026-11-05", "2026-11-12"}, // the missed cycle fell on today
		{"every 10 days", "2026-11-01", "2026-11-01", "2026-11-11"},
		{"monthly", "2026-01-31", "2026-01-31", "2026-02-28"},
		{"monthly", "2026-01-31", "2026-03-01", "2026-03-31"},
		{"monthly", "2026-12-15", "2026-12-15", "2027-01-15"},
		{"monthly", "2028-01-31", "2028-01-01", "2028-02-29"},
	}
	for _, c := range cases {
		r, err := Parse(c.rule)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Next(d(c.from), d(c.today)).Format(Layout); got != c.want {
			t.Errorf("%s from %s today %s = %s, want %s", c.rule, c.from, c.today, got, c.want)
		}
	}
}
