package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
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

// ItemKind is items[].kind. A para item is a vault change: it runs through ops
// when approved. A message or an action is an outward act: it never runs, para
// records approval of its exact text and an agent carries it out with its own
// tools. Each kind has a free-text subtype; no medium is in the code.
//
// op and external are the old words. Files that hold them read as para and
// action, and proposal_add takes them with the old, smaller set of fields.
type ItemKind string

const (
	ItemMessage ItemKind = "message"
	ItemAction  ItemKind = "action"
	ItemPara    ItemKind = "para"

	// A question asks the owner; its answer is the record. A doc edit changes
	// a document outside the vault, often a tracked document; an agent carries
	// it out. Upstream shows the change of a tracked document and its
	// comments: para runs it when approved (seen, and what each comment
	// becomes), like a vault change.
	ItemQuestion ItemKind = "question"
	ItemDocEdit  ItemKind = "doc_edit"
	ItemUpstream ItemKind = "upstream"

	ItemOp       ItemKind = "op"
	ItemExternal ItemKind = "external"
)

// Enum lists the allowed values, for schema generation.
func (ItemKind) Enum() []string { return enumStrings(ItemKinds) }

// ItemKinds lists every ItemKind an input may name.
var ItemKinds = []ItemKind{ItemMessage, ItemAction, ItemPara, ItemQuestion, ItemDocEdit, ItemUpstream, ItemOp, ItemExternal}

// Outward reports whether the kind is an act outside the vault.
func (k ItemKind) Outward() bool {
	return k == ItemMessage || k == ItemAction || k == ItemExternal || k == ItemDocEdit
}

// ItemState is items[].state.
type ItemState string

const (
	ItemPending   ItemState = "pending"   // waits for the owner
	ItemHeld      ItemState = "held"      // waits for held_by to close
	ItemRunning   ItemState = "running"   // approved; the op started and has not reported
	ItemApproved  ItemState = "approved"  // the op ran or waits its turn, or the outward act is approved and not done
	ItemDone      ItemState = "done"      // the outward act was carried out and reported
	ItemFailed    ItemState = "failed"    // the op or the act failed; reason holds the message
	ItemHalted    ItemState = "halted"    // an earlier item of its bundle failed, so this one did not run
	ItemDenied    ItemState = "denied"    // final
	ItemSentBack  ItemState = "sent_back" // waits for the agent
	ItemWithdrawn ItemState = "withdrawn" // the agent removed it in a revision
	ItemAnswered  ItemState = "answered"  // a question the owner answered
	ItemClaimed   ItemState = "claimed"   // an approved outward item an agent is carrying out
	// ItemSkipped is read in older files only: para no longer writes it (a
	// skip is navigation in one web visit, stored nowhere). It waits for the
	// owner as pending does.
	ItemSkipped ItemState = "skipped"
	// ItemMoved is an item vault upgrade step dossiers moved out of its
	// dossier: Result names where it went (a question or an io draft).
	ItemMoved ItemState = "moved"

	// itemRework is the old word for sent_back, in older files.
	itemRework ItemState = "rework"
)

// Enum lists the allowed values, for schema generation.
func (ItemState) Enum() []string { return enumStrings(ItemStates) }

// ItemStates lists every ItemState.
var ItemStates = []ItemState{ItemPending, ItemHeld, ItemRunning, ItemApproved, ItemDone, ItemFailed, ItemHalted, ItemDenied, ItemSentBack, ItemWithdrawn, ItemAnswered, ItemClaimed, ItemSkipped, ItemMoved}

// TurnKind is thread[].kind. Change, clarify, investigate, and redirect wait for
// the agent's answer; the others are the record of what the owner did. Redirect
// is the old word for change, and send_back is only read from older files.
type TurnKind string

const (
	TurnChange      TurnKind = "change"
	TurnClarify     TurnKind = "clarify"
	TurnInvestigate TurnKind = "investigate"
	TurnRedirect    TurnKind = "redirect"
	TurnEdit        TurnKind = "edit"
	TurnSendBack    TurnKind = "send_back"
	TurnClose       TurnKind = "close"
	TurnReply       TurnKind = "reply" // the agent's reply to an owner's comment
)

// Enum lists the allowed values, for schema generation.
func (TurnKind) Enum() []string { return enumStrings(TurnKinds) }

