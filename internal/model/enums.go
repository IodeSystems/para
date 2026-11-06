// Package model holds the typed documents of docs/format.md. It has no
// filesystem code: every surface and store shares these types.
package model

// Type is the frontmatter "type" value.
type Type string

const (
	TypeProject         Type = "project"
	TypeTask            Type = "task"
	TypeRFX             Type = "rfx"
	TypeConstraint      Type = "constraint"
	TypePolicy          Type = "policy"
	TypeTranscript      Type = "transcript"
	TypePolicyException Type = "policy_exception"
	TypePerson          Type = "person"
	TypeTeam            Type = "team"
	TypeRole            Type = "role"
	TypeDomain          Type = "domain"
	TypeConvention      Type = "convention"
	TypeJournal         Type = "journal"
	TypeInbox           Type = "inbox" // read until upgrade step io-folder; new captures are io
	TypeIO              Type = "io"
	TypeTrackedDoc      Type = "tracked_doc"
	TypeSource          Type = "source"
	TypeGroup           Type = "group"
	TypeInteraction     Type = "interaction"
	TypeRequest         Type = "request"
	TypeArea            Type = "area"
	TypeNote            Type = "note"
	TypeIncident        Type = "incident"
	TypeProposal        Type = "proposal"
	TypeReceipt         Type = "receipt"
	TypeQuestion        Type = "question"
	TypeMeeting         Type = "meeting"
)

// Types lists every Type, in the order of paramd.Types.
var Types = []Type{
	TypeProject, TypeTask, TypeRFX, TypeConstraint, TypePolicy, TypeTranscript,
	TypePolicyException, TypePerson, TypeTeam, TypeRole, TypeDomain, TypeConvention,
	TypeJournal, TypeInbox, TypeIO, TypeTrackedDoc, TypeSource, TypeGroup, TypeInteraction, TypeRequest,
	TypeArea, TypeNote, TypeIncident, TypeProposal, TypeQuestion, TypeMeeting, TypeReceipt,
}

// Folder is a top-level vault folder that holds documents.
type Folder string

const (
	FolderInbox        Folder = "inbox" // read until upgrade step io-folder; para init does not make it
	FolderIO           Folder = "io"
	FolderTracked      Folder = "tracked"
	FolderSources      Folder = "sources"
	FolderProjects     Folder = "projects"
	FolderAreas        Folder = "areas"
	FolderResources    Folder = "resources"
	FolderArchive      Folder = "archive"
	FolderRFX          Folder = "rfx"
	FolderConstraints  Folder = "constraints"
	FolderPolicies     Folder = "policies"
	FolderConventions  Folder = "conventions"
	FolderOrg          Folder = "org"
	FolderJournal      Folder = "journal"
	FolderInteractions Folder = "interactions"
	FolderRequests     Folder = "requests"
	FolderIncidents    Folder = "incidents"
	FolderProposals    Folder = "proposals"
	FolderQuestions    Folder = "questions"
	FolderMeetings     Folder = "meetings"
	FolderReceipts     Folder = "receipts"
)

// Folders lists every document folder.
var Folders = []Folder{
	FolderInbox, FolderIO, FolderTracked, FolderSources, FolderProjects, FolderAreas, FolderResources, FolderArchive, FolderRFX,
	FolderConstraints, FolderPolicies, FolderConventions, FolderOrg, FolderJournal,
	FolderInteractions, FolderRequests, FolderIncidents, FolderProposals, FolderQuestions, FolderMeetings,
	FolderReceipts,
}

// Valid reports whether f is a document folder.
func (f Folder) Valid() bool {
	for _, x := range Folders {
		if f == x {
			return true
		}
	}
	return false
}

// ProjectStatus is project.status.
type ProjectStatus string

const (
	ProjectActive  ProjectStatus = "active"
	ProjectPaused  ProjectStatus = "paused"
	ProjectDone    ProjectStatus = "done"
	ProjectDropped ProjectStatus = "dropped"
)

// ProjectStatuses lists every ProjectStatus.
var ProjectStatuses = []ProjectStatus{ProjectActive, ProjectPaused, ProjectDone, ProjectDropped}

// PointerKind is pointers[].kind: what a pointer to outside material is.
type PointerKind string

