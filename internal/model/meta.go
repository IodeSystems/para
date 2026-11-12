package model

import (
	"encoding/json"
	"slices"
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
	// HealthDismissed lists health findings a person set aside for this
	// document (para health). It lives in the file, so it stays with sync.
	HealthDismissed []HealthDismissal `yaml:"health_dismissed,omitempty" json:"health_dismissed,omitempty"`
	// ImportedFrom names the file this document was imported from, so
	// import_verify can say what an old vault holds that this one lacks.
	ImportedFrom *ImportedFrom `yaml:"imported_from,omitempty" json:"imported_from,omitempty"`
	// DuplicateOf links the primary this document was merged into (doc_merge).
	// A merged document leaves every list and search; its id still resolves.
	DuplicateOf Link `yaml:"duplicate_of,omitempty" json:"duplicate_of,omitempty"`
	// Prepared says an agent prepared the item for the owner (prepared.go).
	Prepared *Prepared `yaml:"prepared,omitempty" json:"prepared,omitempty"`
	// Publish names the publish sets the document belongs to (para publish).
	// Only the owner sets it (publish_set): it decides what leaves the vault.
	Publish []string `yaml:"publish,omitempty" json:"publish,omitempty"`
	// Threads lists extra thread keys the document belongs to (thread.go): an
	// item id, "#tag", or "channel:<source>/<thread>". A tag joins a thread
	// only when named here as "#tag"; the tags field alone does not.
	Threads []string `yaml:"threads,omitempty" json:"threads,omitempty"`
	// Pinned is the RFC 3339 time the owner pinned the item to watch it
	// (pin). Only the owner sets it. Empty is not pinned.
	Pinned string `yaml:"pinned,omitempty" json:"pinned,omitempty"`
	// FormerIDs are the ids the document had before an id change (a rename
	// or upgrade step goals): a link, event, or receipt that names one
	// resolves to this document, and no other document may take one.
	FormerIDs []string `yaml:"former_ids,omitempty" json:"former_ids,omitempty"`
}

// WasID reports whether id is one of the document's former ids.
func (c *Common) WasID(id string) bool {
	for _, f := range c.FormerIDs {
		if f == id {
			return true
		}
	}
	return false
}

// ImportedFrom is the source file of an imported document: the path as the
// importer gave it, and sha256:<hex> of the file's bytes.
type ImportedFrom struct {
	Path string `yaml:"path" json:"path"`
	Hash string `yaml:"hash" json:"hash"`
}

// HealthDismissal is one dismissed finding kind on a document.
type HealthDismissal struct {
	Kind   string `yaml:"kind" json:"kind"`
	Reason string `yaml:"reason" json:"reason"`
	Date   Date   `yaml:"date" json:"date"`
}

// Header returns the common fields. Embedding Common makes *T a Meta.
func (c *Common) Header() *Common { return c }

// Meta is the typed frontmatter of a document.
type Meta interface {
	Header() *Common
}

