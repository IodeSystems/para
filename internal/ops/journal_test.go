package ops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// memStore is a tiny vault: documents and events in memory.
type memStore struct {
	store.Store
	docs   map[string]model.Document
	events []model.Event
	writes int
}

func newMemStore() *memStore { return &memStore{docs: map[string]model.Document{}} }

func (m *memStore) Info() store.Info { return store.Info{Surface: "cli"} }

func (m *memStore) emit(ts, op, doc string) {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	m.events = append(m.events, model.Event{TS: t, Op: op, Doc: doc})
}

func (m *memStore) List(context.Context, store.Filter) ([]model.Ref, error) {
	var out []model.Ref
	for _, d := range m.docs {
		out = append(out, model.Ref{ID: d.ID, Folder: d.Folder, Type: d.Meta.Header().Type})
	}
	return out, nil
}

func (m *memStore) Get(_ context.Context, id string) (model.Document, error) {
	d, ok := m.docs[id]
	if !ok {
		return d, model.ErrNotFound
	}
	return d, nil
}

// Now is UTC, so the periods of these tests are the days of the stamps.
func (m *memStore) Now() time.Time { return time.Now().UTC() }

func (m *memStore) Events(context.Context, int) ([]model.Event, error) { return m.events, nil }

// Create and Update log like the vault does: an event stamped now.
func (m *memStore) Create(_ context.Context, d model.Document) (model.Document, error) {
	m.docs[d.ID] = d
	m.writes++
	m.emit("2026-11-02T12:00:00Z", "create", d.ID)
	return d, nil
}

func (m *memStore) Update(_ context.Context, d model.Document) (model.Document, error) {
	m.docs[d.ID] = d
	m.writes++
	m.emit("2026-11-02T12:00:01Z", "update", d.ID)
	return d, nil
}

func (m *memStore) addNote(id string) {
	m.docs[id] = model.Document{ID: id, Folder: model.FolderInbox, Title: id, Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}}}
}

func TestNewPeriod(t *testing.T) {
	cases := []struct {
		scope          model.JournalScope
		date           string
		id, start, end string
	}{
		{model.JournalDay, "2026-11-01", "2026-11-01", "2026-11-01", "2026-11-02"},
		{model.JournalWeek, "2026-11-01", "2026-w44", "2026-10-26", "2026-11-02"}, // Sunday
		{model.JournalWeek, "2026-11-02", "2026-w45", "2026-11-02", "2026-11-09"}, // Monday
		{model.JournalWeek, "2027-01-01", "2026-w53", "2026-12-28", "2027-01-04"}, // ISO year differs
		{model.JournalMonth, "2026-02-17", "2026-02", "2026-02-01", "2026-03-01"},
		{model.JournalMonth, "2026-12-31", "2026-12", "2026-12-01", "2027-01-01"},
	}
	for _, c := range cases {
		day, _ := time.Parse(dateLayout, c.date)
		p := newPeriod(c.scope, day)
		if p.id != c.id || p.start.Format(dateLayout) != c.start || p.end.Format(dateLayout) != c.end {
			t.Errorf("%s %s: %s %s %s", c.scope, c.date, p.id, p.start.Format(dateLayout), p.end.Format(dateLayout))
		}
	}
}

func TestChildren(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse(dateLayout, s); return d }
	ids := func(ps []period) string {
		var s []string
		for _, p := range ps {
			s = append(s, p.id)
		}
		return strings.Join(s, " ")
	}
	cases := []struct {
		name string
		p    period
		want string
	}{
		{"week has seven days", newPeriod(model.JournalWeek, day("2026-11-04")),
			"2026-11-02 2026-11-03 2026-11-04 2026-11-05 2026-11-06 2026-11-07 2026-11-08"},
		{"month lists weeks touching it", newPeriod(model.JournalMonth, day("2026-11-15")),
			"2026-w44 2026-w45 2026-w46 2026-w47 2026-w48 2026-w49"},
		{"day has none", newPeriod(model.JournalDay, day("2026-11-15")), ""},
	}
	for _, c := range cases {
		if got := ids(c.p.children()); got != c.want {
			t.Errorf("%s: %q want %q", c.name, got, c.want)
		}
	}
}