const (
	PointerRepo      PointerKind = "repo"
	PointerPlan      PointerKind = "plan"
	PointerDocs      PointerKind = "docs"
	PointerRunbook   PointerKind = "runbook"
	PointerTracker   PointerKind = "tracker"
	PointerDashboard PointerKind = "dashboard"
	PointerOther     PointerKind = "other"
)

// Enum lists the allowed values, for schema generation.
func (PointerKind) Enum() []string { return enumStrings(PointerKinds) }

// PointerKinds lists every PointerKind.
var PointerKinds = []PointerKind{
	PointerRepo, PointerPlan, PointerDocs, PointerRunbook, PointerTracker, PointerDashboard, PointerOther,
}

// AreaStatus is area.status.
type AreaStatus string

const (
	AreaActive AreaStatus = "active"
	AreaPaused AreaStatus = "paused"
)

// Enum lists the allowed values, for schema generation.
func (AreaStatus) Enum() []string { return enumStrings(AreaStatuses) }

// AreaStatuses lists every AreaStatus.
var AreaStatuses = []AreaStatus{AreaActive, AreaPaused}

// AreaKind is area.kind. A project area is an ongoing body of work that goals
// call home; a concept area is a discipline that spans work. An empty kind
// reads as project.
type AreaKind string

const (
	AreaKindProject AreaKind = "project"
	AreaKindConcept AreaKind = "concept"
)

// Enum lists the allowed values, for schema generation.
func (AreaKind) Enum() []string { return enumStrings(AreaKinds) }

// AreaKinds lists every AreaKind.
var AreaKinds = []AreaKind{AreaKindProject, AreaKindConcept}

// Or returns the kind, with an empty kind read as project.
func (k AreaKind) Or() AreaKind {
	if k == "" {
		return AreaKindProject
	}
	return k
}

// TaskStatus is task.status.
type TaskStatus string

const (
	TaskTodo    TaskStatus = "todo"
	TaskDoing   TaskStatus = "doing"
	TaskBlocked TaskStatus = "blocked"
	TaskDone    TaskStatus = "done"
	TaskDropped TaskStatus = "dropped"
)

// TaskStatuses lists every TaskStatus.
var TaskStatuses = []TaskStatus{TaskTodo, TaskDoing, TaskBlocked, TaskDone, TaskDropped}

// TaskOutcome is task.outcome: how a closed task ended. Empty on a task closed
// before outcomes, and on an open task. The status says closed; the outcome
// says how.
type TaskOutcome string

const (
	OutcomeDone       TaskOutcome = "done"
	OutcomePartial    TaskOutcome = "partial"
	OutcomeDropped    TaskOutcome = "dropped"
	OutcomeDuplicate  TaskOutcome = "duplicate"
	OutcomeSuperseded TaskOutcome = "superseded"
	// OutcomeDeclined: someone said no (the owner, or another person).
	OutcomeDeclined TaskOutcome = "declined"
	// OutcomeFailed: it was tried and could not be done; it may be tried again
	// after retry_after.
	OutcomeFailed TaskOutcome = "failed"
	// OutcomeRerouted: another item or an outside system holds the work now.
	OutcomeRerouted TaskOutcome = "rerouted"
)

// DeclinedBy is task.declined_by: who declined a task, owner or other.
type DeclinedBy string

const (
	DeclinedByOwner DeclinedBy = "owner"
	DeclinedByOther DeclinedBy = "other"
)

// DeclinedBys lists every DeclinedBy.
var DeclinedBys = []DeclinedBy{DeclinedByOwner, DeclinedByOther}

// Enum lists the allowed values, for schema generation.
func (DeclinedBy) Enum() []string { return enumStrings(DeclinedBys) }

// TaskOutcomes lists every TaskOutcome.
var TaskOutcomes = []TaskOutcome{OutcomeDone, OutcomePartial, OutcomeDropped, OutcomeDuplicate, OutcomeSuperseded, OutcomeDeclined, OutcomeFailed, OutcomeRerouted}

// Enum lists the allowed values, for schema generation.
func (TaskOutcome) Enum() []string { return enumStrings(TaskOutcomes) }

// TaskKind is task.kind: what sort of work the task is. Empty means task.
// A meeting is a task of kind meeting; a phone call, time worked with someone,
// and something owed are kinds too.
type TaskKind string

