package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/recur"
	"github.com/iodesystems/para/internal/store"
)

// KPIs on a project (plan/paralog.md § 10; model/kpi.go). A reading is a
// plain fact with a source, so it is safe, like a goal signal reading. Para
// fetches nothing: the caller reads the value with its own tools. The file
// keeps the last model.KPIReadingsKept readings of each KPI; tick, on the
// runner only, moves older ones to archive/<project>-kpis-<yyyy>.md, so each
// archive file has one writer under any sync tool.

// Bundle § KPIs: one line per KPI, at most kpiLinesMax, within kpiBytesMax
// encoded bytes.
const (
	kpiLinesMax = 8
	kpiBytesMax = 600
)

// kpiState is a KPI as health, the bundle, and the list row read it.
type kpiState struct {
	k          model.KPI
	last, prev *model.KPIReading
	off, stale bool
	due        string // the next reading is due; "" with no cadence
}

// kpiStateOf reads k on today. With no reading yet, the cadence counts from
// the day the project was created. A KPI with no reading is not off target.
func kpiStateOf(k model.KPI, created model.Date, today string) kpiState {
	s := kpiState{k: k}
	s.last, s.prev = k.Latest()
	s.off = s.last != nil && !k.OnTarget(s.last.Value)
	from := string(created)
	if s.last != nil {
		from = string(s.last.Date)
	}
	if rule, err := recur.Parse(k.Cadence); k.Cadence != "" && err == nil {
		if t, err := time.Parse(dateLayout, from); err == nil {
			s.due = rule.After(t).Format(dateLayout)
			s.stale = s.due < today
		}
	}
	return s
}

func (s kpiState) alert() bool { return s.off || s.stale }

// word is on, off, stale, or "off, stale".
func (s kpiState) word() string {
	switch {
	case s.off && s.stale:
		return "off, stale"
	case s.off:
		return "off"
	case s.stale:
		return "stale"
	case s.last == nil:
		return "no reading"
	}
	return "on"
}

