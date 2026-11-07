package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/recur"
	"github.com/iodesystems/para/internal/store"
)

// Idea pools. A pool is the open items (tasks, goals, questions) put off
// someday that carry the pool's tag: ideas worth keeping, picked only when a
// trigger comes. There is no pool document. The owner writes the pools and
// their triggers in .para/pools.yaml, like triage.yaml: no op writes it, and
// an agent offers a pool in words.
//
//	pools:
//	  - name: hackathon            # a plain word: letters, digits, -
//	    tag: hackathon-idea        # default: the name
//	    pick: 1                    # default 1
//	    area: area-xyz             # optional: where idea_add puts new ideas
//	    when:                      # exactly one of date, exists, closed
//	      exists: {type: obligation, tag: hackathon, within_days: 14}
//	  - name: quarterly
//	    when: {date: 2027-01-05, recur: quarterly}
//	  - name: after-launch
//	    when: {closed: project-launch}
//
// A trigger fires for one instance at a time: a date (the latest occurrence
// on or before today), an open item of the type with the tag whose date (due,
// or the day it is scheduled) is within within_days of today (the item; the
// soonest first), or the named item once it is closed. A firing pool with
// ideas is one row of the value order and the agenda ("<trigger>: pick N of M
// ideas", reason "pick from pool <name>"); the ideas stay put off and are not
// rows. pool_pick brings the picks back, makes each task prep of the trigger
// item when that is an open task or obligation, and records the instance done
// in vault.properties (pool.<name>.rounds), so the pool stops showing until
// the next instance. Skip records the instance too, with no picks. Ideas are
// ranked by the value order's score (scoreOf).

const poolsFile = "pools.yaml"

// poolRowPrefix starts the id of a pool's row: pool:<name>.
const poolRowPrefix = "pool:"

// poolRoundsKeep is how many done instances a pool remembers.
const poolRoundsKeep = 20

var poolNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// PoolExists is a trigger on an open item of a type with a tag.
type PoolExists struct {
	Type       string `yaml:"type" json:"type"`
	Tag        string `yaml:"tag" json:"tag"`
	WithinDays *int   `yaml:"within_days,omitempty" json:"within_days,omitempty"`
}

// PoolWhen is a pool's trigger: exactly one of Date (with Recur, optional),
// Exists, or Closed.
type PoolWhen struct {
	Date   string      `yaml:"date,omitempty" json:"date,omitempty"`
	Recur  string      `yaml:"recur,omitempty" json:"recur,omitempty"`
	Exists *PoolExists `yaml:"exists,omitempty" json:"exists,omitempty"`
	Closed string      `yaml:"closed,omitempty" json:"closed,omitempty"`
}

// PoolConfig is one pool of .para/pools.yaml.
type PoolConfig struct {
	Name    string   `yaml:"name" json:"name"`
	Tag     string   `yaml:"tag,omitempty" json:"tag,omitempty"`
	Pick    int      `yaml:"pick,omitempty" json:"pick,omitempty"`
	Area    string   `yaml:"area,omitempty" json:"area,omitempty"`
	Project string   `yaml:"project,omitempty" json:"project,omitempty"`
	When    PoolWhen `yaml:"when" json:"when"`
}

type poolsConfig struct {
	Pools []PoolConfig `yaml:"pools"`
}

// defaultWithinDays is within_days of an exists trigger that gives none.
const defaultWithinDays = 14

// pool is a checked pool.
type pool struct {
	name, tag     string
	pick          int
	area, project string
	date          string // date trigger: the first occurrence
	rule          *recur.Rule
	recurWord     string
	existsType    model.Type
	existsTag     string
	within        int
	closed        string
}

// loadPools reads and checks .para/pools.yaml; no file is no pools.
func loadPools(s store.Store) ([]pool, error) {
	b, err := s.ConfigFile(poolsFile)
	if err != nil || b == nil {
		return nil, err
	}
	var cfg poolsConfig
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf(".para/%s: %w", poolsFile, err)
	}
	out := make([]pool, 0, len(cfg.Pools))
	seen := map[string]bool{}
	for i, pc := range cfg.Pools {
		p, err := checkPool(pc)
		if err != nil {
			return nil, fmt.Errorf(".para/%s: pool %d: %w", poolsFile, i+1, err)
		}
		if seen[p.name] {
			return nil, fmt.Errorf(".para/%s: pool %d: name %q is used twice", poolsFile, i+1, p.name)
		}
		seen[p.name] = true
		out = append(out, p)
	}
	return out, nil
}