const (
	KindTask       TaskKind = "task"
	KindMeeting    TaskKind = "meeting"
	KindCall       TaskKind = "call"
	KindWorkWith   TaskKind = "work_with"
	KindCommitment TaskKind = "commitment"
)

// TaskKinds lists every TaskKind.
var TaskKinds = []TaskKind{KindTask, KindMeeting, KindCall, KindWorkWith, KindCommitment}

// Enum lists the allowed values, for schema generation.
func (TaskKind) Enum() []string { return enumStrings(TaskKinds) }

// RFXDirection is rfx.direction. Out: the user asks someone. In: someone asks
// the user.
type RFXDirection string

const (
	RFXIn  RFXDirection = "in"
	RFXOut RFXDirection = "out"
)

// Enum lists the allowed values, for schema generation.
func (RFXDirection) Enum() []string { return enumStrings(RFXDirections) }

// RFXDirections lists every RFXDirection.
var RFXDirections = []RFXDirection{RFXIn, RFXOut}

// RFXStatus is rfx.status.
type RFXStatus string

const (
	RFXOpen     RFXStatus = "open"
	RFXAnswered RFXStatus = "answered"
	RFXClosed   RFXStatus = "closed"
	RFXDeclined RFXStatus = "declined"
)

// Enum lists the allowed values, for schema generation.
func (RFXStatus) Enum() []string { return enumStrings(RFXStatuses) }

// RFXStatuses lists every RFXStatus.
var RFXStatuses = []RFXStatus{RFXOpen, RFXAnswered, RFXClosed, RFXDeclined}

// Fidelity is source.fidelity.
type Fidelity string

const (
	FidelityExact   Fidelity = "exact"
	FidelityOCR     Fidelity = "ocr"
	FidelityPartial Fidelity = "partial"
	FidelityNone    Fidelity = "none"
)

// Fidelities lists every Fidelity.
var Fidelities = []Fidelity{FidelityExact, FidelityOCR, FidelityPartial, FidelityNone}

// ExceptionKind is policy_exception.kind.
type ExceptionKind string

const (
	ExceptionUnderspecified ExceptionKind = "underspecified"
	ExceptionOverlyBroad    ExceptionKind = "overly_broad"
	ExceptionBug            ExceptionKind = "bug"
	ExceptionUnenforced     ExceptionKind = "unenforced"
	ExceptionConflictWith   ExceptionKind = "conflict_with"
	ExceptionInterpretedAs  ExceptionKind = "interpreted_as"
	// ExceptionEstablishedExample: precedent shows how the text is applied. It
	// is a record of practice, not a ruling.
	ExceptionEstablishedExample ExceptionKind = "established_example"
	// ExceptionWideEnoughFor: on a plain reading, the text already covers the case.
	ExceptionWideEnoughFor ExceptionKind = "wide_enough_for"
)

// ExceptionKinds lists every ExceptionKind.
var ExceptionKinds = []ExceptionKind{
	ExceptionUnderspecified, ExceptionOverlyBroad, ExceptionBug,
	ExceptionUnenforced, ExceptionConflictWith, ExceptionInterpretedAs,
	ExceptionEstablishedExample, ExceptionWideEnoughFor,
}

// ByUnknown is the policy_exception.by value for an exception whose author nobody
// recorded. It is valid; health flags it.
const ByUnknown = "unknown"

// ExceptionStatus is policy_exception.status.
type ExceptionStatus string

const (
	ExceptionOpen     ExceptionStatus = "open"
	ExceptionResolved ExceptionStatus = "resolved"
	ExceptionAccepted ExceptionStatus = "accepted"
)

// ExceptionStatuses lists every ExceptionStatus.
var ExceptionStatuses = []ExceptionStatus{ExceptionOpen, ExceptionResolved, ExceptionAccepted}

// ConventionStatus is convention.status.
type ConventionStatus string

const (
	ConventionDraft   ConventionStatus = "draft"
	ConventionAdopted ConventionStatus = "adopted"
	ConventionRetired ConventionStatus = "retired"
)

// ConventionStatuses lists every ConventionStatus.
var ConventionStatuses = []ConventionStatus{ConventionDraft, ConventionAdopted, ConventionRetired}

// JournalScope is journal.scope.
type JournalScope string

const (
	JournalDay     JournalScope = "day"
	JournalWeek    JournalScope = "week"
	JournalMonth   JournalScope = "month"
	JournalProject JournalScope = "project"
)

