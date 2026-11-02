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
	// Status is "current" or "changed" and Checked is the last refresh time.
	// Both are set on transcripts only (para policy refresh).
	Status  string `yaml:"status,omitempty" json:"status,omitempty"`
	Checked string `yaml:"checked,omitempty" json:"checked,omitempty"`
}

// Project is type: project.
type Project struct {
	Common `yaml:",inline"`
	Status ProjectStatus `yaml:"status" json:"status"`
	Owner  string        `yaml:"owner" json:"owner"`
	// Area is the area of life or work the project belongs to; optional.
	Area Link `yaml:"area,omitempty" json:"area,omitempty"`
}

// Area is type: area, an ongoing responsibility with no end date (PARA).
type Area struct {
	Common `yaml:",inline"`
	Status AreaStatus `yaml:"status" json:"status"`
}

// Note is type: note, reference material kept in resources/. Area and Project
// say what it is for; both are optional.
type Note struct {
	Common  `yaml:",inline"`
	Area    Link `yaml:"area,omitempty" json:"area,omitempty"`
	Project Link `yaml:"project,omitempty" json:"project,omitempty"`
}

// Task is type: task.
type Task struct {
	Common  `yaml:",inline"`
	Status  TaskStatus `yaml:"status" json:"status"`
	Project Link       `yaml:"project,omitempty" json:"project,omitempty"` // project or area, at least one
	Area    Link       `yaml:"area,omitempty" json:"area,omitempty"`
	// Due is the date the task is due. Recur is free text such as "weekly";
	// para stores it and never schedules from it.
	Due       Date     `yaml:"due,omitempty" json:"due,omitempty"`
	Recur     string   `yaml:"recur,omitempty" json:"recur,omitempty"`
	BlockedBy []string `yaml:"blocked_by,omitempty" json:"blocked_by,omitempty"`
	DependsOn []Link   `yaml:"depends_on,omitempty" json:"depends_on,omitempty"` // task or rfx links
}

// RFX is type: rfx.
type RFX struct {
	Common         `yaml:",inline"`
	Direction      RFXDirection `yaml:"direction" json:"direction"`
	Status         RFXStatus    `yaml:"status" json:"status"`
	Route          string       `yaml:"route" json:"route"`
	SelfServeCheck string       `yaml:"self_serve_check" json:"self_serve_check"`
	Project        Link         `yaml:"project,omitempty" json:"project,omitempty"`
	// Targets are who is asked (direction out) or who asked (direction in),
	// each by one medium.
	Targets   []Target `yaml:"targets,omitempty" json:"targets,omitempty"`
	Blocks    []Link   `yaml:"blocks,omitempty" json:"blocks,omitempty"`
	DependsOn []Link   `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	NextNudge Date     `yaml:"next_nudge,omitempty" json:"next_nudge,omitempty"`
	// Playbook links the document that answers this ask for good. An inbound
	// RFX closes by linking one.
	Playbook Link `yaml:"playbook,omitempty" json:"playbook,omitempty"`
}

// Target is one person or group an RFX goes to, by one medium. Para records
// it; it never sends.
type Target struct {
	To     Link        `yaml:"to" json:"to"` // person or group
	Medium Medium      `yaml:"medium" json:"medium"`
	Value  string      `yaml:"value,omitempty" json:"value,omitempty"` // address or handle used
	State  TargetState `yaml:"state" json:"state"`
	Ref    string      `yaml:"ref,omitempty" json:"ref,omitempty"`   // thread or ticket URL or id
	Sent   Date        `yaml:"sent,omitempty" json:"sent,omitempty"` // date it was sent
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
	// ReviewedBy names who reviewed the summary against the source.
	ReviewedBy string `yaml:"reviewed_by,omitempty" json:"reviewed_by,omitempty"`
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
	// ReviewBy is when to look at the exception again (para exception expiring).
	ReviewBy Date `yaml:"review_by,omitempty" json:"review_by,omitempty"`
}

// Person is type: person.
type Person struct {
	Common `yaml:",inline"`
	Org    string `yaml:"org" json:"org"`
	Role   Link   `yaml:"role,omitempty" json:"role,omitempty"`
	Team   Link   `yaml:"team,omitempty" json:"team,omitempty"`
	// Aliases are other names the person goes by.
	Aliases []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	// Contacts are how to reach the person.
	Contacts []Contact `yaml:"contacts,omitempty" json:"contacts,omitempty"`
	// Groups are the groups the person belongs to. Membership lives here only.
	Groups []Link `yaml:"groups,omitempty" json:"groups,omitempty"`
}

// Contact is one way to reach a person. At most one contact per method is
// preferred.
type Contact struct {
	Method    Medium `yaml:"method" json:"method"`
	Value     string `yaml:"value" json:"value"`
	Label     string `yaml:"label,omitempty" json:"label,omitempty"`
	Preferred bool   `yaml:"preferred,omitempty" json:"preferred,omitempty"`
}

// SearchText is the text a Finder matches besides id, title, tags, and body.
func (p *Person) SearchText() []string {
	out := append([]string(nil), p.Aliases...)
	for _, c := range p.Contacts {
		out = append(out, c.Value)
	}
	return out
}

// Group is type: group. Members are the people that link to it.
type Group struct {
	Common `yaml:",inline"`
	Org    string    `yaml:"org" json:"org"` // free text
	Kind   GroupKind `yaml:"kind" json:"kind"`
}

// Interaction is type: interaction, one logged contact with someone. The body
// is a short summary in the user's words; url links to the original.
type Interaction struct {
	Common     `yaml:",inline"`
	Date       Date                 `yaml:"date" json:"date"`
	With       []Link               `yaml:"with" json:"with"` // people or groups
	Medium     Medium               `yaml:"medium" json:"medium"`
	Direction  InteractionDirection `yaml:"direction" json:"direction"`
	Visibility Visibility           `yaml:"visibility" json:"visibility"`
	URL        string               `yaml:"url,omitempty" json:"url,omitempty"`
}

// Request is type: request, a task for an agent. The body is the ask, then
// notes and the result. Times are RFC 3339 UTC strings.
type Request struct {
	Common  `yaml:",inline"`
	Status  RequestStatus `yaml:"status" json:"status"`
	Runner  string        `yaml:"runner" json:"runner"`                             // install id; only this install can claim
	Queued  string        `yaml:"queued_at" json:"queued_at"`                       // when it became claimable; orders the queue
	Claimed string        `yaml:"claimed_by,omitempty" json:"claimed_by,omitempty"` // actor holding the lease
	Lease   string        `yaml:"lease_until,omitempty" json:"lease_until,omitempty"`
	Links   []Link        `yaml:"links,omitempty" json:"links,omitempty"`               // task or project the ask is about
	Results []Link        `yaml:"result_links,omitempty" json:"result_links,omitempty"` // documents the agent made or changed
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
	case TypeGroup:
		return &Group{Common: c}, true
	case TypeInteraction:
		return &Interaction{Common: c}, true
	case TypeRequest:
		return &Request{Common: c}, true
	case TypeArea:
		return &Area{Common: c}, true
	case TypeNote:
		return &Note{Common: c}, true
	}
	return nil, false
}
