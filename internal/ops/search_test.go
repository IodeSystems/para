package ops

import (
	"testing"
	"time"
)

func TestParseQuery(t *testing.T) {
	in := ParseQuery("heron type:task status:open  project:p1 area:a1 since:7d http://x.test nokey: unknown:v")
	want := SearchInput{Query: "heron http://x.test nokey: unknown:v", Type: "task", Status: "open", Project: "p1", Area: "a1", Since: "7d"}
	if in != want {
		t.Errorf("got %+v, want %+v", in, want)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{"": "", "2026-10-01": "2026-10-01", "7d": "2026-10-25", "2w": "2026-10-18"} {
		got, err := ParseSince(in, now)
		if err != nil || got != want {
			t.Errorf("ParseSince(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"yesterday", "7", "2026-13-01", "7m"} {
		if _, err := ParseSince(bad, now); err == nil {
			t.Errorf("ParseSince(%q) accepted", bad)
		}
	}
}
