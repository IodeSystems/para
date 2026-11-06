package ops

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/upgrade"
)

// JournalInput is the input of journal_daily, journal_weekly, journal_monthly.
type JournalInput struct {
	Date string `json:"date,omitempty"` // YYYY-MM-DD inside the period; empty means today (local time)
}

// JournalOutput is the output of the journal ops.
type JournalOutput struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Action string `json:"action"` // created, updated, or unchanged
	Events int    `json:"events"` // events in the period, journal files excluded
}

// Journal actions.
const (
	JournalCreated   = "created"
	JournalUpdated   = "updated"
	JournalUnchanged = "unchanged"
)

// JournalDaily writes the journal of one day.
var JournalDaily = defineSafe("journal_daily", "Generate the daily journal from events", func(ctx context.Context, d Deps, in JournalInput) (JournalOutput, error) {
	return journal(ctx, d, in, model.JournalDay)
})

// JournalWeekly writes the journal of one ISO week (Monday to Sunday) and rolls up its dailies.
var JournalWeekly = defineSafe("journal_weekly", "Generate the weekly journal and roll up the dailies", func(ctx context.Context, d Deps, in JournalInput) (JournalOutput, error) {
	return journal(ctx, d, in, model.JournalWeek)
})

// JournalMonthly writes the journal of one month and rolls up the weeklies that touch it.
var JournalMonthly = defineSafe("journal_monthly", "Generate the monthly journal and roll up the weeklies", func(ctx context.Context, d Deps, in JournalInput) (JournalOutput, error) {
	return journal(ctx, d, in, model.JournalMonth)
})

// period is one journal's span: [start, end) in the location of the day given: local time.
type period struct {
	scope      model.JournalScope
	id, title  string
	start, end time.Time
}

func newPeriod(scope model.JournalScope, day time.Time) period {
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	switch scope {
	case model.JournalWeek:
		start := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
		y, w := start.ISOWeek()
		return period{scope, fmt.Sprintf("%04d-w%02d", y, w), fmt.Sprintf("Week %04d-W%02d", y, w), start, start.AddDate(0, 0, 7)}
	case model.JournalMonth:
		start := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location())
		return period{scope, start.Format("2006-01"), "Month " + start.Format("2006-01"), start, start.AddDate(0, 1, 0)}
	default:
		return period{scope, day.Format(dateLayout), "Day " + day.Format(dateLayout), day, day.AddDate(0, 0, 1)}
	}
}

// children are the journals one level down that the period rolls up.
func (p period) children() []period {
	var out []period
	switch p.scope {
	case model.JournalWeek:
		for d := p.start; d.Before(p.end); d = d.AddDate(0, 0, 1) {
			out = append(out, newPeriod(model.JournalDay, d))
		}
	case model.JournalMonth:
		// Every week that has a day in the month.
		for d := p.start; d.Before(p.end); d = d.AddDate(0, 0, 1) {
			w := newPeriod(model.JournalWeek, d)
			if len(out) == 0 || out[len(out)-1].id != w.id {
				out = append(out, w)
			}
		}
	}
	return out
}

