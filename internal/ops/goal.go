package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Goal validation. The brief lives in the goal's frontmatter (model.GoalBrief)
// and is edited with doc_edit, so an agent proposes a change and the owner
// reads the before and after. Three ops sit beside it: a reading is a plain
// fact with a source, so it is safe; the rank is the owner's; the comparison
// is a read with no score.

// GoalSignalAddInput is the input of goal_signal_add.
type GoalSignalAddInput struct {
	ID     string `json:"id"`             // the goal
	What   string `json:"what"`           // which signal, as named in the brief
	Value  string `json:"value"`          // what was read
	Source string `json:"source"`         // where it was read: a URL, a document id, a command
	Date   string `json:"date,omitempty"` // YYYY-MM-DD; default today
}

// GoalSignalAddOutput is the output of goal_signal_add.
type GoalSignalAddOutput struct {
	ID       string `json:"id"`
	What     string `json:"what"`
	Date     string `json:"date"`
	Readings int    `json:"readings"` // readings the signal now holds
	Rev      string `json:"rev"`
}

// GoalSignalAdd adds a dated reading to a signal that the brief defines.
var GoalSignalAdd = defineSafe("goal_signal_add", "Add a dated reading, with its source, to a signal the goal's brief defines", func(ctx context.Context, d Deps, in GoalSignalAddInput) (GoalSignalAddOutput, error) {
	doc, p, err := getGoal(ctx, d.Store, normID(in.ID))
	if err != nil {
		return GoalSignalAddOutput{}, err
	}
	value := strings.Join(strings.Fields(in.Value), " ")
	source := strings.Join(strings.Fields(in.Source), " ")
	if value == "" || source == "" {
		return GoalSignalAddOutput{}, errors.New("goal_signal_add: give value and source")
	}
	for _, f := range [][2]string{{"value", value}, {"source", source}} {
		if err := lintText(f[0], f[1]); err != nil {
			return GoalSignalAddOutput{}, err
		}
	}
	date := in.Date
	if date == "" {
		date = d.Store.Now().Local().Format(dateLayout)
	} else if err := checkDate("date", date); err != nil {
		return GoalSignalAddOutput{}, err
	}
	i := slices.IndexFunc(p.Signals, func(s model.Signal) bool { return strings.EqualFold(s.What, strings.TrimSpace(in.What)) })
	if i < 0 {
		names := make([]string, len(p.Signals))
		for k, s := range p.Signals {
			names[k] = s.What
		}
		return GoalSignalAddOutput{}, fmt.Errorf("goal_signal_add: %s has no signal %q (defined: %s); define it with doc_edit first", doc.ID, in.What, strings.Join(names, "; "))
	}
	s := &p.Signals[i]
	s.Readings = append(s.Readings, model.SignalReading{Date: model.Date(date), Value: value, Source: source})
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return GoalSignalAddOutput{}, err
	}
	return GoalSignalAddOutput{ID: doc.ID, What: s.What, Date: date, Readings: len(s.Readings), Rev: saved.Rev}, nil
})

// GoalRankInput is the input of goal_rank.
type GoalRankInput struct {
	ID   string `json:"id"`
	Rank int    `json:"rank"` // 1 is first; 0 clears the rank
}

// GoalRankOutput is the output of goal_rank.
type GoalRankOutput struct {
	ID   string `json:"id"`
	Rank int    `json:"rank"`
	Rev  string `json:"rev"`
}

// GoalRank sets the owner's rank of a goal. Value order reads it for ties
// among goals, after due dates.
var GoalRank = definePerson("goal_rank", "Set the owner's rank of a goal: 1 is first, 0 clears it. Breaks ties among goals after due dates. People only", func(ctx context.Context, d Deps, in GoalRankInput) (GoalRankOutput, error) {
	if in.Rank < 0 {
		return GoalRankOutput{}, errors.New("goal_rank: rank is negative")
	}
	doc, p, err := getGoal(ctx, d.Store, normID(in.ID))
	if err != nil {
		return GoalRankOutput{}, err
	}
	p.Rank = in.Rank
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return GoalRankOutput{}, err
	}
	return GoalRankOutput{ID: doc.ID, Rank: in.Rank, Rev: saved.Rev}, nil
})