// lastDate is the date of r, or "" for none.
func lastDate(r *model.KPIReading) string {
	if r == nil {
		return ""
	}
	return string(r.Date)
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// targetText is the target with its direction: ">= 50", "<= 2", "= 7 ± 1".
func targetText(k model.KPI) string {
	switch k.Direction {
	case model.KPIDown:
		return "<= " + num(k.Target)
	case model.KPIHold:
		if k.Tolerance > 0 {
			return "= " + num(k.Target) + " ± " + num(k.Tolerance)
		}
		return "= " + num(k.Target)
	}
	return ">= " + num(k.Target)
}

// trend is the arrow from the reading before the latest to the latest:
// up, down, or flat; "" with fewer than two readings.
func (s kpiState) trend() string {
	switch {
	case s.last == nil || s.prev == nil:
		return ""
	case s.last.Value > s.prev.Value:
		return "↑"
	case s.last.Value < s.prev.Value:
		return "↓"
	}
	return "→"
}

// line is the bundle line: `<name>: <value> <unit> ↑ vs >= <target>; <date> (<n>d); <word>`.
func (s kpiState) line(today string) string {
	l := clip(s.k.Name, 40) + ": "
	if s.last == nil {
		l += "no reading"
	} else {
		l += strings.TrimSpace(num(s.last.Value) + " " + clip(s.k.Unit, 12))
		if t := s.trend(); t != "" {
			l += " " + t
		}
	}
	l += " vs " + targetText(s.k)
	if s.last != nil {
		l += "; " + string(s.last.Date) + " (" + strconv.Itoa(daysBetween(string(s.last.Date), today)) + "d)"
	}
	return l + "; " + s.word()
}

// projectKPIs reads the KPIs of p: alerts first, then by name.
func projectKPIs(p *model.Project, today string) []kpiState {
	out := make([]kpiState, 0, len(p.KPIs))
	for _, k := range p.KPIs {
		out = append(out, kpiStateOf(k, p.Created, today))
	}
	slices.SortStableFunc(out, func(a, b kpiState) int {
		ab, bb := 0, 0
		if a.alert() {
			ab = -1
		}
		if b.alert() {
			bb = -1
		}
		return cmp.Or(cmp.Compare(ab, bb), cmp.Compare(strings.ToLower(a.k.Name), strings.ToLower(b.k.Name)))
	})
	return out
}

// kpiBundle returns the bundle lines of the KPIs, how many the cap left out,
// and the header alerts: "<name> off", "<name> stale", at most kpiLinesMax
// and a "+N more".
func kpiBundle(states []kpiState, today string) (lines []string, cut int, alerts []string) {
	lines, alerts = []string{}, []string{}
	for i, s := range states {
		if len(lines) == kpiLinesMax {
			cut = len(states) - i
			break
		}
		next := append(slices.Clone(lines), s.line(today))
		if b, _ := json.Marshal(next); len(b) > kpiBytesMax {
			cut = len(states) - i
			break
		}
		lines = next
	}
	n := 0
	for _, s := range states {
		if !s.alert() {
			continue
		}
		if n++; n > kpiLinesMax {
			continue
		}
		alerts = append(alerts, clip(s.k.Name, 40)+" "+s.word())
	}
	if n > kpiLinesMax {
		alerts = append(alerts, fmt.Sprintf("+%d more", n-kpiLinesMax))
	}
	return lines, cut, alerts
}

// kpiIndex finds the KPI named name (case is ignored), or says which exist.
func kpiIndex(id string, p *model.Project, name string) (int, error) {
	name = strings.TrimSpace(name)
	i := slices.IndexFunc(p.KPIs, func(k model.KPI) bool { return strings.EqualFold(k.Name, name) })
	if i < 0 {
		names := make([]string, len(p.KPIs))
		for j, k := range p.KPIs {
			names[j] = k.Name
		}
		return -1, fmt.Errorf("%s has no KPI %q (defined: %s); define it with kpi_add", id, name, cmp.Or(strings.Join(names, "; "), "none"))
	}
	return i, nil
}

// KPIAddInput is the input of kpi_add.
type KPIAddInput struct {
	ID        string             `json:"id"` // the project
	Name      string             `json:"name"`
	Unit      string             `json:"unit,omitempty"`
	Target    *float64           `json:"target"`
	Direction model.KPIDirection `json:"direction"`           // up: at or above target is on; down: at or below; hold: within tolerance
	Tolerance float64            `json:"tolerance,omitempty"` // hold only
	Cadence   string             `json:"cadence,omitempty"`   // how often it is read: daily, weekly, monthly, quarterly, yearly, every <n> days
}

// KPIAddOutput is the output of kpi_add.
type KPIAddOutput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	KPIs int    `json:"kpis"` // KPIs the project now has
	Rev  string `json:"rev"`
}

// KPIAdd defines a KPI on a project.
var KPIAdd = defineSafe("kpi_add", "Define a KPI on an ongoing project: name, unit, target, direction (up, down, hold with tolerance), and cadence (how often it is read). Change or remove one with edit {fields: {kpis}}", func(ctx context.Context, d Deps, in KPIAddInput) (KPIAddOutput, error) {
	doc, p, err := getOngoing(ctx, d.Store, normID(in.ID))
	if err != nil {
		return KPIAddOutput{}, err
	}
	name := strings.Join(strings.Fields(in.Name), " ")
	if name == "" || in.Target == nil {
		return KPIAddOutput{}, errors.New("kpi_add: give name and target")
	}
	for _, f := range [][2]string{{"name", name}, {"unit", in.Unit}} {
		if err := lintText(f[0], f[1]); err != nil {
			return KPIAddOutput{}, err
		}
	}
	if _, err := kpiIndex(doc.ID, p, name); err == nil {
		return KPIAddOutput{}, fmt.Errorf("kpi_add: %s already has KPI %q: change it with edit {id: %s, fields: {kpis}}", doc.ID, name, doc.ID)
	}
	p.KPIs = append(p.KPIs, model.KPI{Name: name, Unit: strings.TrimSpace(in.Unit), Target: *in.Target, Direction: cmp.Or(in.Direction, model.KPIUp),
		Tolerance: in.Tolerance, Cadence: strings.TrimSpace(in.Cadence)})
	if err := checkDoc(doc); err != nil {
		return KPIAddOutput{}, fmt.Errorf("kpi_add: %w", err)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return KPIAddOutput{}, err
	}
	return KPIAddOutput{ID: saved.ID, Name: name, KPIs: len(p.KPIs), Rev: saved.Rev}, nil
})