// Source is the source block of format.md § 4.
type Source struct {
	URL string `yaml:"url" json:"url"`
	// Verify stands in for URL on a constraint the owner heard with no link: who and where.
	Verify    string   `yaml:"verify,omitempty" json:"verify,omitempty"`
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

// Pointer points at material that lives outside the vault: a repo, a plan, a
// runbook. Para stores the pointer and never reads or copies the target.
// Target is a URL or a path; a ~/ path is stored as written.
type Pointer struct {
	Label  string      `yaml:"label" json:"label"`
	Kind   PointerKind `yaml:"kind" json:"kind"`
	Target string      `yaml:"target" json:"target"`
}

// Goal is type: goal, a finite outcome with a done condition: what a file of
// type project was before upgrade step goals (format 11). Project links the
// ongoing project the goal belongs to; Area is a facet, not its home.
type Goal struct {
	Common  `yaml:",inline"`
	Status  GoalStatus `yaml:"status" json:"status"`
	Owner   string     `yaml:"owner" json:"owner"`
	Project Link       `yaml:"project,omitempty" json:"project,omitempty"`
	Area    Link       `yaml:"area,omitempty" json:"area,omitempty"`
	// Concepts link the concept areas the goal draws on; optional.
	Concepts []Link `yaml:"concepts,omitempty" json:"concepts,omitempty"`
	// DoneWhen says how we know the goal is reached.
	DoneWhen string `yaml:"done_when,omitempty" json:"done_when,omitempty"`
	// Due is the date the goal is due, optional; work that leads to the goal
	// inherits it.
	Due Date `yaml:"due,omitempty" json:"due,omitempty"`
	// ResumeWhen is what a parked goal waits on: a condition or a date. A
	// parked goal needs one (health).
	ResumeWhen string `yaml:"resume_when,omitempty" json:"resume_when,omitempty"`
	Deferral   `yaml:",inline"`
	Links      []Pointer `yaml:"links,omitempty" json:"links,omitempty"`
	GoalBrief  `yaml:",inline"`
	// Assumptions are what the goal takes as true, each with when it was
	// last checked (grill.go).
	Assumptions []Assumption `yaml:"assumptions,omitempty" json:"assumptions,omitempty"`
	// FoldedFrom is the type the goal was before upgrade step goals.
	FoldedFrom Type `yaml:"folded_from,omitempty" json:"folded_from,omitempty"`
}

// Project is type: project from format 11: an ongoing tracked body of work
// with a state line, the goals in flight, and an icebox of parked goals. It
// has no done condition. Below format 11 a file of type project is a goal:
// the vault reads it as one (vault legacy.go).
type Project struct {
	Common `yaml:",inline"`
	Status ProjectStatus `yaml:"status" json:"status"`
	Owner  string        `yaml:"owner,omitempty" json:"owner,omitempty"`
	// Area is a facet: information about working in an area. Optional.
	Area     Link   `yaml:"area,omitempty" json:"area,omitempty"`
	Concepts []Link `yaml:"concepts,omitempty" json:"concepts,omitempty"`
	// StateLine is one line on where the project stands; StateDate is the
	// day it last changed, set by para when the line changes.
	StateLine string    `yaml:"state_line,omitempty" json:"state_line,omitempty"`
	StateDate Date      `yaml:"state_date,omitempty" json:"state_date,omitempty"`
	Links     []Pointer `yaml:"links,omitempty" json:"links,omitempty"`
	// FoldedFrom is the type the project was before upgrade step
	// projects-sort: area or goal.
	FoldedFrom Type `yaml:"folded_from,omitempty" json:"folded_from,omitempty"`
}

// Active reports whether the project shows in the Projects view.
func (p *Project) Active() bool { return p.Status == ProjectActive }

// SomedayWord is the defer_until value for deferred with no date.
const SomedayWord = "someday"

// Deferral is the later state of a task, question, goal, or io item: a person put it
// off. DeferUntil is a YYYY-MM-DD date, or SomedayWord for no date. It leaves
// Today, next, and agent lists until its date, and keeps blocking what
// depends on it.
type Deferral struct {
	DeferUntil     string `yaml:"defer_until,omitempty" json:"defer_until,omitempty"`
	DeferredReason string `yaml:"deferred_reason,omitempty" json:"deferred_reason,omitempty"`
	// DeferredAfter lists ids of items the deferral waits on: it holds while one
	// is open. DeferredAdded is the part of those that defer put in depends_on,
	// so undefer removes only those and never a dependency a person made.
	DeferredAfter []string `yaml:"deferred_after,omitempty" json:"deferred_after,omitempty"`
	DeferredAdded []string `yaml:"deferred_added,omitempty" json:"deferred_added,omitempty"`
	// DeferUntilStart and DeferUntilEnd name the obligation the deferral
	// follows (at most one is set): DeferUntil is its start (or due), or the
	// day after its end, and para moves it when the obligation moves. When
	// the obligation closes, the item comes back.
	DeferUntilStart string `yaml:"defer_until_start,omitempty" json:"defer_until_start,omitempty"`
	DeferUntilEnd   string `yaml:"defer_until_end,omitempty" json:"defer_until_end,omitempty"`
}

// Follows returns the obligation the deferral follows and whether it waits
// for its end; "" when it follows none.
func (d Deferral) Follows() (id string, end bool) {
	if d.DeferUntilEnd != "" {
		return d.DeferUntilEnd, true
	}
	return d.DeferUntilStart, false
}

// IsDeferred reports whether the item is put off on the date today (YYYY-MM-DD,
// local time): someday, or a date after today.
func (d Deferral) IsDeferred(today string) bool {
	return d.DeferUntil == SomedayWord || d.DeferUntil != "" && d.DeferUntil > today
}

// Area is type: area, an ongoing responsibility with no end date (PARA).
type Area struct {
	Common `yaml:",inline"`
	Status AreaStatus `yaml:"status" json:"status"`
	// Kind is project or concept; empty reads as project (AreaKind.Or).
	Kind AreaKind `yaml:"kind,omitempty" json:"kind,omitempty"`
	// Cadence is how often the owner reviews the area, in the recur grammar.
	// LastReviewed is the date of the last review (area_review).
	Cadence      string `yaml:"cadence,omitempty" json:"cadence,omitempty"`
	LastReviewed Date   `yaml:"last_reviewed,omitempty" json:"last_reviewed,omitempty"`
	// Concepts link the concept areas a project area draws on. A concept area
	// has none.
	Concepts []Link `yaml:"concepts,omitempty" json:"concepts,omitempty"`
	// Links point at plans, repos, and docs that live elsewhere.
	Links []Pointer `yaml:"links,omitempty" json:"links,omitempty"`
	// Follows is the remote source the area summarizes, as a policy did. The
	// body is the summary; para never rewrites it from the source.
	Follows *Followed `yaml:"follows,omitempty" json:"follows,omitempty"`
	// FoldedFrom is the type the area was before upgrade step policy-docs.
	FoldedFrom Type `yaml:"folded_from,omitempty" json:"folded_from,omitempty"`
}

// Note is type: note, reference material kept in resources/. Area, Goal, and
// Project say what it is for; all are optional. Below format 11, project
// links a goal (a file of type project).
type Note struct {
	Common  `yaml:",inline"`
	Area    Link `yaml:"area,omitempty" json:"area,omitempty"`
	Project Link `yaml:"project,omitempty" json:"project,omitempty"`
	Goal    Link `yaml:"goal,omitempty" json:"goal,omitempty"`
}

// ThreadDoc is type: thread, the pin holder of a thread key that is not an
// item id (a "#tag" or a "channel:<source>/<thread>"). It holds the key and
// the pinned time, nothing else; the thread's turns stay where they are.
type ThreadDoc struct {
	Common `yaml:",inline"`
	Key    string `yaml:"key" json:"key"`
}

// Task is type: task.
type Task struct {
	Common `yaml:",inline"`
	Status TaskStatus `yaml:"status" json:"status"`
	// Project links the goal below format 11 (read until upgrade step goals
	// moves it to Goal); a task reaches an ongoing project only through its
	// goal. Goal or area, at least one.
	Project Link `yaml:"project,omitempty" json:"project,omitempty"`
	Goal    Link `yaml:"goal,omitempty" json:"goal,omitempty"`
	Area    Link `yaml:"area,omitempty" json:"area,omitempty"`
	// Due is the date the task is due. Recur is daily, weekly, monthly, quarterly, yearly, or
	// every <n> days: marking the task done creates the next one (internal/recur).
	// Previous links the task this one recurred from.
	Due      Date   `yaml:"due,omitempty" json:"due,omitempty"`
	Recur    string `yaml:"recur,omitempty" json:"recur,omitempty"`
	Previous Link   `yaml:"previous,omitempty" json:"previous,omitempty"`
	// Incident links the incident this task follows up.
	Incident Link `yaml:"incident,omitempty" json:"incident,omitempty"`
	// Parent links the task or obligation this one is an action item of
	// (prep for a meeting, or what came out of it). The parent does not wait
	// on it: it is not a dependency.
	Parent Link `yaml:"parent,omitempty" json:"parent,omitempty"`
	// Assignee is empty for the owner, else a person id or agent:<name>.
	Assignee string `yaml:"assignee,omitempty" json:"assignee,omitempty"`
	// RequestedBy is who the task is for: a person or group link, or free
	// text such as a role or an address. Optional.
	RequestedBy string `yaml:"requested_by,omitempty" json:"requested_by,omitempty"`
	// DoneWhen says how we know the task is done. Optional for a person;
	// an agent must give it.
	DoneWhen string `yaml:"done_when,omitempty" json:"done_when,omitempty"`
	// DoURL is the page where the owner does the task, DoCommand the exact
	// command to run, Steps a short markdown list. All optional; no secret goes
	// in them: a step that needs one says where to paste it.
	DoURL     string   `yaml:"do_url,omitempty" json:"do_url,omitempty"`
	DoCommand string   `yaml:"do_command,omitempty" json:"do_command,omitempty"`
	Steps     string   `yaml:"steps,omitempty" json:"steps,omitempty"`
	BlockedBy []string `yaml:"blocked_by,omitempty" json:"blocked_by,omitempty"`
	DependsOn []Link   `yaml:"depends_on,omitempty" json:"depends_on,omitempty"` // task or rfx links
	// OptionalAfter: see Deps.
	OptionalAfter []Link `yaml:"optional_after,omitempty" json:"optional_after,omitempty"`
	// Outcome says how a closed task ended (task_resolve). The note is under
	// ## Outcome in the body.
	Outcome TaskOutcome `yaml:"outcome,omitempty" json:"outcome,omitempty"`
	// DeclinedBy and DeclinedPerson say who declined a task of outcome
	// declined. RetryAfter is when a failed task may be tried again. ReroutedTo
	// is where a rerouted task went: a link to a vault item, or a URL.
	DeclinedBy     DeclinedBy `yaml:"declined_by,omitempty" json:"declined_by,omitempty"`
	DeclinedPerson Link       `yaml:"declined_person,omitempty" json:"declined_person,omitempty"`
	RetryAfter     Date       `yaml:"retry_after,omitempty" json:"retry_after,omitempty"`
	ReroutedTo     string     `yaml:"rerouted_to,omitempty" json:"rerouted_to,omitempty"`
	// Kind is what sort of work this is; empty means task. A kind commitment
	// replaces the older commitment tag; both are read (EffectiveKind).
	Kind TaskKind `yaml:"kind,omitempty" json:"kind,omitempty"`
	// Occasion is when the task happens and with whom: a meeting or a call.
	Occasion `yaml:",inline"`
	Deferral `yaml:",inline"`
	// Assumptions are what the task takes as true (grill.go).
	Assumptions []Assumption `yaml:"assumptions,omitempty" json:"assumptions,omitempty"`
	// Effort is about how long the task takes the owner; empty is not set.
	Effort Effort `yaml:"effort,omitempty" json:"effort,omitempty"`
	// Severity, Started, Ended, and Links are a task of kind incident: how
	// bad (sev1 is the worst), RFC 3339 start and end, and pointers to
	// dashboards, runbooks, and tickets. The body holds the timeline.
	// Status todo is an open incident, doing mitigated, done resolved.
	Severity IncidentSeverity `yaml:"severity,omitempty" json:"severity,omitempty"`
	Started  string           `yaml:"started,omitempty" json:"started,omitempty"`
	Ended    string           `yaml:"ended,omitempty" json:"ended,omitempty"`
	Links    []Pointer        `yaml:"links,omitempty" json:"links,omitempty"`
	// FoldedFrom is the type the task was before upgrade step small-types
	// (incident); the keys of that type stay in the file.
	FoldedFrom Type `yaml:"folded_from,omitempty" json:"folded_from,omitempty"`
}

// Occasion is the when and the who of a meeting, a call, or an obligation.
// Scheduled is when it happens, apart from Due: a date YYYY-MM-DD, or an RFC
// 3339 start with the zone it was booked in. End is the RFC 3339 end of a
// timed one. With names the people (person or group links) it is with.
type Occasion struct {
	Scheduled string `yaml:"scheduled,omitempty" json:"scheduled,omitempty"`
	End       string `yaml:"end,omitempty" json:"end,omitempty"`
	With      []Link `yaml:"with,omitempty" json:"with,omitempty"`
	// Attendees are the other people of a meeting who are not in the directory:
	// free text, a name, an email, or "Name <email>", as the invite shows them.
	Attendees []string `yaml:"attendees,omitempty" json:"attendees,omitempty"`
	// RFX links the requests a meeting is meant to settle.
	RFX []Link `yaml:"rfx,omitempty" json:"rfx,omitempty"`
	// CalendarUIDs are the owner's calendar events of a meeting or a call (the
	// invite, a room booking, an internal prep slot), so calendar_compare
	// matches any of them exactly. CalendarUID is the older single field:
	// read as one entry (UIDs), and upgrade step calendar-uids converts it.
	CalendarUIDs []CalendarEvent `yaml:"calendar_uids,omitempty" json:"calendar_uids,omitempty"`
	CalendarUID  string          `yaml:"calendar_uid,omitempty" json:"calendar_uid,omitempty"`
	// Optional is true on a meeting the owner may skip: an optional attendee in
	// the calendar, or a standing meeting the owner marked optional. Attend is
	// whether the owner goes (yes, no, maybe; empty is undecided).
	// AttendDefault is the answer each next occurrence of a recurring meeting
	// starts with. A recurrence copies Optional and AttendDefault, never Attend.
	Optional      bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
	Attend        Attend `yaml:"attend,omitempty" json:"attend,omitempty"`
	AttendDefault Attend `yaml:"attend_default,omitempty" json:"attend_default,omitempty"`
}

// CalendarEvent is one calendar event of a meeting: its UID, and its role in
// free text (primary, room, internal prep).
type CalendarEvent struct {
	UID  string `yaml:"uid" json:"uid"`
	Role string `yaml:"role,omitempty" json:"role,omitempty"`
}

// RolePrimary is the role of the event a single calendar_uid named.
const RolePrimary = "primary"

// UIDs lists the calendar events, the older calendar_uid first
// as role primary when it is not also in the list.
func (t *Occasion) UIDs() []CalendarEvent {
	out := []CalendarEvent{}
	if t.CalendarUID != "" && !slices.ContainsFunc(t.CalendarUIDs, func(e CalendarEvent) bool { return e.UID == t.CalendarUID }) {
		out = append(out, CalendarEvent{UID: t.CalendarUID, Role: RolePrimary})
	}
	return append(out, t.CalendarUIDs...)
}

// AddUID adds a calendar event, or sets the role of one already there (an
// empty role keeps the stored one). It folds the older field into the list.
func (t *Occasion) AddUID(e CalendarEvent) {
	t.CalendarUIDs, t.CalendarUID = t.UIDs(), ""
	for i := range t.CalendarUIDs {
		if t.CalendarUIDs[i].UID == e.UID {
			if e.Role != "" {
				t.CalendarUIDs[i].Role = e.Role
			}
			return
		}
	}
	t.CalendarUIDs = append(t.CalendarUIDs, e)
}

// CommitmentTag is the tag older vaults use for a commitment. Kind commitment
// replaces it; both are read.
const CommitmentTag = "commitment"

// EffectiveKind is the kind of the task, reading the commitment tag as kind
// commitment. An empty kind is task.
func (t *Task) EffectiveKind() TaskKind {
	switch {
	case t.Kind != "":
		return t.Kind
	case slices.Contains(t.Tags, CommitmentTag):
		return KindCommitment
	}
	return KindTask
}

// Obligation is type: obligation, something the owner owes someone or must
// attend: a meeting, or a commitment. OwedTo are the people it is owed to;
// OwedBy is who owes it, empty for the owner. Schedule says when it falls due:
// once by Due (or no date), at Scheduled to End, a series by Recur (each
// occurrence is a file of its own, made one at a time, with OccurrenceOf the
// first of the series and Previous the one before), or ongoing with no date,
// reviewed on Cadence. Notes are in the body. FoldedFrom is the type it was
// before upgrade step obligations (task, meeting, or rfx); the keys of that
// type stay in the file.
type Obligation struct {
	Common `yaml:",inline"`
	// Kind is meeting or commitment.
	Kind TaskKind `yaml:"kind" json:"kind"`
	// State is open or closed. A closed one has Outcome, and Reason and Closed
	// when they are known.
	State    State       `yaml:"state" json:"state"`
	Outcome  TaskOutcome `yaml:"outcome,omitempty" json:"outcome,omitempty"`
	Reason   string      `yaml:"reason,omitempty" json:"reason,omitempty"`
	Closed   Date        `yaml:"closed,omitempty" json:"closed,omitempty"`
	Schedule Schedule    `yaml:"schedule" json:"schedule"`
	OwedTo   []Link      `yaml:"owed_to,omitempty" json:"owed_to,omitempty"`
	OwedBy   []Link      `yaml:"owed_by,omitempty" json:"owed_by,omitempty"`
	// Project links an ongoing project from format 11, a goal below it.
	Project Link `yaml:"project,omitempty" json:"project,omitempty"`
	Goal    Link `yaml:"goal,omitempty" json:"goal,omitempty"`
	Area    Link `yaml:"area,omitempty" json:"area,omitempty"`
	// Parent links the task or obligation this one came from, as a task's
	// parent does: a follow-up obligation of a meeting.
	Parent Link   `yaml:"parent,omitempty" json:"parent,omitempty"`
	Due    Date   `yaml:"due,omitempty" json:"due,omitempty"`
	Recur  string `yaml:"recur,omitempty" json:"recur,omitempty"`
	// Cadence is how often an ongoing obligation is reviewed, in the recur
	// grammar; LastReviewed is the date of the last review.
	Cadence      string `yaml:"cadence,omitempty" json:"cadence,omitempty"`
	LastReviewed Date   `yaml:"last_reviewed,omitempty" json:"last_reviewed,omitempty"`
	OccurrenceOf Link   `yaml:"occurrence_of,omitempty" json:"occurrence_of,omitempty"`
	Previous     Link   `yaml:"previous,omitempty" json:"previous,omitempty"`
	Occasion     `yaml:",inline"`
	DoneWhen     string `yaml:"done_when,omitempty" json:"done_when,omitempty"`
	DependsOn    []Link `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	// OptionalAfter: see Deps.
	OptionalAfter []Link `yaml:"optional_after,omitempty" json:"optional_after,omitempty"`
	Effort        Effort `yaml:"effort,omitempty" json:"effort,omitempty"`
	Deferral      `yaml:",inline"`
	FoldedFrom    Type `yaml:"folded_from,omitempty" json:"folded_from,omitempty"`
	// Produce is set on an obligation an agent owes (owed_by agent or
	// agent:<name>): the artifact it produces each time one comes due.
	Produce *Produce `yaml:"produce,omitempty" json:"produce,omitempty"`
	// ReplyTo links the incoming io item this obligation is the owner's
	// reply to (io_add with expects response).
	ReplyTo Link `yaml:"reply_to,omitempty" json:"reply_to,omitempty"`
	// Interested are more people or groups to tell when the obligation
	// changes, beside owed_to, with, and attendees.
	Interested []Link `yaml:"interested,omitempty" json:"interested,omitempty"`
	// Notices link the outgoing io drafts obligation_change wrote to tell
	// the interested parties of a change, oldest first.
	Notices []Link `yaml:"notices,omitempty" json:"notices,omitempty"`
	// Promise is how it went: kept, renegotiated, or broken. Para sets it on
	// close, on a change, and on the tick.
	Promise Promise `yaml:"promise,omitempty" json:"promise,omitempty"`
}

// OwedByAgent is the owed_by word of an agent: agent, or agent:<name>.
const OwedByAgent = "agent"

// IsAgentOwer reports whether an owed_by entry is an agent: agent, or
// agent:<name>, stored without wikilink brackets.
func IsAgentOwer(l Link) bool {
	s := string(l)
	return s == OwedByAgent || strings.HasPrefix(s, OwedByAgent+":")
}

// Agent is the first agent the obligation is owed by, or "".
func (o *Obligation) Agent() string {
	for _, l := range o.OwedBy {
		if IsAgentOwer(l) {
			return string(l)
		}
	}
	return ""
}

// Produce is what an agent produces for an obligation it owes. Template is
// the vault document it follows ("[[id]]") or the text itself. To are the
// people or groups a message goes to (links), or free text such as a
// channel; Medium is how. Guide names the prompt the agent follows (default
// the loop prompt, part Produce). Builtin marks an obligation para made itself: journal-summary is
// the daily journal summary tick makes once when journal.agent is set.
type Produce struct {
	Kind     ProduceKind `yaml:"kind" json:"kind"`
	Template string      `yaml:"template,omitempty" json:"template,omitempty"`
	To       []string    `yaml:"to,omitempty" json:"to,omitempty"`
	Medium   Medium      `yaml:"medium,omitempty" json:"medium,omitempty"`
	Guide    string      `yaml:"guide,omitempty" json:"guide,omitempty"`
	Builtin  string      `yaml:"builtin,omitempty" json:"builtin,omitempty"`
}

// Open reports whether the obligation is open.
func (o *Obligation) Open() bool { return o.State != StateClosed }

// AsTask is the task of kind meeting or commitment that holds the same
// obligation: for task views, and for a vault not yet at the obligation
// format. Open is todo; closed done (held) is done, any other outcome dropped.
// The first of OwedBy is the assignee; a commitment's OwedTo is its with.
func (o *Obligation) AsTask() *Task {
	t := &Task{Common: o.Common, Kind: o.Kind, Project: o.Project, Goal: o.Goal, Area: o.Area, Parent: o.Parent, Due: o.Due, Recur: o.Recur,
		Previous: o.Previous, Occasion: o.Occasion, DoneWhen: o.DoneWhen, DependsOn: o.DependsOn, OptionalAfter: o.OptionalAfter, Effort: o.Effort,
		Deferral: o.Deferral, Status: TaskTodo}
	t.Type = TypeTask
	if !o.Open() {
		t.Status, t.Outcome = TaskDropped, o.Outcome
		switch o.Outcome {
		case OutcomeDone, "":
			t.Status, t.Outcome = TaskDone, OutcomeDone
		case OutcomeSkipped:
			t.Outcome = OutcomeDropped
		}
	}
	if len(o.OwedBy) > 0 {
		t.Assignee = o.OwedBy[0].Target()
	}
	if o.Kind == KindCommitment && len(t.With) == 0 {
		t.With = o.OwedTo // a commitment task names who it is owed to in with
	}
	t.Tags = append([]string(nil), o.Tags...)
	return t
}

// RFX is type: rfx.
type RFX struct {
	Common         `yaml:",inline"`
	Direction      RFXDirection `yaml:"direction" json:"direction"`
	Status         RFXStatus    `yaml:"status" json:"status"`
	Route          string       `yaml:"route" json:"route"`
	SelfServeCheck string       `yaml:"self_serve_check" json:"self_serve_check"`
	// Project links an ongoing project from format 11, a goal below it.
	Project Link `yaml:"project,omitempty" json:"project,omitempty"`
	Goal    Link `yaml:"goal,omitempty" json:"goal,omitempty"`
	// Targets are who is asked (direction out) or who asked (direction in),
	// each by one medium.
	Targets   []Target `yaml:"targets,omitempty" json:"targets,omitempty"`
	Blocks    []Link   `yaml:"blocks,omitempty" json:"blocks,omitempty"`
	DependsOn []Link   `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	// OptionalAfter: see Deps.
	OptionalAfter []Link `yaml:"optional_after,omitempty" json:"optional_after,omitempty"`
	NextNudge     Date   `yaml:"next_nudge,omitempty" json:"next_nudge,omitempty"`
	// Playbook links the document that answers this ask for good. An inbound
	// RFX closes by linking one.
	Playbook Link `yaml:"playbook,omitempty" json:"playbook,omitempty"`
	// Question links the question this RFX asks. A reply to it answers the question.
	Question Link `yaml:"question,omitempty" json:"question,omitempty"`
	// Effort is about how long the ask takes the owner; empty is not set.
	Effort Effort `yaml:"effort,omitempty" json:"effort,omitempty"`
	// Parent links the task or obligation this ask is part of: prep for a
	// meeting, or what came out of it. The parent does not wait on it.
	Parent Link `yaml:"parent,omitempty" json:"parent,omitempty"`
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
	// Added is the date the target was added. Reason is the friction code when
	// the target came from a redirect (wrong_owner, no_response, ...) or when a
	// stall was recorded on it. Why is the reason in words. About links the
	// policy, policy exception, or domain that applies.
	Added  Date           `yaml:"added,omitempty" json:"added,omitempty"`
	Reason FrictionReason `yaml:"reason,omitempty" json:"reason,omitempty"`
	Why    string         `yaml:"why,omitempty" json:"why,omitempty"`
	About  Link           `yaml:"about,omitempty" json:"about,omitempty"`
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
	// Refines links the constraint this one specializes; optional.
	Refines Link `yaml:"refines,omitempty" json:"refines,omitempty"`
	// FoldedInto links the area the constraint is an annotation in now.
	FoldedInto Link `yaml:"folded_into,omitempty" json:"folded_into,omitempty"`
}

// Policy is type: policy, the summary file. Upgrade step policy-docs makes
// it an area that follows the same source.
type Policy struct {
	Common   `yaml:",inline"`
	Followed `yaml:",inline"`
}

// Followed is a remote source a summary follows: a policy's, or an area's
// (Area.Follows). The transcript <id>.source holds the newest text; a
// change keeps the text before it as <id>-v<n>.source.
type Followed struct {
	Source   Source `yaml:"source" json:"source"`
	Verified bool   `yaml:"verified" json:"verified"`
	// ReviewedBy names who reviewed the summary against the source.
	ReviewedBy string `yaml:"reviewed_by,omitempty" json:"reviewed_by,omitempty"`
}

// Transcript is type: transcript, the .source.md file. A policy has one,
// named <policy>.source. A tracked document has one per version, named
// <doc>-v<n>.source, with Version n: source.retrieved is when it was
// fetched, FetchedBy who fetched it, and Proposal and Item the approved doc
// edit that made it, when one did.
type Transcript struct {
	Common    `yaml:",inline"`
	Of        Link   `yaml:"of" json:"of"`
	Source    Source `yaml:"source" json:"source"`
	Version   int    `yaml:"version,omitempty" json:"version,omitempty"`
	FetchedBy string `yaml:"fetched_by,omitempty" json:"fetched_by,omitempty"`
	Proposal  Link   `yaml:"proposal,omitempty" json:"proposal,omitempty"`
	Item      string `yaml:"item,omitempty" json:"item,omitempty"`
}

// TrackedDoc is type: tracked_doc, a remote document the owner follows: a
// wiki page, a ticket, a pull request, a comment. Para keeps the original and
// every version after it as transcripts named <id>-v<n>.source; Latest is the
// newest version, LastSeen the newest the owner has reviewed. Para fetches
// nothing: an agent reads the document with its own tools and passes the text.
// Comments are what people wrote about ranges of it.
type TrackedDoc struct {
	Common `yaml:",inline"`
	// URL is a public link a person can open. Via is the private connector
	// reference an agent uses to read it again.
	URL      string    `yaml:"url" json:"url"`
	Via      *IOVia    `yaml:"via,omitempty" json:"via,omitempty"`
	System   DocSystem `yaml:"system" json:"system"`
	Status   DocStatus `yaml:"status" json:"status"`
	Owner    Link      `yaml:"owner,omitempty" json:"owner,omitempty"`
	Watchers []Link    `yaml:"watchers,omitempty" json:"watchers,omitempty"`
	Area     Link      `yaml:"area,omitempty" json:"area,omitempty"`
	// Project links an ongoing project from format 11, a goal below it.
	Project Link `yaml:"project,omitempty" json:"project,omitempty"`
	Goal    Link `yaml:"goal,omitempty" json:"goal,omitempty"`
	// Route is the domain an Ask made from a comment goes to; empty uses the
	// system name.
	Route    string `yaml:"route,omitempty" json:"route,omitempty"`
	Latest   int    `yaml:"latest" json:"latest"`
	LastSeen int    `yaml:"last_seen,omitempty" json:"last_seen,omitempty"`
	// SeenAt is when the owner last marked versions seen, RFC 3339.
	SeenAt string `yaml:"seen_at,omitempty" json:"seen_at,omitempty"`
	// Checked is the last time an agent read the document, with a change or
	// not, RFC 3339.
	Checked  string       `yaml:"checked,omitempty" json:"checked,omitempty"`
	Comments []DocComment `yaml:"comments,omitempty" json:"comments,omitempty"`
}

// DocComment is one comment a person made on a tracked document. Quote is the
// text it is about, from Version; Start and End are offsets of the quote in
// that version's text, counted in Unicode code points. Para keeps Anchor
// where the quote is in the newest version it looked in, or marks it lost.
type DocComment struct {
	ID      string `yaml:"id" json:"id"`
	By      string `yaml:"by" json:"by"` // a person link, or free text
	At      string `yaml:"at" json:"at"` // RFC 3339
	URL     string `yaml:"url,omitempty" json:"url,omitempty"`
	Text    string `yaml:"text" json:"text"`
	Quote   string `yaml:"quote,omitempty" json:"quote,omitempty"`
	Version int    `yaml:"version" json:"version"`
	Start   *int   `yaml:"start,omitempty" json:"start,omitempty"`
	End     *int   `yaml:"end,omitempty" json:"end,omitempty"`
	// Anchor is para's: where the quote is in a later version.
	Anchor *DocAnchor `yaml:"anchor,omitempty" json:"anchor,omitempty"`
	// Became links the Ask, question, or task the comment turned into.
	Became     Link              `yaml:"became,omitempty" json:"became,omitempty"`
	Resolution CommentResolution `yaml:"resolution,omitempty" json:"resolution,omitempty"`
	ResolvedAt string            `yaml:"resolved_at,omitempty" json:"resolved_at,omitempty"`
	Note       string            `yaml:"note,omitempty" json:"note,omitempty"`
}

// Open reports whether the comment has no resolution.
func (c DocComment) Open() bool { return c.Resolution == "" }

// DocAnchor is where a comment's quote was found in Version, or Lost when it
// was not found there.
type DocAnchor struct {
	Version int  `yaml:"version" json:"version"`
	Start   int  `yaml:"start,omitempty" json:"start,omitempty"`
	End     int  `yaml:"end,omitempty" json:"end,omitempty"`
	Lost    bool `yaml:"lost,omitempty" json:"lost,omitempty"`
}

// PolicyException is type: policy_exception.
type PolicyException struct {
	Common `yaml:",inline"`
	// Policy is the policy the exception is about. It may be empty when
	// PolicyText names the text instead (lint needs one of the two).
	Policy Link `yaml:"policy,omitempty" json:"policy,omitempty"`
	// PolicyText names the policy text in words, for a policy not in the vault.
	PolicyText string          `yaml:"policy_text,omitempty" json:"policy_text,omitempty"`
	Rule       Link            `yaml:"rule,omitempty" json:"rule,omitempty"`
	Kind       ExceptionKind   `yaml:"kind" json:"kind"`
	Status     ExceptionStatus `yaml:"status" json:"status"`
	By         string          `yaml:"by" json:"by"`
	With       Link            `yaml:"with,omitempty" json:"with,omitempty"`
	// ReviewBy is when to look at the exception again (para exception expiring).
	ReviewBy Date `yaml:"review_by,omitempty" json:"review_by,omitempty"`
	// FoldedInto links the area the exception is an annotation in now.
	FoldedInto Link `yaml:"folded_into,omitempty" json:"folded_into,omitempty"`
}

// Person is type: person.
type Person struct {
	Common `yaml:",inline"`
	Org    string `yaml:"org,omitempty" json:"org,omitempty"` // optional: a private person has none
	Role   Link   `yaml:"role,omitempty" json:"role,omitempty"`
	// RoleText is a role as free text, when no role document exists.
	RoleText string `yaml:"role_text,omitempty" json:"role_text,omitempty"`
	Team     Link   `yaml:"team,omitempty" json:"team,omitempty"`
	// Aliases are other names the person goes by.
	Aliases []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	// Contacts are how to reach the person.
	Contacts []Contact `yaml:"contacts,omitempty" json:"contacts,omitempty"`
	// Groups are the groups the person belongs to. Membership lives here only.
	Groups []Link `yaml:"groups,omitempty" json:"groups,omitempty"`
	// AwayUntil is the last day the person is away. Only this structured date is
	// used to hold back a repeat Ask; an away note in the body is not read.
	AwayUntil Date `yaml:"away_until,omitempty" json:"away_until,omitempty"`
	// QuietDays is how many days to leave between Asks to the person, as the
	// person stated it.
	QuietDays int `yaml:"quiet_days,omitempty" json:"quiet_days,omitempty"`
	// Priority is how much the person matters to the owner. Empty takes the
	// highest priority set on the person's groups, else normal.
	Priority Priority `yaml:"priority,omitempty" json:"priority,omitempty"`
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
	Org    string    `yaml:"org,omitempty" json:"org"` // free text; a group of kind domain may have none
	Kind   GroupKind `yaml:"kind" json:"kind"`
	// Priority is how much the group matters to the owner; a member with no
	// priority of their own takes it. Empty is normal.
	Priority Priority `yaml:"priority,omitempty" json:"priority,omitempty"`
	// Owner is who owns a group of kind domain: a role, team, or person
	// link, or self (the vault owner).
	Owner Link `yaml:"owner,omitempty" json:"owner,omitempty"`
	// FoldedFrom is the type the group was before upgrade step small-types
	// (team, role, or domain); the keys of that type stay in the file.
	FoldedFrom Type `yaml:"folded_from,omitempty" json:"folded_from,omitempty"`
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
	// ClaimedAt is when the actor in Claimed took it. ActiveAt is when the
	// request last changed: queued, claimed, a note, or an end. A request with
	// no change in a day is stale (health agent_work_stale).
	ClaimedAt string `yaml:"claimed_at,omitempty" json:"claimed_at,omitempty"`
	ActiveAt  string `yaml:"active_at,omitempty" json:"active_at,omitempty"`
	Links     []Link `yaml:"links,omitempty" json:"links,omitempty"`               // task or project the ask is about
	Results   []Link `yaml:"result_links,omitempty" json:"result_links,omitempty"` // documents the agent made or changed
	// Kind is free text. A rule in .para/proposals.yaml can require a policy or a
	// convention for a kind.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	// Context links what the work must follow: policies, conventions, a goal,
	// documents. Required is the part a rule demands. Followed is what the agent
	// cited at the end.
	Context  []Link `yaml:"context,omitempty" json:"context,omitempty"`
	Required []Link `yaml:"required,omitempty" json:"required,omitempty"`
	Followed []Link `yaml:"followed,omitempty" json:"followed,omitempty"`
	// Resolution is how the last turn ended. Turn counts responses: each is the
	// next turn in one thread, and the body keeps them all.
	Resolution RequestResolution `yaml:"resolution,omitempty" json:"resolution,omitempty"`
	Turn       int               `yaml:"turn,omitempty" json:"turn,omitempty"`
	// RequiresSource links a source: only an actor that declared it
	// (agent_sources) within sources.reach_days may claim the request.
	RequiresSource Link `yaml:"requires_source,omitempty" json:"requires_source,omitempty"`
	// Seen is when the owner opened the result of finished work (request_seen).
	// Done or failed work with no Seen waits for the owner to check it.
	Seen string `yaml:"seen,omitempty" json:"seen,omitempty"`
	// Thread is the one thread key this owner turn belongs to. Reply is the
	// agent's answer to the owner. TurnKind says what the owner turn asks for.
	// Quote is the owner-quoted text the turn is about.
	Thread   string          `yaml:"thread,omitempty" json:"thread,omitempty"`
	Reply    string          `yaml:"reply,omitempty" json:"reply,omitempty"`
	TurnKind RequestTurnKind `yaml:"turn_kind,omitempty" json:"turn_kind,omitempty"`
	Quote    string          `yaml:"quote,omitempty" json:"quote,omitempty"`
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
	// Refines links the convention this one specializes; optional.
	Refines Link `yaml:"refines,omitempty" json:"refines,omitempty"`
	// FoldedInto links the area the convention is an annotation in now.
	FoldedInto Link `yaml:"folded_into,omitempty" json:"folded_into,omitempty"`
}

// Journal is type: journal.
type Journal struct {
	Common `yaml:",inline"`
	Date   Date         `yaml:"date" json:"date"`
	Scope  JournalScope `yaml:"scope" json:"scope"`
}

// Inbox is type: inbox, an unsorted capture. It is read, never written: upgrade
// step io-folder turns it into an io item.
type Inbox struct {
	Common `yaml:",inline"`
}

// IO is type: io, one message not dealt with yet, in either direction. The
// body is the message text; the title is the subject, or the first line.
// State lives here, never in the folder: an item stays in io/ when it is
// filed, sent, or dropped, and the inbox and outbox views leave it out.
type IO struct {
	Common    `yaml:",inline"`
	Direction IODirection `yaml:"direction" json:"direction"`
	Medium    Medium      `yaml:"medium" json:"medium"`
	State     IOState     `yaml:"state" json:"state"`
	// From and To are person or group links, or free text such as an address.
	From string   `yaml:"from,omitempty" json:"from,omitempty"`
	To   []string `yaml:"to,omitempty" json:"to,omitempty"`
	// At is when it came or went, RFC 3339.
	At      string `yaml:"at,omitempty" json:"at,omitempty"`
	Subject string `yaml:"subject,omitempty" json:"subject,omitempty"`
	// Ref is a public link a person can open. Via is the private connector
	// reference an agent uses to read it again.
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty"`
	Via *IOVia `yaml:"via,omitempty" json:"via,omitempty"`
	// Verify tells the owner how to find a message that has Via and no Ref.
	Verify string `yaml:"verify,omitempty" json:"verify,omitempty"`
	// Expects is set on an outgoing message: response makes an Ask on send,
	// none an interaction. Route is the domain of that Ask.
	Expects IOExpects `yaml:"expects,omitempty" json:"expects,omitempty"`
	Route   string    `yaml:"route,omitempty" json:"route,omitempty"`
	// Became links what the item was filed into or turned into on send.
	Became Link `yaml:"became,omitempty" json:"became,omitempty"`
	// Thread links an earlier io item, interaction, Ask, or question this one
	// answers or acts on.
	Thread Link `yaml:"thread,omitempty" json:"thread,omitempty"`
	// Reason says why the item was dropped.
	Reason string `yaml:"reason,omitempty" json:"reason,omitempty"`
	// Effort is about how long the item takes the owner; empty is not set.
	Effort Effort `yaml:"effort,omitempty" json:"effort,omitempty"`
	// ApprovedHash is the DraftHash the owner approved (io_approve): an
	// outgoing draft in state ready whose text still hashes to it may be
	// sent. Empty on a ready draft from before 0.3.0.
	ApprovedHash string `yaml:"approved_hash,omitempty" json:"approved_hash,omitempty"`
	// ApprovedAt is when the owner approved the draft (RFC 3339), and
	// ApprovedSeen the newest at of the incoming messages in its
	// conversation then (empty: none). io_send_check counts what came in
	// after ApprovedSeen; CheckedAt is when that check last passed. Held says
	// why a check sent the draft back to the owner; HeldBy links the new
	// messages.
	ApprovedAt   string `yaml:"approved_at,omitempty" json:"approved_at,omitempty"`
	ApprovedSeen string `yaml:"approved_seen,omitempty" json:"approved_seen,omitempty"`
	CheckedAt    string `yaml:"checked_at,omitempty" json:"checked_at,omitempty"`
	Held         string `yaml:"held,omitempty" json:"held,omitempty"`
	HeldBy       []Link `yaml:"held_by,omitempty" json:"held_by,omitempty"`
	// Voice is as_me (empty) or for_me (IOVoice). A for_me draft names the
	// Agent that sends it (a name, no agent: prefix), ends with Stamp (the
	// filled stamp para appended), and may carry Kind, a free label that
	// delegation rules match (scheduling, acknowledgement). DraftedSeen is
	// the newest at of the incoming messages of its conversation when it was
	// drafted: io_send_check of a for_me draft counts what came after it.
	// AllowedBy names the delegation rule that lets the agent send it ("rule
	// 2 (name)"): set when it is drafted, checked, and sent.
	Voice       IOVoice `yaml:"voice,omitempty" json:"voice,omitempty"`
	Agent       string  `yaml:"agent,omitempty" json:"agent,omitempty"`
	Stamp       string  `yaml:"stamp,omitempty" json:"stamp,omitempty"`
	Kind        string  `yaml:"kind,omitempty" json:"kind,omitempty"`
	DraftedSeen string  `yaml:"drafted_seen,omitempty" json:"drafted_seen,omitempty"`
	AllowedBy   string  `yaml:"allowed_by,omitempty" json:"allowed_by,omitempty"`
	// Reply links the obligation that holds the owner's reply to an incoming
	// message that expects one (io.reply_days).
	Reply Link `yaml:"reply,omitempty" json:"reply,omitempty"`
	// Triage is what a triage rule (.para/triage.yaml) decided for an
	// incoming message, and TriageBy names the rule ("rule 2" or "rule 2
	// (name)"). Links are items a rule linked the message to; it stays open.
	Triage   IOTriage `yaml:"triage,omitempty" json:"triage,omitempty"`
	TriageBy string   `yaml:"triage_by,omitempty" json:"triage_by,omitempty"`
	Links    []Link   `yaml:"links,omitempty" json:"links,omitempty"`
	// Deferral puts the item off: it leaves the reply lane of the order until
	// its date. An io item waits on no other item.
	Deferral `yaml:",inline"`
	// Date, With, Visibility, URL, and Observed make the item a logged
	// contact (what an interaction was): the day, the people or groups,
	// where it happened, a link to the original, and that the owner only saw
	// it (direction in). A logged contact is closed: filed, or sent when the
	// owner wrote it. The body is a short summary in the owner's words.
	Date       Date       `yaml:"date,omitempty" json:"date,omitempty"`
	With       []Link     `yaml:"with,omitempty" json:"with,omitempty"`
	Visibility Visibility `yaml:"visibility,omitempty" json:"visibility,omitempty"`
	URL        string     `yaml:"url,omitempty" json:"url,omitempty"`
	Observed   bool       `yaml:"observed,omitempty" json:"observed,omitempty"`
	// FoldedFrom is the type the item was before upgrade step small-types
	// (interaction); the keys of that type stay in the file.
	FoldedFrom Type `yaml:"folded_from,omitempty" json:"folded_from,omitempty"`
	// Reviewed is when the owner reviewed the item (RFC 3339), in or out.
	Reviewed string `yaml:"reviewed,omitempty" json:"reviewed,omitempty"`
	// An outgoing draft is a Proposal: a preview of any outward action
	// (message, PR, ticket, deploy, comment). Why and Impact say the reason
	// and the effect; Assessments are named percentages. Group is a batch
	// key; VariantOf is the id of the first draft of a variation set.
	// Revision is the current version n and Revisions index the version
	// files io/<id>.r<n>.md. Attempts is one entry per try to carry it out.
	Why         string         `yaml:"why,omitempty" json:"why,omitempty"`
	Impact      string         `yaml:"impact,omitempty" json:"impact,omitempty"`
	Assessments []IOAssessment `yaml:"assessments,omitempty" json:"assessments,omitempty"`
	Group       string         `yaml:"group,omitempty" json:"group,omitempty"`
	VariantOf   string         `yaml:"variant_of,omitempty" json:"variant_of,omitempty"`
	Revision    int            `yaml:"revision,omitempty" json:"revision,omitempty"`
	Revisions   []IORevision   `yaml:"revisions,omitempty" json:"revisions,omitempty"`
	Attempts    []IOAttempt    `yaml:"attempts,omitempty" json:"attempts,omitempty"`
}

// IOAssessment is one named judgment of a proposal: Name is confidence,
// recommend, risk (AssessmentNames) or free text; Percent is 0 to 100; Basis
// says what it rests on. A proposal without one shows "not stated".
type IOAssessment struct {
	Name    string `yaml:"name" json:"name"`
	Percent int    `yaml:"percent" json:"percent"`
	Basis   string `yaml:"basis,omitempty" json:"basis,omitempty"`
}

// IORevision indexes one version file io/<id>.r<n>.md of a proposal.
type IORevision struct {
	N    int    `yaml:"n" json:"n"`
	Hash string `yaml:"hash" json:"hash"`
	By   string `yaml:"by,omitempty" json:"by,omitempty"`
	At   string `yaml:"at" json:"at"`
}

// IOAttempt is one try to carry out a proposal (OutputAttempt). Error is
// content, so it lives here and never in an event.
type IOAttempt struct {
	At     string        `yaml:"at" json:"at"`
	Actor  string        `yaml:"actor,omitempty" json:"actor,omitempty"`
	Tool   string        `yaml:"tool,omitempty" json:"tool,omitempty"`
	Medium Medium        `yaml:"medium,omitempty" json:"medium,omitempty"`
	Result AttemptResult `yaml:"result" json:"result"`
	URL    string        `yaml:"url,omitempty" json:"url,omitempty"`
	Error  string        `yaml:"error,omitempty" json:"error,omitempty"`
}

// ForMe reports whether the draft goes out for the owner, stamped, without
// their review.
func (m *IO) ForMe() bool { return m.Voice == VoiceForMe }

// Logged reports whether the item is a logged contact: what an interaction was.
func (m *IO) Logged() bool { return m.Date != "" }

// DraftHash is the hash an approval of an outgoing draft binds: medium,
// recipients, subject, and the exact body.
func (m *IO) DraftHash(body string) string {
	b, _ := json.Marshal(map[string]any{"medium": m.Medium, "to": m.To, "subject": m.Subject, "body": body})
	return hashOf(string(b))
}

// Approved reports whether an outgoing draft is ready with the text the
// owner approved. A ready draft with no hash (before 0.3.0) counts.
func (m *IO) Approved(body string) bool {
	return m.State == IOReady && (m.ApprovedHash == "" || m.ApprovedHash == m.DraftHash(body))
}

// Verifiable reports whether the owner can find the message themself: a ref,
// or via with verify.
func (m *IO) Verifiable() bool {
	return strings.TrimSpace(m.Ref) != "" ||
		m.Via != nil && strings.TrimSpace(m.Via.Source) != "" && strings.TrimSpace(m.Via.Ref) != "" && strings.TrimSpace(m.Verify) != ""
}

// PermalinkMediums have a link to each message: an incoming one needs ref, or
// via with verify.
var PermalinkMediums = []Medium{MediumEmail, MediumSlack, MediumJira, MediumConfluence, MediumGithub, MediumTeams}

// SourceDoc is type: source, one place work comes in or goes out: a mailbox, a
// calendar, a chat, a tracker, a folder. Para fetches nothing and holds no
// secret: the source says how an agent reaches it with its own tools (Via,
// Server, Tools, or the commands in its steps), what counts as input
// (ScanIn) and output (ScanOut), and how to read one item again (Fetch) or
// its status (State). ReachedBy names the agents that can reach it; an agent
// also declares the sources it reaches at session start (agent_sources),
// kept on this machine, not in the vault. LastScan and Cursor are what the
// last sweep recorded: the cursor is opaque text the agent returns.
type SourceDoc struct {
	Common `yaml:",inline"`
	System SourceSystem `yaml:"system" json:"system"`
	// Vendor is the product, as a free label (for example the name of a mail
	// or chat product). Para compares it as text; vendor names are in one
	// mapping table only (ops/sourceclass.go).
	Vendor string       `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Status SourceStatus `yaml:"status" json:"status"`
	// Priority is how much the source matters to the owner: an incoming
	// message from a high one needs the owner. Empty is normal.
	Priority Priority  `yaml:"priority,omitempty" json:"priority,omitempty"`
	Via      SourceVia `yaml:"via" json:"via"`
	Server   string    `yaml:"server,omitempty" json:"server,omitempty"` // MCP server name
	// RefURL turns a ref of this source into a link: a template with {ref}.
	// String templating only; para fetches nothing.
	RefURL    string      `yaml:"ref_url,omitempty" json:"ref_url,omitempty"`
	Tools     []string    `yaml:"tools,omitempty" json:"tools,omitempty"` // MCP tool names
	Fetch     *SourceStep `yaml:"fetch,omitempty" json:"fetch,omitempty"`
	ScanIn    *SourceStep `yaml:"scan_in,omitempty" json:"scan_in,omitempty"`
	ScanOut   *SourceStep `yaml:"scan_out,omitempty" json:"scan_out,omitempty"`
	State     *SourceStep `yaml:"state,omitempty" json:"state,omitempty"`
	ReachedBy []string    `yaml:"reached_by,omitempty" json:"reached_by,omitempty"`
	Cadence   string      `yaml:"cadence,omitempty" json:"cadence,omitempty"` // recur grammar
	LastScan  string      `yaml:"last_scan,omitempty" json:"last_scan,omitempty"`
	Cursor    string      `yaml:"cursor,omitempty" json:"cursor,omitempty"`
	// LastFailure is when a sweep last failed or reported errors, and
	// LastError its error words (never a secret).
	LastFailure string `yaml:"last_failure,omitempty" json:"last_failure,omitempty"`
	LastError   string `yaml:"last_error,omitempty" json:"last_error,omitempty"`
}

// SourceStep is one thing an agent does on a source. Tool and Args are an
// MCP tool and its argument template; Command is a command template for a
// cli source. Query is the filter text (a search query, a JQL, a path glob);
// How says the rest in words. Templates may hold {ref}, {url}, {cursor},
// {since}, {since_date}, and {query}.
type SourceStep struct {
	Tool    string `yaml:"tool,omitempty" json:"tool,omitempty"`
	Args    string `yaml:"args,omitempty" json:"args,omitempty"`
	Command string `yaml:"command,omitempty" json:"command,omitempty"`
	Query   string `yaml:"query,omitempty" json:"query,omitempty"`
	How     string `yaml:"how,omitempty" json:"how,omitempty"`
}

// IOVia is a private source reference: the connector and its own id of the
// message. Source names a source document or a connector, as free text.
type IOVia struct {
	Source string `yaml:"source" json:"source"`
	Ref    string `yaml:"ref" json:"ref"`
	// Thread is the connector's own id of the conversation the message is
	// in (a mail thread, a chat thread, a ticket), when the tool has one.
	// Messages with the same source and thread are one conversation.
	Thread string `yaml:"thread,omitempty" json:"thread,omitempty"`
}

// Incident is type: incident, the record of one outage or fault in a system
// you run. Started and Ended are RFC 3339 times. The body holds a timeline, one
// timestamped line per note. Follow-up tasks link here with their incident field.
// Para is not a pager: alerts stay in monitoring.
type Incident struct {
	Common   `yaml:",inline"`
	Severity IncidentSeverity `yaml:"severity" json:"severity"`
	Status   IncidentStatus   `yaml:"status" json:"status"`
	Started  string           `yaml:"started" json:"started"`
	Ended    string           `yaml:"ended,omitempty" json:"ended,omitempty"`
	Area     Link             `yaml:"area,omitempty" json:"area,omitempty"`
	// Links point at dashboards, runbooks, and the ticket that live elsewhere.
	Links []Pointer `yaml:"links,omitempty" json:"links,omitempty"`
}

// Meeting is type: meeting, the older shape of a meeting. It is read, never
// written: new meetings are tasks of kind meeting, and an adapter (internal/ops)
// reads this type as one. A later vault upgrade rewrites the files.
//
// Meeting is type: meeting. Start and End are RFC 3339 times with a zone, as
// written, so the zone the meeting was booked in survives. A requested meeting
// has no time yet. With names the people or groups; RFX the requests the
// meeting is meant to settle. The body holds "## Questions" (gathered from the
// RFX and open questions in scope) and "## Notes". Para does not record or
// transcribe: it stores what a person or an agent gives it.
type Meeting struct {
	Common `yaml:",inline"`
	Status MeetingStatus `yaml:"status" json:"status"`
	Start  string        `yaml:"start,omitempty" json:"start,omitempty"`
	End    string        `yaml:"end,omitempty" json:"end,omitempty"`
	With   []Link        `yaml:"with" json:"with"`
	RFX    []Link        `yaml:"rfx,omitempty" json:"rfx,omitempty"`
}

// AsTask is the task of kind meeting that holds the same meeting: the same
// common fields, links, times, and tags. A meeting that was requested or
// scheduled is a todo task; the time is what tells them apart.
func (m *Meeting) AsTask() *Task {
	t := &Task{Common: m.Common, Kind: KindMeeting, Occasion: Occasion{Scheduled: m.Start, End: m.End, With: m.With, RFX: m.RFX}, Status: TaskTodo}
	t.Type = TypeTask
	switch m.Status {
	case MeetingHeld:
		t.Status, t.Outcome = TaskDone, OutcomeDone
	case MeetingCancelled:
		t.Status, t.Outcome = TaskDropped, OutcomeDropped
	}
	t.Tags = append([]string(nil), m.Tags...)
	return t
}

// ScopeRoot is the question scope of a vault-level question.
const ScopeRoot Link = "root"

// Question is type: question, something unknown that nobody has been asked
// yet, or a decision or an input the owner owes. Scope is required: a link to
// a project, a task, or an area, or ScopeRoot for the whole vault. The body
// holds the question, then the answer under "## Answer".
type Question struct {
	Common `yaml:",inline"`
	Status QuestionStatus `yaml:"status" json:"status"`
	Kind   QuestionKind   `yaml:"kind,omitempty" json:"kind,omitempty"` // empty means question
	Scope  Link           `yaml:"scope" json:"scope"`
	// Options are the choices, when the question has a fixed set. Recommended is
	// the one the asker suggests; it is one of Options.
	Options     []string `yaml:"options,omitempty" json:"options,omitempty"`
	Recommended string   `yaml:"recommended,omitempty" json:"recommended,omitempty"`
	// Context is what the owner needs to know to decide. OptionDetails weighs
	// each option, by name.
	Context       string         `yaml:"context,omitempty" json:"context,omitempty"`
	OptionDetails []OptionDetail `yaml:"option_details,omitempty" json:"option_details,omitempty"`
	// RFX links the request that asks this question of a person.
	RFX Link `yaml:"rfx,omitempty" json:"rfx,omitempty"`
	// Choice, AnsweredBy, and Answered record the answer: the option picked, who
	// answered, and when. The text and the reason are in the body.
	Choice     string `yaml:"choice,omitempty" json:"choice,omitempty"`
	AnsweredBy string `yaml:"answered_by,omitempty" json:"answered_by,omitempty"`
	Answered   Date   `yaml:"answered,omitempty" json:"answered,omitempty"`
	// DependsOn lists what must close before this question can be answered: a
	// task, a request, or another question.
	DependsOn []Link `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	// OptionalAfter: see Deps.
	OptionalAfter []Link `yaml:"optional_after,omitempty" json:"optional_after,omitempty"`
	// An agent never answers: it proposes. ProposedAnswer and ProposedChoice
	// wait for a person to accept, change, or reject them. ProposedSources are
	// the citations; ProposedWhy is the agent's reason. After an accept the
	// proposer and sources stay as the record. A reject clears the proposal
	// and keeps Rejected* so the agent can read why.
	ProposedAnswer  string         `yaml:"proposed_answer,omitempty" json:"proposed_answer,omitempty"`
	ProposedChoice  string         `yaml:"proposed_choice,omitempty" json:"proposed_choice,omitempty"`
	ProposedBy      string         `yaml:"proposed_by,omitempty" json:"proposed_by,omitempty"`
	ProposedSources []AnswerSource `yaml:"proposed_sources,omitempty" json:"proposed_sources,omitempty"`
	ProposedWhy     string         `yaml:"proposed_why,omitempty" json:"proposed_why,omitempty"`
	Proposed        Date           `yaml:"proposed,omitempty" json:"proposed,omitempty"`
	RejectedReason  string         `yaml:"rejected_reason,omitempty" json:"rejected_reason,omitempty"`
	RejectedBy      string         `yaml:"rejected_by,omitempty" json:"rejected_by,omitempty"`
	// ReviewState is what the owner did with the question on the review
	// stack while it is open: skipped, or sent_back to an agent (with
	// ReviewNote, ReviewRefs, and the agent work in ReviewRequest). An
	// agent's proposed answer clears them.
	ReviewState   string `yaml:"review_state,omitempty" json:"review_state,omitempty"`
	ReviewNote    string `yaml:"review_note,omitempty" json:"review_note,omitempty"`
	ReviewRefs    []Link `yaml:"review_refs,omitempty" json:"review_refs,omitempty"`
	ReviewRequest Link   `yaml:"review_request,omitempty" json:"review_request,omitempty"`
	// ReviewAt is when the owner last reviewed the question. ReviewBatch and
	// a review_state of skipped are older keys, read and ignored: a skip is
	// stored nowhere.
	ReviewAt    string `yaml:"review_at,omitempty" json:"review_at,omitempty"`
	ReviewBatch string `yaml:"review_batch,omitempty" json:"review_batch,omitempty"`
	Deferral    `yaml:",inline"`
	// Effort is about how long the question takes the owner; empty is not set.
	Effort Effort `yaml:"effort,omitempty" json:"effort,omitempty"`
}

// AnswerSource is where an agent's proposed answer came from: a URL or the id
// of a vault document, with an optional quote.
type AnswerSource struct {
	Ref   string `yaml:"ref" json:"ref"`
	Quote string `yaml:"quote,omitempty" json:"quote,omitempty"`
}

// HasProposal reports whether an agent's answer waits for a person.
func (q *Question) HasProposal() bool {
	return q.Status == QuestionOpen && q.ProposedBy != ""
}

// IsRoot reports whether the scope is the whole vault.
func (q *Question) IsRoot() bool { return q.Scope == ScopeRoot }

// AsKind returns the kind, with an empty kind read as a question.
func (q *Question) AsKind() QuestionKind {
	if q.Kind == "" {
		return KindQuestion
	}
	return q.Kind
}

// NewMeta returns an empty Meta of type t with Common.Type set.
func NewMeta(t Type) (Meta, bool) {
	c := Common{Type: t}
	switch t {
	case TypeProject:
		return &Project{Common: c}, true
	case TypeGoal:
		return &Goal{Common: c}, true
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
	case TypeIO:
		return &IO{Common: c}, true
	case TypeTrackedDoc:
		return &TrackedDoc{Common: c}, true
	case TypeSource:
		return &SourceDoc{Common: c}, true
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
	case TypeIncident:
		return &Incident{Common: c}, true
	case TypeProposal:
		return &Proposal{Common: c}, true
	case TypeQuestion:
		return &Question{Common: c}, true
	case TypeMeeting:
		return &Meeting{Common: c}, true
	case TypeReceipt:
		return &Receipt{Common: c}, true
	case TypeObligation:
		return &Obligation{Common: c}, true
	case TypeThread:
		return &ThreadDoc{Common: c}, true
	}
	return nil, false
}