// GoalCompareInput is the input of goal_compare.
type GoalCompareInput struct {
	IDs string `json:"ids,omitempty" query:"ids"` // goal ids, separated by commas or spaces; default every active goal
}

// SignalState is the latest reading of a signal.
type SignalState struct {
	What      string `json:"what"`
	Expected  string `json:"expected,omitempty"`
	EveryDays int    `json:"every_days,omitempty"`
	Last      string `json:"last,omitempty"`       // date of the newest reading
	LastValue string `json:"last_value,omitempty"` // what it read
	Source    string `json:"source,omitempty"`
	Due       string `json:"due,omitempty"` // date the next reading is due
	Overdue   bool   `json:"overdue,omitempty"`
}

// GoalCompareRow is one goal in the comparison.
type GoalCompareRow struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Status       string        `json:"status"`
	Due          string        `json:"due,omitempty"`
	Rank         int           `json:"rank,omitempty"`
	Validation   string        `json:"validation,omitempty"`
	Why          string        `json:"why,omitempty"`
	Benefit      string        `json:"benefit,omitempty"`
	Cost         string        `json:"cost,omitempty"`
	Confidence   int           `json:"confidence,omitempty"`
	Basis        string        `json:"basis,omitempty"`
	Alternatives int           `json:"alternatives,omitempty"` // options in the brief
	Unknowns     int           `json:"unknowns,omitempty"`
	Signals      []SignalState `json:"signals,omitempty"`
	PivotDate    string        `json:"pivot_date,omitempty"`
	PivotAction  string        `json:"pivot_action,omitempty"`
	KeyHolder    bool          `json:"key_holder,omitempty"`
}

// GoalCompareOutput is the output of goal_compare.
type GoalCompareOutput struct {
	Goals []GoalCompareRow `json:"goals"`
}

