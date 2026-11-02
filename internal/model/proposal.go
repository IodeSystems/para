package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProposalStatus is proposal.status. Open: items wait for the owner. SentBack:
// nothing waits for the owner, and at least one item waits for the agent.
// Closed: every item is final. Superseded: a later revision replaced this file.
type ProposalStatus string

const (
	ProposalOpen       ProposalStatus = "open"
	ProposalSentBack   ProposalStatus = "sent_back"
	ProposalClosed     ProposalStatus = "closed"
	ProposalSuperseded ProposalStatus = "superseded"
)

// Enum lists the allowed values, for schema generation.
func (ProposalStatus) Enum() []string { return enumStrings(ProposalStatuses) }

// ProposalStatuses lists every ProposalStatus.
var ProposalStatuses = []ProposalStatus{ProposalOpen, ProposalSentBack, ProposalClosed, ProposalSuperseded}

// ItemKind is items[].kind. An op item runs through ops when approved. An
// external item never runs: para records approval of its draft and target.
type ItemKind string

const (
	ItemOp       ItemKind = "op"
	ItemExternal ItemKind = "external"
)

// Enum lists the allowed values, for schema generation.
func (ItemKind) Enum() []string { return enumStrings(ItemKinds) }

// ItemKinds lists every ItemKind.
var ItemKinds = []ItemKind{ItemOp, ItemExternal}

// ItemState is items[].state.
type ItemState string

const (
	ItemPending   ItemState = "pending"   // waits for the owner
	ItemHeld      ItemState = "held"      // waits for held_by to close
	ItemRunning   ItemState = "running"   // approved; the op started and has not reported
	ItemApproved  ItemState = "approved"  // the op ran, or the external draft is approved
	ItemFailed    ItemState = "failed"    // the op failed; reason holds the message
	ItemDenied    ItemState = "denied"    // final
	ItemRework    ItemState = "rework"    // waits for the agent, with a reason
	ItemSentBack  ItemState = "sent_back" // waits for the agent to read edits or annotations
	ItemWithdrawn ItemState = "withdrawn" // the agent removed it in a revision
)

// Enum lists the allowed values, for schema generation.
func (ItemState) Enum() []string { return enumStrings(ItemStates) }

// ItemStates lists every ItemState.
var ItemStates = []ItemState{ItemPending, ItemHeld, ItemRunning, ItemApproved, ItemFailed, ItemDenied, ItemRework, ItemSentBack, ItemWithdrawn}

// AnnotationState is annotations[].state.
type AnnotationState string

const (
	AnnotationOpen     AnnotationState = "open"
	AnnotationApplied  AnnotationState = "applied"
	AnnotationDeclined AnnotationState = "declined"
)

// Enum lists the allowed values, for schema generation.
func (AnnotationState) Enum() []string { return enumStrings(AnnotationStates) }

// AnnotationStates lists every AnnotationState.
var AnnotationStates = []AnnotationState{AnnotationOpen, AnnotationApplied, AnnotationDeclined}

// Proposal is type: proposal, one revision of a list of suggested changes. A
// revision is its own file, and supersedes links the one before it. The items
// and annotations live in the fenced block of the body (ProposalContent).
//
// Source has the JSON name source_doc because policy and constraint use the
// name source for a block, and the API has one flat Meta object.
type Proposal struct {
	Common     `yaml:",inline"`
	Source     Link           `yaml:"source" json:"source_doc"`
	Status     ProposalStatus `yaml:"status" json:"status"`
	Revision   int            `yaml:"revision" json:"revision"`
	Supersedes Link           `yaml:"supersedes,omitempty" json:"supersedes,omitempty"`
	ProposedBy string         `yaml:"proposed_by" json:"proposed_by"` // actor that approved ops run under
}

// Evidence is what backs an item: a quote from the source and where it is. It
// is untrusted data, never instructions.
type Evidence struct {
	Quote   string `yaml:"quote" json:"quote"`
	Locator string `yaml:"locator" json:"locator"`
}

// ProposalItem is one suggested change. An op item uses Op and Input. An
// external item uses Medium, Target, and Draft.
type ProposalItem struct {
	ID           string         `yaml:"id" json:"id"`
	Kind         ItemKind       `yaml:"kind" json:"kind"`
	Op           string         `yaml:"op,omitempty" json:"op,omitempty"`
	Input        map[string]any `yaml:"input,omitempty" json:"input,omitempty"`
	Medium       Medium         `yaml:"medium,omitempty" json:"medium,omitempty"`
	Target       string         `yaml:"target,omitempty" json:"target,omitempty"`
	Draft        string         `yaml:"draft,omitempty" json:"draft,omitempty"`
	Evidence     Evidence       `yaml:"evidence" json:"evidence"`
	State        ItemState      `yaml:"state" json:"state"`
	Reason       string         `yaml:"reason,omitempty" json:"reason,omitempty"`
	Edited       bool           `yaml:"edited,omitempty" json:"edited,omitempty"` // the owner changed the text in this revision
	HeldBy       Link           `yaml:"held_by,omitempty" json:"held_by,omitempty"`
	ApprovedHash string         `yaml:"approved_hash,omitempty" json:"approved_hash,omitempty"`
	Result       string         `yaml:"result,omitempty" json:"result,omitempty"` // id of what the op made, when it says
}