// JournalScopes lists every JournalScope.
var JournalScopes = []JournalScope{JournalDay, JournalWeek, JournalMonth, JournalProject}

// Medium is how people are reached: a contact method, an RFX target medium,
// and an interaction medium.
type Medium string

const (
	MediumEmail      Medium = "email"
	MediumSlack      Medium = "slack"
	MediumJira       Medium = "jira"
	MediumConfluence Medium = "confluence"
	MediumGithub     Medium = "github"
	MediumTeams      Medium = "teams"
	MediumPhone      Medium = "phone"
	MediumMeeting    Medium = "meeting"
	MediumOther      Medium = "other"
)

// Enum lists the allowed values, for schema generation.
func (Medium) Enum() []string { return enumStrings(Mediums) }

// Mediums lists every Medium.
var Mediums = []Medium{
	MediumEmail, MediumSlack, MediumJira, MediumConfluence, MediumGithub,
	MediumTeams, MediumPhone, MediumMeeting, MediumOther,
}

// GroupKind is group.kind.
type GroupKind string

const (
	GroupTeam       GroupKind = "team"
	GroupDepartment GroupKind = "department"
	GroupChannel    GroupKind = "channel"
	GroupList       GroupKind = "list"
	GroupOther      GroupKind = "other"
)

// Enum lists the allowed values, for schema generation.
func (GroupKind) Enum() []string { return enumStrings(GroupKinds) }

// GroupKinds lists every GroupKind.
var GroupKinds = []GroupKind{GroupTeam, GroupDepartment, GroupChannel, GroupList, GroupOther}

// TargetState is rfx.targets[].state.
type TargetState string

const (
	TargetDrafted TargetState = "drafted"
	TargetSent    TargetState = "sent"
	TargetReplied TargetState = "replied"
	TargetNoReply TargetState = "no_reply"
	// TargetRequested and TargetScheduled apply to a target whose medium is
	// meeting: the meeting is asked for, then booked.
	TargetRequested TargetState = "requested"
	TargetScheduled TargetState = "scheduled"
)

// Enum lists the allowed values, for schema generation.
func (TargetState) Enum() []string { return enumStrings(TargetStates) }

// TargetStates lists every TargetState.
var TargetStates = []TargetState{TargetDrafted, TargetSent, TargetReplied, TargetNoReply, TargetRequested, TargetScheduled}

// FrictionReason is rfx.targets[].reason: why an Ask was redirected or stalled.
// It feeds the friction report. It says what went wrong with the process, never
// who is to blame.
type FrictionReason string

const (
	FrictionWrongOwner     FrictionReason = "wrong_owner"
	FrictionNoOwner        FrictionReason = "no_owner"
	FrictionPolicyConflict FrictionReason = "policy_conflict"
	FrictionPolicyUnclear  FrictionReason = "policy_unclear"
	FrictionNoResponse     FrictionReason = "no_response"
	FrictionCapacity       FrictionReason = "capacity"
	FrictionNeedsApproval  FrictionReason = "needs_approval"
	FrictionToolGap        FrictionReason = "tool_gap"
	FrictionOther          FrictionReason = "other"
)

// Enum lists the allowed values, for schema generation.
func (FrictionReason) Enum() []string { return enumStrings(FrictionReasons) }

// FrictionReasons lists every FrictionReason.
var FrictionReasons = []FrictionReason{FrictionWrongOwner, FrictionNoOwner, FrictionPolicyConflict, FrictionPolicyUnclear,
	FrictionNoResponse, FrictionCapacity, FrictionNeedsApproval, FrictionToolGap, FrictionOther}

// InteractionDirection is interaction.direction. Observed: the user saw it
// and took no part.
type InteractionDirection string

const (
	InteractionIn       InteractionDirection = "in"
	InteractionOut      InteractionDirection = "out"
	InteractionObserved InteractionDirection = "observed"
)

// Enum lists the allowed values, for schema generation.
func (InteractionDirection) Enum() []string { return enumStrings(InteractionDirections) }

// InteractionDirections lists every InteractionDirection.
var InteractionDirections = []InteractionDirection{InteractionIn, InteractionOut, InteractionObserved}

// IODirection is io.direction. In: it came to the owner. Out: the owner sends it.
type IODirection string

const (
	IOIn  IODirection = "in"
	IOOut IODirection = "out"
)

