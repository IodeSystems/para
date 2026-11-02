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

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Level-of-detail journals. The mechanical journals (journal_daily, _weekly,
// _monthly) hold counts and links. An agent adds prose to each in one marked
// section, summaryAgentSection, and nowhere else: a day from its events, a
// week from its days, a month from its weeks. Text outside the markers is
// never touched, and a rerun of the mechanical op leaves the section alone.

const (
	// SummaryAgentSection is the marker name of the section an agent writes.
	SummaryAgentSection = "summary-agent"

	// summaryMaxBytes bounds one summary. A summary that needs more is not one.
	summaryMaxBytes = 6000
	// sourceMax bounds the event sources a day returns.
	sourceMax = 300
)

var (
	dayIDRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	weekIDRe  = regexp.MustCompile(`^(\d{4})-w(\d{2})$`)
	monthIDRe = regexp.MustCompile(`^\d{4}-\d{2}$`)
	// A raw HTML tag is a para-md lint error.
	htmlTagRe = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
)

// periodFromID parses a journal id: 2026-11-01 (day), 2026-w44 (ISO week),
// 2026-11 (month). Periods are in loc.
func periodFromID(id string, loc *time.Location) (period, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	var day time.Time
	var err error
	switch {
	case dayIDRe.MatchString(id):
		day, err = time.ParseInLocation(dateLayout, id, loc)
	case monthIDRe.MatchString(id):
		day, err = time.ParseInLocation("2006-01", id, loc)
	case weekIDRe.MatchString(id):
		m := weekIDRe.FindStringSubmatch(id)
		y, _ := strconv.Atoi(m[1]) // the regexp admits digits only
		w, _ := strconv.Atoi(m[2])
		// Week 1 is the week with January 4th.
		jan4 := time.Date(y, time.January, 4, 0, 0, 0, 0, loc)
		week1 := jan4.AddDate(0, 0, -((int(jan4.Weekday()) + 6) % 7))
		day = week1.AddDate(0, 0, (w-1)*7)
	default:
		return period{}, fmt.Errorf("id %q: want a day 2026-11-01, an ISO week 2026-w44, or a month 2026-11", id)
	}
	if err != nil {
		return period{}, fmt.Errorf("id %q: %w", id, err)
	}
	scope := model.JournalDay
	switch {
	case weekIDRe.MatchString(id):
		scope = model.JournalWeek
	case monthIDRe.MatchString(id):
		scope = model.JournalMonth
	}
	p := newPeriod(scope, day)
	if p.id != id {
		return period{}, fmt.Errorf("id %q: no such period", id)
	}
	return p, nil
}

// JournalSource is one thing the agent reads to write a summary.
type JournalSource struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // journal (week and month) or event (day)
	// Op and At are set for an event; At is the local time of day.
	Op    string `json:"op,omitempty"`
	At    string `json:"at,omitempty"`
	Title string `json:"title,omitempty"`
	// Summary is set for a journal: it already holds a summary-agent section.
	Summary bool `json:"summary,omitempty"`
}

// JournalSummaryInput is the input of journal_summary.
type JournalSummaryInput struct {
	ID   string `json:"id"`             // 2026-11-01, 2026-w44, or 2026-11
	Text string `json:"text,omitempty"` // the prose; empty reads the sources and writes nothing
}

// JournalSummaryOutput is the output of journal_summary.
type JournalSummaryOutput struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Level  string `json:"level"`
	Action string `json:"action"` // read, written, replaced, or unchanged
	// Sources is what to read: for a day, the events (doc ids, ops, titles);
	// for a week or month, the journals one level down that exist.
	Sources []JournalSource `json:"sources"`
	// Omitted counts day events left out of Sources.
	Omitted int `json:"omitted,omitempty"`
}

