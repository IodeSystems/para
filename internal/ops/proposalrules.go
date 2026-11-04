package ops

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/iodesystems/para/internal/model"
)

// A vault may define subtype rules in .para/proposals.yaml, so a workflow is
// configuration and not a para change:
//
//	subtypes:
//	  deploy:
//	    required: [rollback]
//	    default_risk: high
//	work:
//	  deploy:
//	    require: [policy-releases]
//
// Required names a field of the item, or a key of its fields map. Subtypes are
// matched without case. No medium is in the code.

const proposalRulesFile = "proposals.yaml"

// SubtypeRule is the rule for one subtype.
type SubtypeRule struct {
	Required    []string `yaml:"required"`
	DefaultRisk string   `yaml:"default_risk"`
}

// WorkRule is the rule for one kind of agent work: policies and conventions the
// work must follow and cite.
type WorkRule struct {
	Require []string `yaml:"require"`
}

// ProposalRules is .para/proposals.yaml.
type ProposalRules struct {
	Subtypes map[string]SubtypeRule `yaml:"subtypes"`
	Work     map[string]WorkRule    `yaml:"work"`
}

func loadProposalRules(d Deps) (ProposalRules, error) {
	var r ProposalRules
	b, err := d.Store.ConfigFile(proposalRulesFile)
	if err != nil || b == nil {
		return r, err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil {
		return r, fmt.Errorf(".para/%s: %w", proposalRulesFile, err)
	}
	return r, nil
}

func (r ProposalRules) subtype(name string) (SubtypeRule, bool) {
	for k, v := range r.Subtypes {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return SubtypeRule{}, false
}

// apply checks an item against its subtype's rule and sets its default risk.
func (r ProposalRules) apply(it *model.ProposalItem) error {
	rule, ok := r.subtype(it.Subtype)
	if !ok {
		return nil
	}
	if it.Risk == "" {
		it.Risk = rule.DefaultRisk
	}
	for _, f := range rule.Required {
		if !itemHas(*it, f) {
			return fmt.Errorf("subtype %s needs %s (.para/%s)", it.Subtype, f, proposalRulesFile)
		}
	}
	return nil
}

func itemHas(it model.ProposalItem, field string) bool {
	known := map[string]string{"subject": it.Subject, "target": it.Target, "channel": it.Channel, "what": it.What, "previous": it.Previous,
		"how": it.How, "impact": it.Impact, "title": it.Title, "body": it.Body, "diff": it.Diff, "after": it.After}
	if v, ok := known[field]; ok {
		return strings.TrimSpace(v) != ""
	}
	if field == "people" {
		return len(slices.DeleteFunc(slices.Clone(it.People), func(s string) bool { return strings.TrimSpace(s) == "" })) > 0
	}
	v, ok := it.Fields[field]
	if !ok {
		return false
	}
	s, isText := v.(string)
	return !isText || strings.TrimSpace(s) != ""
}