func checkPool(pc PoolConfig) (pool, error) {
	p := pool{name: normTag(pc.Name), pick: pc.Pick, area: normID(pc.Area), project: normID(pc.Project)}
	if !poolNameRe.MatchString(p.name) {
		return p, fmt.Errorf("name %q: want a plain word of letters, digits, and -", pc.Name)
	}
	p.tag = cmp.Or(normTag(pc.Tag), p.name)
	switch {
	case p.pick == 0:
		p.pick = 1
	case p.pick < 0:
		return p, fmt.Errorf("pick %d: want 1 or more", p.pick)
	}
	w := pc.When
	n := 0
	for _, set := range []bool{w.Date != "", w.Exists != nil, w.Closed != ""} {
		if set {
			n++
		}
	}
	if n != 1 {
		return p, errors.New("when: give exactly one of date, exists, or closed")
	}
	if w.Recur != "" && w.Date == "" {
		return p, errors.New("when: recur goes with date")
	}
	switch {
	case w.Date != "":
		t, err := time.Parse(dateLayout, strings.TrimSpace(w.Date))
		if err != nil {
			return p, fmt.Errorf("when: date %q: want YYYY-MM-DD", w.Date)
		}
		p.date = t.Format(dateLayout)
		if w.Recur != "" {
			r, err := recur.Parse(w.Recur)
			if err != nil {
				return p, fmt.Errorf("when: recur: %w", err)
			}
			p.rule, p.recurWord = &r, strings.Join(strings.Fields(strings.ToLower(w.Recur)), " ")
		}
	case w.Exists != nil:
		switch strings.ToLower(strings.TrimSpace(w.Exists.Type)) {
		case "obligation":
			p.existsType = model.TypeObligation
		case "task":
			p.existsType = model.TypeTask
		case "goal", "project":
			p.existsType = model.TypeProject
		default:
			return p, fmt.Errorf("when: exists type %q: want obligation, task, or goal", w.Exists.Type)
		}
		if p.existsTag = normTag(w.Exists.Tag); p.existsTag == "" {
			return p, errors.New("when: exists needs a tag")
		}
		p.within = defaultWithinDays
		if w.Exists.WithinDays != nil {
			if p.within = *w.Exists.WithinDays; p.within < 0 {
				return p, errors.New("when: within_days: want 0 or more")
			}
		}
	default:
		p.closed = normID(w.Closed)
	}
	return p, nil
}

// trigger says the trigger in words.
func (p pool) trigger() string {
	switch {
	case p.rule != nil:
		return p.recurWord + " from " + p.date
	case p.date != "":
		return "on " + p.date
	case p.existsType != "":
		return fmt.Sprintf("an open %s tagged %s within %s", poolTypeName(p.existsType), p.existsTag, plural(p.within, "day"))
	}
	return "when " + p.closed + " is closed"
}

func poolTypeName(t model.Type) string {
	if t == model.TypeProject {
		return "goal"
	}
	return string(t)
}

func poolRoundsKey(name string) string { return "pool." + name + ".rounds" }

