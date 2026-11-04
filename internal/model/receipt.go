package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReceiptCheck is receipt.check: what the owner said about a write.
type ReceiptCheck string

const (
	ReceiptUnchecked ReceiptCheck = "unchecked"
	ReceiptVerified  ReceiptCheck = "verified"
	ReceiptWrong     ReceiptCheck = "wrong"
)

// Enum lists the allowed values, for schema generation.
func (ReceiptCheck) Enum() []string { return enumStrings(ReceiptChecks) }

// ReceiptChecks lists every ReceiptCheck.
var ReceiptChecks = []ReceiptCheck{ReceiptUnchecked, ReceiptVerified, ReceiptWrong}

// Confidence is how sure an agent is of a write. It is a label on the receipt.
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// Enum lists the allowed values, for schema generation.
func (Confidence) Enum() []string { return enumStrings(Confidences) }

// Confidences lists every Confidence.
var Confidences = []Confidence{ConfidenceLow, ConfidenceMedium, ConfidenceHigh}

// Receipt is type: receipt, the record of one agent write to one document. It
// holds content, so it lives in the vault under receipts/ and follows the
// vault's retention, not events/. The before and after text is in the body
// (ReceiptContent), so a revert depends on no git history.
//
// Doc is an id and not a link: the document it names may be archived or gone.
type Receipt struct {
	Common     `yaml:",inline"`
	Actor      string       `yaml:"actor" json:"actor"`
	Op         string       `yaml:"op" json:"op"`
	Doc        string       `yaml:"doc" json:"doc"`
	Request    string       `yaml:"request,omitempty" json:"request,omitempty"`
	Why        string       `yaml:"why,omitempty" json:"why,omitempty"`
	Sources    []string     `yaml:"sources,omitempty" json:"sources,omitempty"`
	Verify     string       `yaml:"verify,omitempty" json:"verify,omitempty"`
	Confidence Confidence   `yaml:"confidence,omitempty" json:"confidence,omitempty"`
	Check      ReceiptCheck `yaml:"check" json:"check"`
	CheckedBy  string       `yaml:"checked_by,omitempty" json:"checked_by,omitempty"`
	CheckNote  string       `yaml:"check_note,omitempty" json:"check_note,omitempty"`
	// Undone is when the write was undone from this receipt, RFC 3339.
	Undone       string `yaml:"undone,omitempty" json:"undone,omitempty"`
	SelfReported bool   `yaml:"self_reported,omitempty" json:"self_reported,omitempty"`
	NotDuplicate string `yaml:"not_duplicate,omitempty" json:"not_duplicate,omitempty"`
	// OwnerSaid is the owner's words from the chat, when the agent gives them
	// (action_record). Para cannot see the chat: it is self-reported.
	OwnerSaid string `yaml:"owner_said,omitempty" json:"owner_said,omitempty"`
	// Surface is where the write came from (mcp, api, tick). Background is true
	// for a write made by background agent work: tick, or an agent that holds a
	// request.
	Surface string `yaml:"surface,omitempty" json:"surface,omitempty"`
	// At is when the write happened, RFC 3339 UTC, to the nanosecond. Created is only the day.
	At         string `yaml:"at,omitempty" json:"at,omitempty"`
	Background bool   `yaml:"background,omitempty" json:"background,omitempty"`
	// Align is set on an align_record receipt: what one comparison of a scope with
	// reality checked. Doc is then the goal or area aligned, or "vault".
	Align *AlignResult `yaml:"align,omitempty" json:"align,omitempty"`
}

// AlignResult is the outcome of one align run. Checked, Matched, and Differ
// count items of the vault; Unreachable names the sources the agent could not
// read. Proposal is the proposal that held the differences, from older runs.
type AlignResult struct {
	Checked     int      `yaml:"checked" json:"checked"`
	Matched     int      `yaml:"matched" json:"matched"`
	Differ      int      `yaml:"differ" json:"differ"`
	Unreachable []string `yaml:"unreachable,omitempty" json:"unreachable,omitempty"`
	Proposal    string   `yaml:"proposal,omitempty" json:"proposal,omitempty"`
}

