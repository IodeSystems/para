package ops

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	evlog "github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// sweep_report and io_stats read what the vault holds about each source:
// the io items from it, their reply obligations, its tracked documents, and
// the sweep_record events (ids and outcomes only). Every source class and
// every way of reaching it (via mcp, cli, or none) is read the same way; para
// cannot check a connector itself, so reach is what agents declared
// (agent_sources).

// opSweepRecord is the event sweep_record leaves: the source and its outcome.
const opSweepRecord = "sweep_record"

// sweepRunGap is the quiet time that ends a sweep run: sweep_record events
// closer than this belong to one run.
const sweepRunGap = 30 * time.Minute

// noSource is the bucket of items no source document claims.
const noSource = "(no source)"

func sweepOutcome(in SweepRecordInput) string {
	switch {
	case in.Failed:
		return "failed"
	case len(in.Errors) > 0:
		return "errors"
	}
	return "ok"
}

// ioWorld is the vault read once for a report.
type ioWorld struct {
	sources []model.Document
	res     sourceResolver
	ios     map[string]model.Document
	order   []string // io ids, oldest first
	replies map[string]model.Document
	tracked []model.Document
	meet    map[string]bool // obligation ids of kind meeting
	evs     []model.Event
	decl    []Declaration
	cut     time.Time
}

// loadIOWorld reads the vault; with reach, also the agents' declarations on this
// machine (a journal does not need them).
func loadIOWorld(ctx context.Context, d Deps, reach bool) (*ioWorld, error) {
	w := &ioWorld{ios: map[string]model.Document{}, replies: map[string]model.Document{}, meet: map[string]bool{}}
	var err error
	if w.sources, err = loadSources(ctx, d.Store); err != nil {
		return nil, err
	}
	w.res = newSourceResolver(w.sources)
	for _, t := range []model.Type{model.TypeIO, model.TypeObligation, model.TypeTrackedDoc} {
		refs, err := d.Store.List(ctx, store.Filter{Type: t})
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			doc, err := d.Store.Get(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			switch m := doc.Meta.(type) {
			case *model.IO:
				w.ios[doc.ID] = doc
				w.order = append(w.order, doc.ID)
			case *model.Obligation:
				if m.ReplyTo != "" {
					w.replies[doc.ID] = doc
				}
				if m.Kind == model.KindMeeting {
					w.meet[doc.ID] = true
				}
			case *model.TrackedDoc:
				w.tracked = append(w.tracked, doc)
			}
		}
	}
	slices.SortStableFunc(w.order, func(a, b string) int {
		return strings.Compare(firstOf(w.ios[a].Meta.(*model.IO).At, string(w.ios[a].Meta.Header().Created)),
			firstOf(w.ios[b].Meta.(*model.IO).At, string(w.ios[b].Meta.Header().Created)))
	})
	if w.evs, err = d.Store.Events(ctx, math.MaxInt32); err != nil {
		return nil, err
	}
	if !reach {
		return w, nil
	}
	days, err := reachDays(d.Store)
	if err != nil {
		return nil, err
	}
	w.cut = reachCut(d.Store.Now(), days)
	if w.decl, err = readDeclarations(d.Store.Info().StateDir); err != nil {
		return nil, err
	}
	return w, nil
}

// sourceOf is the source an io item belongs to: its via, else for a reply
// the source of the message it answers, else the one source of its medium.
func (w *ioWorld) sourceOf(id string) string {
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		doc, ok := w.ios[id]
		if !ok {
			break
		}
		m := doc.Meta.(*model.IO)
		if m.Via != nil {
			if s := w.res.resolve(m.Via.Source); s != "" {
				return s
			}
		}
		if s := w.byMedium(m.Medium); s != "" && m.Via == nil {
			return s
		}
		id = m.Thread.Target()
	}
	return noSource
}

// byMedium is the one source whose medium is md, or "".
func (w *ioWorld) byMedium(md model.Medium) string {
	if md == "" || md == model.MediumOther {
		return ""
	}
	found := ""
	for _, s := range w.sources {
		if mediumOfSource(s.Meta.(*model.SourceDoc)) == md {
			if found != "" {
				return ""
			}
			found = s.ID
		}
	}
	return found
}