// poolRounds is the instances of a pool that were picked or skipped.
func poolRounds(s store.Store, name string) ([]string, error) {
	v, err := s.Config(poolRoundsKey(name))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range strings.Split(v, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out, nil
}

// poolFire is a pool on a snapshot's date: whether it fires, for which
// instance, and its ideas.
type poolFire struct {
	p pool
	// fires is true when an instance is open: not picked or skipped.
	fires    bool
	instance string
	// trigger is the item that fired it (exists, closed), or "".
	trigger, triggerTitle string
	// date is the instance's date: the occurrence, the item's date, or the
	// day the item closed.
	date string
	// next is the next day the pool fires, when known.
	next    string
	problem string
	ideas   []model.Document
}

// title is the row's title: "<trigger>: pick N of M ideas".
func (f poolFire) title() string {
	return fmt.Sprintf("%s: pick %d of %s", cmp.Or(f.triggerTitle, f.p.name), min(f.p.pick, len(f.ideas)), plural(len(f.ideas), "idea"))
}

// itemDay is the date of an item for an exists trigger: due, else the day it
// is scheduled.
func itemDay(m model.Meta) string {
	switch x := m.(type) {
	case *model.Obligation:
		return obligationDate(x)
	case *model.Task:
		if x.Due != "" {
			return string(x.Due)
		}
		day, _ := meetingDay(x.Scheduled)
		return day
	case *model.Project:
		return string(x.Due)
	}
	return ""
}

// openItem reports whether a document is an open, filed item.
func openItem(doc model.Document) bool {
	return doc.Folder != model.FolderArchive && lifeOf(doc.Meta) == LifeOpen
}

// poolIdeas are the pool's ideas: open tasks, goals, and questions put off
// someday that carry its tag.
func poolIdeas(sn *Snapshot, tag string) []model.Document {
	var out []model.Document
	for _, doc := range sn.Docs() {
		switch doc.Meta.Header().Type {
		case model.TypeTask, model.TypeProject, model.TypeQuestion:
		default:
			continue
		}
		df := deferralOf(doc.Meta)
		if df == nil || df.DeferUntil != model.SomedayWord || !openItem(doc) || !slices.Contains(doc.Meta.Header().Tags, tag) {
			continue
		}
		out = append(out, doc)
	}
	return out
}

func addDays(day string, n int) string {
	t, err := time.Parse(dateLayout, day)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, n).Format(dateLayout)
}

// evalPool says whether a pool fires on the snapshot's date.
func evalPool(sn *Snapshot, p pool) (poolFire, error) {
	f := poolFire{p: p}
	rounds, err := poolRounds(sn.s, p.name)
	if err != nil {
		return f, err
	}
	done := func(inst string) bool { return slices.Contains(rounds, inst) }
	today := sn.Today
	switch {
	case p.date != "":
		cur, next := "", p.date
		if p.date <= today {
			cur = p.date
			if p.rule != nil {
				t, _ := time.Parse(dateLayout, p.date)
				for i := 0; i < 100000; i++ {
					n := p.rule.After(t)
					if n.Format(dateLayout) > today {
						next = n.Format(dateLayout)
						break
					}
					t = n
				}
				cur = t.Format(dateLayout)
			} else {
				next = ""
			}
		}
		if cur != "" && !done(cur) {
			f.fires, f.instance, f.date = true, cur, cur
		}
		f.next = next
	case p.existsType != "":
		end := addDays(today, p.within)
		var best, later *model.Document
		var bestDay, laterDay string
		for _, doc := range sn.Docs() {
			if doc.Meta.Header().Type != p.existsType || !openItem(doc) || !slices.Contains(doc.Meta.Header().Tags, p.existsTag) {
				continue
			}
			day := itemDay(doc.Meta)
			switch {
			case day == "" || day < today:
			case day <= end:
				if !done(doc.ID) && (best == nil || day < bestDay || day == bestDay && doc.ID < best.ID) {
					best, bestDay = &doc, day
				}
			default:
				if later == nil || day < laterDay {
					later, laterDay = &doc, day
				}
			}
		}
		if best != nil {
			f.fires, f.instance, f.trigger, f.triggerTitle, f.date = true, best.ID, best.ID, best.Title, bestDay
		}
		if later != nil {
			f.next = addDays(laterDay, -p.within)
		}
	default:
		doc, err := sn.Get(p.closed)
		if errors.Is(err, model.ErrNotFound) {
			f.problem = "no item " + p.closed
			break
		}
		if err != nil {
			return f, err
		}
		f.trigger, f.triggerTitle = doc.ID, doc.Title
		if lifeOf(doc.Meta) == LifeClosed && !done(doc.ID) {
			f.fires, f.instance = true, doc.ID
			f.date = cmp.Or(closedDay(doc), today)
		}
	}
	f.ideas = poolIdeas(sn, p.tag)
	return f, nil
}

// closedDay is the day an item closed, as well as it is known: an
// obligation's closed date, else its last write.
func closedDay(doc model.Document) string {
	if o, ok := doc.Meta.(*model.Obligation); ok && o.Closed != "" {
		return string(o.Closed)
	}
	return string(doc.Meta.Header().Updated)
}

