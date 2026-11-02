package ops

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store"
)

// ruleMax is the longest rule line (docs/format.md § 6).
const ruleMax = 500

// RuleAddInput is the input of rule_add.
type RuleAddInput struct {
	Policy string `json:"policy"`
	Text   string `json:"text"`
}

// RuleAddOutput is the output of rule_add.
type RuleAddOutput struct {
	Rule `json:",inline"`
	Rev  string `json:"rev"`
}

// RuleAdd appends a rule to a policy summary. The id is the next free number
// in that policy; para owns it and never renumbers.
var RuleAdd = define("rule_add", "Add a rule with a stable id to a policy summary", func(ctx context.Context, d Deps, in RuleAddInput) (RuleAddOutput, error) {
	text, err := oneLine("text", in.Text)
	if err != nil {
		return RuleAddOutput{}, err
	}
	doc, _, err := getPolicy(ctx, d, in.Policy)
	if err != nil {
		return RuleAddOutput{}, err
	}
	id := fmt.Sprintf("R%d", nextRuleNumber(parseRules(doc.ID, doc.Body)))
	line := fmt.Sprintf("- %s %s: %s ^%s", id, doc.ID, text, id)
	if n := utf8.RuneCountInString(line) - 2; n > ruleMax {
		return RuleAddOutput{}, fmt.Errorf("rule is %d characters with id and block id; the limit is %d", n, ruleMax)
	}
	doc.Body = addRuleLine(doc.Body, line)
	doc, err = d.Store.Update(ctx, doc)
	if err != nil {
		return RuleAddOutput{}, err
	}
	return RuleAddOutput{Rule: Rule{Policy: doc.ID, ID: id, Text: text, Link: model.RuleLink(doc.ID, id)}, Rev: doc.Rev}, nil
})

// RuleListInput is the input of rule_list.
type RuleListInput struct {
	Policy string `json:"policy,omitempty"` // empty means every policy
}

// RuleListOutput is the output of rule_list.
type RuleListOutput struct {
	Rules []Rule `json:"rules"`
}

// RuleList lists rules.
var RuleList = define("rule_list", "List rules of one policy or of all policies", func(ctx context.Context, d Deps, in RuleListInput) (RuleListOutput, error) {
	ids := []string{in.Policy}
	if in.Policy == "" {
		refs, err := d.Store.List(ctx, store.Filter{Type: model.TypePolicy})
		if err != nil {
			return RuleListOutput{}, err
		}
		ids = ids[:0]
		for _, r := range refs {
			ids = append(ids, r.ID)
		}
	}
	out := RuleListOutput{Rules: []Rule{}}
	for _, id := range ids {
		doc, _, err := getPolicy(ctx, d, id)
		if err != nil {
			return RuleListOutput{}, err
		}
		out.Rules = append(out.Rules, parseRules(doc.ID, doc.Body)...)
	}
	return out, nil
})
