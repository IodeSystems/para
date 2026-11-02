package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func foldOf(t *testing.T, lines ...string) *foldResult {
	t.Helper()
	p := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fr, err := foldEvents(p)
	if err != nil {
		t.Fatal(err)
	}
	return fr
}

func ev(ts, action, id, rest string) string {
	if rest != "" {
		rest = "," + rest
	}
	return `{"ts":"2025-03-01T` + ts + `Z","actor":"a","action":"` + action + `","id":"` + id + `"` + rest + `}`
}

func TestFoldLatestStatusWins(t *testing.T) {
	fr := foldOf(t,
		ev("09:00:00", "add", "T-250301-01", `"type":"task","text":"x"`),
		ev("09:03:00", "status", "T-250301-01", `"status":"done"`),
		ev("09:01:00", "status", "T-250301-01", `"status":"inflight"`), // out of file order: ts decides
		ev("09:04:00", "status", "T-250301-01", `"status":"nonsense"`), // ignored, reported
	)
	it := fr.items["T-250301-01"]
	if got := it.effective(); got != "done" {
		t.Errorf("status = %s, want done", got)
	}
	if fr.report.TasksByStatus["done"] != 1 || len(fr.report.BadLines) != 1 {
		t.Errorf("report = %+v", fr.report)
	}
}

func TestFoldContactRules(t *testing.T) {
	// A contact is keyed by who: later events of the same person update one contact.
	fr := foldOf(t,
		ev("09:00:00", "add", "T-250301-01", `"type":"task","text":"x"`),
		ev("09:01:00", "contact", "T-250301-01", `"who":"Ada Example","via":"email","state":"drafted"`),
		ev("09:02:00", "contact", "T-250301-01", `"who":"ada example","state":"sent","reply_by":"2025-03-09"`),
		ev("09:03:00", "contact", "T-250301-01", `"who":"Bo Example","via":"phone","state":"sent"`),
	)
	it := fr.items["T-250301-01"]
	if len(it.Contacts) != 2 {
		t.Fatalf("contacts = %d, want 2 (keyed by who)", len(it.Contacts))
	}
	if c := it.Contacts[0]; c.Via != "email" || c.State != "sent" || c.ReplyBy != "2025-03-09" {
		t.Errorf("contact = %+v", c)
	}
	if got := it.effective(); got != "waiting" {
		t.Errorf("any sent: status = %s, want waiting", got)
	}

	// Waiting while any is sent: one reply is not enough.
	fr = foldOf(t,
		ev("09:00:00", "add", "T-250301-01", `"type":"task","text":"x"`),
		ev("09:01:00", "contact", "T-250301-01", `"who":"A","state":"sent"`),
		ev("09:02:00", "contact", "T-250301-01", `"who":"B","state":"sent"`),
		ev("09:03:00", "contact", "T-250301-01", `"who":"A","state":"replied"`),
	)
	if got := fr.items["T-250301-01"].effective(); got != "waiting" {
		t.Errorf("one of two replied: %s, want waiting", got)
	}
	// Every contact replied or no-reply: back to todo.
	fr = foldOf(t,
		ev("09:00:00", "add", "T-250301-01", `"type":"task","text":"x"`),
		ev("09:00:30", "status", "T-250301-01", `"status":"waiting"`),
		ev("09:01:00", "contact", "T-250301-01", `"who":"A","state":"sent"`),
		ev("09:02:00", "contact", "T-250301-01", `"who":"B","state":"sent"`),
		ev("09:03:00", "contact", "T-250301-01", `"who":"A","state":"replied"`),
		ev("09:04:00", "contact", "T-250301-01", `"who":"B","state":"no-reply"`),
	)
	if got := fr.items["T-250301-01"].effective(); got != "todo" {
		t.Errorf("all closed: %s, want todo", got)
	}
	if fr.report.TasksByStatus["todo"] != 1 {
		t.Errorf("report = %+v", fr.report.TasksByStatus)
	}
	// A finished item stays finished while a contact is sent; drafted alone does not wait.
	fr = foldOf(t,
		ev("09:00:00", "add", "T-250301-01", `"type":"task","text":"x"`),
		ev("09:01:00", "contact", "T-250301-01", `"who":"A","state":"sent"`),
		ev("09:02:00", "status", "T-250301-01", `"status":"done"`),
		ev("09:00:00", "add", "T-250301-02", `"type":"task","text":"y"`),
		ev("09:01:00", "contact", "T-250301-02", `"who":"A","state":"drafted"`),
	)
	if got := fr.items["T-250301-01"].effective(); got != "done" {
		t.Errorf("done with sent contact: %s", got)
	}
	if got := fr.items["T-250301-02"].effective(); got != "todo" {
		t.Errorf("drafted only: %s, want todo", got)
	}
}

func TestFoldDependsAccumulateAndLatestCheckWins(t *testing.T) {
	fr := foldOf(t,
		ev("09:00:00", "add", "T-250301-01", `"type":"task","text":"x","depends_on":"T-250301-09"`),
		ev("09:01:00", "depends", "T-250301-01", `"depends_on":["T-250301-08","T-250301-09"]`),
		ev("09:02:00", "depends", "T-250301-01", `"depends_on":"T-250301-07"`),
		ev("09:03:00", "check", "T-250301-01", `"check":"first"`),
		ev("09:04:00", "check", "T-250301-01", `"check":"second"`),
	)
	it := fr.items["T-250301-01"]
	if strings.Join(it.Depends, ",") != "T-250301-09,T-250301-08,T-250301-07" {
		t.Errorf("depends = %v", it.Depends)
	}
	if it.Check != "second" {
		t.Errorf("check = %q, want the latest", it.Check)
	}
	if fr.report.DependsEdges != 3 || fr.report.Checks != 1 {
		t.Errorf("report = %+v", fr.report)
	}
}

func TestFoldQuestions(t *testing.T) {
	fr := foldOf(t,
		ev("09:00:00", "add", "Q-250301-01", `"type":"question","text":"a"`),
		ev("09:01:00", "answer", "Q-250301-01", `"answer":"yes"`),
		ev("09:00:00", "add", "Q-250301-02", `"text":"b"`), // type from the id
		ev("09:00:00", "add", "Q-250301-03", `"text":"c"`),
		ev("09:01:00", "status", "Q-250301-03", `"status":"dropped"`),
	)
	r := fr.report
	if r.OpenQuestions != 1 || r.QuestionsByStatus["answered"] != 1 || r.QuestionsByStatus["dropped"] != 1 || r.QuestionsByStatus["open"] != 1 {
		t.Errorf("report = %+v", r)
	}
	if fr.items["Q-250301-01"].Answer != "yes" {
		t.Error("answer lost")
	}
}

func TestFoldReportsBadLinesWithNumbers(t *testing.T) {
	fr := foldOf(t,
		`not json`,
		ev("09:00:00", "add", "T-250301-01", `"type":"task","text":"x"`),
		ev("09:01:00", "frobnicate", "T-250301-01", ``),
		ev("09:01:00", "status", "T-111111-11", `"status":"done"`),
		`{"ts":"2025-03-01T09:02:00Z","action":"add"}`,
	)
	if len(fr.report.BadLines) != 4 {
		t.Fatalf("bad lines = %+v", fr.report.BadLines)
	}
	if fr.report.BadLines[0].Path != "events.jsonl:1" {
		t.Errorf("first = %+v", fr.report.BadLines[0])
	}
}
