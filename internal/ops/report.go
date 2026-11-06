package ops

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// The outcome report counts how work ended over a window: task closes by
// outcome and by actor (task_close events), and receipt undos (receipt_undo
// events). It reads events; it writes nothing.

// opReceiptUndo is the event of a write undone from its receipt; doc is the
// receipt, to the document.
const opReceiptUndo = "receipt_undo"

// ReportOutcomesInput is the input of report_outcomes. The window is from
// since to until, both dates and both in it; default the last 30 days.
type ReportOutcomesInput struct {
	Since string `json:"since,omitempty" query:"since"` // YYYY-MM-DD; default 30 days before until
	Until string `json:"until,omitempty" query:"until"` // YYYY-MM-DD; default today
}

// OutcomeCount is a count of one outcome, or of one actor's closes.
type OutcomeCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// ActorOutcomes is one actor's task closes by outcome.
type ActorOutcomes struct {
	Actor    string         `json:"actor"`
	Total    int            `json:"total"`
	Outcomes []OutcomeCount `json:"outcomes"`
}

// UndoRow is one write undone from its receipt.
type UndoRow struct {
	Receipt string    `json:"receipt"`
	Doc     string    `json:"doc"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
}

// ReportOutcomesOutput is the output of report_outcomes.
type ReportOutcomesOutput struct {
	Since    string          `json:"since"`
	Until    string          `json:"until"`
	Closes   int             `json:"closes"`
	Outcomes []OutcomeCount  `json:"outcomes"` // every outcome, in model order, zero included
	ByActor  []ActorOutcomes `json:"by_actor"`
	Undos    []UndoRow       `json:"undos"`
}

// ReportOutcomes counts how work ended over a window.
var ReportOutcomes = defineRead("report_outcomes", "Report how work ended over a window (since, until; default the last 30 days): task closes by outcome and by actor, and writes undone from receipts", func(ctx context.Context, d Deps, in ReportOutcomesInput) (ReportOutcomesOutput, error) {
	loc := time.Local
	until := d.Store.Now().In(loc)
	if in.Until != "" {
		t, err := time.ParseInLocation(dateLayout, in.Until, loc)
		if err != nil {
			return ReportOutcomesOutput{}, fmt.Errorf("until %q: want YYYY-MM-DD", in.Until)
		}
		until = t
	}
	end := time.Date(until.Year(), until.Month(), until.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	start := end.AddDate(0, 0, -30)
	if in.Since != "" {
		t, err := time.ParseInLocation(dateLayout, in.Since, loc)
		if err != nil {
			return ReportOutcomesOutput{}, fmt.Errorf("since %q: want YYYY-MM-DD", in.Since)
		}
		start = t
	}
	if !start.Before(end) {
		return ReportOutcomesOutput{}, errors.New("since is after until")
	}
	out := ReportOutcomesOutput{Since: start.Format(dateLayout), Until: end.AddDate(0, 0, -1).Format(dateLayout),
		Outcomes: []OutcomeCount{}, ByActor: []ActorOutcomes{}, Undos: []UndoRow{}}
	inWindow := func(t time.Time) bool { return !t.Before(start) && t.Before(end) }
	evs, err := d.Store.Events(ctx, math.MaxInt32)
	if err != nil {
		return out, err
	}
	byOutcome := map[string]int{}
	byActor := map[string]map[string]int{}
	for _, e := range evs {
		if !inWindow(e.TS) {
			continue
		}
		switch e.Op {
		case opTaskClose:
			out.Closes++
			byOutcome[e.To]++
			if byActor[e.Actor] == nil {
				byActor[e.Actor] = map[string]int{}
			}
			byActor[e.Actor][e.To]++
		case opReceiptUndo:
			out.Undos = append(out.Undos, UndoRow{Receipt: e.Doc, Doc: e.To, By: e.Actor, At: e.TS})
		}
	}
	for _, o := range model.TaskOutcomes {
		out.Outcomes = append(out.Outcomes, OutcomeCount{Key: string(o), Count: byOutcome[string(o)]})
	}
	for actor, m := range byActor {
		a := ActorOutcomes{Actor: actor, Outcomes: []OutcomeCount{}}
		for _, o := range model.TaskOutcomes {
			if n := m[string(o)]; n > 0 {
				a.Outcomes = append(a.Outcomes, OutcomeCount{Key: string(o), Count: n})
				a.Total += n
			}
		}
		out.ByActor = append(out.ByActor, a)
	}
	slices.SortFunc(out.ByActor, func(a, b ActorOutcomes) int {
		if a.Total != b.Total {
			return b.Total - a.Total
		}
		return strings.Compare(a.Actor, b.Actor)
	})
	slices.SortFunc(out.Undos, func(a, b UndoRow) int { return b.At.Compare(a.At) })
	return out, nil
})