// Enum lists the allowed values, for schema generation.
func (IODirection) Enum() []string { return enumStrings(IODirections) }

// IODirections lists every IODirection.
var IODirections = []IODirection{IOIn, IOOut}

// IOState is io.state. In: unread, read, filed, dropped. Out: draft, ready,
// sent, dropped. Filed, sent, and dropped items are dealt with.
type IOState string

const (
	IOUnread  IOState = "unread"
	IORead    IOState = "read"
	IOFiled   IOState = "filed"
	IODraft   IOState = "draft"
	IOReady   IOState = "ready"
	IOSent    IOState = "sent"
	IODropped IOState = "dropped"
)

// Enum lists the allowed values, for schema generation.
func (IOState) Enum() []string { return enumStrings(IOStates) }

// IOStates lists every IOState.
var IOStates = []IOState{IOUnread, IORead, IOFiled, IODraft, IOReady, IOSent, IODropped}

// IOStatesOf lists the states of one direction.
func IOStatesOf(dir IODirection) []IOState {
	if dir == IOOut {
		return []IOState{IODraft, IOReady, IOSent, IODropped}
	}
	return []IOState{IOUnread, IORead, IOFiled, IODropped}
}

// Open reports whether an item in state s is not dealt with yet.
func (s IOState) Open() bool {
	return s == IOUnread || s == IORead || s == IODraft || s == IOReady
}

// Effort is the effort field of a task, question, rfx, or io item: about how
// long it takes the owner. Empty is not set; the value order then takes a
// default (ops.RankValue).
type Effort string

const (
	EffortXS Effort = "xs" // under 15 minutes
	EffortS  Effort = "s"  // under an hour
	EffortM  Effort = "m"  // half a day
	EffortL  Effort = "l"  // a day or more
)

// Enum lists the allowed values, for schema generation.
func (Effort) Enum() []string { return enumStrings(Efforts) }

// Efforts lists every Effort, smallest first.
var Efforts = []Effort{EffortXS, EffortS, EffortM, EffortL}

// Attend is a meeting's attend field: whether the owner goes. Empty is
// undecided. An undecided optional meeting asks nothing of the owner; a
// meeting with attend no leaves the agenda and holds nothing up.
type Attend string

const (
	AttendYes   Attend = "yes"
	AttendNo    Attend = "no"
	AttendMaybe Attend = "maybe"
)

// Enum lists the allowed values, for schema generation.
func (Attend) Enum() []string { return enumStrings(Attends) }

// Attends lists every Attend.
var Attends = []Attend{AttendYes, AttendNo, AttendMaybe}

// IOExpects is io.expects: whether a reply is wanted. On an outgoing message
// it says what sending makes; on an incoming one, response means the sender
// waits for the owner's reply.
type IOExpects string

const (
	ExpectsResponse IOExpects = "response"
	ExpectsNone     IOExpects = "none"
)

// Enum lists the allowed values, for schema generation.
func (IOExpects) Enum() []string { return enumStrings(IOExpectsAll) }

// IOExpectsAll lists every IOExpects.
var IOExpectsAll = []IOExpects{ExpectsResponse, ExpectsNone}

// IOTriage is io.triage: what a triage rule decided for an incoming message.
// needs_you: it needs the owner; digest: it goes to the day's inbox digest.
// Empty: no rule decided, and the default rule applies (ops/needs.go).
type IOTriage string

const (
	TriageNeedsYou IOTriage = "needs_you"
	TriageDigest   IOTriage = "digest"
)

// Enum lists the allowed values, for schema generation.
func (IOTriage) Enum() []string { return enumStrings(IOTriages) }

// IOTriages lists every IOTriage.
var IOTriages = []IOTriage{TriageNeedsYou, TriageDigest}

// DocSystem is tracked_doc.system: where the tracked document lives.
type DocSystem string

const (
	SystemConfluence DocSystem = "confluence"
	SystemJira       DocSystem = "jira"
	SystemGitHub     DocSystem = "github"
	SystemGDoc       DocSystem = "gdoc"
	SystemOther      DocSystem = "other"
)

// Enum lists the allowed values, for schema generation.
func (DocSystem) Enum() []string { return enumStrings(DocSystems) }

// DocSystems lists every DocSystem.
var DocSystems = []DocSystem{SystemConfluence, SystemJira, SystemGitHub, SystemGDoc, SystemOther}