// TurnKinds lists every TurnKind.
var TurnKinds = []TurnKind{TurnChange, TurnClarify, TurnInvestigate, TurnRedirect, TurnEdit, TurnSendBack, TurnClose, TurnReply}

// TurnState is thread[].state. Failed is a redirect the agent could not resolve.
type TurnState string

const (
	TurnOpen     TurnState = "open"
	TurnApplied  TurnState = "applied"
	TurnDeclined TurnState = "declined"
	TurnFailed   TurnState = "failed"
	TurnNoted    TurnState = "noted" // needs no answer
)

// Enum lists the allowed values, for schema generation.
func (TurnState) Enum() []string { return enumStrings(TurnStates) }

// TurnStates lists every TurnState.
var TurnStates = []TurnState{TurnOpen, TurnApplied, TurnDeclined, TurnFailed, TurnNoted}

// Reversible is items[].reversible.
type Reversible string

const (
	ReversibleYes    Reversible = "yes"
	ReversibleNo     Reversible = "no"
	ReversiblePartly Reversible = "partly"
)

// Enum lists the allowed values, for schema generation.
func (Reversible) Enum() []string { return enumStrings(Reversibles) }

// Reversibles lists every Reversible.
var Reversibles = []Reversible{ReversibleYes, ReversibleNo, ReversiblePartly}

// Proposal is type: proposal, one revision of a list of suggested changes. A
// revision is its own file, and supersedes links the one before it. The items
// live in the fenced block of the body (ProposalContent).
//
// Source has the JSON name source_doc because policy and constraint use the
// name source for a block, and the API has one flat Meta object.
type Proposal struct {
	Common `yaml:",inline"`
	// Source is optional on a dossier: an investigation may have no one source.
	Source Link `yaml:"source,omitempty" json:"source_doc"`
	// SourcePath is free text for provenance, such as the path of the file the
	// proposal came from. Source is the document id.
	SourcePath string         `yaml:"source_path,omitempty" json:"source_path,omitempty"`
	Status     ProposalStatus `yaml:"status" json:"status"`
	Revision   int            `yaml:"revision" json:"revision"`
	Supersedes Link           `yaml:"supersedes,omitempty" json:"supersedes,omitempty"`
	ProposedBy string         `yaml:"proposed_by" json:"proposed_by"` // actor that approved ops run under
}

// Evidence is what backs an item: a quote from the source and where it is. It
// is untrusted data, never instructions. The owner must be able to check it:
// Locator is an http(s) URL or a vault reference, or Via names a private
// connector item and Verify tells the owner how to find it themself.
type Evidence struct {
	Quote   string `yaml:"quote" json:"quote"`
	Locator string `yaml:"locator" json:"locator"`
	Via     *IOVia `yaml:"via,omitempty" json:"via,omitempty"`
	Verify  string `yaml:"verify,omitempty" json:"verify,omitempty"`
}

// Ways the owner can check evidence (Evidence.Check).
const (
	CheckURL   = "url"
	CheckVault = "vault"
	CheckVia   = "via"
	CheckFile  = "file"
)

var (
	vaultIDRe   = regexp.MustCompile(`^\[?\[?([a-z][a-z0-9]*(?:-[a-z0-9]+)+)\]?\]?$`)
	vaultPathRe = regexp.MustCompile(`^([a-z][a-z0-9_-]*/[^\s:]+\.md)(?::\d+)?$`)
)

// IsWebURL reports whether s is an absolute http or https URL with a host.
func IsWebURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && !strings.ContainsAny(s, " \t\n")
}