// KPIReadingAddInput is the input of kpi_reading_add.
type KPIReadingAddInput struct {
	ID     string   `json:"id"`   // the project
	Name   string   `json:"name"` // which KPI
	Value  *float64 `json:"value"`
	Source string   `json:"source"`         // where it was read: a URL, a document id, a path, or "owner in chat, <date>"
	Date   string   `json:"date,omitempty"` // YYYY-MM-DD; default today
}

// KPIReadingAddOutput is the output of kpi_reading_add.
type KPIReadingAddOutput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Date     string `json:"date"`
	Line     string `json:"line"`     // the KPI's bundle line after the reading
	Readings int    `json:"readings"` // readings in the file; tick archives all but the last 12
	Rev      string `json:"rev"`
}

// KPIReadingAdd adds a dated reading, with its source, to a KPI.
var KPIReadingAdd = defineSafe("kpi_reading_add", "Add a dated reading of a project's KPI: a number and its source (a URL, a document id, a path, or \"owner in chat, <date>\"). Read the value with your own tools; para fetches nothing", func(ctx context.Context, d Deps, in KPIReadingAddInput) (KPIReadingAddOutput, error) {
	doc, p, err := getOngoing(ctx, d.Store, normID(in.ID))
	if err != nil {
		return KPIReadingAddOutput{}, err
	}
	source := strings.Join(strings.Fields(in.Source), " ")
	if in.Value == nil || source == "" {
		return KPIReadingAddOutput{}, errors.New("kpi_reading_add: give value and source")
	}
	if err := lintText("source", source); err != nil {
		return KPIReadingAddOutput{}, err
	}
	today := d.Store.Now().Local().Format(dateLayout)
	date := cmp.Or(in.Date, today)
	if err := checkDate("date", date); err != nil {
		return KPIReadingAddOutput{}, err
	}
	i, err := kpiIndex(doc.ID, p, in.Name)
	if err != nil {
		return KPIReadingAddOutput{}, fmt.Errorf("kpi_reading_add: %w", err)
	}
	k := &p.KPIs[i]
	k.Readings = append(k.Readings, model.KPIReading{Date: model.Date(date), Value: *in.Value, Source: source})
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return KPIReadingAddOutput{}, err
	}
	return KPIReadingAddOutput{ID: saved.ID, Name: k.Name, Date: date, Line: kpiStateOf(*k, p.Created, today).line(today), Readings: len(k.Readings), Rev: saved.Rev}, nil
})

// KPIListInput is the input of kpi_list.
type KPIListInput struct {
	ID string `json:"id" query:"id" required:"true"` // the project
}

// KPIView is one KPI with its state on today.
type KPIView struct {
	KPI   model.KPI `json:"kpi"`
	Line  string    `json:"line"`
	Off   bool      `json:"off"`
	Stale bool      `json:"stale"`
	Due   string    `json:"due,omitempty"` // the next reading is due
}

// KPIListOutput is the output of kpi_list.
type KPIListOutput struct {
	ID   string    `json:"id"`
	KPIs []KPIView `json:"kpis"`
}

// KPIList lists a project's KPIs with their readings in the file.
var KPIList = defineRead("kpi_list", "List a project's KPIs with the readings in its file (the last 12 each), whether each is off target or stale, and when the next reading is due", func(ctx context.Context, d Deps, in KPIListInput) (KPIListOutput, error) {
	doc, p, err := getOngoing(ctx, d.Store, normID(in.ID))
	if err != nil {
		return KPIListOutput{}, err
	}
	today := d.Store.Now().Local().Format(dateLayout)
	out := KPIListOutput{ID: doc.ID, KPIs: []KPIView{}}
	for _, s := range projectKPIs(p, today) {
		out.KPIs = append(out.KPIs, KPIView{KPI: s.k, Line: s.line(today), Off: s.off, Stale: s.stale, Due: s.due})
	}
	return out, nil
})