// firingPools are the pools that fire now and have ideas.
func firingPools(sn *Snapshot) ([]poolFire, error) {
	ps, err := loadPools(sn.s)
	if err != nil {
		return nil, err
	}
	var out []poolFire
	for _, p := range ps {
		f, err := evalPool(sn, p)
		if err != nil {
			return nil, err
		}
		if f.fires && len(f.ideas) > 0 {
			out = append(out, f)
		}
	}
	return out, nil
}

// poolNodes are the value order's rows of the firing pools. A pools file
// that does not parse gives none here: pool_list says why.
func poolNodes(sn *Snapshot) []ValueNode {
	fs, err := firingPools(sn)
	if err != nil {
		return nil
	}
	out := make([]ValueNode, 0, len(fs))
	for _, f := range fs {
		n := ValueNode{ID: poolRowPrefix + f.p.name, Kind: KindPool, Title: f.title(), Due: f.date, Created: sn.Today,
			NeedsYou: true, Reason: "pick from pool " + f.p.name, Effort: model.EffortS, ReasonRank: 3}
		// Prep for the item that fired it: the pick rises with that item's
		// date, through the owed tier's backward date when it is a promise.
		if f.trigger != "" && f.p.existsType != "" {
			n.Parent = f.trigger
		}
		out = append(out, n)
	}
	return out
}

// PoolIdea is one idea of a pool, ranked.
type PoolIdea struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	// Text is the first sentence of the body.
	Text      string       `json:"text,omitempty"`
	Effort    model.Effort `json:"effort"`
	EffortSet bool         `json:"effort_set,omitempty"`
	Impact    int          `json:"impact"`
	// Score is the value order's score: (impact+1) per effort weight,
	// doubled for a date within the goal window.
	Score float64 `json:"score"`
}

// PoolView is one pool.
type PoolView struct {
	Name    string `json:"name"`
	Tag     string `json:"tag"`
	Pick    int    `json:"pick"`
	Trigger string `json:"trigger"`
	// Fires is true when an instance is open (not picked or skipped) and the
	// pool has ideas. Instance names it: a date or an item id.
	Fires    bool   `json:"fires"`
	Instance string `json:"instance,omitempty"`
	// TriggerID and TriggerTitle are the item that fired it.
	TriggerID    string `json:"trigger_id,omitempty"`
	TriggerTitle string `json:"trigger_title,omitempty"`
	// Date is the instance's date; Next the next day it fires, when known.
	Date string `json:"date,omitempty"`
	Next string `json:"next,omitempty"`
	// Title is the row's title when it fires; Row its id in the order.
	Title string `json:"title"`
	Row   string `json:"row"`
	Size  int    `json:"size"`
	// Area is where idea_add puts a new idea when it names none.
	Area    string `json:"area,omitempty"`
	Project string `json:"project,omitempty"`
	// Problem says why the trigger cannot be read, such as a missing item.
	Problem string     `json:"problem,omitempty"`
	Ideas   []PoolIdea `json:"ideas"`
}

// firstSentence is the first sentence of a body, at most 200 characters.
func firstSentence(body string) string {
	s := strings.Join(strings.Fields(body), " ")
	if i := strings.IndexAny(s, ".!?"); i >= 0 {
		s = s[:i+1]
	}
	if r := []rune(s); len(r) > 200 {
		s = string(r[:199]) + "…"
	}
	return s
}

// rankOptions reads the vault settings of the value order.
func rankOptions(sn *Snapshot) (RankOptions, error) {
	opt := RankOptions{Today: sn.Today, DueSoonDays: sn.DueSoonDays}
	var err error
	for _, c := range []struct {
		key string
		def int
		to  *int
	}{{FocusCountKey, DefaultFocusCount, &opt.FocusCount}, {StaleDaysKey, DefaultStaleDays, &opt.StaleDays},
		{GoalSoonKey, DefaultGoalSoonDays, &opt.GoalSoonDays}, {OwedSoonKey, DefaultOwedSoonDays, &opt.OwedSoonDays}} {
		if *c.to, err = configInt(sn.s, c.key, c.def); err != nil {
			return opt, err
		}
	}
	return opt, nil
}

