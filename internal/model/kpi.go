package model

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/recur"
)

// KPIs are named metrics on a project (plan/paralog.md § 10): a target, the
// direction that is good, how often to read it, and dated readings, each with
// where it was read. Para never fetches a value: a person or an agent reads it
// with their own tools and passes it with its source.

// KPIDirection says which side of the target is on target.
type KPIDirection string

// KPI directions: up is on target at or above it, down at or below it, hold
// within its tolerance of it.
const (
	KPIUp   KPIDirection = "up"
	KPIDown KPIDirection = "down"
	KPIHold KPIDirection = "hold"
)

// KPIDirections lists every KPIDirection.
var KPIDirections = []KPIDirection{KPIUp, KPIDown, KPIHold}

// Enum lists the values, for schemas.
func (KPIDirection) Enum() []string { return enumStrings(KPIDirections) }

// KPIReadingsKept is how many readings of a KPI stay in the project file;
// tick moves older ones to the project's KPI archive (ops/kpi.go).
const KPIReadingsKept = 12

// KPIReading is one dated reading of a KPI, with where it was read.
type KPIReading struct {
	Date   Date    `yaml:"date" json:"date"`
	Value  float64 `yaml:"value" json:"value"`
	Source string  `yaml:"source" json:"source"`
}

// KPIOwnerSelf is the KPI owner word for the vault's owner.
const KPIOwnerSelf = "self"

// KPIAgentPrefix starts a KPI owner that is an agent: agent:<name>.
const KPIAgentPrefix = "agent:"

var (
	kpiAgentNameRe = regexp.MustCompile(`^[a-z0-9]+(?:[-_.][a-z0-9]+)*$`)
	kpiOwnerIDRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
)

// KPI is one named metric of a project. Cadence is how often it is read, in
// the recur grammar; with none it is never stale. Tolerance applies to hold.
// Owner says who acts when it is off target or stale: self (the owner: a
// Next card), agent:<name> (agent work), or a person id (shown only); empty
// is no one.
type KPI struct {
	Name      string       `yaml:"name" json:"name"`
	Unit      string       `yaml:"unit,omitempty" json:"unit,omitempty"`
	Target    float64      `yaml:"target" json:"target"`
	Direction KPIDirection `yaml:"direction" json:"direction"`
	Tolerance float64      `yaml:"tolerance,omitempty" json:"tolerance,omitempty"`
	Cadence   string       `yaml:"cadence,omitempty" json:"cadence,omitempty"`
	Owner     string       `yaml:"owner,omitempty" json:"owner,omitempty"`
	Readings  []KPIReading `yaml:"readings,omitempty" json:"readings,omitempty"`
}

// CheckKPIOwner checks the form of a KPI owner: empty, self, agent:<name>,
// or a document id (a person; ops checks that it is one).
func CheckKPIOwner(owner string) error {
	switch {
	case owner == "" || owner == KPIOwnerSelf:
		return nil
	case strings.HasPrefix(owner, KPIAgentPrefix):
		if kpiAgentNameRe.MatchString(strings.TrimPrefix(owner, KPIAgentPrefix)) {
			return nil
		}
	case kpiOwnerIDRe.MatchString(owner):
		return nil
	}
	return fmt.Errorf("owner %q: want self, a person id, or agent:<name> using lowercase letters, digits, and - _ . separators", owner)
}

// Latest returns the newest reading and the one before it (nil when absent).
// Readings of one date keep their file order: the later one is newer.
func (k KPI) Latest() (last, prev *KPIReading) {
	for i := range k.Readings {
		r := &k.Readings[i]
		switch {
		case last == nil || r.Date >= last.Date:
			last, prev = r, last
		case prev == nil || r.Date >= prev.Date:
			prev = r
		}
	}
	return last, prev
}

// OnTarget reports whether v is on target for the KPI's direction.
func (k KPI) OnTarget(v float64) bool {
	switch k.Direction {
	case KPIDown:
		return v <= k.Target
	case KPIHold:
		return math.Abs(v-k.Target) <= k.Tolerance
	}
	return v >= k.Target
}

// CheckKPIs enforces the rules of a project's KPIs: a unique name, a known
// direction, a cadence in the recur grammar, and dated readings with a source.
func CheckKPIs(ks []KPI) error {
	seen := map[string]bool{}
	for _, k := range ks {
		name := strings.TrimSpace(k.Name)
		if name == "" {
			return errors.New("kpis: a KPI has no name")
		}
		if seen[strings.ToLower(name)] {
			return fmt.Errorf("kpis: %q is named twice", name)
		}
		seen[strings.ToLower(name)] = true
		if !slices.Contains(KPIDirections, k.Direction) {
			return fmt.Errorf("kpis: %q: direction is %q; want up, down, or hold", name, k.Direction)
		}
		if k.Tolerance < 0 {
			return fmt.Errorf("kpis: %q: tolerance is negative", name)
		}
		if err := CheckKPIOwner(k.Owner); err != nil {
			return fmt.Errorf("kpis: %q: %w", name, err)
		}
		if k.Cadence != "" {
			if _, err := recur.Parse(k.Cadence); err != nil {
				return fmt.Errorf("kpis: %q: cadence: %w", name, err)
			}
		}
		for _, r := range k.Readings {
			if !briefDateRe.MatchString(string(r.Date)) || strings.TrimSpace(r.Source) == "" {
				return fmt.Errorf("kpis: %q: reading %q %v: want a date YYYY-MM-DD and a source", name, r.Date, r.Value)
			}
		}
	}
	return nil
}
