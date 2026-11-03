package model

import "gopkg.in/yaml.v3"

// Proposal is type: proposal, a legacy document. Para no longer writes or
// acts on proposals; vaults that hold them keep them as read-only records, and
// lint and read accept them. The items stay in the body, untouched.
type Proposal struct {
	Common     `yaml:",inline"`
	Source     Link   `yaml:"source" json:"source_doc"`
	Status     string `yaml:"status" json:"status"`
	Revision   int    `yaml:"revision" json:"revision"`
	Supersedes Link   `yaml:"supersedes,omitempty" json:"supersedes,omitempty"`
	ProposedBy string `yaml:"proposed_by" json:"proposed_by"`
}

// receiptFence opens and closes the block of a receipt body. Text from a
// document is always indented four or more spaces in the YAML, and a fence
// closes only at three or fewer, so no text can close the block.
const receiptFence = "````"

// blockStyle puts every node back to block style. yaml.v3 cannot write a
// string that starts with a newline in block style, so such a string stays
// double-quoted.
func blockStyle(n *yaml.Node) {
	n.Style = 0
	if n.Kind == yaml.ScalarNode && len(n.Value) > 0 && n.Value[0] == '\n' {
		n.Style = yaml.DoubleQuotedStyle
	}
	for _, c := range n.Content {
		blockStyle(c)
	}
}