// DocStatus is tracked_doc.status. Watching documents are checked and their
// changes shown; paused ones keep their versions and are not checked;
// archived ones are history.
type DocStatus string

const (
	DocWatching DocStatus = "watching"
	DocPaused   DocStatus = "paused"
	DocArchived DocStatus = "archived"
)

// Enum lists the allowed values, for schema generation.
func (DocStatus) Enum() []string { return enumStrings(DocStatuses) }

// DocStatuses lists every DocStatus.
var DocStatuses = []DocStatus{DocWatching, DocPaused, DocArchived}

// CommentResolution is tracked_doc.comments[].resolution. Empty is open.
// Became: an Ask, question, or commitment holds it (became links it).
// Resolved: it needs nothing more. Dropped: set aside with a note.
type CommentResolution string

const (
	CommentBecame   CommentResolution = "became"
	CommentResolved CommentResolution = "resolved"
	CommentDropped  CommentResolution = "dropped"
)

// Enum lists the allowed values, for schema generation.
func (CommentResolution) Enum() []string { return enumStrings(CommentResolutions) }

// CommentResolutions lists every CommentResolution.
var CommentResolutions = []CommentResolution{CommentBecame, CommentResolved, CommentDropped}

// SourceSystem is source.system: the class of system a source is (mail,
// chat, tracker, wiki, calendar, other). The product is source.vendor, a free
// label. An older source names a product here (gmail, slack, jira, ...): it
// reads as an alias of a class and a vendor (ops/sourceclass.go) and is not
// rewritten; a new or edited source is written as class and vendor.
type SourceSystem string

const (
	SourceMail     SourceSystem = "mail"
	SourceChat     SourceSystem = "chat"
	SourceTracker  SourceSystem = "tracker"
	SourceWiki     SourceSystem = "wiki"
	SourceCalendar SourceSystem = "calendar"
	SourceOther    SourceSystem = "other"

	// Older values, read as aliases of a class and a vendor.
	SourceGmail      SourceSystem = "gmail"
	SourceGDocs      SourceSystem = "gdocs"
	SourceGCal       SourceSystem = "gcal"
	SourceSlack      SourceSystem = "slack"
	SourceGitHub     SourceSystem = "github"
	SourceJira       SourceSystem = "jira"
	SourceConfluence SourceSystem = "confluence"
	SourceFolder     SourceSystem = "folder"
)

// Enum lists the allowed values, for schema generation.
func (SourceSystem) Enum() []string { return enumStrings(SourceSystems) }

// SourceClasses lists the classes a new source is written with.
var SourceClasses = []SourceSystem{SourceMail, SourceChat, SourceTracker, SourceWiki, SourceCalendar, SourceOther}

// SourceSystems lists every SourceSystem a file may hold: the classes, then
// the older aliases.
var SourceSystems = []SourceSystem{SourceMail, SourceChat, SourceTracker, SourceWiki, SourceCalendar, SourceOther,
	SourceGmail, SourceGDocs, SourceGCal, SourceSlack, SourceGitHub, SourceJira, SourceConfluence, SourceFolder}

// Priority is how much a person, a group, or a source matters to the owner:
// an incoming message from a high one needs the owner (ops/needs.go). Empty
// is normal.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// Enum lists the allowed values, for schema generation.
func (Priority) Enum() []string { return enumStrings(Priorities) }

// Priorities lists every Priority.
var Priorities = []Priority{PriorityHigh, PriorityNormal, PriorityLow}

// SourceVia is source.via: how an agent reaches the source. mcp: tools of an
// MCP server the agent has (it may exist only inside the agent's harness, so
// para cannot check it). cli: commands on this machine's PATH. none: the agent
// reads it some other way, described in words.
type SourceVia string

const (
	SourceViaMCP  SourceVia = "mcp"
	SourceViaCLI  SourceVia = "cli"
	SourceViaNone SourceVia = "none"
)

// Enum lists the allowed values, for schema generation.
func (SourceVia) Enum() []string { return enumStrings(SourceVias) }

// SourceVias lists every SourceVia.
var SourceVias = []SourceVia{SourceViaMCP, SourceViaCLI, SourceViaNone}

// SourceStatus is source.status. A paused source is not swept or scheduled.
type SourceStatus string