// rankIdeas ranks ideas by the value order's score, as if they were not put
// off: the same RankValue rows, compared with scoreCmp.
func rankIdeas(sn *Snapshot, ideas []model.Document) ([]PoolIdea, error) {
	out := []PoolIdea{}
	if len(ideas) == 0 {
		return out, nil
	}
	nodes, err := valueNodes(sn, false)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, d := range ideas {
		want[d.ID] = true
	}
	for i := range nodes {
		if want[nodes[i].ID] {
			nodes[i].Deferred, nodes[i].Hidden, nodes[i].NeedsYou = false, false, true
		}
	}
	opt, err := rankOptions(sn)
	if err != nil {
		return nil, err
	}
	opt.FocusCount = 0
	rows := map[string]ValueRow{}
	for _, r := range RankValue(nodes, opt).Rows {
		if want[r.ID] {
			rows[r.ID] = r
		}
	}
	var ranked []ValueRow
	byID := map[string]model.Document{}
	for _, d := range ideas {
		byID[d.ID] = d
		r, ok := rows[d.ID]
		if !ok {
			// Not a node of the order (a paused goal): no impact, default effort.
			r = ValueRow{ID: d.ID, Kind: string(d.Meta.Header().Type), Effort: model.EffortM}
		}
		ranked = append(ranked, r)
	}
	slices.SortStableFunc(ranked, func(a, b ValueRow) int {
		return cmp.Or(scoreCmp(a, b), cmp.Compare(string(byID[a.ID].Meta.Header().Created), string(byID[b.ID].Meta.Header().Created)), cmp.Compare(a.ID, b.ID))
	})
	for _, r := range ranked {
		d := byID[r.ID]
		num, den := scoreOf(r)
		kind := r.Kind
		if d.Meta.Header().Type == model.TypeProject {
			kind = KindGoal
		}
		out = append(out, PoolIdea{ID: d.ID, Title: d.Title, Kind: kind, Text: firstSentence(d.Body), Effort: r.Effort, EffortSet: r.EffortSet,
			Impact: r.Impact, Score: float64(int(float64(num)/float64(den)*100+0.5)) / 100})
	}
	return out, nil
}

func poolView(sn *Snapshot, f poolFire) (PoolView, error) {
	v := PoolView{Name: f.p.name, Tag: f.p.tag, Pick: f.p.pick, Trigger: f.p.trigger(), Fires: f.fires && len(f.ideas) > 0,
		TriggerID: f.trigger, TriggerTitle: f.triggerTitle, Date: f.date, Next: f.next, Title: f.title(), Row: poolRowPrefix + f.p.name,
		Size: len(f.ideas), Area: f.p.area, Project: f.p.project, Problem: f.problem}
	if v.Fires {
		v.Instance = f.instance
	}
	var err error
	v.Ideas, err = rankIdeas(sn, f.ideas)
	return v, err
}

// PoolListInput is the input of pool_list.
type PoolListInput struct {
	// Name keeps one pool.
	Name string `json:"name,omitempty" query:"name"`
}

// PoolListOutput is the output of pool_list.
type PoolListOutput struct {
	Pools []PoolView `json:"pools"`
}

// PoolList lists the pools.
var PoolList = defineRead("pool_list", "List the idea pools of .para/pools.yaml: each pool's tag, trigger, size (open items put off someday with its tag), whether it fires now and for which instance (a date or the item that fired it), the next day it fires, and its ideas ranked by the value order's score ((impact+1) per effort). A firing pool is one row of order and agenda; its ideas are not", func(ctx context.Context, d Deps, in PoolListInput) (PoolListOutput, error) {
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return PoolListOutput{}, err
	}
	ps, err := loadPools(d.Store)
	if err != nil {
		return PoolListOutput{}, err
	}
	out := PoolListOutput{Pools: []PoolView{}}
	name := normTag(in.Name)
	for _, p := range ps {
		if name != "" && p.name != name {
			continue
		}
		f, err := evalPool(sn, p)
		if err != nil {
			return out, err
		}
		v, err := poolView(sn, f)
		if err != nil {
			return out, err
		}
		out.Pools = append(out.Pools, v)
	}
	if name != "" && len(out.Pools) == 0 {
		return out, fmt.Errorf("no pool %q in .para/%s", name, poolsFile)
	}
	return out, nil
})

