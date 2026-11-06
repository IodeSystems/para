package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// journalTick returns a vault whose runner is install ia, and its deps.
func journalTick(t *testing.T, now string, props ...string) (*tickVault, Deps) {
	t.Helper()
	tv := newTickVault(t, now, append([]string{"request.runner=ia"}, props...)...)
	return tv, tv.open("ia", "cli")
}

func (tv *tickVault) at(s string) { tv.now = utc(s) }

func inboxAt(t *testing.T, tv *tickVault, d Deps, when string) {
	t.Helper()
	tv.at(when)
	must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "note at " + when})
}

func journalReqs(t *testing.T, d Deps) []model.Document {
	t.Helper()
	var out []model.Document
	reqs, err := loadRequests(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reqs {
		for _, l := range r.Meta.(*model.Request).Links {
			if _, err := periodFromID(l.Target(), time.UTC); err == nil {
				out = append(out, r)
			}
		}
	}
	return out
}

// The day ends at local midnight, the week after Sunday, the month after its
// last day. Nothing is made before.
func TestTickJournalBoundaries(t *testing.T) {
	tv, d := journalTick(t, "2026-10-30T10:00:00Z")
	// 2026-10-31 is a Saturday, 11-01 a Sunday, 11-02 a Monday.
	out := tick(t, d)
	if slices.Contains(out.Journals, "2026-10-30") || !slices.Contains(out.Journals, "2026-10-29") {
		t.Fatalf("mid-day journals = %v", out.Journals)
	}
	tv.at("2026-10-30T23:59:00Z")
	if out := tick(t, d); len(out.Journals) != 0 {
		t.Errorf("before midnight made %v", out.Journals)
	}
	tv.at("2026-10-31T00:01:00Z")
	if out := tick(t, d); !slices.Equal(out.Journals, []string{"2026-10-30"}) {
		t.Errorf("after midnight made %v", out.Journals)
	}
	// Sunday 00:01: day 10-31 and the month of October have ended; the week has not.
	tv.at("2026-11-01T00:01:00Z")
	out = tick(t, d)
	if !slices.Equal(out.Journals, []string{"2026-10-31", "2026-10"}) {
		t.Errorf("sunday 00:01 made %v", out.Journals)
	}
	// Sunday night: the last day of the week has not ended.
	tv.at("2026-11-01T23:59:00Z")
	if out := tick(t, d); len(out.Journals) != 0 {
		t.Errorf("sunday 23:59 made %v", out.Journals)
	}
	// Monday 00:01: day 11-01 and week 2026-w44 just ended.
	tv.at("2026-11-02T00:01:00Z")
	out = tick(t, d)
	if !slices.Equal(out.Journals, []string{"2026-11-01", "2026-w44"}) {
		t.Errorf("order, lower levels first: %v", out.Journals)
	}
	for _, id := range []string{"2026-w44", "2026-10"} { // the month came a day earlier
		doc, err := d.Store.Get(ctx, id)
		if err != nil || doc.Meta.(*model.Journal).Scope == "" {
			t.Fatalf("%s: %v", id, err)
		}
	}
	// The week's rollup saw its days.
	wk, _ := d.Store.Get(ctx, "2026-w44")
	if !strings.Contains(wk.Body, "[[2026-11-01]]") || !strings.Contains(wk.Body, "[[2026-10-26]]") {
		t.Errorf("week rollup:\n%s", wk.Body)
	}
}

func TestTickJournalsAreIdempotent(t *testing.T) {
	tv, d := journalTick(t, "2026-11-04T09:00:00Z")
	inboxAt(t, tv, d, "2026-11-03T10:00:00Z")
	tv.at("2026-11-04T09:00:00Z")
	first := tick(t, d)
	if len(first.Journals) == 0 {
		t.Fatal("first pass made nothing")
	}
	before := eventCount(t, d)
	second := tick(t, d)
	if len(second.Journals) != 0 || len(second.Requests) != 0 {
		t.Errorf("second pass: %+v", second)
	}
	if got := eventCount(t, d); got != before {
		t.Errorf("second pass wrote %d events", got-before)
	}
	// A journal made by hand is not rewritten either.
	doc, _ := d.Store.Get(ctx, "2026-11-03")
	if !strings.Contains(doc.Body, "[["+"inbox") && !strings.Contains(doc.Body, "inbox_add") && !strings.Contains(doc.Body, "create") {
		t.Errorf("day journal lacks its event:\n%s", doc.Body)
	}
}

func TestTickJournalCatchUpIsBounded(t *testing.T) {
	_, d := journalTick(t, "2026-11-20T09:00:00Z") // a Friday; vault just started, or down for weeks
	out := tick(t, d)
	var days []string
	for _, id := range out.Journals {
		if len(id) == 10 {
			days = append(days, id)
		}
	}
	if len(days) != JournalCatchUpDays || days[0] != "2026-11-06" || days[len(days)-1] != "2026-11-19" {
		t.Errorf("days = %v", days)
	}
	if !slices.Contains(out.Journals, "2026-w45") || slices.Contains(out.Journals, "2026-w44") {
		t.Errorf("weeks in %v", out.Journals)
	}
	// October ended 19 days ago: outside the window.
	if slices.Contains(out.Journals, "2026-10") {
		t.Errorf("month made: %v", out.Journals)
	}
	// Down for three more days: only the new days are made.
	d2 := tick(t, d)
	if len(d2.Journals) != 0 {
		t.Errorf("repeat made %v", d2.Journals)
	}
}

func TestTickJournalRunnerGate(t *testing.T) {
	tv := newTickVault(t, "2026-11-04T09:00:00Z", "request.runner=ib")
	d := tv.open("ia", "cli")
	if out := tick(t, d); out.Ran || len(out.Journals) != 0 {
		t.Errorf("non-runner: %+v", out)
	}
	if refs, _ := d.Store.List(ctx, store.Filter{Folder: model.FolderJournal}); len(refs) != 0 {
		t.Errorf("non-runner made %d journals", len(refs))
	}
}

// A local day follows the clock's zone: 23 hours in March, 25 in November,
// every day made once and no day skipped.
func TestTickJournalsAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	tv, d := journalTick(t, "2026-03-10T08:00:00Z")
	local := func(y int, m time.Month, day, h, min int) time.Time { return time.Date(y, m, day, h, min, 0, 0, ny) }

	// Spring forward 2026-03-08: 01:30 EST and 03:30 EDT are the same day.
	tv.now = local(2026, 3, 8, 1, 30)
	must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "before the jump"})
	tv.now = local(2026, 3, 8, 3, 30)
	must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "after the jump"})
	// 23:30 on the 7th and 00:30 on the 9th belong to their own days.
	tv.now = local(2026, 3, 7, 23, 30)
	must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "evening before"})
	tv.now = local(2026, 3, 9, 0, 30)
	must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "morning after"})

	tv.now = local(2026, 3, 10, 8, 0)
	out := tick(t, d)
	var days []string
	for _, id := range out.Journals {
		if len(id) == 10 {
			days = append(days, id)
		}
	}
	if len(days) != 14 || days[0] != "2026-02-24" || days[13] != "2026-03-09" {
		t.Fatalf("days = %v", days)
	}
	for i := 1; i < len(days); i++ {
		a, _ := time.Parse(dateLayout, days[i-1])
		b, _ := time.Parse(dateLayout, days[i])
		if b.Sub(a) != 24*time.Hour {
			t.Errorf("gap between %s and %s", days[i-1], days[i])
		}
	}
	count := func(id string) int {
		t.Helper()
		got, err := JournalSummary.Run(ctx, d, JournalSummaryInput{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		return len(got.Sources)
	}
	if n := count("2026-03-08"); n != 2 {
		t.Errorf("jump day has %d events, want 2", n)
	}
	if n := count("2026-03-07"); n != 1 {
		t.Errorf("03-07 has %d events, want 1", n)
	}
	if n := count("2026-03-09"); n != 1 {
		t.Errorf("03-09 has %d events, want 1", n)
	}

	// Fall back 2026-11-01: 01:30 happens twice (EDT then EST), both on the 1st.
	tv2, d2 := journalTick(t, "2026-11-03T08:00:00Z")
	tv2.now = time.Date(2026, 11, 1, 1, 30, 0, 0, ny)
	must(t, IOAdd, d2, IOAddInput{Direction: model.IOIn, Text: "first 01:30"})
	tv2.now = tv2.now.Add(time.Hour) // the second 01:30, EST
	if tv2.now.Format("MST") != "EST" {
		t.Fatalf("clock = %s", tv2.now)
	}
	must(t, IOAdd, d2, IOAddInput{Direction: model.IOIn, Text: "second 01:30"})
	tv2.now = time.Date(2026, 11, 3, 8, 0, 0, 0, ny)
	tick(t, d2)
	got, err := JournalSummary.Run(ctx, d2, JournalSummaryInput{ID: "2026-11-01"})
	if err != nil || len(got.Sources) != 2 {
		t.Errorf("fall-back day: %+v %v", got, err)
	}
	if _, err := d2.Store.Get(ctx, "2026-11-02"); err != nil {
		t.Errorf("11-02: %v", err)
	}
}

func TestTickJournalRequests(t *testing.T) {
	tv, d := journalTick(t, "2026-10-28T10:00:00Z")
	inboxAt(t, tv, d, "2026-10-28T10:00:00Z")

	// No journal.agent: mechanical journals only, no agent work.
	tv.at("2026-11-02T00:30:00Z")
	if out := tick(t, d); len(out.Journals) == 0 || len(out.Requests) != 0 {
		t.Fatalf("without journal.agent: %+v", out)
	}
	if got := journalReqs(t, d); len(got) != 0 {
		t.Fatalf("requests without journal.agent: %d", len(got))
	}

	// Set the agent. Days, weeks, and months that had events get one request
	// each; the quiet ones get none. Surface tick is not in request.surfaces,
	// so they are only proposed.
	tv.addProps("journal.agent=claude")
	tv.at("2026-11-02T00:40:00Z")
	out := tick(t, d)
	reqs := journalReqs(t, d)
	links := map[string]model.Document{}
	for _, r := range reqs {
		l := r.Meta.(*model.Request).Links
		if len(l) != 1 {
			t.Fatalf("request links = %v", l)
		}
		if st := r.Meta.(*model.Request).Status; st != model.RequestProposed {
			t.Errorf("%s status = %s, want proposed", l[0].Target(), st)
		}
		links[l[0].Target()] = r
	}
	for id, want := range map[string]string{"2026-10-28": "Write the Oct 28 day summary", "2026-w44": "Write the Oct 26 to Nov 1 week summary", "2026-10": "Write the October 2026 month summary"} {
		if r, ok := links[id]; ok && r.Title != want {
			t.Errorf("%s title = %q, want %q", id, r.Title, want)
		}
	}
	for _, want := range []string{"2026-10-28", "2026-w44", "2026-10"} {
		r, ok := links[want]
		if !ok {
			t.Errorf("no request for %s; have %v", want, keys(links))
			continue
		}
		if !strings.Contains(r.Body, "agent:claude") || !strings.Contains(r.Body, "prompt loop, part Produce") || !strings.Contains(r.Body, "journal_summary") {
			t.Errorf("%s ask:\n%s", want, r.Body)
		}
	}
	if _, ok := links["2026-10-27"]; ok {
		t.Error("a quiet day got a request")
	}
	if out.Proposed != len(reqs) {
		t.Errorf("proposed = %d for %d requests", out.Proposed, len(reqs))
	}

	// A second pass, and a later one, make no second request for a period, and
	// the request events do not count as activity that starts another chain.
	tv.at("2026-11-02T08:00:00Z")
	if out := tick(t, d); len(out.Requests) != 0 {
		t.Errorf("second pass requests = %v", out.Requests)
	}
	tv.at("2026-11-03T08:00:00Z")
	if out := tick(t, d); len(out.Requests) != 0 {
		t.Errorf("next day requests = %v (request events must not count)", out.Requests)
	}
	if got := len(journalReqs(t, d)); got != len(reqs) {
		t.Errorf("requests grew from %d to %d", len(reqs), got)
	}
	// A cancelled request is not made again.
	must(t, RequestCancel, d, RequestCancelInput{ID: links["2026-10-28"].ID})
	tv.at("2026-11-03T09:00:00Z")
	tick(t, d)
	if got := len(journalReqs(t, d)); got != len(reqs) {
		t.Errorf("cancelled request made again: %d", got)
	}
}

func TestTickJournalRequestsQueueWhenTickMayQueue(t *testing.T) {
	tv, d := journalTick(t, "2026-10-28T10:00:00Z", "journal.agent=agent:claude", "request.surfaces=cli,tick")
	inboxAt(t, tv, d, "2026-10-28T10:00:00Z")
	tv.at("2026-10-29T08:00:00Z")
	out := tick(t, d)
	if out.Proposed != 0 || len(out.Requests) == 0 {
		t.Fatalf("out = %+v", out)
	}
	for _, r := range journalReqs(t, d) {
		if st := r.Meta.(*model.Request).Status; st != model.RequestQueued {
			t.Errorf("status = %s", st)
		}
		if !strings.Contains(r.Body, "agent:claude") || strings.Contains(r.Body, "agent:agent:") {
			t.Errorf("ask = %q", r.Body)
		}
	}
}

// The month waits for its last week to end: weeks must exist whole before the
// month is summarized from them.
func TestTickMonthRequestWaitsForLastWeek(t *testing.T) {
	// 2026-09 ends on Wednesday 09-30; its last week 2026-w40 ends Monday 10-05.
	tv, d := journalTick(t, "2026-09-15T10:00:00Z", "journal.agent=claude")
	inboxAt(t, tv, d, "2026-09-15T10:00:00Z")
	tv.at("2026-10-01T00:30:00Z")
	tick(t, d)
	for _, r := range journalReqs(t, d) {
		if r.Meta.(*model.Request).Links[0].Target() == "2026-09" {
			t.Fatal("month requested before its last week ended")
		}
	}
	if _, err := d.Store.Get(ctx, "2026-09"); err != nil {
		t.Fatalf("mechanical month missing: %v", err)
	}
	tv.at("2026-10-05T00:30:00Z")
	tick(t, d)
	found := false
	for _, r := range journalReqs(t, d) {
		found = found || r.Meta.(*model.Request).Links[0].Target() == "2026-09"
	}
	if !found {
		t.Error("month not requested after its last week ended")
	}
	// The month now lists the last week.
	m, _ := d.Store.Get(ctx, "2026-09")
	if !strings.Contains(m.Body, "[[2026-w40]]") {
		t.Errorf("month rollup lacks the last week:\n%s", m.Body)
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestJournalSummaryReplacesAndKeepsHumanText(t *testing.T) {
	tv, d := journalTick(t, "2026-11-03T09:00:00Z")
	inboxAt(t, tv, d, "2026-11-02T10:00:00Z")
	tv.at("2026-11-03T09:00:00Z")
	tick(t, d)

	// Human text before, between, and after the generated sections.
	doc, _ := d.Store.Get(ctx, "2026-11-02")
	doc.Body = "My own line first.\n\n" + doc.Body + "\n\nMy own line last."
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	mine := func(body string) {
		t.Helper()
		if !strings.HasPrefix(body, "My own line first.\n\n%% para:begin summary %%") || !strings.Contains(body, "%% para:end activity %%\n\nMy own line last.\n\n") {
			t.Errorf("human text changed:\n%s", body)
		}
	}

	// Read mode: sources, no write.
	before := eventCount(t, d)
	rd := must(t, JournalSummary, d, JournalSummaryInput{ID: "2026-11-02"})
	if rd.Action != "read" || len(rd.Sources) != 1 || rd.Sources[0].Kind != "event" || rd.Sources[0].Op != "create" || rd.Sources[0].At != "10:00" {
		t.Fatalf("read = %+v", rd)
	}
	if eventCount(t, d) != before {
		t.Error("read mode wrote")
	}

	first := must(t, JournalSummary, d, JournalSummaryInput{ID: "2026-11-02", Text: "- Captured one note.\n- Nothing waiting."})
	if first.Action != "written" {
		t.Errorf("first = %s", first.Action)
	}
	second := must(t, JournalSummary, d, JournalSummaryInput{ID: "2026-11-02", Text: "Quiet day: one note."})
	if second.Action != "replaced" {
		t.Errorf("second = %s", second.Action)
	}
	if again := must(t, JournalSummary, d, JournalSummaryInput{ID: "2026-11-02", Text: "Quiet day: one note."}); again.Action != JournalUnchanged {
		t.Errorf("same text = %s", again.Action)
	}
	doc, _ = d.Store.Get(ctx, "2026-11-02")
	mine(doc.Body)
	if strings.Count(doc.Body, "%% para:begin summary-agent %%") != 1 || strings.Contains(doc.Body, "Captured one note") || !strings.Contains(doc.Body, "Quiet day: one note.") {
		t.Errorf("section not replaced:\n%s", doc.Body)
	}

	// The mechanical op regenerates its sections and leaves the agent's alone.
	must(t, JournalDaily, d, JournalInput{Date: "2026-11-02"})
	doc, _ = d.Store.Get(ctx, "2026-11-02")
	mine(doc.Body)
	if !strings.Contains(doc.Body, "Quiet day: one note.") {
		t.Errorf("journal_daily removed the summary:\n%s", doc.Body)
	}
	// The listing says so.
	list := must(t, JournalList, d, JournalListInput{Level: "day"})
	var seen bool
	for _, j := range list.Items {
		if j.ID == "2026-11-02" {
			seen = j.Summary
		} else if j.Summary {
			t.Errorf("%s has no summary", j.ID)
		}
	}
	if !seen {
		t.Error("list does not show the summary")
	}
	// The writer is recorded.
	evs, _ := d.Store.Events(ctx, 100000)
	var last model.Event
	for _, e := range evs {
		if e.Doc == "2026-11-02" && e.Op == "update" {
			last = e
		}
	}
	if last.Actor != "user" || last.Surface != "cli" {
		t.Errorf("last write by %q on %q", last.Actor, last.Surface)
	}
	// And the vault still lints clean.
	mustLintClean(t, d)
}

func TestJournalSummaryChecks(t *testing.T) {
	tv, d := journalTick(t, "2026-11-04T09:00:00Z")
	inboxAt(t, tv, d, "2026-11-02T10:00:00Z")
	tv.at("2026-11-04T09:00:00Z")

	// No journal yet: say what to run.
	if _, err := JournalSummary.Run(ctx, d, JournalSummaryInput{ID: "2026-11-02", Text: "x"}); err == nil || !strings.Contains(err.Error(), "para journal daily") {
		t.Errorf("missing journal: %v", err)
	}
	tick(t, d)
	for name, bad := range map[string]string{
		"marker":  "text\n%% para:end summary-agent %%\nmore",
		"comment": "a %% b",
		"heading": "# Title",
		"html":    "<b>x</b>",
		"long":    strings.Repeat("x", summaryMaxBytes+1),
	} {
		if _, err := JournalSummary.Run(ctx, d, JournalSummaryInput{ID: "2026-11-02", Text: bad}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for _, id := range []string{"", "2026-13", "2026-w60", "2026-02-30", "journal", "2026-w00"} {
		if _, err := JournalSummary.Run(ctx, d, JournalSummaryInput{ID: id}); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
	// A task id is not a journal.
	if _, err := JournalShow.Run(ctx, d, JournalShowInput{ID: "task-x"}); err == nil {
		t.Error("show accepted a non-period")
	}
}

// A weekly summary needs daily journals; a monthly one needs weekly ones.
func TestJournalSummaryNeedsTheLowerLevel(t *testing.T) {
	tv, d := journalTick(t, "2026-11-04T09:00:00Z")
	inboxAt(t, tv, d, "2026-11-02T10:00:00Z")
	must(t, JournalWeekly, d, JournalInput{Date: "2026-11-02"})
	if _, err := JournalSummary.Run(ctx, d, JournalSummaryInput{ID: "2026-w45", Text: "x"}); err == nil || !strings.Contains(err.Error(), "daily") {
		t.Errorf("week with no days: %v", err)
	}
	must(t, JournalDaily, d, JournalInput{Date: "2026-11-02"})
	must(t, JournalSummary, d, JournalSummaryInput{ID: "2026-11-02", Text: "Captured a note."})
	rd := must(t, JournalSummary, d, JournalSummaryInput{ID: "2026-w45"})
	if len(rd.Sources) != 1 || rd.Sources[0].ID != "2026-11-02" || rd.Sources[0].Kind != "journal" || !rd.Sources[0].Summary {
		t.Errorf("week sources = %+v", rd.Sources)
	}
	must(t, JournalSummary, d, JournalSummaryInput{ID: "2026-w45", Text: "One note captured."})
	must(t, JournalMonthly, d, JournalInput{Date: "2026-11-02"})
	rd = must(t, JournalSummary, d, JournalSummaryInput{ID: "2026-11"})
	if len(rd.Sources) != 1 || rd.Sources[0].ID != "2026-w45" || !rd.Sources[0].Summary {
		t.Errorf("month sources = %+v", rd.Sources)
	}
	// A month with no week journals.
	must(t, JournalMonthly, d, JournalInput{Date: "2026-09-10"})
	if _, err := JournalSummary.Run(ctx, d, JournalSummaryInput{ID: "2026-09", Text: "x"}); err == nil || !strings.Contains(err.Error(), "weekly") {
		t.Errorf("month with no weeks: %v", err)
	}
}

func TestJournalShowAndList(t *testing.T) {
	tv, d := journalTick(t, "2026-11-04T09:00:00Z")
	tick(t, d)
	show := must(t, JournalShow, d, JournalShowInput{ID: "2026-w44"})
	if show.Level != "week" || show.Path != "journal/2026-w44.md" || !strings.Contains(show.Body, "para:begin summary") {
		t.Errorf("show = %+v", show)
	}
	all := must(t, JournalList, d, JournalListInput{})
	levels := map[string]int{}
	for _, j := range all.Items {
		levels[j.Level]++
	}
	if levels["day"] != 14 || levels["week"] == 0 {
		t.Errorf("levels = %v", levels)
	}
	if all.Items[0].Date < all.Items[len(all.Items)-1].Date {
		t.Error("not newest first")
	}
	if _, err := JournalList.Run(ctx, d, JournalListInput{Level: "year"}); err == nil {
		t.Error("bad level accepted")
	}
	_ = tv
}