func TestApplySections(t *testing.T) {
	a := section{"a", []string{"one"}}
	b := section{"b", []string{"two"}}
	cases := []struct {
		name, body string
		secs       []section
		want       string
	}{
		{"empty body", "", []section{a, b},
			"%% para:begin a %%\none\n%% para:end a %%\n\n%% para:begin b %%\ntwo\n%% para:end b %%"},
		{"replace keeps outside text", "My notes.\n\n%% para:begin a %%\nold\nlines\n%% para:end a %%\n\nMore notes.", []section{a},
			"My notes.\n\n%% para:begin a %%\none\n%% para:end a %%\n\nMore notes."},
		{"missing marker appended after text", "My notes.", []section{a},
			"My notes.\n\n%% para:begin a %%\none\n%% para:end a %%"},
		{"unterminated begin is not replaced", "x\n%% para:begin a %%\nstray", []section{a},
			"x\n%% para:begin a %%\nstray\n\n%% para:begin a %%\none\n%% para:end a %%"},
		{"other sections untouched", "%% para:begin z %%\nkeep\n%% para:end z %%", []section{a},
			"%% para:begin z %%\nkeep\n%% para:end z %%\n\n%% para:begin a %%\none\n%% para:end a %%"},
		{"crlf and edge newlines", "\n\nhi\r\n\n", []section{a}, "hi\n\n%% para:begin a %%\none\n%% para:end a %%"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := applySections(c.body, c.secs)
			if got != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, c.want)
			}
			if again := applySections(got, c.secs); again != got && c.name != "unterminated begin is not replaced" {
				t.Fatalf("not idempotent:\n%s", again)
			}
		})
	}
}

func daily(t *testing.T, m *memStore, date string) JournalOutput {
	t.Helper()
	out, err := JournalDaily.Run(context.Background(), Deps{Store: m}, JournalInput{Date: date})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestJournalDaily(t *testing.T) {
	m := newMemStore()
	m.addNote("inbox-a")
	m.emit("2026-11-01T09:12:00Z", "create", "inbox-a")
	m.emit("2026-11-01T10:00:00Z", "update", "inbox-a")
	m.emit("2026-11-01T11:00:00Z", "move", "inbox-gone")
	m.emit("2026-10-31T23:59:59Z", "create", "inbox-a") // yesterday
	m.emit("2026-11-02T00:00:00Z", "create", "inbox-a") // tomorrow

	out := daily(t, m, "2026-11-01")
	if out.Action != JournalCreated || out.ID != "2026-11-01" || out.Events != 3 || out.Path != "journal/2026-11-01.md" {
		t.Fatalf("out = %+v", out)
	}
	d := m.docs["2026-11-01"]
	j := d.Meta.(*model.Journal)
	if j.Scope != model.JournalDay || j.Date != "2026-11-01" || d.Folder != model.FolderJournal {
		t.Fatalf("meta = %+v", j)
	}
	for _, want := range []string{
		"events: 3", "- create: 1", "- move: 1", "- update: 1",
		"- 09:12 create [[inbox-a]]", "- 11:00 move `inbox-gone`",
	} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, d.Body)
		}
	}

	// Rerun: the journal's own create event is outside the count, nothing is written.
	writes := m.writes
	for i := range 3 {
		if out := daily(t, m, "2026-11-01"); out.Action != JournalUnchanged || out.Events != 3 {
			t.Fatalf("rerun %d: %+v", i, out)
		}
	}
	if m.writes != writes {
		t.Errorf("rerun wrote %d times", m.writes-writes)
	}

	// New event + hand text outside the markers: only the generated part changes.
	d.Body = "Morning thoughts.\n\n" + d.Body + "\n\nEvening thoughts."
	m.docs["2026-11-01"] = d
	m.emit("2026-11-01T18:30:00Z", "update", "inbox-a")
	if out := daily(t, m, "2026-11-01"); out.Action != JournalUpdated || out.Events != 4 {
		t.Fatalf("after event: %+v", out)
	}
	body := m.docs["2026-11-01"].Body
	if !strings.HasPrefix(body, "Morning thoughts.\n\n%% para:begin") || !strings.HasSuffix(body, "\n\nEvening thoughts.") {
		t.Errorf("outside text changed:\n%s", body)
	}
	if !strings.Contains(body, "- 18:30 update [[inbox-a]]") || strings.Count(body, "para:begin summary") != 1 {
		t.Errorf("body:\n%s", body)
	}
}

func TestJournalEmptyDay(t *testing.T) {
	m := newMemStore()
	daily(t, m, "2026-11-01")
	if b := m.docs["2026-11-01"].Body; !strings.Contains(b, "events: 0") || !strings.Contains(b, "No activity.") {
		t.Errorf("body:\n%s", b)
	}
}