// VaultRef returns the document id or vault path a locator names, or "".
// A path may end in :line.
func VaultRef(s string) string {
	s = strings.TrimSpace(s)
	if m := vaultPathRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	if m := vaultIDRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// IsLocalPath reports whether s has the form of a file path on this machine:
// absolute, or starting with ~/. It does not check that the file exists.
func IsLocalPath(s string) bool {
	s = strings.TrimSpace(s)
	return (strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~/")) && !strings.ContainsAny(s, "\n\t")
}

// Check says how the owner can check the evidence: CheckURL, CheckVault,
// CheckFile, CheckVia, or "" when it gives no way.
func (e Evidence) Check() string {
	switch {
	case IsWebURL(e.Locator):
		return CheckURL
	case VaultRef(e.Locator) != "":
		return CheckVault
	case IsLocalPath(e.Locator):
		return CheckFile
	case e.Via != nil && strings.TrimSpace(e.Via.Source) != "" && strings.TrimSpace(e.Via.Ref) != "" && strings.TrimSpace(e.Verify) != "":
		return CheckVia
	}
	return ""
}

// EvidenceForms names the locators the owner can check, for messages.
const EvidenceForms = "give an http(s) URL (a Slack, Gmail, GitHub, or Jira item has a permalink: use it), a vault id or path (io/io-x.md:12), " +
	"an absolute path (or ~/path) to a file on this machine, or via {source, ref} for a private connector item with verify, a sentence that tells the owner how to find it themself"

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

// Turn is one entry of an item's conversation. It stays with the item across
// revisions, with the agent's answer. Quote is optional: the text it is about.
type Turn struct {
	ID       string    `yaml:"id" json:"id"`
	Kind     TurnKind  `yaml:"kind" json:"kind"`
	Quote    string    `yaml:"quote,omitempty" json:"quote,omitempty"`
	Text     string    `yaml:"text" json:"text"`
	By       string    `yaml:"by" json:"by"`
	Revision int       `yaml:"revision" json:"revision"` // the revision it was made on
	State    TurnState `yaml:"state" json:"state"`
	Answer   string    `yaml:"answer,omitempty" json:"answer,omitempty"` // required when declined or failed
}

// Execution is what an agent reported after it carried out an approved item.
// Filled holds the placeholder values used, and nothing else may differ from
// the approved text. SelfReported is true: para cannot see the act.
type Execution struct {
	Result       string         `yaml:"result" json:"result"` // done or failed
	URL          string         `yaml:"url,omitempty" json:"url,omitempty"`
	Locator      string         `yaml:"locator,omitempty" json:"locator,omitempty"`
	By           string         `yaml:"by" json:"by"`
	At           string         `yaml:"at" json:"at"`
	Receipt      string         `yaml:"receipt,omitempty" json:"receipt,omitempty"`
	SelfReported bool           `yaml:"self_reported,omitempty" json:"self_reported,omitempty"`
	Filled       map[string]any `yaml:"filled,omitempty" json:"filled,omitempty"`
}

// ProposalItem is one suggested change. Which fields apply depends on Kind:
//
//	message: People and/or Channel, optional Subject, Body.
//	action:  What, Previous (link to the state it changes), How, and Title,
//	         Body, or Diff as the subtype needs. Target names where it lands.
//	para:    Op and Input, with How (the change) and Impact in words.
type ProposalItem struct {
	ID      string   `yaml:"id" json:"id"`
	Kind    ItemKind `yaml:"kind" json:"kind"`
	Subtype string   `yaml:"subtype,omitempty" json:"subtype,omitempty"` // free text: email, pr, deploy
	People  []string `yaml:"people,omitempty" json:"people,omitempty"`
	Channel string   `yaml:"channel,omitempty" json:"channel,omitempty"`
	Subject string   `yaml:"subject,omitempty" json:"subject,omitempty"`
	// IO is the id of an outgoing io draft that holds a message's text. Para
	// fills People, Subject, and Body from it when it reads the dossier, and
	// writes an edit back to it; the block itself holds none of them.
	IO     string `yaml:"io,omitempty" json:"io,omitempty"`
	Target string `yaml:"target,omitempty" json:"target,omitempty"`
	What   string `yaml:"what,omitempty" json:"what,omitempty"`
	// Previous is a link to the state an action changes, when there is one.
	Previous string         `yaml:"previous,omitempty" json:"previous,omitempty"`
	How      string         `yaml:"how,omitempty" json:"how,omitempty"`
	Impact   string         `yaml:"impact,omitempty" json:"impact,omitempty"`
	Title    string         `yaml:"title,omitempty" json:"title,omitempty"`
	Body     string         `yaml:"body,omitempty" json:"body,omitempty"`
	Diff     string         `yaml:"diff,omitempty" json:"diff,omitempty"`
	Op       string         `yaml:"op,omitempty" json:"op,omitempty"`
	Input    map[string]any `yaml:"input,omitempty" json:"input,omitempty"`
	// Fields holds what a subtype rule in .para/proposals.yaml asks for, such as
	// rollback on a deploy.
	Fields map[string]any `yaml:"fields,omitempty" json:"fields,omitempty"`

	Evidence  Evidence `yaml:"evidence" json:"evidence"`
	Tradeoffs `yaml:",inline"`
	// Reversible says whether the item can be undone, ReversibleHow how.
	Reversible    Reversible `yaml:"reversible,omitempty" json:"reversible,omitempty"`
	ReversibleHow string     `yaml:"reversible_how,omitempty" json:"reversible_how,omitempty"`
	// Risk is the default_risk of a subtype rule. Informational.
	Risk string `yaml:"risk,omitempty" json:"risk,omitempty"`
	// After names an item that must be done first. Bundle groups items that are
	// approved and denied as one. Choice groups alternatives: approving one
	// closes the others. For names the task or Ask the item works on, where a
	// redirect's result is recorded.
	After  string `yaml:"after,omitempty" json:"after,omitempty"`
	Bundle string `yaml:"bundle,omitempty" json:"bundle,omitempty"`
	Choice string `yaml:"choice,omitempty" json:"choice,omitempty"`
	For    string `yaml:"for,omitempty" json:"for,omitempty"`
	// Group is a label for bulk approve, such as a type in an import. It only
	// groups; unlike Bundle it ties nothing together.
	Group string `yaml:"group,omitempty" json:"group,omitempty"`
	// NotDuplicate says why a create is not a likely match. It goes to the receipt.
	NotDuplicate string `yaml:"not_duplicate,omitempty" json:"not_duplicate,omitempty"`
	// Tags are topics, as on any vault document; the dossier's own tags
	// apply to every item too (ItemTags). They do not change the item's hash.
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// DeferUntil puts an item that waits for the owner off until a date
	// (YYYY-MM-DD) or someday, as Deferral does a task; DeferredReason says
	// why. Para's: an agent sets them with defer, never in a block.
	DeferUntil     string `yaml:"defer_until,omitempty" json:"defer_until,omitempty"`
	DeferredReason string `yaml:"deferred_reason,omitempty" json:"deferred_reason,omitempty"`
	// SkippedAt and SkipBatch are an older record of a skip, read and ignored:
	// para no longer writes them, and keeps what an older file holds.
	SkippedAt string `yaml:"skipped_at,omitempty" json:"skipped_at,omitempty"`
	SkipBatch string `yaml:"skip_batch,omitempty" json:"skip_batch,omitempty"`

	State        ItemState `yaml:"state" json:"state"`
	Reason       string    `yaml:"reason,omitempty" json:"reason,omitempty"`
	Edited       bool      `yaml:"edited,omitempty" json:"edited,omitempty"` // the owner changed the text in this revision
	HeldBy       Link      `yaml:"held_by,omitempty" json:"held_by,omitempty"`
	ApprovedHash string    `yaml:"approved_hash,omitempty" json:"approved_hash,omitempty"`
	Result       string    `yaml:"result,omitempty" json:"result,omitempty"` // id of what the op made, when it says
	// Waits marks an approved para item whose After is not done yet: its op has
	// not run.
	Waits     bool       `yaml:"waits,omitempty" json:"waits,omitempty"`
	Execution *Execution `yaml:"execution,omitempty" json:"execution,omitempty"`
	// Decided is how the owner decided the item, last.
	Decided *ItemDecision `yaml:"decided,omitempty" json:"decided,omitempty"`
	// Thread holds the agent's replies to the owner's comments (kind reply),
	// and turns read from older files.
	Thread []Turn `yaml:"thread,omitempty" json:"thread,omitempty"`

	// Question: options to pick from, more than one when multi.
	Multi         bool           `yaml:"multi,omitempty" json:"multi,omitempty"`
	Options       []string       `yaml:"options,omitempty" json:"options,omitempty"`
	Recommended   string         `yaml:"recommended,omitempty" json:"recommended,omitempty"`
	OptionDetails []OptionDetail `yaml:"option_details,omitempty" json:"option_details,omitempty"`
	// Doc edit: the range of target it changes, its text now, and the new text.
	Range string `yaml:"range,omitempty" json:"range,omitempty"`
	Old   string `yaml:"old,omitempty" json:"old,omitempty"`
	New   string `yaml:"new,omitempty" json:"new,omitempty"`
	// Upstream: the change of the tracked document Target from version From
	// (0: nothing seen) to version To, and the ids of its comments to settle.
	From     int      `yaml:"from,omitempty" json:"from,omitempty"`
	To       int      `yaml:"to,omitempty" json:"to,omitempty"`
	Comments []string `yaml:"comments,omitempty" json:"comments,omitempty"`
	// Rev is the dossier revision of the item's last change.
	Rev int `yaml:"rev,omitempty" json:"rev,omitempty"`
	// ClaimedBy holds a claimed item until LeaseUntil (RFC 3339).
	ClaimedBy  string `yaml:"claimed_by,omitempty" json:"claimed_by,omitempty"`
	LeaseUntil string `yaml:"lease_until,omitempty" json:"lease_until,omitempty"`
	// Answer is the owner's answer, a draft until the dossier is submitted.
	Answer *ItemAnswer `yaml:"answer,omitempty" json:"answer,omitempty"`
	// Section is the id of the dossier heading the block sits under. Not stored.
	Section string `yaml:"-" json:"section,omitempty"`
}

// Decisions an owner makes on an item.
const (
	DecideApprove = "approve"
	DecideDeny    = "deny"
	DecideChange  = "change"
)

// ItemDecision records one decision on an item. OwnerSaid is the owner's words
// from a chat, as the agent that relayed them reported them: SelfReported is
// then true, because para cannot see the chat.
type ItemDecision struct {
	Decision     string `yaml:"decision" json:"decision"` // approve, deny, or change
	By           string `yaml:"by" json:"by"`             // the actor that recorded it
	Surface      string `yaml:"surface,omitempty" json:"surface,omitempty"`
	At           string `yaml:"at" json:"at"`
	OwnerSaid    string `yaml:"owner_said,omitempty" json:"owner_said,omitempty"`
	SelfReported bool   `yaml:"self_reported,omitempty" json:"self_reported,omitempty"`
}

// GateTarget is the target a harness hook names: where the act lands.
func (it ProposalItem) GateTarget() string {
	switch {
	case it.Target != "":
		return it.Target
	case it.Channel != "":
		return it.Channel
	case len(it.People) > 0:
		return strings.Join(it.People, ", ")
	}
	return it.What
}

// GateBody is the text a harness hook names: what is sent or written.
func (it ProposalItem) GateBody() string {
	switch {
	case it.Body != "":
		return it.Body
	case it.Diff != "":
		return it.Diff
	case it.Title != "":
		return it.Title
	}
	return it.How
}

// Hash is the hash an approval binds to: the exact final text of the item,
// placeholders as written. An item with only a target and a body hashes as
// GateHash, so a hash from before these fields still matches.
func (it ProposalItem) Hash() (string, error) {
	switch it.Kind {
	case ItemQuestion:
		return jsonHash(it.ID, map[string]any{"kind": it.Kind, "title": it.Title, "multi": it.Multi, "options": it.Options,
			"recommended": it.Recommended, "option_details": it.OptionDetails, "for": it.For, "body": it.Body})
	case ItemDocEdit:
		return jsonHash(it.ID, map[string]any{"kind": it.Kind, "target": it.Target, "range": it.Range, "old": it.Old, "new": it.New})
	case ItemUpstream:
		return jsonHash(it.ID, map[string]any{"kind": it.Kind, "target": it.Target, "from": it.From, "to": it.To, "comments": it.Comments})
	}
	if !it.Kind.Outward() {
		return OpHash(it.Op, it.Input)
	}
	if it.Title == "" && it.Diff == "" && it.How == "" && it.What == "" && it.Previous == "" && it.Channel == "" && len(it.People) == 0 && len(it.Fields) == 0 {
		return it.GateHash(), nil
	}
	b, err := json.Marshal(map[string]any{"kind": it.Kind, "subtype": it.Subtype, "people": it.People, "channel": it.Channel, "subject": it.Subject,
		"target": it.Target, "what": it.What, "previous": it.Previous, "how": it.How, "title": it.Title, "body": it.Body, "diff": it.Diff, "fields": it.Fields})
	if err != nil {
		return "", fmt.Errorf("model: hash %s: %w", it.ID, err)
	}
	return hashOf(string(b)), nil
}

func jsonHash(id string, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("model: hash %s: %w", id, err)
	}
	return hashOf(string(b)), nil
}

// GateHash is the hash a harness hook can compute from what it is about to send.
func (it ProposalItem) GateHash() string {
	return GateHash(it.GateTarget(), it.Subject, it.GateBody())
}

// GateHash is ExternalHash, with the subject when there is one.
func GateHash(target, subject, body string) string {
	if subject == "" {
		return ExternalHash(target, body)
	}
	return hashOf(target + "\x00" + subject + "\x00" + body)
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

// placeholder is {{i1.url}}: an earlier item's result.
var placeholder = regexp.MustCompile(`\{\{(i[0-9]+)\.(url|locator|result)\}\}`)

// Placeholders lists the {{item.field}} references in text, as item and field.
func Placeholders(text string) [][2]string {
	out := [][2]string{}
	for _, m := range placeholder.FindAllStringSubmatch(text, -1) {
		out = append(out, [2]string{m[1], m[2]})
	}
	return out
}

// FillPlaceholders replaces each placeholder with its value. A placeholder with
// no value stays as written.
func FillPlaceholders(text string, value func(item, field string) (string, bool)) string {
	return placeholder.ReplaceAllStringFunc(text, func(p string) string {
		m := placeholder.FindStringSubmatch(p)
		if v, ok := value(m[1], m[2]); ok {
			return v
		}
		return p
	})
}

// ProposalContent is the fenced block of a proposal's body. Annotations and the
// item fields Draft and Medium are old forms, read and never written.
type ProposalContent struct {
	TLDR    string         `yaml:"tldr,omitempty" json:"tldr,omitempty"`
	Context string         `yaml:"context,omitempty" json:"context,omitempty"`
	Items   []ProposalItem `yaml:"items" json:"items"`
}

// rawContent reads every form a file may hold.
type rawContent struct {
	TLDR        string          `yaml:"tldr"`
	Context     string          `yaml:"context"`
	Items       []rawItem       `yaml:"items"`
	Annotations []rawAnnotation `yaml:"annotations"`
}

type rawItem struct {
	ProposalItem `yaml:",inline"`
	Draft        string `yaml:"draft"`
	Medium       string `yaml:"medium"`
}

type rawAnnotation struct {
	ID       string `yaml:"id"`
	Item     string `yaml:"item"`
	Quote    string `yaml:"quote"`
	Note     string `yaml:"note"`
	By       string `yaml:"by"`
	Revision int    `yaml:"revision"`
	State    string `yaml:"state"`
	Answer   string `yaml:"answer"`
}

// upgrade reads old forms as new ones: op as para, external as action, draft as
// body, medium as subtype, rework as sent_back, annotations as thread turns.
func (r rawContent) upgrade() ProposalContent {
	c := ProposalContent{TLDR: r.TLDR, Context: r.Context, Items: make([]ProposalItem, 0, len(r.Items))}
	for _, raw := range r.Items {
		it := raw.ProposalItem
		switch it.Kind {
		case ItemOp:
			it.Kind = ItemPara
		case ItemExternal:
			it.Kind = ItemAction
		}
		if it.Body == "" {
			it.Body = raw.Draft
		}
		if it.Subtype == "" {
			it.Subtype = raw.Medium
		}
		if it.State == itemRework {
			it.State = ItemSentBack
		}
		c.Items = append(c.Items, it)
	}
	for _, a := range r.Annotations {
		for i := range c.Items {
			if c.Items[i].ID == a.Item {
				c.Items[i].Thread = append(c.Items[i].Thread, Turn{ID: a.ID, Kind: TurnClarify, Quote: a.Quote, Text: a.Note,
					By: a.By, Revision: a.Revision, State: TurnState(a.State), Answer: a.Answer})
			}
		}
	}
	return c
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
	if IsDossier(body) {
		d, err := ParseDossier(body)
		if err != nil {
			return ProposalContent{}, err
		}
		return d.Content(), nil
	}
	var r rawContent
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
	if err := dec.Decode(&r); err != nil {
		return c, fmt.Errorf("model: proposal body: %w", err)
	}
	return r.upgrade(), nil
}

// ItemTags are the topics of an item: its own tags, then the dossier's.
func ItemTags(dossier []string, it ProposalItem) []string {
	out := append([]string{}, it.Tags...)
	for _, t := range dossier {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// Deferred reports whether the item is put off on the date today.
func (it ProposalItem) Deferred(today string) bool {
	return Deferral{DeferUntil: it.DeferUntil}.IsDeferred(today)
}