// trackedSource is the source of a tracked document: its via, else the one
// source of its system.
func (w *ioWorld) trackedSource(m *model.TrackedDoc) string {
	if m.Via != nil {
		if s := w.res.resolve(m.Via.Source); s != "" {
			return s
		}
	}
	found := ""
	for _, s := range w.sources {
		if sys := docSystemOfSource(s.Meta.(*model.SourceDoc)); sys != "" && sys == m.System {
			if found != "" {
				return noSource
			}
			found = s.ID
		}
	}
	return cmp.Or(found, noSource)
}

// reachedBy lists the agents whose declaration names the source and is fresh.
func (w *ioWorld) reachedBy(id string) []string {
	out := []string{}
	for _, dc := range w.decl {
		if dc.Fresh(w.cut) && slices.Contains(dc.Sources, id) {
			out = append(out, dc.Actor)
		}
	}
	slices.Sort(out)
	return out
}

func parseAt(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

// SourceInfo names a source in a report: its class, vendor label, and how it
// is reached.
type SourceInfo struct {
	ID     string             `json:"id"`
	Title  string             `json:"title,omitempty"`
	System model.SourceSystem `json:"system,omitempty"` // the class
	Vendor string             `json:"vendor,omitempty"`
	Via    model.SourceVia    `json:"via,omitempty"` // mcp, cli, or none
	Status model.SourceStatus `json:"status,omitempty"`
	// ReachedBy are the agents whose declaration (agent_sources) names it now.
	ReachedBy []string `json:"reached_by"`
}

func (w *ioWorld) info(id string) SourceInfo {
	for _, s := range w.sources {
		if s.ID == id {
			m := s.Meta.(*model.SourceDoc)
			k := sourceKindOf(m)
			return SourceInfo{ID: id, Title: s.Title, System: k.class, Vendor: k.vendor, Via: m.Via, Status: m.Status, ReachedBy: w.reachedBy(id)}
		}
	}
	return SourceInfo{ID: id, ReachedBy: []string{}}
}

// ---- sweep_report ----

// SweepReportInput is the input of sweep_report.
type SweepReportInput struct {
	// Since is RFC 3339 or YYYY-MM-DD. Default: the end of the sweep run
	// before the last one (sweep_record events more than 30 minutes apart),
	// else the start of today.
	Since string `json:"since,omitempty" query:"since"`
}

// ReportItem is one thing a sweep wrote, and what it became.
type ReportItem struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Direction model.IODirection `json:"direction,omitempty"`
	From      string            `json:"from,omitempty"`
	At        string            `json:"at,omitempty"`
	// Became says in words what the item is now: a reply obligation, filed
	// into an item, the digest, needs you, a triage rule, sent, held.
	Became string `json:"became"`
	Reply  string `json:"reply,omitempty"` // the reply obligation
	Link   string `json:"link,omitempty"`  // what it was filed into or became
}

// ReportSource is what one source brought since.
type ReportSource struct {
	SourceInfo
	In  []ReportItem `json:"in"`
	Out []ReportItem `json:"out"`
	// Replies are the reply obligations made for messages from it.
	Replies  []string `json:"replies"`
	Versions []string `json:"versions"` // "<tracked doc> v<n>"
	Comments int      `json:"comments"`
	Held     []string `json:"held"` // drafts the pre-send check sent back
	Sweeps   int      `json:"sweeps"`
	Errors   []string `json:"errors"`
}

// SweepReportOutput is the output of sweep_report.
type SweepReportOutput struct {
	Since   string         `json:"since"`
	Sources []ReportSource `json:"sources"`
	// Calendar lists the meetings made or changed since (calendar_compare
	// fixes, schedule changes).
	Calendar []string `json:"calendar"`
	Text     string   `json:"text"` // the report, for the owner
}