// JournalSummary writes the summary-agent section of one journal.
var JournalSummary = define("journal_summary", "Write the agent summary of a day, week, or month journal into its marked section, or read what to summarize when text is empty", func(ctx context.Context, d Deps, in JournalSummaryInput) (JournalSummaryOutput, error) {
	loc := d.Store.Now().Location()
	p, err := periodFromID(in.ID, loc)
	if err != nil {
		return JournalSummaryOutput{}, err
	}
	out := JournalSummaryOutput{ID: p.id, Path: "journal/" + p.id + ".md", Level: string(p.scope), Action: "read", Sources: []JournalSource{}}
	doc, err := d.Store.Get(ctx, p.id)
	if errors.Is(err, model.ErrNotFound) {
		return out, fmt.Errorf("no %s journal %s: run para journal %s %s first", p.scope, p.id, levelCommand(p.scope), p.start.Format(dateLayout))
	} else if err != nil {
		return out, err
	}
	if j, ok := doc.Meta.(*model.Journal); !ok || j.Scope != p.scope {
		return out, fmt.Errorf("%s is not a %s journal", p.id, p.scope)
	}
	refs, err := d.Store.List(ctx, store.Filter{})
	if err != nil {
		return out, err
	}
	exist := map[string]model.Ref{}
	for _, r := range refs {
		exist[r.ID] = r
	}
	if p.scope == model.JournalDay {
		if out.Sources, out.Omitted, err = daySources(ctx, d, p, exist, loc); err != nil {
			return out, err
		}
	} else {
		for _, c := range p.children() {
			r, ok := exist[c.id]
			if !ok || r.Type != model.TypeJournal {
				continue
			}
			child, err := d.Store.Get(ctx, c.id)
			if err != nil {
				return out, err
			}
			out.Sources = append(out.Sources, JournalSource{ID: c.id, Kind: "journal", Title: child.Title,
				Summary: strings.TrimSpace(sectionText(child.Body, SummaryAgentSection)) != ""})
		}
		if len(out.Sources) == 0 {
			lower := "daily"
			if p.scope == model.JournalMonth {
				lower = "weekly"
			}
			return out, fmt.Errorf("no %s journals under %s: run para journal %s first", lower, p.id, lower)
		}
	}
	text, err := cleanSummary(in.Text)
	if err != nil {
		return out, err
	}
	if text == "" {
		return out, nil
	}
	had := strings.Contains(doc.Body, "%% para:begin "+SummaryAgentSection+" %%")
	body := applySections(doc.Body, []section{{SummaryAgentSection, strings.Split(text, "\n")}})
	if body == strings.Trim(strings.ReplaceAll(doc.Body, "\r\n", "\n"), "\n") {
		out.Action = JournalUnchanged
		return out, nil
	}
	doc.Body = body
	if _, err := d.Store.Update(ctx, doc); err != nil {
		return out, err
	}
	out.Action = "written"
	if had {
		out.Action = "replaced"
	}
	return out, nil
})

func levelCommand(s model.JournalScope) string {
	switch s {
	case model.JournalWeek:
		return "weekly"
	case model.JournalMonth:
		return "monthly"
	}
	return "daily"
}

// cleanSummary checks the prose. It must not carry a marker (that would break
// the section or open another), a heading, or raw HTML: para lint rejects
// each in a journal.
func cleanSummary(text string) (string, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return "", nil
	}
	if len(text) > summaryMaxBytes {
		return "", fmt.Errorf("text is %d bytes; at most %d. Shorter is better", len(text), summaryMaxBytes)
	}
	if strings.Contains(text, "%%") {
		return "", errors.New("text contains %%: that is the marker syntax; leave it out")
	}
	if htmlTagRe.MatchString(text) {
		return "", errors.New("text contains an HTML tag; para-md has no raw HTML")
	}
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			return "", errors.New("text contains a heading line; the journal has its own title. Use a list or plain lines")
		}
	}
	return text, nil
}

// daySources lists the events of the day, journal files left out, as the
// document they touched, with its title when it still exists.
func daySources(ctx context.Context, d Deps, p period, exist map[string]model.Ref, loc *time.Location) ([]JournalSource, int, error) {
	evs, err := d.Store.Events(ctx, math.MaxInt32)
	if err != nil {
		return nil, 0, err
	}
	skip, err := journalPlumbing(ctx, d, exist)
	if err != nil {
		return nil, 0, err
	}
	out := []JournalSource{}
	omitted := 0
	for _, e := range evs {
		r := exist[e.Doc]
		if skip[e.Doc] {
			continue
		}
		t := e.TS.In(loc)
		if t.Before(p.start) || !t.Before(p.end) {
			continue
		}
		if len(out) >= sourceMax {
			omitted++
			continue
		}
		out = append(out, JournalSource{ID: e.Doc, Kind: "event", Op: e.Op, At: t.Format("15:04"), Title: r.Title})
	}
	return out, omitted, nil
}