func TestJournalBadInput(t *testing.T) {
	m := newMemStore()
	for _, date := range []string{"yesterday", "2026-13-01", "2026-11-1x", "11/01/2026"} {
		if _, err := JournalDaily.Run(context.Background(), Deps{Store: m}, JournalInput{Date: date}); err == nil {
			t.Errorf("%q: want error", date)
		}
	}
	// A non-journal document with the journal's id is not overwritten.
	m.addNote("2026-11-01")
	if _, err := JournalDaily.Run(context.Background(), Deps{Store: m}, JournalInput{Date: "2026-11-01"}); err == nil {
		t.Error("overwrote a non-journal document")
	}
}

func TestJournalRollup(t *testing.T) {
	m := newMemStore()
	m.addNote("inbox-a")
	for _, e := range []struct{ ts, op string }{
		{"2026-11-02T09:00:00Z", "create"}, {"2026-11-02T10:00:00Z", "update"},
		{"2026-11-04T09:00:00Z", "create"},
		{"2026-11-10T09:00:00Z", "create"}, // next week
	} {
		m.emit(e.ts, e.op, "inbox-a")
	}
	daily(t, m, "2026-11-02")
	daily(t, m, "2026-11-04") // 2026-11-03 has no daily

	w, err := JournalWeekly.Run(context.Background(), Deps{Store: m}, JournalInput{Date: "2026-11-05"})
	if err != nil {
		t.Fatal(err)
	}
	// The dailies' own writes are not counted.
	if w.ID != "2026-w45" || w.Events != 3 || w.Action != JournalCreated {
		t.Fatalf("weekly = %+v", w)
	}
	body := m.docs["2026-w45"].Body
	for _, want := range []string{"events: 3", "- [[2026-11-02]]: 2 events", "- [[2026-11-04]]: 1 events"} {
		if !strings.Contains(body, want) {
			t.Errorf("weekly lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "2026-11-03") || strings.Contains(body, "2026-11-10") {
		t.Errorf("weekly lists a missing or foreign day:\n%s", body)
	}
	if m.docs["2026-w45"].Meta.(*model.Journal).Scope != model.JournalWeek {
		t.Error("scope")
	}

	mo, err := JournalMonthly.Run(context.Background(), Deps{Store: m}, JournalInput{Date: "2026-11-20"})
	if err != nil {
		t.Fatal(err)
	}
	if mo.ID != "2026-11" || mo.Events != 4 {
		t.Fatalf("monthly = %+v", mo)
	}
	if b := m.docs["2026-11"].Body; !strings.Contains(b, "- [[2026-w45]]: 3 events") || strings.Contains(b, "2026-w46") {
		t.Errorf("monthly:\n%s", b)
	}
	// Weekly rerun is a no-op; weekly picks up a daily written later.
	if out, _ := JournalWeekly.Run(context.Background(), Deps{Store: m}, JournalInput{Date: "2026-11-05"}); out.Action != JournalUnchanged {
		t.Errorf("weekly rerun: %+v", out)
	}
	daily(t, m, "2026-11-03")
	out, _ := JournalWeekly.Run(context.Background(), Deps{Store: m}, JournalInput{Date: "2026-11-05"})
	if out.Action != JournalUpdated || !strings.Contains(m.docs["2026-w45"].Body, "- [[2026-11-03]]: 0 events") {
		t.Errorf("weekly after new daily: %+v\n%s", out, m.docs["2026-w45"].Body)
	}
}

// A journal day is the owner's local day: an event at 23:30 local, which is
// the next day in UTC, belongs to the local day.
func TestJournalDayIsLocal(t *testing.T) {
	loc := time.FixedZone("test", -8*3600)
	m := &zoneStore{memStore: newMemStore(), loc: loc}
	m.addNote("n1")
	m.emit("2026-11-02T07:30:00Z", "create", "n1") // 23:30 on Nov 1 in -08:00
	out, err := journal(context.Background(), Deps{Store: m}, JournalInput{Date: "2026-11-01"}, model.JournalDay)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "2026-11-01" || out.Events != 1 {
		t.Fatalf("journal %s with %d events, want 2026-11-01 with 1", out.ID, out.Events)
	}
	if body := m.docs["2026-11-01"].Body; !strings.Contains(body, "23:30") {
		t.Errorf("activity is not in local time:\n%s", body)
	}
}

type zoneStore struct {
	*memStore
	loc *time.Location
}

func (z *zoneStore) Now() time.Time { return time.Date(2026, 11, 1, 23, 45, 0, 0, z.loc) }
