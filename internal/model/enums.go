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
)

// Types lists every Type, in the order of paramd.Types.
var Types = []Type{
	TypeProject, TypeTask, TypeRFX, TypeConstraint, TypePolicy, TypeTranscript,
	TypePolicyException, TypePerson, TypeTeam, TypeRole, TypeDomain, TypeConvention,
	TypeJournal, TypeInbox,
}

// Folder is a top-level vault folder that holds documents.
type Folder string

const (
	FolderInbox       Folder = "inbox"
	FolderProjects    Folder = "projects"
	FolderAreas       Folder = "areas"
	FolderResources   Folder = "resources"
	FolderArchive     Folder = "archive"
	FolderRFX         Folder = "rfx"
	FolderConstraints Folder = "constraints"
	FolderPolicies    Folder = "policies"
	FolderConventions Folder = "conventions"
	FolderOrg         Folder = "org"
	FolderJournal     Folder = "journal"
)

// Folders lists every document folder.
var Folders = []Folder{
	FolderInbox, FolderProjects, FolderAreas, FolderResources, FolderArchive, FolderRFX,
	FolderConstraints, FolderPolicies, FolderConventions, FolderOrg, FolderJournal,
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
	JournalProject JournalScope = "project"
)

// JournalScopes lists every JournalScope.
var JournalScopes = []JournalScope{JournalDay, JournalWeek, JournalProject}
