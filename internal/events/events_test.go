package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

func testLog(t *testing.T, now *time.Time) (*Log, string) {
	t.Helper()
	dir := t.TempDir()
	return NewLog(dir, Source{Vault: "acme", Install: "m1", Actor: "user", Surface: "cli", Now: func() time.Time { return *now }}), dir
}

func TestAppendFieldsAndMonthFile(t *testing.T) {
	now := time.Date(2026, 11, 30, 23, 0, 0, 0, time.FixedZone("x", -3*3600))
	l, dir := testLog(t, &now)
	if err := l.Append(OpMove, "inbox-1", "inbox", "archive"); err != nil {
		t.Fatal(err)
	}
	// 23:00 at UTC-3 is December in UTC.
	data, err := os.ReadFile(filepath.Join(dir, "2026-12.m1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"install": "m1", "actor": "user", "surface": "cli", "vault": "acme", "op": "move", "doc": "inbox-1", "from": "inbox", "to": "archive"}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
	if m["ts"] != "2026-12-01T02:00:00Z" {
		t.Errorf("ts = %v", m["ts"])
	}
	if len(m) != 9 {
		t.Errorf("fields = %v", m)
	}
}

func TestAppendRejectsContentShapedValues(t *testing.T) {
	now := time.Now()
	l, dir := testLog(t, &now)
	cases := []struct {
		name, op, doc, from, to string
	}{
		{"sentence in from", "update", "x", "the quick brown fox", ""},
		{"long to", "update", "x", "", strings.Repeat("a", 65)},
		{"newline in doc", "update", "a\nb", "", ""},
		{"bad op", "Not An Op", "x", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := l.Append(c.op, c.doc, c.from, c.to); err == nil {
				t.Fatal("want error")
			}
		})
	}
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Fatalf("wrote %v", files)
	}
}

func TestTailAcrossMonths(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	l, _ := testLog(t, &now)
	for _, d := range []string{"a", "b"} {
		l.Append(OpCreate, d, "", "")
	}
	now = now.AddDate(0, 0, 1)
	for _, d := range []string{"c", "d"} {
		l.Append(OpCreate, d, "", "")
	}
	cases := []struct {
		n    int
		want string
	}{{1, "d"}, {3, "bcd"}, {10, "abcd"}}
	for _, c := range cases {
		recs, err := l.Tail(c.n)
		if err != nil {
			t.Fatal(err)
		}
		var got strings.Builder
		for _, r := range recs {
			got.WriteString(r.Doc)
		}
		if got.String() != c.want {
			t.Errorf("Tail(%d) = %q, want %q", c.n, got.String(), c.want)
		}
	}
}

func TestTailReportsBadLine(t *testing.T) {
	now := time.Now()
	l, dir := testLog(t, &now)
	os.WriteFile(filepath.Join(dir, "2026-11.m1.jsonl"), []byte("{bad\n"), 0o600)
	if _, err := l.Tail(1); err == nil || !strings.Contains(err.Error(), "2026-11.m1.jsonl:1") {
		t.Fatalf("err = %v", err)
	}
}

func TestReconcile(t *testing.T) {
	type edit func(root string)
	write := func(rel, s string) edit {
		return func(root string) {
			p := filepath.Join(root, rel)
			os.MkdirAll(filepath.Dir(p), 0o700)
			os.WriteFile(p, []byte(s), 0o600)
		}
	}
	cases := []struct {
		name string
		edit edit
		want []model.Change
		from bool
		to   bool
	}{
		{"nothing", func(string) {}, nil, false, false},
		{"changed", write("inbox/a.md", "edited"), []model.Change{{Path: "inbox/a.md", Kind: model.ChangeChanged, Outcome: model.OutcomeLogged}}, true, true},
		{"added", write("inbox/b.md", "new"), []model.Change{{Path: "inbox/b.md", Kind: model.ChangeAdded, Outcome: model.OutcomeLogged}}, false, true},
		{"removed", func(r string) { os.Remove(filepath.Join(r, "inbox/a.md")) }, []model.Change{{Path: "inbox/a.md", Kind: model.ChangeRemoved, Outcome: model.OutcomeLogged}}, true, false},
		{"dot file ignored", write("inbox/.draft.md", "x"), nil, false, false},
		{"untracked folder ignored", write("other/x.md", "x"), nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			write("inbox/a.md", "one")(root)
			now := time.Now()
			l := NewLog(t.TempDir(), Source{Vault: "acme", Install: "m1", Now: func() time.Time { return now }})
			files, err := Scan(root, []string{"inbox"})
			if err != nil {
				t.Fatal(err)
			}
			m := Manifest{Files: files}
			c.edit(root)
			got, err := Reconcile(l, root, []string{"inbox"}, &m, Pending{}, now, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(c.want) || (len(got) == 1 && got[0] != c.want[0]) {
				t.Fatalf("changes = %v, want %v", got, c.want)
			}
			recs, _ := l.Tail(10)
			if len(recs) != len(c.want) {
				t.Fatalf("events = %v", recs)
			}
			for _, r := range recs {
				if r.Op != OpExternalEdit || (r.From != "") != c.from || (r.To != "") != c.to {
					t.Errorf("event = %+v", r)
				}
			}
			again, err := Reconcile(l, root, []string{"inbox"}, &m, Pending{}, now, 0)
			if err != nil || len(again) != 0 {
				t.Fatalf("second run = %v, %v", again, err)
			}
		})
	}
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "m.json")
	if _, found, err := LoadManifest(file); found || err != nil {
		t.Fatalf("absent: found=%v err=%v", found, err)
	}
	if err := (Manifest{Files: map[string]string{"a": "h"}}).Save(file); err != nil {
		t.Fatal(err)
	}
	m, found, err := LoadManifest(file)
	if err != nil || !found || m.Files["a"] != "h" {
		t.Fatalf("m=%v found=%v err=%v", m, found, err)
	}
	os.WriteFile(file, []byte("{"), 0o600)
	if _, _, err := LoadManifest(file); err == nil {
		t.Fatal("want error on a corrupt manifest")
	}
}
