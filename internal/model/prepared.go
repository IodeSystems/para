package model

// PreparedKind is what an agent prepared for an item that needs the owner.
type PreparedKind string

const (
	PreparedDraft  PreparedKind = "draft"  // a reply: an outgoing io draft
	PreparedAnswer PreparedKind = "answer" // a proposed answer on a question
	PreparedFields PreparedKind = "fields" // the gaps of the item filled in
	PreparedBrief  PreparedKind = "brief"  // a meeting brief and agenda
	PreparedPlan   PreparedKind = "plan"   // a short "how to do it" plan
)

// PreparedKinds lists every PreparedKind.
var PreparedKinds = []PreparedKind{PreparedDraft, PreparedAnswer, PreparedFields, PreparedBrief, PreparedPlan}

// Enum lists the allowed values, for schema generation.
func (PreparedKind) Enum() []string {
	out := make([]string, len(PreparedKinds))
	for i, k := range PreparedKinds {
		out[i] = string(k)
	}
	return out
}

// Prepared records that an agent prepared the item so the owner only
// approves: who, when (RFC 3339), what kind, and the artifact (Ref) when it is
// another document, such as an io draft. Ref empty means the artifact is on
// the item itself (its fields, steps, or notes). Hash is ItemHash of the item
// when it was recorded: an edit of the item after it makes the preparation
// stale.
type Prepared struct {
	By   string       `yaml:"by" json:"by"`
	At   string       `yaml:"at" json:"at"`
	Kind PreparedKind `yaml:"kind" json:"kind"`
	Ref  Link         `yaml:"ref,omitempty" json:"ref,omitempty"`
	Hash string       `yaml:"hash" json:"hash"`
}
