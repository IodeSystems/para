package ops

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
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
