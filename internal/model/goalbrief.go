package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The goal brief. Every field is optional: a small goal needs none. The
// weighing fields reuse the one option shape of a decision (Tradeoffs and
// OptionDetail), so a choice among Alternatives is a decision question.

// Validation is project.validation: how far a goal has been checked.
type Validation string

const (
	ValidationIdea      Validation = "idea"
	ValidationValidated Validation = "validated"
	ValidationActive    Validation = "active"
	ValidationPivoted   Validation = "pivoted"
	ValidationStopped   Validation = "stopped"
)

// Validations lists every Validation.
var Validations = []Validation{ValidationIdea, ValidationValidated, ValidationActive, ValidationPivoted, ValidationStopped}

// Enum lists the allowed values, for schema generation.
func (Validation) Enum() []string { return enumStrings(Validations) }

// PivotAction is pivot.action: what happens when the condition holds.
type PivotAction string

const (
	PivotStop   PivotAction = "stop"
	PivotChange PivotAction = "change"
	PivotAsk    PivotAction = "ask"
)

// PivotActions lists every PivotAction.
var PivotActions = []PivotAction{PivotStop, PivotChange, PivotAsk}

// Enum lists the allowed values, for schema generation.
func (PivotAction) Enum() []string { return enumStrings(PivotActions) }

// GoalWhy says who benefits and how much. Text is the line; the claims carry
// sources.
type GoalWhy struct {
	Text      string `yaml:"text" json:"text"`
	Tradeoffs `yaml:",inline"`
}

// ConfidenceReading is one dated confidence of a goal.
type ConfidenceReading struct {
	Date    Date   `yaml:"date" json:"date"`
	Percent int    `yaml:"percent" json:"percent"`
	Basis   string `yaml:"basis,omitempty" json:"basis,omitempty"`
}

// GoalConfidence is how sure the owner or an agent is that the goal pays off,
// with the basis, and the dated history.
type GoalConfidence struct {
	Percent int                 `yaml:"percent" json:"percent"`
	Basis   string              `yaml:"basis,omitempty" json:"basis,omitempty"`
	History []ConfidenceReading `yaml:"history,omitempty" json:"history,omitempty"`
}

// Unknown is research the goal waits on. Link points at a question or a task.
type Unknown struct {
	Text string `yaml:"text" json:"text"`
	Link Link   `yaml:"link,omitempty" json:"link,omitempty"`
}

// SignalReading is one dated reading of a signal, with where it came from.
type SignalReading struct {
	Date   Date   `yaml:"date" json:"date"`
	Value  string `yaml:"value" json:"value"`
	Source string `yaml:"source" json:"source"`
}

// Signal is something to read to learn whether the goal works: what, how it is
// read, how often (days), and the value expected.
type Signal struct {
	What      string          `yaml:"what" json:"what"`
	How       string          `yaml:"how,omitempty" json:"how,omitempty"`
	EveryDays int             `yaml:"every_days,omitempty" json:"every_days,omitempty"`
	Expected  string          `yaml:"expected,omitempty" json:"expected,omitempty"`
	Readings  []SignalReading `yaml:"readings,omitempty" json:"readings,omitempty"`
}

// Latest returns the newest reading, or nil.
func (s Signal) Latest() *SignalReading {
	var best *SignalReading
	for i := range s.Readings {
		if best == nil || s.Readings[i].Date >= best.Date {
			best = &s.Readings[i]
		}
	}
	return best
}

// Last returns the date of the newest reading, or "".
func (s Signal) Last() Date {
	if r := s.Latest(); r != nil {
		return r.Date
	}
	return ""
}

// GoalPivot is the condition under which the goal stops, changes, or goes back
// to the owner, and the date to check it by. The weighing of that action uses
// the option shape.
type GoalPivot struct {
	Condition string      `yaml:"condition" json:"condition"`
	Date      Date        `yaml:"date,omitempty" json:"date,omitempty"`
	Action    PivotAction `yaml:"action" json:"action"`
	Tradeoffs `yaml:",inline"`
}

// Ecosystem is what the goal does to people and things around it.
type Ecosystem struct {
	Gains     []Claim  `yaml:"gains,omitempty" json:"gains,omitempty"`
	Costs     []Claim  `yaml:"costs,omitempty" json:"costs,omitempty"`
	Replaces  []string `yaml:"replaces,omitempty" json:"replaces,omitempty"`
	Reusable  []string `yaml:"reusable,omitempty" json:"reusable,omitempty"`
	KeyHolder bool     `yaml:"key_holder,omitempty" json:"key_holder,omitempty"`
}