const (
	SourceActive SourceStatus = "active"
	SourcePaused SourceStatus = "paused"
)

// Enum lists the allowed values, for schema generation.
func (SourceStatus) Enum() []string { return enumStrings(SourceStatuses) }

// SourceStatuses lists every SourceStatus.
var SourceStatuses = []SourceStatus{SourceActive, SourcePaused}

// Visibility is interaction.visibility: where it happened.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

// Enum lists the allowed values, for schema generation.
func (Visibility) Enum() []string { return enumStrings(Visibilities) }

// Visibilities lists every Visibility.
var Visibilities = []Visibility{VisibilityPublic, VisibilityPrivate}

// RequestStatus is request.status. Proposed is a request an agent surface
// made: it is never claimed until a person accepts it.
type RequestStatus string

const (
	RequestProposed  RequestStatus = "proposed"
	RequestQueued    RequestStatus = "queued"
	RequestClaimed   RequestStatus = "claimed"
	RequestDone      RequestStatus = "done"
	RequestFailed    RequestStatus = "failed"
	RequestCancelled RequestStatus = "cancelled"
)

// Enum lists the allowed values, for schema generation.
func (RequestStatus) Enum() []string { return enumStrings(RequestStatuses) }

// RequestStatuses lists every RequestStatus.
var RequestStatuses = []RequestStatus{
	RequestProposed, RequestQueued, RequestClaimed, RequestDone, RequestFailed, RequestCancelled,
}

func enumStrings[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

// IncidentSeverity is incident.severity: sev1 is the worst.
type IncidentSeverity string

const (
	Sev1 IncidentSeverity = "sev1"
	Sev2 IncidentSeverity = "sev2"
	Sev3 IncidentSeverity = "sev3"
)

// Enum lists the allowed values, for schema generation.
func (IncidentSeverity) Enum() []string { return enumStrings(IncidentSeverities) }

// IncidentSeverities lists every IncidentSeverity, worst first.
var IncidentSeverities = []IncidentSeverity{Sev1, Sev2, Sev3}

// IncidentStatus is incident.status.
type IncidentStatus string

const (
	IncidentOpen      IncidentStatus = "open"
	IncidentMitigated IncidentStatus = "mitigated"
	IncidentResolved  IncidentStatus = "resolved"
)

// Enum lists the allowed values, for schema generation.
func (IncidentStatus) Enum() []string { return enumStrings(IncidentStatuses) }

// IncidentStatuses lists every IncidentStatus.
var IncidentStatuses = []IncidentStatus{IncidentOpen, IncidentMitigated, IncidentResolved}

// QuestionStatus is question.status.
type QuestionStatus string

const (
	QuestionOpen     QuestionStatus = "open"
	QuestionAnswered QuestionStatus = "answered"
	QuestionDropped  QuestionStatus = "dropped"
)

// Enum lists the allowed values, for schema generation.
func (QuestionStatus) Enum() []string { return enumStrings(QuestionStatuses) }

// QuestionStatuses lists every QuestionStatus.
var QuestionStatuses = []QuestionStatus{QuestionOpen, QuestionAnswered, QuestionDropped}

// QuestionKind is question.kind: what the owner is asked to give. A question
// wants facts, a decision wants a choice and its reason, an input wants a thing
// (a key, a date) that only the owner has.
type QuestionKind string

const (
	KindQuestion QuestionKind = "question"
	KindDecision QuestionKind = "decision"
	KindInput    QuestionKind = "input"
)

// Enum lists the allowed values, for schema generation.
func (QuestionKind) Enum() []string { return enumStrings(QuestionKinds) }

// QuestionKinds lists every QuestionKind.
var QuestionKinds = []QuestionKind{KindQuestion, KindDecision, KindInput}

// MeetingStatus is meeting.status.
type MeetingStatus string

const (
	MeetingRequested MeetingStatus = "requested"
	MeetingScheduled MeetingStatus = "scheduled"
	MeetingHeld      MeetingStatus = "held"
	MeetingCancelled MeetingStatus = "cancelled"
)

// Enum lists the allowed values, for schema generation.
func (MeetingStatus) Enum() []string { return enumStrings(MeetingStatuses) }

// MeetingStatuses lists every MeetingStatus.
var MeetingStatuses = []MeetingStatus{MeetingRequested, MeetingScheduled, MeetingHeld, MeetingCancelled}
