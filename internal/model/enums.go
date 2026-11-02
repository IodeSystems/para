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
	TypeInbox           Type = "inbox"
	TypeGroup           Type = "group"
	TypeInteraction     Type = "interaction"
	TypeRequest         Type = "request"
	TypeArea            Type = "area"
	TypeNote            Type = "note"
	TypeIncident        Type = "incident"
	TypeProposal        Type = "proposal"
)

// Types lists every Type, in the order of paramd.Types.
var Types = []Type{
	TypeProject, TypeTask, TypeRFX, TypeConstraint, TypePolicy, TypeTranscript,
	TypePolicyException, TypePerson, TypeTeam, TypeRole, TypeDomain, TypeConvention,
	TypeJournal, TypeInbox, TypeGroup, TypeInteraction, TypeRequest,
	TypeArea, TypeNote, TypeIncident, TypeProposal,
}

// Folder is a top-level vault folder that holds documents.
type Folder string

const (
	FolderInbox        Folder = "inbox"
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
)

// Folders lists every document folder.
var Folders = []Folder{
	FolderInbox, FolderProjects, FolderAreas, FolderResources, FolderArchive, FolderRFX,
	FolderConstraints, FolderPolicies, FolderConventions, FolderOrg, FolderJournal,
	FolderInteractions, FolderRequests, FolderIncidents, FolderProposals,
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
)

// ExceptionKinds lists every ExceptionKind.
var ExceptionKinds = []ExceptionKind{
	ExceptionUnderspecified, ExceptionOverlyBroad, ExceptionBug,
	ExceptionUnenforced, ExceptionConflictWith, ExceptionInterpretedAs,
}

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
)

// Enum lists the allowed values, for schema generation.
func (TargetState) Enum() []string { return enumStrings(TargetStates) }

// TargetStates lists every TargetState.
var TargetStates = []TargetState{TargetDrafted, TargetSent, TargetReplied, TargetNoReply}

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