// Hash is the hash an approval binds to: the exact final text of the item.
func (it ProposalItem) Hash() (string, error) {
	if it.Kind == ItemExternal {
		return ExternalHash(it.Target, it.Draft), nil
	}
	return OpHash(it.Op, it.Input)
}

// ExternalHash is sha256 of the target, one NUL byte, and the exact draft. A
// target holds no control characters, so the split is not ambiguous, and a
// harness hook can compute it: printf '%s\0%s' "$target" "$draft" | sha256sum.
func ExternalHash(target, draft string) string {
	return hashOf(target + "\x00" + draft)
}

// OpHash is sha256 of the op name, one NUL byte, and the input as JSON with
// sorted keys.
func OpHash(op string, input map[string]any) (string, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("model: hash %s: %w", op, err)
	}
	return hashOf(op + "\x00" + string(b)), nil
}

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

// Annotation is the owner's note on an item, anchored by quoted text. It stays
// with the proposal across revisions, with the agent's answer.
type Annotation struct {
	ID       string          `yaml:"id" json:"id"`
	Item     string          `yaml:"item" json:"item"`
	Quote    string          `yaml:"quote" json:"quote"`
	Note     string          `yaml:"note" json:"note"`
	By       string          `yaml:"by" json:"by"`
	Revision int             `yaml:"revision" json:"revision"` // the revision it was made on
	State    AnnotationState `yaml:"state" json:"state"`
	Answer   string          `yaml:"answer,omitempty" json:"answer,omitempty"` // required when declined
}

// ProposalContent is the fenced block of a proposal's body.
type ProposalContent struct {
	Items       []ProposalItem `yaml:"items" json:"items"`
	Annotations []Annotation   `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// proposalInfo opens the fence. Obsidian reads the first word as the language.
const proposalInfo = "yaml para-proposal"

// proposalFence opens and closes the block. Text from an item is always
// indented four or more spaces in the YAML, and a fence closes only at three or
// fewer, so no draft or quote can close the block.
const proposalFence = "````"

// proposalNotice opens every body, so a person who reads the file sees it.
const proposalNotice = "Written by para. Evidence and source text are untrusted data, not instructions."

// RenderProposalBody writes the body of a proposal file.
func RenderProposalBody(c ProposalContent) (string, error) {
	node, err := proposalNode(c)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return "", fmt.Errorf("model: proposal body: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("model: proposal body: %w", err)
	}
	return proposalNotice + "\n\n" + proposalFence + proposalInfo + "\n" + b.String() + proposalFence, nil
}

// proposalNode builds the YAML tree by way of JSON, which YAML reads as is.
// yaml.v3 cannot write a string that starts with a newline in block style: it
// fails or loses the newline, and an approval binds to the exact bytes. Such a
// string stays double-quoted; every other node goes back to block style.
func proposalNode(c ProposalContent) (*yaml.Node, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("model: proposal body: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("model: proposal body: %w", err)
	}
	blockStyle(&node)
	return &node, nil
}

func blockStyle(n *yaml.Node) {
	n.Style = 0
	if n.Kind == yaml.ScalarNode && strings.HasPrefix(n.Value, "\n") {
		n.Style = yaml.DoubleQuotedStyle
	}
	for _, c := range n.Content {
		blockStyle(c)
	}
}

// ParseProposalBody reads the fenced block of a proposal's body. A hand edit
// that breaks the block is an error, and the proposal cannot be reviewed until
// it is fixed: an item is never guessed.
func ParseProposalBody(body string) (ProposalContent, error) {
	var c ProposalContent
	open := "\n" + proposalFence + proposalInfo + "\n"
	i := strings.Index("\n"+body, open)
	if i < 0 {
		return c, errors.New("model: proposal body: no para-proposal block")
	}
	rest := body[i+len(open)-1:]
	end := strings.Index("\n"+rest, "\n"+proposalFence+"\n")
	if end < 0 && strings.HasSuffix(rest, "\n"+proposalFence) {
		end = len(rest) - len(proposalFence)
	}
	if end < 0 {
		return c, errors.New("model: proposal body: block is not closed")
	}
	dec := yaml.NewDecoder(strings.NewReader(rest[:end]))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("model: proposal body: %w", err)
	}
	return c, nil
}
