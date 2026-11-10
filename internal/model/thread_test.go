package model

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/paramd"
)

func TestParseThreadKey(t *testing.T) {
	cases := []struct {
		in    string
		kind  ThreadKind
		value string
		bad   bool
	}{
		{"task-ship-it", ThreadItem, "task-ship-it", false},
		{"io-2026-11-01.a", ThreadItem, "io-2026-11-01.a", false},
		{"#billing", ThreadTag, "billing", false},
		{"#q4-plan", ThreadTag, "q4-plan", false},
		{"channel:gmail/18c2f", ThreadChannel, "gmail/18c2f", false},
		{"channel:slack/1699.123", ThreadChannel, "slack/1699.123", false},
		{"", "", "", true},
		{" task-a", "", "", true},
		{"#", "", "", true},
		{"#Billing", "", "", true},
		{"#two words", "", "", true},
		{"channel:gmail", "", "", true},
		{"channel:/x", "", "", true},
		{"channel:gmail/", "", "", true},
		{"[[task-a]]", "", "", true},
		{"a b", "", "", true},
		{"-x", "", "", true},
	}
	for _, c := range cases {
		kind, value, err := ParseThreadKey(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q: want error, got %s %q", c.in, kind, value)
			}
			continue
		}
		if err != nil || kind != c.kind || value != c.value {
			t.Errorf("%q: got %s %q %v, want %s %q", c.in, kind, value, err, c.kind, c.value)
		}
	}
}

func TestNormalizeThreadKey(t *testing.T) {
	for in, want := range map[string]string{
		"  #Q4 Plan ":         "#q4-plan",
		"##x":                 "", // second # is not a tag character
		"task-a":              "task-a",
		" channel:gmail/abc ": "channel:gmail/abc",
		"Task-A":              "Task-A", // ids keep case
		"#":                   "",
		"channel:gmail/a b":   "",
	} {
		got, err := NormalizeThreadKey(in)
		if (err != nil) != (want == "") || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, err, want)
		}
	}
}

func TestThreadKeysOfIO(t *testing.T) {
	io := &IO{Thread: LinkTo("io-earlier")}
	if got := io.ThreadKey(); got != "io-earlier" {
		t.Errorf("io key %q", got)
	}
	if got := (&IO{}).ThreadKey(); got != "" {
		t.Errorf("empty io key %q", got)
	}
	via := IOVia{Source: "gmail", Ref: "m1", Thread: "t9"}
	if got := via.ThreadKey(); got != "channel:gmail/t9" {
		t.Errorf("via key %q", got)
	}
	if _, _, err := ParseThreadKey(via.ThreadKey()); err != nil {
		t.Error(err)
	}
	if got := (IOVia{Source: "gmail", Ref: "m1"}).ThreadKey(); got != "" {
		t.Errorf("via without thread %q", got)
	}
}

func lintCodes(name, src string) []string {
	var out []string
	for _, f := range paramd.Lint(paramd.Parse(name, []byte(src))) {
		out = append(out, f.Code)
	}
	return out
}

func TestThreadsRoundTrip(t *testing.T) {
	fm := "type: task\ncreated: 2026-11-01\nstatus: todo\narea: \"[[area-a]]\"\nthreads:\n  - task-other\n  - \"#billing\"\n  - channel:gmail/t9\nfuture_key: keep me\n"
	d, err := Decode("task-a", FolderProjects, []byte(fm), "A", "")
	if err != nil {
		t.Fatal(err)
	}
	h := d.Meta.Header()
	if strings.Join(h.Threads, ",") != "task-other,#billing,channel:gmail/t9" {
		t.Fatalf("threads %v", h.Threads)
	}
	data, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"threads:\n", "- task-other\n", "- '#billing'\n", "- channel:gmail/t9\n", "future_key: keep me\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in:\n%s", want, data)
		}
	}
	for _, c := range lintCodes("task-a.md", string(data)) {
		if c == paramd.CodeFieldValue {
			t.Errorf("lint %s:\n%s", c, data)
		}
	}
	bad := strings.Replace(string(data), "- task-other\n", "- \"#Two Words\"\n", 1)
	found := false
	for _, c := range lintCodes("task-a.md", bad) {
		found = found || c == paramd.CodeFieldValue
	}
	if !found {
		t.Errorf("bad thread key not linted:\n%s", bad)
	}
}

func TestRequestTurnRoundTrip(t *testing.T) {
	fm := "type: request\ncreated: 2026-11-01\nstatus: queued\nrunner: box1\nqueued_at: 2026-11-01T09:00:00Z\n" +
		"thread: task-a\nreply: Done, see the note.\nturn_kind: modify\nquote: \"ship it Friday\"\n"
	d, err := Decode("request-a", FolderRequests, []byte(fm), "R", "")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := d.Meta.(*Request)
	if !ok {
		t.Fatalf("meta %T", d.Meta)
	}
	if r.Thread != "task-a" || r.Reply != "Done, see the note." || r.TurnKind != RequestTurnModify || r.Quote != "ship it Friday" {
		t.Fatalf("request %+v", r)
	}
	data, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"thread: task-a\n", "reply: Done, see the note.\n", "turn_kind: modify\n", "quote: ship it Friday\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in:\n%s", want, data)
		}
	}
	src := string(data) + "\n# R\n"
	if !strings.HasPrefix(src, "---") {
		src = "---\n" + string(data) + "---\n\n# R\n"
	}
	for _, c := range lintCodes("request-a.md", src) {
		if c == paramd.CodeFieldValue || c == paramd.CodeField {
			t.Errorf("lint %s:\n%s", c, src)
		}
	}
	badKind := strings.Replace(src, "turn_kind: modify", "turn_kind: shout", 1)
	found := false
	for _, c := range lintCodes("request-a.md", badKind) {
		found = found || c == paramd.CodeFieldValue
	}
	if !found {
		t.Error("bad turn_kind not linted")
	}
}