// SweepReport says what came in and went out since a time, per source.
var SweepReport = defineRead("sweep_report", "Report what the sweeps brought since a time (default: since the sweep run before the last one, else today), per source: io in and out and what each became (a reply obligation, filed into an item, the digest, needs you, a triage rule, sent), reply obligations made, tracked-document versions and comments, drafts the pre-send check held, sweep errors, and meetings made or changed. Print text to the owner at the end of /para io", func(ctx context.Context, d Deps, in SweepReportInput) (SweepReportOutput, error) {
	w, err := loadIOWorld(ctx, d, true)
	if err != nil {
		return SweepReportOutput{}, err
	}
	since, err := reportSince(d, w, in.Since)
	if err != nil {
		return SweepReportOutput{}, err
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return SweepReportOutput{}, err
	}
	return buildReport(w, sn, since), nil
})

// reportSince is the start of a report: the given time, or the default.
func reportSince(d Deps, w *ioWorld, raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	loc := d.Store.Now().Location()
	if raw != "" {
		if t, ok := parseAt(raw); ok {
			return t, nil
		}
		if t, err := time.ParseInLocation(dateLayout, raw, loc); err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("since %q: want RFC 3339 or YYYY-MM-DD", raw)
	}
	var runs []time.Time
	for _, e := range w.evs {
		if e.Op == opSweepRecord {
			runs = append(runs, e.TS)
		}
	}
	slices.SortFunc(runs, func(a, b time.Time) int { return a.Compare(b) })
	for i := len(runs) - 1; i > 0; i-- {
		if runs[i].Sub(runs[i-1]) > sweepRunGap {
			return runs[i-1], nil
		}
	}
	n := d.Store.Now().In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc), nil
}

func buildReport(w *ioWorld, sn *Snapshot, since time.Time) SweepReportOutput {
	out := SweepReportOutput{Since: since.UTC().Format(time.RFC3339), Sources: []ReportSource{}, Calendar: []string{}}
	by := map[string]*ReportSource{}
	get := func(id string) *ReportSource {
		if r, ok := by[id]; ok {
			return r
		}
		r := &ReportSource{SourceInfo: w.info(id), In: []ReportItem{}, Out: []ReportItem{}, Replies: []string{}, Versions: []string{}, Held: []string{}, Errors: []string{}}
		by[id] = r
		return r
	}
	for _, s := range w.sources {
		get(s.ID)
	}
	made := map[string]bool{}
	calendar := map[string]bool{}
	for _, e := range w.evs {
		if e.TS.Before(since) {
			continue
		}
		switch {
		case e.Op == evlog.OpCreate:
			made[e.Doc] = true
			if w.meet[e.Doc] {
				calendar[e.Doc] = true
			}
		case e.Op == evlog.OpUpdate && w.meet[e.Doc]:
			calendar[e.Doc] = true
		case e.Op == "doc_version_add":
			for _, td := range w.tracked {
				if td.ID == e.Doc {
					r := get(w.trackedSource(td.Meta.(*model.TrackedDoc)))
					r.Versions = append(r.Versions, e.Doc+" v"+e.To)
				}
			}
		case e.Op == opSweepRecord:
			get(e.Doc).Sweeps++
		}
	}
	for _, id := range w.order {
		if !made[id] {
			continue
		}
		doc := w.ios[id]
		m := doc.Meta.(*model.IO)
		r := get(w.sourceOf(id))
		it := ReportItem{ID: id, Title: doc.Title, Direction: m.Direction, From: m.From, At: m.At, Reply: m.Reply.Target(), Link: m.Became.Target()}
		it.Became = becameWords(w, sn, doc, m)
		if m.Direction == model.IOIn {
			r.In = append(r.In, it)
			if it.Reply != "" {
				r.Replies = append(r.Replies, it.Reply)
			}
		} else {
			r.Out = append(r.Out, it)
		}
	}
	for _, id := range w.order {
		m := w.ios[id].Meta.(*model.IO)
		if m.Direction == model.IOOut && m.State.Open() && m.Held != "" {
			r := get(w.sourceOf(id))
			r.Held = append(r.Held, id)
		}
	}
	for _, td := range w.tracked {
		m := td.Meta.(*model.TrackedDoc)
		n := 0
		for _, c := range m.Comments {
			if t, ok := parseAt(c.At); ok && !t.Before(since) {
				n++
			}
		}
		if n > 0 {
			get(w.trackedSource(m)).Comments += n
		}
	}
	for _, s := range w.sources {
		m := s.Meta.(*model.SourceDoc)
		if t, ok := parseAt(m.LastFailure); ok && !t.Before(since) {
			r := get(s.ID)
			r.Errors = append(r.Errors, m.LastFailure+": "+m.LastError)
		}
	}
	for id := range calendar {
		out.Calendar = append(out.Calendar, id)
	}
	slices.Sort(out.Calendar)
	ids := make([]string, 0, len(by))
	for id := range by {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		// Sources first, by id; the bucket of no source last.
		return cmp.Or(cmp.Compare(boolInt(a == noSource), boolInt(b == noSource)), strings.Compare(a, b))
	})
	for _, id := range ids {
		r := by[id]
		if id == noSource && len(r.In)+len(r.Out)+len(r.Versions)+len(r.Held)+r.Comments == 0 {
			continue
		}
		out.Sources = append(out.Sources, *r)
	}
	out.Text = reportText(out)
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// becameWords says what an io item is now.
func becameWords(w *ioWorld, sn *Snapshot, doc model.Document, m *model.IO) string {
	reply := ""
	if od, ok := w.replies[m.Reply.Target()]; ok {
		o := od.Meta.(*model.Obligation)
		if o.Open() {
			reply = "reply owed by " + string(o.Due) + " (" + od.ID + ")"
		} else {
			reply = "reply " + string(o.Outcome) + " (" + od.ID + ")"
		}
	}
	var words string
	switch {
	case m.Direction == model.IOOut && m.Held != "" && m.State.Open():
		words = "held: " + m.Held
	case m.Direction == model.IOOut:
		words = string(m.State)
	case m.State == model.IOFiled:
		words = "filed into " + m.Became.Target()
	case m.State == model.IODropped:
		words = "dropped: " + m.Reason
	case reply != "":
	case m.Triage != "":
		words = string(m.Triage) + " by " + firstOf(m.TriageBy, "triage")
	default:
		if n := sn.Need(doc.ID); n.Yes {
			words = "needs you: " + n.Reason
		} else {
			words = "digest"
		}
	}
	return strings.Trim(strings.Join([]string{reply, words}, "; "), "; ")
}

