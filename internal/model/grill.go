package model

import (
	"fmt"
	"slices"
	"strings"
)

// Risks and assumptions. A goal brief lists risks; a goal or a task lists
// assumptions. Both are optional. The grill writes them; the recheck checks
// assumptions again.

// Level is low, medium, or high: the likelihood and impact of a risk.
type Level string

const (
	LevelLow    Level = "low"
	LevelMedium Level = "medium"
	LevelHigh   Level = "high"
)

// Levels lists every Level.
var Levels = []Level{LevelLow, LevelMedium, LevelHigh}

// Enum lists the allowed values, for schema generation.
func (Level) Enum() []string { return enumStrings(Levels) }

// AssumptionStatus is what the last check of an assumption found.
type AssumptionStatus string

const (
	AssumptionHolds   AssumptionStatus = "holds"
	AssumptionChanged AssumptionStatus = "changed"
	AssumptionUnknown AssumptionStatus = "unknown"
)

// AssumptionStatuses lists every AssumptionStatus.
var AssumptionStatuses = []AssumptionStatus{AssumptionHolds, AssumptionChanged, AssumptionUnknown}

// Enum lists the allowed values, for schema generation.
func (AssumptionStatus) Enum() []string { return enumStrings(AssumptionStatuses) }

// GoalRisk is one risk to a goal: what can go wrong, how likely, how bad, who
// owns it (a link or a name), what reduces it, and where it was said.
type GoalRisk struct {
	Text       string `yaml:"text" json:"text"`
	Likelihood Level  `yaml:"likelihood,omitempty" json:"likelihood,omitempty"`
	Impact     Level  `yaml:"impact,omitempty" json:"impact,omitempty"`
	Owner      string `yaml:"owner,omitempty" json:"owner,omitempty"`
	Mitigation string `yaml:"mitigation,omitempty" json:"mitigation,omitempty"`
	Source     string `yaml:"source,omitempty" json:"source,omitempty"`
}

// Assumption is something a goal or a task takes as true. Checked is the date
// of the last check, By who checked, Status what the check found, and Fact what
// is true now when the status is changed.
type Assumption struct {
	Text    string           `yaml:"text" json:"text"`
	Source  string           `yaml:"source,omitempty" json:"source,omitempty"`
	Checked Date             `yaml:"checked,omitempty" json:"checked,omitempty"`
	By      string           `yaml:"by,omitempty" json:"by,omitempty"`
	Status  AssumptionStatus `yaml:"status,omitempty" json:"status,omitempty"`
	Fact    string           `yaml:"fact,omitempty" json:"fact,omitempty"`
}

// SameText reports whether two texts name the same risk or assumption: equal
// after case and spaces are folded.
func SameText(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

// CheckRisks enforces the rules of a list of risks.
func CheckRisks(rs []GoalRisk) error {
	for i, r := range rs {
		if strings.TrimSpace(r.Text) == "" {
			return fmt.Errorf("risks: item %d has no text", i+1)
		}
		for _, l := range []Level{r.Likelihood, r.Impact} {
			if l != "" && !slices.Contains(Levels, l) {
				return fmt.Errorf("risks: %q: %q: want low, medium, or high", r.Text, l)
			}
		}
		for _, o := range rs[:i] {
			if SameText(o.Text, r.Text) {
				return fmt.Errorf("risks: %q is listed twice", r.Text)
			}
		}
	}
	return nil
}

// CheckAssumptions enforces the rules of a list of assumptions.
func CheckAssumptions(as []Assumption) error {
	for i, a := range as {
		if strings.TrimSpace(a.Text) == "" {
			return fmt.Errorf("assumptions: item %d has no text", i+1)
		}
		if a.Checked != "" && !briefDateRe.MatchString(string(a.Checked)) {
			return fmt.Errorf("assumptions: %q: checked %q must be YYYY-MM-DD", a.Text, a.Checked)
		}
		if a.Status != "" && !slices.Contains(AssumptionStatuses, a.Status) {
			return fmt.Errorf("assumptions: %q: status %q: want holds, changed, or unknown", a.Text, a.Status)
		}
		for _, o := range as[:i] {
			if SameText(o.Text, a.Text) {
				return fmt.Errorf("assumptions: %q is listed twice", a.Text)
			}
		}
	}
	return nil
}
