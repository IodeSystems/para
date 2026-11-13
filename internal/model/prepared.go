package model

// PreparedKind is what an agent prepared for an item that needs the owner.
type PreparedKind string

const (
	PreparedDraft  PreparedKind = "draft"  // a reply: an outgoing io draft
	PreparedAnswer PreparedKind = "answer" // a proposed answer on a question
	PreparedFields PreparedKind = "fields" // the gaps of the item filled in
	PreparedBrief  PreparedKind = "brief"  // a meeting brief and agenda
	PreparedPlan   PreparedKind = "plan"   // a short "how to do it" plan
	// PreparedArtifact is what an agent produced for an obligation it owes:
	// a journal summary, a document, or an outgoing draft (ref).
	PreparedArtifact PreparedKind = "artifact"
	// PreparedClose is a proposed close of a task: the agent found it done or
	// no longer needed (Prepared.Close); the owner approves it.
	PreparedClose PreparedKind = "close"
	// PreparedNone says an agent looked and found nothing to prepare (for a
	// task: no obvious close). It is not ready; it keeps the prepare tick from
	// asking again until the item changes.
	PreparedNone PreparedKind = "none"
)

// PreparedKinds lists every PreparedKind.
var PreparedKinds = []PreparedKind{PreparedDraft, PreparedAnswer, PreparedFields, PreparedBrief, PreparedPlan, PreparedArtifact, PreparedClose, PreparedNone}

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
	// Close is the proposed close, for kind close.
	Close *CloseProposal `yaml:"close,omitempty" json:"close,omitempty"`
}

// CloseProposal is an agent's proposed close of a task: the outcome (done,
// or a close outcome such as dropped, duplicate, or superseded), why, the
// evidence the owner can open, the other inputs the outcome needs (fields:
// link for a duplicate), and how sure the agent is: Confidence is a percent
// and ConfidenceBasis says why. Without evidence the confidence is held at
// LowConfidence or under.
type CloseProposal struct {
	Outcome         string         `yaml:"outcome" json:"outcome"`
	Reason          string         `yaml:"reason" json:"reason"`
	Evidence        *Evidence      `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Fields          map[string]any `yaml:"fields,omitempty" json:"fields,omitempty"`
	Confidence      int            `yaml:"confidence" json:"confidence"`
	ConfidenceBasis string         `yaml:"confidence_basis" json:"confidence_basis"`
}

// HighCloseConfidence is the percent from which a proposed close is shown
// highlighted.
const HighCloseConfidence = 80