// GoalCompare shows goals side by side. It computes no score: the owner reads
// the fields. Rows come in the owner's rank, then due date, then id.
var GoalCompare = defineRead("goal_compare", "Show goals side by side: why, benefit, cost, confidence, signals, and pivot. No computed score", func(ctx context.Context, d Deps, in GoalCompareInput) (GoalCompareOutput, error) {
	today := d.Store.Now().Local().Format(dateLayout)
	var docs []model.Document
	if ids := strings.FieldsFunc(in.IDs, func(r rune) bool { return r == ',' || r == ' ' }); len(ids) > 0 {
		for _, id := range ids {
			doc, _, err := getGoal(ctx, d.Store, normID(id))
			if err != nil {
				return GoalCompareOutput{}, err
			}
			docs = append(docs, doc)
		}
	} else {
		refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeGoal})
		if err != nil {
			return GoalCompareOutput{}, err
		}
		for _, r := range refs {
			doc, err := d.Store.Get(ctx, r.ID)
			if err != nil {
				return GoalCompareOutput{}, err
			}
			if p := doc.Meta.(*model.Goal); p.Status == model.GoalActive {
				docs = append(docs, doc)
			}
		}
	}
	out := GoalCompareOutput{Goals: []GoalCompareRow{}}
	for _, doc := range docs {
		p := doc.Meta.(*model.Goal)
		row := GoalCompareRow{ID: doc.ID, Title: doc.Title, Status: string(p.Status), Due: string(p.Due), Rank: p.Rank,
			Validation: string(p.Validation), Alternatives: len(p.Alternatives), Unknowns: len(p.Unknowns)}
		if w := p.Why; w != nil {
			row.Why = w.Text
			if w.Benefit != nil {
				row.Benefit = w.Benefit.Text
			}
			if w.Cost != nil {
				row.Cost = w.Cost.Text
			}
		}
		if c := p.Confidence; c != nil {
			row.Confidence, row.Basis = c.Percent, c.Basis
		}
		if pv := p.Pivot; pv != nil {
			row.PivotDate, row.PivotAction = string(pv.Date), string(pv.Action)
		}
		row.KeyHolder = p.Ecosystem != nil && p.Ecosystem.KeyHolder
		for _, s := range p.Signals {
			st := SignalState{What: s.What, Expected: s.Expected, EveryDays: s.EveryDays}
			if last := s.Latest(); last != nil {
				st.Last, st.LastValue, st.Source = string(last.Date), last.Value, last.Source
			}
			st.Due, st.Overdue = signalDue(s, doc.Meta.Header().Created, today)
			row.Signals = append(row.Signals, st)
		}
		out.Goals = append(out.Goals, row)
	}
	slices.SortStableFunc(out.Goals, func(a, b GoalCompareRow) int {
		return cmp.Or(rankKey(a.Rank)-rankKey(b.Rank), cmp.Compare(dueKey(a.Due), dueKey(b.Due)), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
})

// rankKey puts an unranked goal after every ranked one.
func rankKey(r int) int {
	if r <= 0 {
		return 1 << 30
	}
	return r
}

func dueKey(d string) string {
	if d == "" {
		return "9999-99-99"
	}
	return d
}

// signalDue returns the date the next reading of s is due, and whether it is
// due today or earlier. A signal with no frequency is never due. With no
// reading yet, the count starts at the day the goal was created.
func signalDue(s model.Signal, created model.Date, today string) (string, bool) {
	if s.EveryDays <= 0 {
		return "", false
	}
	from := string(s.Last())
	if from == "" {
		from = string(created)
	}
	t, err := time.Parse(dateLayout, from)
	if err != nil {
		return "", false
	}
	due := t.AddDate(0, 0, s.EveryDays).Format(dateLayout)
	return due, due <= today
}

// GoalWatch is a goal date that has come: a pivot date, or a signal reading
// that is due. The agenda, tick, and health read it.
type GoalWatch struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Kind   string `json:"kind"` // pivot or signal
	Date   string `json:"date"`
	Reason string `json:"reason"`
}

// Kinds of GoalWatch.
const (
	WatchPivot  = "pivot"
	WatchSignal = "signal"
)

// goalWatches lists, for every active goal that is not deferred, its pivot
// date and the due date of each signal that has a frequency. A goal whose
// validation is stopped or pivoted is left out: its pivot has happened.
func goalWatches(ctx context.Context, s store.Store, today string) ([]GoalWatch, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeGoal})
	if err != nil {
		return nil, err
	}
	var out []GoalWatch
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		p := doc.Meta.(*model.Goal)
		if p.Status != model.GoalActive || p.IsDeferred(today) || !p.HasBrief() ||
			p.Validation == model.ValidationStopped || p.Validation == model.ValidationPivoted {
			continue
		}
		if pv := p.Pivot; pv != nil && pv.Date != "" {
			out = append(out, GoalWatch{ID: doc.ID, Title: doc.Title, Kind: WatchPivot, Date: string(pv.Date),
				Reason: fmt.Sprintf("date to decide on a change of course, %s: %s (%s)", ShortDate(string(pv.Date)), pv.Condition, pv.Action)})
		}
		for _, sg := range p.Signals {
			due, _ := signalDue(sg, doc.Meta.Header().Created, today)
			if due == "" {
				continue
			}
			out = append(out, GoalWatch{ID: doc.ID, Title: doc.Title, Kind: WatchSignal, Date: due,
				Reason: fmt.Sprintf("read %q by %s (every %d days)", sg.What, ShortDate(due), sg.EveryDays)})
		}
	}
	slices.SortFunc(out, func(a, b GoalWatch) int {
		return cmp.Or(cmp.Compare(a.Date, b.Date), cmp.Compare(a.ID, b.ID), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Reason, b.Reason))
	})
	return out, nil
}