// PoolRunInput is the input of pool_run.
type PoolRunInput struct {
	// DryRun lists the firing pools and changes nothing.
	DryRun bool `json:"dry_run,omitempty"`
}

// PoolRunOutput is the output of pool_run.
type PoolRunOutput struct {
	Pools []PoolView `json:"pools"` // the pools that fire now
	// Gaps are the fill-gap agent work made for their ideas.
	Gaps     []string `json:"gaps"`
	Warnings []string `json:"warnings"`
	DryRun   bool     `json:"dry_run,omitempty"`
}

// PoolRun lists the firing pools and asks an agent to fill in their ideas.
var PoolRun = defineSafe("pool_run", "List the idea pools (.para/pools.yaml) that fire now, with their ideas ranked; without dry_run, make fill-gap agent work for the ideas that lack done_when, how to do it, or effort, as tick does, within gap.daily_cap. No model decides; nothing is picked: the owner picks with pool_pick", func(ctx context.Context, d Deps, in PoolRunInput) (PoolRunOutput, error) {
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return PoolRunOutput{}, err
	}
	fs, err := firingPools(sn)
	if err != nil {
		return PoolRunOutput{}, err
	}
	out := PoolRunOutput{Pools: []PoolView{}, Gaps: []string{}, Warnings: []string{}, DryRun: in.DryRun}
	var ids []string
	for _, f := range fs {
		v, err := poolView(sn, f)
		if err != nil {
			return out, err
		}
		out.Pools = append(out.Pools, v)
		for _, it := range v.Ideas {
			ids = append(ids, it.ID)
		}
	}
	if in.DryRun || len(ids) == 0 {
		return out, nil
	}
	on, err := d.Store.Config(GapEnabledKey)
	if err != nil || strings.TrimSpace(on) == "false" {
		return out, err
	}
	limit, err := configInt(d.Store, GapCapKey, DefaultGapCap)
	if err != nil {
		return out, err
	}
	var t TickOutput
	if err := fileGaps(ctx, d, sn, ids, limit, &t); err != nil {
		return out, err
	}
	out.Gaps = append(out.Gaps, t.Gaps...)
	out.Warnings = append(out.Warnings, t.Warnings...)
	return out, nil
})

// PoolPickInput is the input of pool_pick.
type PoolPickInput struct {
	Pool string   `json:"pool"`
	IDs  []string `json:"ids,omitempty"`
	// Skip passes this instance with no picks, so the pool does not ask again
	// until the next one.
	Skip bool `json:"skip,omitempty"`
}

// PoolPickOutput is the output of pool_pick.
type PoolPickOutput struct {
	Pool     string   `json:"pool"`
	Instance string   `json:"instance"`
	Picked   []string `json:"picked"`
	// Linked are the picks made prep of the item that fired the pool.
	Linked   []string `json:"linked"`
	Skipped  bool     `json:"skipped,omitempty"`
	Warnings []string `json:"warnings"`
}