func journal(ctx context.Context, d Deps, in JournalInput, scope model.JournalScope) (JournalOutput, error) {
	day := d.Store.Now()
	loc := day.Location()
	if in.Date != "" {
		var err error
		if day, err = time.ParseInLocation(dateLayout, in.Date, loc); err != nil {
			return JournalOutput{}, fmt.Errorf("date %q: want YYYY-MM-DD", in.Date)
		}
	}
	p := newPeriod(scope, day)
	refs, err := d.Store.List(ctx, store.Filter{})
	if err != nil {
		return JournalOutput{}, err
	}
	exist := map[string]model.Ref{}
	for _, r := range refs {
		exist[r.ID] = r
	}
	evs, err := d.Store.Events(ctx, math.MaxInt32)
	if err != nil {
		return JournalOutput{}, err
	}
	skip, err := journalPlumbing(ctx, d, exist)
	if err != nil {
		return JournalOutput{}, err
	}
	var in2 []model.Event
	for _, e := range evs {
		// A journal's own writes are left out, or a rerun would change what it writes.
		if skip[e.Doc] || e.Op == upgrade.OpSession {
			continue
		}
		if t := e.TS.In(loc); !t.Before(p.start) && t.Before(p.end) {
			in2 = append(in2, e)
		}
	}
	sections := []section{{"summary", summaryLines(in2)}}
	if scope == model.JournalDay {
		sections = append(sections, section{"activity", activityLines(in2, exist, loc)})
		if lines := agentDigest(ctx, d, p.start, p.end); len(lines) > 0 {
			sections = append(sections, section{"agent-activity", lines})
		}
		lines, err := inboxDigest(ctx, d, p.start, p.end)
		if err != nil {
			return JournalOutput{}, err
		}
		if len(lines) == 0 {
			if cur, err := d.Store.Get(ctx, p.id); err == nil && strings.Contains(cur.Body, "%% para:begin "+inboxDigestSection+" %%") {
				lines = []string{inboxDigestHeading, "Nothing came in that did not need you."}
			}
		}
		if len(lines) > 0 {
			sections = append(sections, section{inboxDigestSection, lines})
		}
	} else {
		lines, err := rollupLines(ctx, d, p, exist)
		if err != nil {
			return JournalOutput{}, err
		}
		sections = append(sections, section{"rollup", lines})
	}
	out := JournalOutput{ID: p.id, Path: "journal/" + p.id + ".md", Events: len(in2)}
	cur, err := d.Store.Get(ctx, p.id)
	switch {
	case errors.Is(err, model.ErrNotFound):
		_, err := d.Store.Create(ctx, model.Document{
			ID: p.id, Folder: model.FolderJournal, Title: p.title,
			Body: applySections("", sections),
			Meta: &model.Journal{Common: model.Common{Type: model.TypeJournal}, Date: model.Date(p.start.Format(dateLayout)), Scope: scope},
		})
		if err != nil {
			return out, err
		}
		out.Action = JournalCreated
		return out, nil
	case err != nil:
		return out, err
	}
	if cur.Meta.Header().Type != model.TypeJournal {
		return out, fmt.Errorf("%s exists and is not a journal", p.id)
	}
	body := applySections(cur.Body, sections)
	if body == strings.Trim(cur.Body, "\n") {
		out.Action = JournalUnchanged
		return out, nil
	}
	cur.Body = body
	if _, err := d.Store.Update(ctx, cur); err != nil {
		return out, err
	}
	out.Action = JournalUpdated
	return out, nil
}

// journalPlumbing returns the ids of documents whose events stay out of a
// journal: the journals themselves, and the requests for their narratives
// (a request that links a journal). Counting those would make each narrative
// request count as activity, and so start the next one.
func journalPlumbing(ctx context.Context, d Deps, exist map[string]model.Ref) (map[string]bool, error) {
	skip := map[string]bool{}
	hasReq := false
	for id, r := range exist {
		switch r.Type {
		case model.TypeJournal:
			skip[id] = true
		case model.TypeRequest:
			hasReq = true
		}
	}
	if !hasReq {
		return skip, nil
	}
	reqs, err := loadRequests(ctx, d.Store)
	if err != nil {
		return nil, err
	}
	for _, r := range reqs {
		for _, l := range r.Meta.(*model.Request).Links {
			if skip[l.Target()] {
				skip[r.ID] = true
			}
		}
	}
	return skip, nil
}

// section is one generated block of a journal.
type section struct {
	name  string
	lines []string
}

func (s section) render() string {
	var b strings.Builder
	b.WriteString("%% para:begin " + s.name + " %%\n")
	for _, l := range s.lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("%% para:end " + s.name + " %%")
	return b.String()
}