// JournalShowInput is the input of journal_show.
type JournalShowInput struct {
	ID string `json:"id" query:"id" required:"true"` // 2026-11-01, 2026-w44, or 2026-11
}

// JournalShowOutput is the output of journal_show.
type JournalShowOutput struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Level   string `json:"level"`
	Title   string `json:"title"`
	Summary bool   `json:"summary"` // an agent summary is present
	Body    string `json:"body"`
}

// JournalShow shows one journal by its period id.
var JournalShow = define("journal_show", "Show a journal by period: 2026-11-01, 2026-w44, or 2026-11", func(ctx context.Context, d Deps, in JournalShowInput) (JournalShowOutput, error) {
	p, err := periodFromID(in.ID, d.Store.Now().Location())
	if err != nil {
		return JournalShowOutput{}, err
	}
	doc, err := d.Store.Get(ctx, p.id)
	if errors.Is(err, model.ErrNotFound) {
		return JournalShowOutput{}, fmt.Errorf("no journal %s: run para journal %s %s", p.id, levelCommand(p.scope), p.start.Format(dateLayout))
	} else if err != nil {
		return JournalShowOutput{}, err
	}
	if _, ok := doc.Meta.(*model.Journal); !ok {
		return JournalShowOutput{}, fmt.Errorf("%s is not a journal", p.id)
	}
	return JournalShowOutput{ID: doc.ID, Path: "journal/" + doc.ID + ".md", Level: string(p.scope), Title: doc.Title,
		Summary: strings.TrimSpace(sectionText(doc.Body, SummaryAgentSection)) != "", Body: doc.Body}, nil
})

// JournalListInput is the input of journal_list.
type JournalListInput struct {
	Level string `json:"level,omitempty" query:"level"` // day, week, or month; empty is all
	Limit int    `json:"limit,omitempty" query:"limit"` // 0 means all
}

// JournalView is one journal in a list.
type JournalView struct {
	ID      string `json:"id"`
	Level   string `json:"level"`
	Date    string `json:"date"`
	Title   string `json:"title"`
	Summary bool   `json:"summary"`
}

// JournalListOutput is the output of journal_list.
type JournalListOutput struct {
	Items []JournalView `json:"items"`
}

// JournalList lists journals, newest first, with their level.
var JournalList = define("journal_list", "List journals by level (day, week, month), newest first, and whether an agent summary exists", func(ctx context.Context, d Deps, in JournalListInput) (JournalListOutput, error) {
	level := model.JournalScope(in.Level)
	if level != "" && level != model.JournalDay && level != model.JournalWeek && level != model.JournalMonth {
		return JournalListOutput{}, fmt.Errorf("level %q: want day, week, or month", in.Level)
	}
	if in.Limit < 0 {
		return JournalListOutput{}, errors.New("limit is negative")
	}
	refs, err := d.Store.List(ctx, store.Filter{Folder: model.FolderJournal, Type: model.TypeJournal})
	if err != nil {
		return JournalListOutput{}, err
	}
	out := JournalListOutput{Items: []JournalView{}}
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return out, err
		}
		j, ok := doc.Meta.(*model.Journal)
		if !ok || (level != "" && j.Scope != level) {
			continue
		}
		out.Items = append(out.Items, JournalView{ID: doc.ID, Level: string(j.Scope), Date: string(j.Date), Title: doc.Title,
			Summary: strings.TrimSpace(sectionText(doc.Body, SummaryAgentSection)) != ""})
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if a.Date != b.Date {
			return a.Date > b.Date
		}
		return a.ID > b.ID
	})
	if in.Limit > 0 && len(out.Items) > in.Limit {
		out.Items = out.Items[:in.Limit]
	}
	return out, nil
})