// reportText is the report in lines, for the owner and the journal.
func reportText(r SweepReportOutput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Since %s:\n", r.Since)
	empty := true
	for _, s := range r.Sources {
		if len(s.In)+len(s.Out)+len(s.Versions)+len(s.Held)+len(s.Errors)+s.Comments+s.Sweeps == 0 {
			continue
		}
		empty = false
		label := s.ID
		if s.System != "" {
			label += " (" + strings.Trim(string(s.System)+" "+s.Vendor, " ") + ", via " + string(s.Via) + ")"
		}
		fmt.Fprintf(&b, "- %s: %d in, %d out, %d replies owed, %d versions, %d comments, %d held, %d sweeps\n",
			label, len(s.In), len(s.Out), len(s.Replies), len(s.Versions), s.Comments, len(s.Held), s.Sweeps)
		for _, it := range append(append([]ReportItem{}, s.In...), s.Out...) {
			fmt.Fprintf(&b, "  - %s %s: %s\n", it.ID, it.Title, it.Became)
		}
		for _, e := range s.Errors {
			fmt.Fprintf(&b, "  - error %s\n", e)
		}
	}
	if len(r.Calendar) > 0 {
		fmt.Fprintf(&b, "- calendar: %s\n", strings.Join(r.Calendar, ", "))
		empty = false
	}
	if empty {
		b.WriteString("- nothing came in or went out\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// reportLines is the journal section of a day's sweeps: one line per source
// that had any, nil when none did.
func reportLines(ctx context.Context, d Deps, start, end time.Time) ([]string, error) {
	w, err := loadIOWorld(ctx, d, false)
	if err != nil {
		return nil, err
	}
	var evs []model.Event
	for _, e := range w.evs {
		if !e.TS.Before(start) && e.TS.Before(end) {
			evs = append(evs, e)
		}
	}
	swept := false
	for _, e := range evs {
		swept = swept || e.Op == opSweepRecord
	}
	if !swept {
		return nil, nil
	}
	w.evs = evs
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return nil, err
	}
	r := buildReport(w, sn, start)
	lines := []string{sweepReportHeading}
	for _, s := range r.Sources {
		if len(s.In)+len(s.Out)+len(s.Versions)+len(s.Held)+len(s.Errors)+s.Comments+s.Sweeps == 0 {
			continue
		}
		line := fmt.Sprintf("- [[%s]]: %d in, %d out, %d replies owed, %d versions, %d comments, %d held, %d sweeps", s.ID,
			len(s.In), len(s.Out), len(s.Replies), len(s.Versions), s.Comments, len(s.Held), s.Sweeps)
		if s.ID == noSource {
			line = strings.Replace(line, "[["+noSource+"]]", noSource, 1)
		}
		if len(s.Errors) > 0 {
			line += "; errors: " + strings.Join(s.Errors, "; ")
		}
		lines = append(lines, line)
	}
	return lines, nil
}

const (
	sweepReportSection = "sweeps"
	sweepReportHeading = "### Sweeps"
)

// ---- io_stats ----

// IOStatsInput is the input of io_stats.
type IOStatsInput struct {
	// Days is one window in days; 0 gives two: 7 and 30.
	Days int `json:"days,omitempty" query:"days"`
}

// IOWindow is one window of a source's numbers.
type IOWindow struct {
	Days   int `json:"days"`
	Sweeps int `json:"sweeps"`
	Failed int `json:"failed"` // sweeps that failed or reported errors
	In     int `json:"in"`
	Out    int `json:"out"`
	// ReplyHours is the median hours from an incoming message to the reply
	// sent in its thread; -1 when no reply in the window can be timed.
	ReplyHours float64 `json:"reply_hours"`
	Replied    int     `json:"replied"` // replies timed
}

// IOSourceStats are the numbers of one source.
type IOSourceStats struct {
	SourceInfo
	LastScan    string     `json:"last_scan,omitempty"`
	LastFailure string     `json:"last_failure,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	OpenReplies int        `json:"open_replies"`
	Overdue     int        `json:"overdue"` // open replies past due
	Held        int        `json:"held"`    // drafts the pre-send check sent back
	Windows     []IOWindow `json:"windows"`
}

// IOStatsOutput is the output of io_stats.
type IOStatsOutput struct {
	Sources []IOSourceStats `json:"sources"`
	Text    string          `json:"text"`
}

// IOStats gives the numbers of each source.
var IOStats = defineRead("io_stats", "Numbers per source (any class, reached via mcp, cli, or none): last sweep, last failure and its words, who declared reach (agent_sources), and over 7 and 30 days (or days): sweeps and failed sweeps, io in and out, the median hours from an incoming message to the reply sent; now: open reply obligations, how many are overdue, and drafts the pre-send check held", func(ctx context.Context, d Deps, in IOStatsInput) (IOStatsOutput, error) {
	if in.Days < 0 {
		return IOStatsOutput{}, fmt.Errorf("days %d: want 1 or more, or 0 for 7 and 30", in.Days)
	}
	w, err := loadIOWorld(ctx, d, true)
	if err != nil {
		return IOStatsOutput{}, err
	}
	return ioStats(w, d.Store.Now(), in.Days), nil
})

func ioStats(w *ioWorld, now time.Time, days int) IOStatsOutput {
	windows := []int{7, 30}
	if days > 0 {
		windows = []int{days}
	}
	today := now.Format(dateLayout)
	out := IOStatsOutput{Sources: []IOSourceStats{}}
	by := map[string]*IOSourceStats{}
	get := func(id string) *IOSourceStats {
		if s, ok := by[id]; ok {
			return s
		}
		s := &IOSourceStats{SourceInfo: w.info(id)}
		for _, n := range windows {
			s.Windows = append(s.Windows, IOWindow{Days: n, ReplyHours: -1})
		}
		by[id] = s
		return s
	}
	for _, s := range w.sources {
		x := get(s.ID)
		m := s.Meta.(*model.SourceDoc)
		x.LastScan, x.LastFailure, x.LastError = m.LastScan, m.LastFailure, m.LastError
	}
	cutOf := func(n int) time.Time { return now.Add(-time.Duration(n) * 24 * time.Hour) }
	for _, e := range w.evs {
		if e.Op != opSweepRecord {
			continue
		}
		x := get(e.Doc)
		for i, n := range windows {
			if !e.TS.Before(cutOf(n)) {
				x.Windows[i].Sweeps++
				if e.From != "ok" {
					x.Windows[i].Failed++
				}
			}
		}
	}
	hours := map[string][][]float64{}
	for _, id := range w.order {
		doc := w.ios[id]
		m := doc.Meta.(*model.IO)
		if m.Logged() {
			continue
		}
		src := w.sourceOf(id)
		x := get(src)
		at, ok := parseAt(m.At)
		if m.Direction == model.IOOut && m.Held != "" && m.State.Open() {
			x.Held++
		}
		if ok {
			for i, n := range windows {
				if at.Before(cutOf(n)) {
					continue
				}
				switch {
				case m.Direction == model.IOIn:
					x.Windows[i].In++
				case m.State == model.IOSent:
					x.Windows[i].Out++
				}
			}
		}
		// Time to reply: an incoming message filed into the reply sent in
		// its thread.
		if m.Direction != model.IOIn || m.State != model.IOFiled || !ok {
			continue
		}
		rd, found := w.ios[m.Became.Target()]
		if !found {
			continue
		}
		rm := rd.Meta.(*model.IO)
		sent, sok := parseAt(rm.At)
		if rm.Direction != model.IOOut || rm.State != model.IOSent || !sok || sent.Before(at) {
			continue
		}
		if hours[src] == nil {
			hours[src] = make([][]float64, len(windows))
		}
		for i, n := range windows {
			if !sent.Before(cutOf(n)) {
				hours[src][i] = append(hours[src][i], sent.Sub(at).Hours())
			}
		}
	}
	for _, od := range w.replies {
		o := od.Meta.(*model.Obligation)
		if !o.Open() {
			continue
		}
		x := get(w.sourceOf(o.ReplyTo.Target()))
		x.OpenReplies++
		if o.Due != "" && string(o.Due) < today {
			x.Overdue++
		}
	}
	for src, hs := range hours {
		x := get(src)
		for i, list := range hs {
			if len(list) > 0 {
				slices.Sort(list)
				x.Windows[i].Replied = len(list)
				x.Windows[i].ReplyHours = math.Round(medianOf(list)*10) / 10
			}
		}
	}
	ids := make([]string, 0, len(by))
	for id := range by {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(cmp.Compare(boolInt(a == noSource), boolInt(b == noSource)), strings.Compare(a, b))
	})
	var b strings.Builder
	for _, id := range ids {
		x := by[id]
		if id == noSource && x.OpenReplies+x.Held == 0 && !slices.ContainsFunc(x.Windows, func(w IOWindow) bool { return w.In+w.Out > 0 }) {
			continue
		}
		out.Sources = append(out.Sources, *x)
		fmt.Fprintf(&b, "%s", id)
		if x.System != "" {
			fmt.Fprintf(&b, " (%s, via %s", strings.Trim(string(x.System)+" "+x.Vendor, " "), x.Via)
			if len(x.ReachedBy) > 0 {
				fmt.Fprintf(&b, ", reached by %s", strings.Join(x.ReachedBy, ", "))
			} else {
				b.WriteString(", no agent declared reach")
			}
			b.WriteString(")")
		}
		fmt.Fprintf(&b, ": last sweep %s", firstOf(x.LastScan, "never"))
		if x.LastFailure != "" {
			fmt.Fprintf(&b, "; last failure %s: %s", x.LastFailure, x.LastError)
		}
		fmt.Fprintf(&b, "; replies open %d, overdue %d; held %d\n", x.OpenReplies, x.Overdue, x.Held)
		for _, wd := range x.Windows {
			reply := "n/a"
			if wd.ReplyHours >= 0 {
				reply = fmt.Sprintf("%.1f h (%d)", wd.ReplyHours, wd.Replied)
			}
			fmt.Fprintf(&b, "  %dd: sweeps %d (failed %d), in %d, out %d, median reply %s\n", wd.Days, wd.Sweeps, wd.Failed, wd.In, wd.Out, reply)
		}
	}
	out.Text = strings.TrimRight(b.String(), "\n")
	return out
}

func medianOf(sorted []float64) float64 {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