// applySections puts each section into body. A section whose markers exist
// has the text between them replaced; a missing one is appended after a blank
// line. Text outside the markers is never changed. The result has no leading
// or trailing newline.
func applySections(body string, secs []section) string {
	body = strings.Trim(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for _, s := range secs {
		begin := "%% para:begin " + s.name + " %%"
		end := "%% para:end " + s.name + " %%"
		i := strings.Index(body, begin)
		if i >= 0 {
			if j := strings.Index(body[i:], end); j >= 0 {
				body = body[:i] + s.render() + body[i+j+len(end):]
				continue
			}
		}
		if body != "" {
			body += "\n\n"
		}
		body += s.render()
	}
	return body
}

// summaryLines: "events: N" then one line per op, sorted. A parent journal
// reads these lines back.
func summaryLines(evs []model.Event) []string {
	counts := map[string]int{}
	for _, e := range evs {
		counts[e.Op]++
	}
	ops := make([]string, 0, len(counts))
	for op := range counts {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	lines := []string{fmt.Sprintf("events: %d", len(evs))}
	for _, op := range ops {
		lines = append(lines, fmt.Sprintf("- %s: %d", op, counts[op]))
	}
	return lines
}

// activityLines lists events in order. A document that exists is a wikilink;
// a gone one is a code span, so the journal never has a dead link.
func activityLines(evs []model.Event, exist map[string]model.Ref, loc *time.Location) []string {
	if len(evs) == 0 {
		return []string{"No activity."}
	}
	lines := make([]string, 0, len(evs))
	for _, e := range evs {
		doc := "`" + e.Doc + "`"
		if _, ok := exist[e.Doc]; ok {
			doc = "[[" + e.Doc + "]]"
		}
		l := fmt.Sprintf("- %s %s %s", e.TS.In(loc).Format("15:04"), e.Op, doc)
		if e.Op != "external_edit" && (e.From != "" || e.To != "") {
			l += fmt.Sprintf(" %s -> %s", orDash(e.From), orDash(e.To))
		}
		lines = append(lines, l)
	}
	return lines
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

var eventsLineRe = regexp.MustCompile(`(?m)^events: (\d+)$`)

// rollupLines lists the child journals that exist, each with the event count
// its own summary holds. A week at the edge of a month counts the whole
// week, so the month's summary (computed from events) can be smaller than
// the sum here.
func rollupLines(ctx context.Context, d Deps, p period, exist map[string]model.Ref) ([]string, error) {
	var lines []string
	for _, c := range p.children() {
		if _, ok := exist[c.id]; !ok {
			continue
		}
		doc, err := d.Store.Get(ctx, c.id)
		if err != nil {
			return nil, err
		}
		n := 0
		if m := eventsLineRe.FindStringSubmatch(sectionText(doc.Body, "summary")); m != nil {
			n, _ = strconv.Atoi(m[1])
		}
		lines = append(lines, fmt.Sprintf("- [[%s]]: %d events", c.id, n))
	}
	if len(lines) == 0 {
		return []string{"No journals below."}, nil
	}
	return lines, nil
}

// sectionText is the text between the markers of one section, or "".
func sectionText(body, name string) string {
	begin := "%% para:begin " + name + " %%"
	i := strings.Index(body, begin)
	if i < 0 {
		return ""
	}
	rest := body[i+len(begin):]
	if j := strings.Index(rest, "%% para:end "+name+" %%"); j >= 0 {
		return rest[:j]
	}
	return ""
}

// agentDigest is the day's agent work: a line per agent, with self-reported acts
// counted apart.
func agentDigest(ctx context.Context, d Deps, start, end time.Time) []string {
	rows, err := activityRows(ctx, d, "", start, end)
	if err != nil || len(rows) == 0 {
		return nil
	}
	type tally struct{ writes, self int }
	by := map[string]*tally{}
	var names []string
	for _, r := range rows {
		t := by[r.Actor]
		if t == nil {
			t = &tally{}
			by[r.Actor] = t
			names = append(names, r.Actor)
		}
		t.writes++
		if r.SelfReported {
			t.self++
		}
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, n := range names {
		line := fmt.Sprintf("- %s: %d writes", n, by[n].writes)
		if by[n].self > 0 {
			line += fmt.Sprintf(", %d self-reported", by[n].self)
		}
		out = append(out, line)
	}
	return out
}
