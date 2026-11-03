package ops

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// A vault may define rules for kinds of agent work in .para/work.yaml, so a
// workflow is configuration and not a para change:
//
//	work:
//	  deploy:
//	    require: [policy-releases]

const workRulesFile = "work.yaml"

// WorkRule is the rule for one kind of agent work: policies and conventions the
// work must follow and cite.
type WorkRule struct {
	Require []string `yaml:"require"`
}

// WorkRules is .para/work.yaml.
type WorkRules struct {
	Work map[string]WorkRule `yaml:"work"`
}

func loadWorkRules(d Deps) (WorkRules, error) {
	var r WorkRules
	b, err := d.Store.ConfigFile(workRulesFile)
	if err != nil || b == nil {
		return r, err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil {
		return r, fmt.Errorf(".para/%s: %w", workRulesFile, err)
	}
	return r, nil
}