// PoolPick brings picked ideas back for a firing pool.
var PoolPick = withOneReceipt(definePerson("pool_pick", "Pick ideas from a firing pool (.para/pools.yaml): ids are brought back now (undeferred); a picked task becomes prep (parent) of the item that fired the pool when that is an open task or obligation; the rest stay put off. The pool's instance is recorded done, so it stops asking until the next one. skip with no ids passes this instance. One receipt undoes the picks", func(ctx context.Context, d Deps, in PoolPickInput) (PoolPickOutput, error) {
	name := normTag(in.Pool)
	if name == "" {
		return PoolPickOutput{}, errors.New("pool is empty: give the pool's name")
	}
	if in.Skip && len(in.IDs) > 0 {
		return PoolPickOutput{}, errors.New("give ids or skip, not both")
	}
	if !in.Skip && len(in.IDs) == 0 {
		return PoolPickOutput{}, errors.New("nothing picked: give ids, or skip to pass this round")
	}
	ps, err := loadPools(d.Store)
	if err != nil {
		return PoolPickOutput{}, err
	}
	i := slices.IndexFunc(ps, func(p pool) bool { return p.name == name })
	if i < 0 {
		return PoolPickOutput{}, fmt.Errorf("no pool %q in .para/%s", name, poolsFile)
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return PoolPickOutput{}, err
	}
	f, err := evalPool(sn, ps[i])
	if err != nil {
		return PoolPickOutput{}, err
	}
	out := PoolPickOutput{Pool: name, Picked: []string{}, Linked: []string{}, Warnings: []string{}, Skipped: in.Skip}
	if !f.fires {
		if in.Skip {
			out.Warnings = append(out.Warnings, "pool "+name+" does not fire now: nothing to skip")
			return out, nil
		}
		return out, fmt.Errorf("pool %s does not fire now (%s)", name, ps[i].trigger())
	}
	out.Instance = f.instance
	ideas := map[string]bool{}
	for _, doc := range f.ideas {
		ideas[doc.ID] = true
	}
	var parent model.Link
	if f.trigger != "" {
		if p, err := parentOf(ctx, d, f.trigger); err == nil {
			parent = p.link
		}
	}
	for _, raw := range in.IDs {
		id := normID(raw)
		if !ideas[id] {
			return out, fmt.Errorf("%s is not an idea of pool %s: an open task, goal, or question put off someday with tag %s", id, name, f.p.tag)
		}
		if slices.Contains(out.Picked, id) {
			continue
		}
		if _, _, err := deferOnce(ctx, d, id, "", "", "", true); err != nil {
			return out, err
		}
		out.Picked = append(out.Picked, id)
		if parent == "" {
			continue
		}
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			return out, err
		}
		t, ok := doc.Meta.(*model.Task)
		switch {
		case !ok:
			out.Warnings = append(out.Warnings, id+" is a "+typeWord(doc)+": not linked to "+f.trigger+"; only a task is prep")
		case t.Parent != "":
			out.Warnings = append(out.Warnings, id+" already has parent "+t.Parent.Target()+": not linked to "+f.trigger)
		default:
			t.Parent = parent
			if _, err := d.Store.Update(ctx, doc); err != nil {
				return out, err
			}
			out.Linked = append(out.Linked, id)
		}
	}
	if len(out.Picked) > f.p.pick {
		out.Warnings = append(out.Warnings, fmt.Sprintf("picked %d; the pool asks for %d", len(out.Picked), f.p.pick))
	}
	rounds, err := poolRounds(d.Store, name)
	if err != nil {
		return out, err
	}
	rounds = append(rounds, f.instance)
	if len(rounds) > poolRoundsKeep {
		rounds = rounds[len(rounds)-poolRoundsKeep:]
	}
	if err := d.Store.SetConfig(ctx, poolRoundsKey(name), strings.Join(rounds, ",")); err != nil {
		return out, err
	}
	setReceiptRound(ctx, name+":"+f.instance)
	return out, d.Store.Record(ctx, "pool_round", f.instance, "", "", "")
}))

// IdeaAddInput is the input of idea_add.
type IdeaAddInput struct {
	Pool  string `json:"pool"`
	Title string `json:"title"`
	// Text is a sentence or two; an agent fills in the rest when the pool
	// fires (fill-gap).
	Text string `json:"text,omitempty"`
	// Kind is task (default) or goal.
	Kind string `json:"kind,omitempty"`
	// Area and Project are where the idea lives; default the pool's area or
	// project.
	Area    string       `json:"area,omitempty"`
	Project string       `json:"project,omitempty"`
	Effort  model.Effort `json:"effort,omitempty"`
}

// IdeaAddOutput is the output of idea_add.
type IdeaAddOutput struct {
	ID       string   `json:"id"`
	Pool     string   `json:"pool"`
	Tag      string   `json:"tag"`
	Rev      string   `json:"rev"`
	Warnings []string `json:"warnings"`
}

