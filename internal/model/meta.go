package model

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Date is a YYYY-MM-DD value. It is a string, so a date keeps its exact form
// from file to struct to file. Lint rejects a bad value at write time.
type Date string

// MarshalYAML emits a plain scalar, not a quoted string.
func (d Date) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: string(d)}, nil
}

// Link is a frontmatter wikilink, for example "[[acme-portal]]" or
// "[[policy-logging#^R4]]".
type Link string

// LinkTo builds the link to a document id.
func LinkTo(id string) Link { return Link("[[" + id + "]]") }

// RuleLink builds the link to a rule in a policy.
func RuleLink(policy, rule string) Link { return Link("[[" + policy + "#^" + rule + "]]") }

// Target is the document id the link names.
func (l Link) Target() string {
	s := strings.TrimSuffix(strings.TrimPrefix(string(l), "[["), "]]")
	s, _, _ = strings.Cut(s, "#")
	return s
}

// Common is the frontmatter every type carries.
type Common struct {
	Type    Type     `yaml:"type" json:"type"`
	Created Date     `yaml:"created" json:"created"`
	Updated Date     `yaml:"updated,omitempty" json:"updated,omitempty"`
	Tags    []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// Header returns the common fields. Embedding Common makes *T a Meta.
func (c *Common) Header() *Common { return c }

// Meta is the typed frontmatter of a document.
type Meta interface {
	Header() *Common
}

// Source is the source block of format.md § 4.
type Source struct {
	URL       string   `yaml:"url" json:"url"`
	Retrieved string   `yaml:"retrieved" json:"retrieved"`
	Hash      string   `yaml:"hash,omitempty" json:"hash,omitempty"`
	Fidelity  Fidelity `yaml:"fidelity" json:"fidelity"`
	Complete  *bool    `yaml:"complete,omitempty" json:"complete,omitempty"`
	Untrusted *bool    `yaml:"untrusted,omitempty" json:"untrusted,omitempty"`
	Backend   string   `yaml:"backend,omitempty" json:"backend,omitempty"`
}

// Project is type: project.
type Project struct {
	Common `yaml:",inline"`
	Status ProjectStatus `yaml:"status" json:"status"`
	Owner  string        `yaml:"owner" json:"owner"`
}

// Task is type: task.
type Task struct {
	Common    `yaml:",inline"`
	Status    TaskStatus `yaml:"status" json:"status"`
	Project   Link       `yaml:"project" json:"project"`
	BlockedBy []string   `yaml:"blocked_by,omitempty" json:"blocked_by,omitempty"`
	DependsOn []Link     `yaml:"depends_on,omitempty" json:"depends_on,omitempty"` // task or rfx links
}

// RFX is type: rfx.
type RFX struct {
	Common         `yaml:",inline"`
	Direction      RFXDirection `yaml:"direction" json:"direction"`
	Status         RFXStatus    `yaml:"status" json:"status"`
	Route          string       `yaml:"route" json:"route"`
	SelfServeCheck string       `yaml:"self_serve_check" json:"self_serve_check"`
	Project        Link         `yaml:"project,omitempty" json:"project,omitempty"`
	// To is who is asked (direction out) or who asked (direction in).
	To        string `yaml:"to,omitempty" json:"to,omitempty"`
	Channel   string `yaml:"channel,omitempty" json:"channel,omitempty"`
	Blocks    []Link `yaml:"blocks,omitempty" json:"blocks,omitempty"`
	DependsOn []Link `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	NextNudge Date   `yaml:"next_nudge,omitempty" json:"next_nudge,omitempty"`
	// Playbook links the document that answers this ask for good. An inbound
	// RFX closes by linking one.
	Playbook Link `yaml:"playbook,omitempty" json:"playbook,omitempty"`
}

// Constraint is type: constraint, a justified "no".
type Constraint struct {
	Common    `yaml:",inline"`
	Why       string   `yaml:"why" json:"why"`
	By        string   `yaml:"by" json:"by"`
	Source    Source   `yaml:"source" json:"source"`
	AppliesIn []string `yaml:"applies_in" json:"applies_in"`
	NotIn     []string `yaml:"not_in" json:"not_in"`
	ReviewBy  Date     `yaml:"review_by" json:"review_by"`
}

// Policy is type: policy, the summary file.
type Policy struct {
	Common   `yaml:",inline"`
	Source   Source `yaml:"source" json:"source"`
	Verified bool   `yaml:"verified" json:"verified"`
}

// Transcript is type: transcript, the .source.md file.
type Transcript struct {
	Common `yaml:",inline"`
	Of     Link   `yaml:"of" json:"of"`
	Source Source `yaml:"source" json:"source"`
}

// PolicyException is type: policy_exception.
type PolicyException struct {
	Common `yaml:",inline"`
	Policy Link            `yaml:"policy" json:"policy"`
	Rule   Link            `yaml:"rule,omitempty" json:"rule,omitempty"`
	Kind   ExceptionKind   `yaml:"kind" json:"kind"`
	Status ExceptionStatus `yaml:"status" json:"status"`
	By     string          `yaml:"by" json:"by"`
	With   Link            `yaml:"with,omitempty" json:"with,omitempty"`
}

// Person is type: person.
type Person struct {
	Common `yaml:",inline"`
	Org    string `yaml:"org" json:"org"`
	Role   Link   `yaml:"role,omitempty" json:"role,omitempty"`
	Team   Link   `yaml:"team,omitempty" json:"team,omitempty"`
}

// Team is type: team.
type Team struct {
	Common `yaml:",inline"`
	Org    string `yaml:"org" json:"org"`
}

// Role is type: role.
type Role struct {
	Common `yaml:",inline"`
	Org    string `yaml:"org" json:"org"`
}

// Domain is type: domain.
type Domain struct {
	Common `yaml:",inline"`
	Owner  Link `yaml:"owner" json:"owner"`
}

// Convention is type: convention.
type Convention struct {
	Common    `yaml:",inline"`
	Status    ConventionStatus `yaml:"status" json:"status"`
	AppliesIn []string         `yaml:"applies_in" json:"applies_in"`
}

// Journal is type: journal.
type Journal struct {
	Common `yaml:",inline"`
	Date   Date         `yaml:"date" json:"date"`
	Scope  JournalScope `yaml:"scope" json:"scope"`
}

// Inbox is type: inbox, an unsorted capture.
type Inbox struct {
	Common `yaml:",inline"`
}

// NewMeta returns an empty Meta of type t with Common.Type set.
func NewMeta(t Type) (Meta, bool) {
	c := Common{Type: t}
	switch t {
	case TypeProject:
		return &Project{Common: c}, true
	case TypeTask:
		return &Task{Common: c}, true
	case TypeRFX:
		return &RFX{Common: c}, true
	case TypeConstraint:
		return &Constraint{Common: c}, true
	case TypePolicy:
		return &Policy{Common: c}, true
	case TypeTranscript:
		return &Transcript{Common: c}, true
	case TypePolicyException:
		return &PolicyException{Common: c}, true
	case TypePerson:
		return &Person{Common: c}, true
	case TypeTeam:
		return &Team{Common: c}, true
	case TypeRole:
		return &Role{Common: c}, true
	case TypeDomain:
		return &Domain{Common: c}, true
	case TypeConvention:
		return &Convention{Common: c}, true
	case TypeJournal:
		return &Journal{Common: c}, true
	case TypeInbox:
		return &Inbox{Common: c}, true
	}
	return nil, false
}