// GoalBrief is the brief of a goal, in its frontmatter.
type GoalBrief struct {
	Why          *GoalWhy        `yaml:"why,omitempty" json:"why,omitempty"`
	Confidence   *GoalConfidence `yaml:"confidence,omitempty" json:"confidence,omitempty"`
	Alternatives []OptionDetail  `yaml:"alternatives,omitempty" json:"alternatives,omitempty"`
	Unknowns     []Unknown       `yaml:"unknowns,omitempty" json:"unknowns,omitempty"`
	Risks        []GoalRisk      `yaml:"risks,omitempty" json:"risks,omitempty"`
	Signals      []Signal        `yaml:"signals,omitempty" json:"signals,omitempty"`
	Pivot        *GoalPivot      `yaml:"pivot,omitempty" json:"pivot,omitempty"`
	Extensions   []string        `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	Ecosystem    *Ecosystem      `yaml:"ecosystem,omitempty" json:"ecosystem,omitempty"`
	// Rank is set by the owner: 1 is first. It breaks ties among goals in value order.
	Rank       int        `yaml:"rank,omitempty" json:"rank,omitempty"`
	Validation Validation `yaml:"validation,omitempty" json:"validation,omitempty"`
}

// DoNothing is the name of the alternative that every list of alternatives
// includes.
const DoNothing = "do nothing"

var briefDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// HasBrief reports whether the goal has any brief field set.
func (p *GoalBrief) HasBrief() bool {
	return p.Why != nil || p.Confidence != nil || len(p.Alternatives) > 0 || len(p.Unknowns) > 0 || len(p.Risks) > 0 ||
		len(p.Signals) > 0 || p.Pivot != nil || len(p.Extensions) > 0 || p.Ecosystem != nil || p.Rank > 0 || p.Validation != ""
}

// CheckBrief enforces the rules of the brief: percents in range, cited claims,
// do nothing among the alternatives, known pivot action, dated readings with a
// source. A goal with no brief passes.
func (p *GoalBrief) CheckBrief() error {
	if p.Why != nil {
		if strings.TrimSpace(p.Why.Text) == "" {
			return errors.New("why: text is empty")
		}
		if err := p.Why.Check(); err != nil {
			return fmt.Errorf("why: %w", err)
		}
	}
	if c := p.Confidence; c != nil {
		if c.Percent < 0 || c.Percent > 100 {
			return errors.New("confidence: percent is 0 to 100")
		}
		for _, h := range c.History {
			if !briefDateRe.MatchString(string(h.Date)) || h.Percent < 0 || h.Percent > 100 {
				return fmt.Errorf("confidence: history entry %q %d: want a date YYYY-MM-DD and a percent 0 to 100", h.Date, h.Percent)
			}
		}
	}
	if len(p.Alternatives) > 0 {
		doNothing := false
		for _, o := range p.Alternatives {
			if strings.TrimSpace(o.Name) == "" {
				return errors.New("alternatives: an option has no name")
			}
			if err := o.Check(); err != nil {
				return fmt.Errorf("alternatives: %q: %w", o.Name, err)
			}
			doNothing = doNothing || strings.EqualFold(strings.TrimSpace(o.Name), DoNothing)
		}
		if !doNothing {
			return fmt.Errorf("alternatives: include %q", DoNothing)
		}
	}
	for _, u := range p.Unknowns {
		if strings.TrimSpace(u.Text) == "" {
			return errors.New("unknowns: an unknown has no text")
		}
	}
	if err := CheckRisks(p.Risks); err != nil {
		return err
	}
	for _, s := range p.Signals {
		if strings.TrimSpace(s.What) == "" {
			return errors.New("signals: a signal has no what")
		}
		if s.EveryDays < 0 {
			return fmt.Errorf("signals: %q: every_days is negative", s.What)
		}
		for _, r := range s.Readings {
			if !briefDateRe.MatchString(string(r.Date)) || strings.TrimSpace(r.Value) == "" || strings.TrimSpace(r.Source) == "" {
				return fmt.Errorf("signals: %q: a reading needs a date YYYY-MM-DD, a value, and a source", s.What)
			}
		}
	}
	if pv := p.Pivot; pv != nil {
		if strings.TrimSpace(pv.Condition) == "" {
			return errors.New("pivot: condition is empty")
		}
		if pv.Date != "" && !briefDateRe.MatchString(string(pv.Date)) {
			return fmt.Errorf("pivot: date %q must be YYYY-MM-DD", pv.Date)
		}
		ok := false
		for _, a := range PivotActions {
			ok = ok || a == pv.Action
		}
		if !ok {
			return fmt.Errorf("pivot: action %q: want stop, change, or ask", pv.Action)
		}
		if err := pv.Check(); err != nil {
			return fmt.Errorf("pivot: %w", err)
		}
	}
	if e := p.Ecosystem; e != nil {
		if err := (Tradeoffs{Pros: e.Gains, Cons: e.Costs, Confidence: LowConfidence}).Check(); err != nil {
			return fmt.Errorf("ecosystem: %w", err)
		}
	}
	if p.Rank < 0 {
		return errors.New("rank is negative")
	}
	if p.Validation != "" {
		ok := false
		for _, v := range Validations {
			ok = ok || v == p.Validation
		}
		if !ok {
			return fmt.Errorf("validation %q: want one of idea, validated, active, pivoted, stopped", p.Validation)
		}
	}
	return nil
}