// IdeaAdd keeps an idea in a pool.
var IdeaAdd = defineSafe("idea_add", "Keep an idea in a pool: a task (default) or a goal put off someday with the pool's tag, from a title and a sentence. done_when and how to do it are not needed here, from anyone: an agent fills them in when the pool fires (fill-gap). area or project, else the pool's, says where it lives. A pool not yet in .para/pools.yaml still takes the idea, tagged with its name", func(ctx context.Context, d Deps, in IdeaAddInput) (IdeaAddOutput, error) {
	name := normTag(in.Pool)
	if !poolNameRe.MatchString(name) {
		return IdeaAddOutput{}, fmt.Errorf("pool %q: give the pool's name, a plain word", in.Pool)
	}
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return IdeaAddOutput{}, errors.New("title is empty")
	}
	if err := lintText("text", in.Text); err != nil {
		return IdeaAddOutput{}, err
	}
	if err := checkEffort(in.Effort); err != nil {
		return IdeaAddOutput{}, err
	}
	out := IdeaAddOutput{Pool: name, Tag: name, Warnings: []string{}}
	ps, err := loadPools(d.Store)
	if err != nil {
		return out, err
	}
	area, project := normID(in.Area), normID(in.Project)
	if i := slices.IndexFunc(ps, func(p pool) bool { return p.name == name }); i >= 0 {
		out.Tag = ps[i].tag
		if area == "" && project == "" {
			area, project = ps[i].area, ps[i].project
		}
	} else {
		out.Warnings = append(out.Warnings, "no pool "+name+" in .para/"+poolsFile+" yet: the idea is tagged "+name+"; the owner adds the pool and its trigger")
	}
	areaLk, err := areaLink(ctx, d.Store, area)
	if err != nil {
		return out, err
	}
	var projLk model.Link
	if project != "" {
		if _, _, err := getProject(ctx, d.Store, project); err != nil {
			return out, err
		}
		projLk = model.LinkTo(project)
	}
	common := model.Common{Tags: []string{out.Tag}}
	later := model.Deferral{DeferUntil: model.SomedayWord, DeferredReason: "idea in pool " + name}
	var doc model.Document
	switch strings.ToLower(strings.TrimSpace(in.Kind)) {
	case "", "task":
		if areaLk == "" && projLk == "" {
			return out, fmt.Errorf("a task needs a project or an area: give area or project, or set area on pool %s in .para/%s", name, poolsFile)
		}
		common.Type = model.TypeTask
		doc = model.Document{Folder: model.FolderProjects, Meta: &model.Task{Common: common, Status: model.TaskTodo, Project: projLk, Area: areaLk,
			Effort: in.Effort, Deferral: later}}
	case "goal":
		owner, err := d.Store.Config("owner")
		if err != nil {
			return out, err
		}
		common.Type = model.TypeProject
		doc = model.Document{Folder: model.FolderProjects, Meta: &model.Project{Common: common, Status: model.ProjectActive,
			Owner: cmp.Or(strings.TrimSpace(owner), "self"), Area: areaLk, Deferral: later}}
		if in.Effort != "" {
			out.Warnings = append(out.Warnings, "a goal has no effort: effort not kept")
		}
	default:
		return out, fmt.Errorf("kind %q: want task or goal", in.Kind)
	}
	doc.Title, doc.Body = title, demoteH1(strings.TrimSpace(in.Text))
	if doc, err = storedShape(d, doc); err != nil {
		return out, err
	}
	if doc, err = d.Store.Create(ctx, doc); err != nil {
		return out, err
	}
	out.ID, out.Rev = doc.ID, doc.Rev
	return out, nil
})

// pooledIdeas are the ranked idea ids of the firing pool whose row is row.
func pooledIdeas(sn *Snapshot, row string) ([]string, error) {
	fs, err := firingPools(sn)
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		if poolRowPrefix+f.p.name != row {
			continue
		}
		ranked, err := rankIdeas(sn, f.ideas)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(ranked))
		for _, it := range ranked {
			ids = append(ids, it.ID)
		}
		return ids, nil
	}
	return nil, nil
}

// undoPoolRound takes a pool round back (<pool>:<instance>), so the pool's
// card returns for that instance. A round no longer held is nothing to do.
func undoPoolRound(ctx context.Context, d Deps, round string) error {
	name, inst, ok := strings.Cut(round, ":")
	if !ok || name == "" || inst == "" {
		return nil
	}
	rounds, err := poolRounds(d.Store, name)
	if err != nil {
		return fmt.Errorf("pool: undo round: %w", err)
	}
	kept := slices.DeleteFunc(slices.Clone(rounds), func(r string) bool { return r == inst })
	if len(kept) == len(rounds) {
		return nil
	}
	if err := d.Store.SetConfig(ctx, poolRoundsKey(name), strings.Join(kept, ",")); err != nil {
		return fmt.Errorf("pool: undo round: %w", err)
	}
	return nil
}
