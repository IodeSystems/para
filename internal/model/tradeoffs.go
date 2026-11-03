package model

import (
	"errors"
	"fmt"
	"strings"
)

// Evidence is what backs an item: a quote from the source and where it is. It
// is untrusted data, never instructions.
type Evidence struct {
	Quote   string `yaml:"quote" json:"quote"`
	Locator string `yaml:"locator" json:"locator"`
}

// NoSource is what an agent states for a claim with no source. A claim with
// this source forces a low confidence.
const NoSource = "no source found"

// LowConfidence is the highest percent that still counts as low.
const LowConfidence = 40

// Claim is one statement and where it comes from: a URL, a document id, or
// NoSource.
type Claim struct {
	Text   string `yaml:"text" json:"text"`
	Source string `yaml:"source" json:"source"`
}

// Tradeoffs weighs an item or an option. Confidence and Recommendation are
// percents; zero means not stated.
type Tradeoffs struct {
	Pros            []Claim `yaml:"pros,omitempty" json:"pros,omitempty"`
	Cons            []Claim `yaml:"cons,omitempty" json:"cons,omitempty"`
	Cost            *Claim  `yaml:"cost,omitempty" json:"cost,omitempty"`
	Benefit         *Claim  `yaml:"benefit,omitempty" json:"benefit,omitempty"`
	Confidence      int     `yaml:"confidence,omitempty" json:"confidence,omitempty"`
	ConfidenceBasis string  `yaml:"confidence_basis,omitempty" json:"confidence_basis,omitempty"`
	Recommendation  int     `yaml:"recommendation,omitempty" json:"recommendation,omitempty"`
}

// Claims lists every claim, in order.
func (t Tradeoffs) Claims() []Claim {
	out := append(append([]Claim{}, t.Pros...), t.Cons...)
	if t.Cost != nil {
		out = append(out, *t.Cost)
	}
	if t.Benefit != nil {
		out = append(out, *t.Benefit)
	}
	return out
}

// Check enforces the rule for cited claims: every claim has a source, and a
// claim with no source found holds the confidence to low.
func (t Tradeoffs) Check() error {
	if t.Confidence < 0 || t.Confidence > 100 || t.Recommendation < 0 || t.Recommendation > 100 {
		return errors.New("confidence and recommendation are percents from 0 to 100")
	}
	for _, c := range t.Claims() {
		if strings.TrimSpace(c.Text) == "" {
			return errors.New("a claim has no text")
		}
		source := strings.TrimSpace(c.Source)
		if source == "" {
			return fmt.Errorf("claim %q has no source: cite one, or state %q", c.Text, NoSource)
		}
		if strings.EqualFold(source, NoSource) && t.Confidence > LowConfidence {
			return fmt.Errorf("claim %q has no source, so confidence must be low: %d or under", c.Text, LowConfidence)
		}
	}
	return nil
}

// Example is a case that bears on an option: what happened elsewhere, with its
// source. Kind is success or failure.
type Example struct {
	Kind   string `yaml:"kind" json:"kind"`
	Text   string `yaml:"text" json:"text"`
	Source string `yaml:"source" json:"source"`
}

// OptionDetail weighs one option of a decision.
type OptionDetail struct {
	Name            string    `yaml:"name" json:"name"`
	Pros            []Claim   `yaml:"pros,omitempty" json:"pros,omitempty"`
	Cons            []Claim   `yaml:"cons,omitempty" json:"cons,omitempty"`
	Cost            *Claim    `yaml:"cost,omitempty" json:"cost,omitempty"`
	Benefit         *Claim    `yaml:"benefit,omitempty" json:"benefit,omitempty"`
	Confidence      int       `yaml:"confidence,omitempty" json:"confidence,omitempty"`
	ConfidenceBasis string    `yaml:"confidence_basis,omitempty" json:"confidence_basis,omitempty"`
	Recommendation  int       `yaml:"recommendation,omitempty" json:"recommendation,omitempty"`
	Examples        []Example `yaml:"examples,omitempty" json:"examples,omitempty"`
}

// Weighing returns the option's Tradeoffs.
func (o OptionDetail) Weighing() Tradeoffs {
	return Tradeoffs{Pros: o.Pros, Cons: o.Cons, Cost: o.Cost, Benefit: o.Benefit, Confidence: o.Confidence,
		ConfidenceBasis: o.ConfidenceBasis, Recommendation: o.Recommendation}
}

// Check enforces cited claims, and that each example has a kind and a source.
func (o OptionDetail) Check() error {
	if err := o.Weighing().Check(); err != nil {
		return err
	}
	for _, e := range o.Examples {
		if e.Kind != "success" && e.Kind != "failure" {
			return fmt.Errorf("example %q: kind %q: want success or failure", e.Text, e.Kind)
		}
		if strings.TrimSpace(e.Text) == "" || strings.TrimSpace(e.Source) == "" {
			return errors.New("an example needs text and a source")
		}
		if strings.EqualFold(strings.TrimSpace(e.Source), NoSource) && o.Confidence > LowConfidence {
			return fmt.Errorf("example %q has no source, so confidence must be low: %d or under", e.Text, LowConfidence)
		}
	}
	return nil
}