// ReceiptContent is the fenced block of a receipt's body. An empty Before means
// the write created the document. Folder is where the document was before.
//
// Also holds the other documents of one write that changed several (a merge
// rewrites links across the vault): one receipt, and an undo restores all.
type ReceiptContent struct {
	Folder Folder        `yaml:"folder,omitempty" json:"folder,omitempty"`
	Before string        `yaml:"before,omitempty" json:"before,omitempty"`
	After  string        `yaml:"after,omitempty" json:"after,omitempty"`
	Also   []ReceiptPart `yaml:"also,omitempty" json:"also,omitempty"`
}

// ReceiptPart is one more document of a receipt: its id, the folder it was
// in, and its text before and after the write. An empty Before means the
// write created it.
type ReceiptPart struct {
	Doc    string `yaml:"doc" json:"doc"`
	Folder Folder `yaml:"folder,omitempty" json:"folder,omitempty"`
	Before string `yaml:"before,omitempty" json:"before,omitempty"`
	After  string `yaml:"after,omitempty" json:"after,omitempty"`
}

const receiptInfo = "yaml para-receipt"

// RenderReceiptBody writes the body of a receipt file. It is built as a proposal
// body is: text is indented in the YAML, so no document text can close the block.
func RenderReceiptBody(c ReceiptContent) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("model: receipt body: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return "", fmt.Errorf("model: receipt body: %w", err)
	}
	blockStyle(&node)
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return "", fmt.Errorf("model: receipt body: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("model: receipt body: %w", err)
	}
	return "Written by para. The text below is the document before and after one write.\n\n" + receiptFence + receiptInfo + "\n" + b.String() + receiptFence, nil
}

// ParseReceiptBody reads the fenced block of a receipt's body.
func ParseReceiptBody(body string) (ReceiptContent, error) {
	var c ReceiptContent
	open := "\n" + receiptFence + receiptInfo + "\n"
	i := strings.Index("\n"+body, open)
	if i < 0 {
		return c, errors.New("model: receipt body: no para-receipt block")
	}
	rest := body[i+len(open)-1:]
	end := strings.LastIndex(rest, "\n"+receiptFence)
	if end < 0 {
		end = strings.Index(rest, receiptFence)
	}
	if end < 0 {
		return c, errors.New("model: receipt body: block is not closed")
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &c); err != nil {
		return c, fmt.Errorf("model: receipt body: %w", err)
	}
	return c, nil
}

// ParseDocument reads text that Render wrote back into a Document.
func ParseDocument(id string, folder Folder, text string) (Document, error) {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return Document{}, fmt.Errorf("model: parse %s: no frontmatter", id)
	}
	front, rest, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return Document{}, fmt.Errorf("model: parse %s: frontmatter is not closed", id)
	}
	rest = strings.TrimLeft(rest, "\n")
	line, body, _ := strings.Cut(rest, "\n")
	title, ok := strings.CutPrefix(line, "# ")
	if !ok {
		return Document{}, fmt.Errorf("model: parse %s: no title", id)
	}
	return Decode(id, folder, []byte(front), title, strings.Trim(body, "\n"))
}

// RequestResolution is how a turn of agent work ended.
type RequestResolution string

const (
	ResolutionDone               RequestResolution = "done"
	ResolutionPartial            RequestResolution = "partial"
	ResolutionStuck              RequestResolution = "stuck"
	ResolutionBlocked            RequestResolution = "blocked"
	ResolutionNeedsClarification RequestResolution = "needs_clarification"
	ResolutionFailed             RequestResolution = "failed"
	ResolutionCancelled          RequestResolution = "cancelled"
)

// Enum lists the allowed values, for schema generation.
func (RequestResolution) Enum() []string { return enumStrings(RequestResolutions) }

// RequestResolutions lists every RequestResolution.
var RequestResolutions = []RequestResolution{ResolutionDone, ResolutionPartial, ResolutionStuck, ResolutionBlocked,
	ResolutionNeedsClarification, ResolutionFailed, ResolutionCancelled}

// NeedsOwner reports whether the work ended waiting for a person.
func (r RequestResolution) NeedsOwner() bool {
	return r == ResolutionStuck || r == ResolutionBlocked || r == ResolutionNeedsClarification
}