// kpiArchiveID is the archive document of a project's KPI readings of a year.
func kpiArchiveID(project, year string) string { return project + "-kpis-" + year }

// kpiArchiveRow is one archived reading, a row of the archive's table.
func kpiArchiveRow(name string, r model.KPIReading) string {
	cell := func(s string) string { return strings.ReplaceAll(s, "|", "/") }
	return "| " + cell(name) + " | " + string(r.Date) + " | " + num(r.Value) + " | " + cell(r.Source) + " |"
}

const kpiArchiveHead = "| KPI | Date | Value | Source |\n| --- | --- | --- | --- |"

// tickKPIs moves the readings of each KPI past the last model.KPIReadingsKept
// to the project's archive of their year. The archive is written first and
// skips rows it holds, so a pass that stops between the two writes repeats
// safely.
func tickKPIs(ctx context.Context, d Deps, out *TickOutput) error {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeProject})
	if err != nil {
		return err
	}
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			continue
		}
		p, ok := doc.Meta.(*model.Project)
		if !ok {
			continue
		}
		rows := map[string][]string{} // year -> rows
		moved := 0
		for i := range p.KPIs {
			k := &p.KPIs[i]
			if len(k.Readings) <= model.KPIReadingsKept {
				continue
			}
			rs := slices.Clone(k.Readings)
			slices.SortStableFunc(rs, func(a, b model.KPIReading) int { return cmp.Compare(a.Date, b.Date) })
			old := rs[:len(rs)-model.KPIReadingsKept]
			for _, o := range old {
				y := string(o.Date)[:4]
				rows[y] = append(rows[y], kpiArchiveRow(k.Name, o))
			}
			k.Readings, moved = rs[len(old):], moved+len(old)
		}
		if moved == 0 {
			continue
		}
		var ids []string
		for _, y := range slices.Sorted(maps.Keys(rows)) {
			id, err := kpiArchive(ctx, d.Store, doc, y, rows[y])
			if err != nil {
				return fmt.Errorf("%s: %w", doc.ID, err)
			}
			ids = append(ids, id)
		}
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return fmt.Errorf("%s: %w", doc.ID, err)
		}
		out.KPIArchived = append(out.KPIArchived, fmt.Sprintf("%s: %d readings to %s", doc.ID, moved, strings.Join(ids, ", ")))
	}
	return nil
}

// kpiArchive appends rows to the KPI archive of project for year, making it
// when it is not there, and returns its id.
func kpiArchive(ctx context.Context, s store.Store, project model.Document, year string, rows []string) (string, error) {
	id := kpiArchiveID(project.ID, year)
	a, err := s.Get(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		body := kpiArchiveHead + "\n" + strings.Join(rows, "\n")
		_, err := s.Create(ctx, model.Document{ID: id, Folder: model.FolderArchive, Title: project.Title + " KPIs " + year,
			Body: body, Meta: &model.Note{Common: model.Common{Type: model.TypeNote}, Project: model.LinkTo(project.ID)}})
		return id, err
	}
	if err != nil {
		return "", err
	}
	have := map[string]bool{}
	for l := range strings.SplitSeq(a.Body, "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, r := range rows {
		if !have[r] {
			add, have[r] = append(add, r), true
		}
	}
	if len(add) == 0 {
		return id, nil
	}
	body := strings.TrimRight(a.Body, "\n")
	if !strings.Contains(body, "| --- |") {
		body = strings.TrimSpace(body + "\n\n" + kpiArchiveHead)
	}
	a.Body = body + "\n" + strings.Join(add, "\n")
	_, err = s.Update(ctx, a)
	return id, err
}
