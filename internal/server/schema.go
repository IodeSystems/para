package server

import (
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/iodesystems/para/internal/export"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// The op types stay free of huma. Everything the API schema needs beyond their
// json tags is a type alias on the registry, set in registerAliases.

// enum[T] is the schema of a named string type. The values have one source,
// the same lists MCP uses.
type enum[T ~string] string

func (enum[T]) Schema(huma.Registry) *huma.Schema {
	v, _ := enumValues.Load(reflect.TypeFor[T]())
	vals, _ := v.([]string)
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}
	return &huma.Schema{Type: huma.TypeString, Enum: out}
}

var enumValues sync.Map // reflect.Type to []string

func addEnum[T ~string](reg huma.Registry, vals []T) {
	s := make([]string, len(vals))
	for i, v := range vals {
		s[i] = string(v)
	}
	enumValues.Store(reflect.TypeFor[T](), s)
	reg.RegisterTypeAlias(reflect.TypeFor[T](), reflect.TypeFor[enum[T]]())
}

// jsonValue is the GraphQL JSON scalar: a value with no single flat type.
type jsonValue struct{}

func (jsonValue) Schema(huma.Registry) *huma.Schema { return &huma.Schema{} }

// showDetail is the schema of show's detail: ShowDetail, a union of the show
// Outputs. Its "op" names the show op, so it picks the member.
type showDetail struct{}

func (showDetail) Schema(reg huma.Registry) *huma.Schema {
	u := &huma.Schema{Discriminator: &huma.Discriminator{PropertyName: "op", Mapping: map[string]string{}}}
	prefix := ""
	for _, e := range ops.ShowOps() {
		ref := reg.Schema(e.OutputType(), true, "")
		prefix = ref.Ref[:strings.LastIndex(ref.Ref, "/")+1]
		if !slices.ContainsFunc(u.OneOf, func(x *huma.Schema) bool { return x.Ref == ref.Ref }) {
			u.OneOf = append(u.OneOf, ref)
		}
		u.Discriminator.Mapping[e.Name()] = ref.Ref
	}
	reg.Map()["ShowDetail"] = u
	return &huma.Schema{Ref: prefix + "ShowDetail"}
}

// Meta is the frontmatter of every document type, flattened into one object:
// gat has no sealed union. `type` says which fields apply. A field that holds
// different enums per type is a plain string. `with` is a link on an exception
// and a list of links on an interaction, so it is the JSON scalar. `links` is
// a list of wikilinks on a request and a list of {label, kind, target}
// pointers on an area and a project, so it is the JSON scalar too. `rfx` is one
// link on a question and a list of links on a meeting: the JSON scalar.
type Meta struct {
	Type            model.Type            `json:"type"`
	Created         string                `json:"created"`
	Updated         string                `json:"updated,omitempty"`
	Tags            []string              `json:"tags,omitempty"`
	Status          string                `json:"status,omitempty"`
	Owner           string                `json:"owner,omitempty"`
	Project         string                `json:"project,omitempty"`
	Goal            string                `json:"goal,omitempty"`
	ResumeWhen      string                `json:"resume_when,omitempty"`
	StateLine       string                `json:"state_line,omitempty"`
	StateDate       string                `json:"state_date,omitempty"`
	Area            string                `json:"area,omitempty"`
	Due             string                `json:"due,omitempty"`
	Scheduled       string                `json:"scheduled,omitempty"`
	Recur           string                `json:"recur,omitempty"`
	Assignee        string                `json:"assignee,omitempty"`
	RequestedBy     string                `json:"requested_by,omitempty"`
	Previous        string                `json:"previous,omitempty"`
	Incident        string                `json:"incident,omitempty"`
	Parent          string                `json:"parent,omitempty"`
	Severity        string                `json:"severity,omitempty"`
	Started         string                `json:"started,omitempty"`
	Ended           string                `json:"ended,omitempty"`
	BlockedBy       []string              `json:"blocked_by,omitempty"`
	Outcome         model.TaskOutcome     `json:"outcome,omitempty"`
	DeclinedBy      model.DeclinedBy      `json:"declined_by,omitempty"`
	DeclinedPerson  string                `json:"declined_person,omitempty"`
	RetryAfter      string                `json:"retry_after,omitempty"`
	ReviewState     string                `json:"review_state,omitempty"`
	ReviewNote      string                `json:"review_note,omitempty"`
	ReviewRefs      []string              `json:"review_refs,omitempty"`
	ReviewRequest   string                `json:"review_request,omitempty"`
	ReviewAt        string                `json:"review_at,omitempty"`
	ReviewBatch     string                `json:"review_batch,omitempty"`
	ReroutedTo      string                `json:"rerouted_to,omitempty"`
	DependsOn       []string              `json:"depends_on,omitempty"`
	OptionalAfter   []string              `json:"optional_after,omitempty"`
	Direction       string                `json:"direction,omitempty"`
	Route           string                `json:"route,omitempty"`
	SelfServeCheck  string                `json:"self_serve_check,omitempty"`
	Targets         []model.Target        `json:"targets,omitempty"`
	Blocks          []string              `json:"blocks,omitempty"`
	NextNudge       string                `json:"next_nudge,omitempty"`
	Playbook        string                `json:"playbook,omitempty"`
	Why             *jsonValue            `json:"why,omitempty"`
	By              string                `json:"by,omitempty"`
	Source          *model.Source         `json:"source,omitempty"`
	AppliesIn       []string              `json:"applies_in,omitempty"`
	NotIn           []string              `json:"not_in,omitempty"`
	ReviewBy        string                `json:"review_by,omitempty"`
	Of              string                `json:"of,omitempty"`
	Org             string                `json:"org,omitempty"`
	Role            string                `json:"role,omitempty"`
	RoleText        string                `json:"role_text,omitempty"`
	Team            string                `json:"team,omitempty"`
	Aliases         []string              `json:"aliases,omitempty"`
	Contacts        []model.Contact       `json:"contacts,omitempty"`
	Groups          []string              `json:"groups,omitempty"`
	AwayUntil       string                `json:"away_until,omitempty"`
	QuietDays       int                   `json:"quiet_days,omitempty"`
	Kind            string                `json:"kind,omitempty"`
	Date            string                `json:"date,omitempty"`
	Medium          model.Medium          `json:"medium,omitempty"`
	URL             string                `json:"url,omitempty"`
	Visibility      model.Visibility      `json:"visibility,omitempty"`
	With            *jsonValue            `json:"with,omitempty"`
	Scope           string                `json:"scope,omitempty"`
	Policy          string                `json:"policy,omitempty"`
	PolicyText      string                `json:"policy_text,omitempty"`
	Rule            string                `json:"rule,omitempty"`
	ReviewedBy      string                `json:"reviewed_by,omitempty"`
	Verified        *bool                 `json:"verified,omitempty"`
	Runner          string                `json:"runner,omitempty"`
	QueuedAt        string                `json:"queued_at,omitempty"`
	ClaimedBy       string                `json:"claimed_by,omitempty"`
	LeaseUntil      string                `json:"lease_until,omitempty"`
	ClaimedAt       string                `json:"claimed_at,omitempty"`
	ActiveAt        string                `json:"active_at,omitempty"`
	Links           *jsonValue            `json:"links,omitempty"`
	ResultLinks     []string              `json:"result_links,omitempty"`
	Required        []string              `json:"required,omitempty"`
	Followed        []string              `json:"followed,omitempty"`
	Resolution      string                `json:"resolution,omitempty"`
	Turn            int                   `json:"turn,omitempty"`
	SourceDoc       string                `json:"source_doc,omitempty"`
	SourcePath      string                `json:"source_path,omitempty"`
	Revision        int                   `json:"revision,omitempty"`
	Supersedes      string                `json:"supersedes,omitempty"`
	ProposedBy      string                `json:"proposed_by,omitempty"`
	DoneWhen        string                `json:"done_when,omitempty"`
	DoURL           string                `json:"do_url,omitempty"`
	DoCommand       string                `json:"do_command,omitempty"`
	Steps           string                `json:"steps,omitempty"`
	RefURL          string                `json:"ref_url,omitempty"`
	ProposedAnswer  string                `json:"proposed_answer,omitempty"`
	ProposedChoice  string                `json:"proposed_choice,omitempty"`
	ProposedSources *jsonValue            `json:"proposed_sources,omitempty"`
	ProposedWhy     string                `json:"proposed_why,omitempty"`
	Proposed        string                `json:"proposed,omitempty"`
	RejectedReason  string                `json:"rejected_reason,omitempty"`
	RejectedBy      string                `json:"rejected_by,omitempty"`
	Options         []string              `json:"options,omitempty"`
	Recommended     string                `json:"recommended,omitempty"`
	Context         *jsonValue            `json:"context,omitempty"`
	OptionDetails   *jsonValue            `json:"option_details,omitempty"`
	RFX             *jsonValue            `json:"rfx,omitempty"`
	Question        string                `json:"question,omitempty"`
	Choice          string                `json:"choice,omitempty"`
	AnsweredBy      string                `json:"answered_by,omitempty"`
	Answered        string                `json:"answered,omitempty"`
	Actor           string                `json:"actor,omitempty"`
	Op              string                `json:"op,omitempty"`
	Doc             string                `json:"doc,omitempty"`
	Request         string                `json:"request,omitempty"`
	Input           string                `json:"input,omitempty"`
	Sources         []string              `json:"sources,omitempty"`
	Verify          string                `json:"verify,omitempty"`
	Confidence      *jsonValue            `json:"confidence,omitempty"`
	Check           string                `json:"check,omitempty"`
	CheckedBy       string                `json:"checked_by,omitempty"`
	CheckNote       string                `json:"check_note,omitempty"`
	Undone          string                `json:"undone,omitempty"`
	SelfReported    *bool                 `json:"self_reported,omitempty"`
	NotDuplicate    string                `json:"not_duplicate,omitempty"`
	Round           string                `json:"round,omitempty"`
	Batch           string                `json:"batch,omitempty"`
	OwnerSaid       string                `json:"owner_said,omitempty"`
	OnBehalfOf      string                `json:"on_behalf_of,omitempty"`
	Surface         string                `json:"surface,omitempty"`
	At              string                `json:"at,omitempty"`
	Background      *bool                 `json:"background,omitempty"`
	Align           *model.AlignResult    `json:"align,omitempty"`
	Start           string                `json:"start,omitempty"`
	End             string                `json:"end,omitempty"`
	DeferUntil      string                `json:"defer_until,omitempty"`
	DeferredReason  string                `json:"deferred_reason,omitempty"`
	DeferredAfter   []string              `json:"deferred_after,omitempty"`
	DeferredAdded   []string              `json:"deferred_added,omitempty"`
	DeferUntilStart string                `json:"defer_until_start,omitempty"`
	DeferUntilEnd   string                `json:"defer_until_end,omitempty"`
	Alternatives    *jsonValue            `json:"alternatives,omitempty"`
	Unknowns        *jsonValue            `json:"unknowns,omitempty"`
	Signals         *jsonValue            `json:"signals,omitempty"`
	KPIs            *jsonValue            `json:"kpis,omitempty"`
	Risks           *jsonValue            `json:"risks,omitempty"`
	Assumptions     *jsonValue            `json:"assumptions,omitempty"`
	Pivot           *jsonValue            `json:"pivot,omitempty"`
	Extensions      []string              `json:"extensions,omitempty"`
	Ecosystem       *jsonValue            `json:"ecosystem,omitempty"`
	Rank            int                   `json:"rank,omitempty"`
	Validation      string                `json:"validation,omitempty"`
	Cadence         string                `json:"cadence,omitempty"`
	LastReviewed    string                `json:"last_reviewed,omitempty"`
	Concepts        []string              `json:"concepts,omitempty"`
	Refines         string                `json:"refines,omitempty"`
	CalendarUID     string                `json:"calendar_uid,omitempty"`
	CalendarUIDs    []model.CalendarEvent `json:"calendar_uids,omitempty"`
	State           *jsonValue            `json:"state,omitempty"` // io state text, or a source step
	From            string                `json:"from,omitempty"`
	To              []string              `json:"to,omitempty"`
	Subject         string                `json:"subject,omitempty"`
	Ref             string                `json:"ref,omitempty"`
	Via             *jsonValue            `json:"via,omitempty"` // io and tracked_doc {source, ref}, or a source's mcp, cli, none
	Expects         string                `json:"expects,omitempty"`
	Effort          string                `json:"effort,omitempty"`
	ApprovedHash    string                `json:"approved_hash,omitempty"`
	Attendees       []string              `json:"attendees,omitempty"`
	Optional        *bool                 `json:"optional,omitempty"`
	Observed        *bool                 `json:"observed,omitempty"` // a logged io item the owner only saw
	Attend          string                `json:"attend,omitempty"`
	AttendDefault   string                `json:"attend_default,omitempty"`
	Became          string                `json:"became,omitempty"`
	Thread          string                `json:"thread,omitempty"`
	Reason          string                `json:"reason,omitempty"`
	System          string                `json:"system,omitempty"`
	Watchers        []string              `json:"watchers,omitempty"`
	Latest          int                   `json:"latest,omitempty"`
	LastSeen        int                   `json:"last_seen,omitempty"`
	SeenAt          string                `json:"seen_at,omitempty"`
	Checked         string                `json:"checked,omitempty"`
	Comments        *jsonValue            `json:"comments,omitempty"`
	Version         int                   `json:"version,omitempty"`
	FetchedBy       string                `json:"fetched_by,omitempty"`
	Proposal        string                `json:"proposal,omitempty"`
	Item            string                `json:"item,omitempty"`
	Server          string                `json:"server,omitempty"`
	Tools           []string              `json:"tools,omitempty"`
	Fetch           *model.SourceStep     `json:"fetch,omitempty"`
	ScanIn          *model.SourceStep     `json:"scan_in,omitempty"`
	ScanOut         *model.SourceStep     `json:"scan_out,omitempty"`
	ReachedBy       []string              `json:"reached_by,omitempty"`
	LastScan        string                `json:"last_scan,omitempty"`
	Cursor          string                `json:"cursor,omitempty"`
	RequiresSource  string                `json:"requires_source,omitempty"`
	Seen            string                `json:"seen,omitempty"`
	Vendor          string                `json:"vendor,omitempty"`
	Priority        string                `json:"priority,omitempty"` // person, group, source: high, normal, low
	Triage          string                `json:"triage,omitempty"`   // io: needs_you or digest, set by a triage rule
	TriageBy        string                `json:"triage_by,omitempty"`
	Closed          string                `json:"closed,omitempty"` // obligation: the day it closed
	Schedule        string                `json:"schedule,omitempty"`
	OwedTo          []string              `json:"owed_to,omitempty"`
	OwedBy          []string              `json:"owed_by,omitempty"`
	OccurrenceOf    string                `json:"occurrence_of,omitempty"`
	Makes           string                `json:"makes,omitempty"` // obligation, a cron: what each run makes
	Cron            string                `json:"cron,omitempty"`  // task: the cron run that made it
	FoldedFrom      string                `json:"folded_from,omitempty"`
	FoldedInto      string                `json:"folded_into,omitempty"` // an exception, convention, or constraint: the area it is an annotation in
	Follows         *model.Followed       `json:"follows,omitempty"`     // area: the source it summarizes
	Produce         *model.Produce        `json:"produce,omitempty"`     // obligation owed by an agent: what it produces
	ApprovedAt      string                `json:"approved_at,omitempty"` // io draft: when the owner approved it
	ApprovedSeen    string                `json:"approved_seen,omitempty"`
	CheckedAt       string                `json:"checked_at,omitempty"` // io draft: the last passing pre-send check
	Held            string                `json:"held,omitempty"`       // io draft: why the pre-send check held it
	HeldBy          []string              `json:"held_by,omitempty"`
	Voice           string                `json:"voice,omitempty"` // outgoing io: as_me or for_me
	Agent           string                `json:"agent,omitempty"` // for_me io draft: the agent that sends it
	Stamp           string                `json:"stamp,omitempty"`
	DraftedSeen     string                `json:"drafted_seen,omitempty"`
	AllowedBy       string                `json:"allowed_by,omitempty"` // io: the delegation rule that lets an agent send it
	Reviewed        string                `json:"reviewed,omitempty"`   // io: when the owner reviewed it
	Impact          string                `json:"impact,omitempty"`     // io out proposal: the effect
	Assessments     []model.IOAssessment  `json:"assessments,omitempty"`
	Group           string                `json:"group,omitempty"`      // io out proposal: batch key
	VariantOf       string                `json:"variant_of,omitempty"` // io out proposal: first draft of its variation set
	Revisions       []model.IORevision    `json:"revisions,omitempty"`
	Attempts        []model.IOAttempt     `json:"attempts,omitempty"`
	Reply           string                `json:"reply,omitempty"`      // incoming io: its reply obligation
	ReplyTo         string                `json:"reply_to,omitempty"`   // obligation: the io item it replies to
	Interested      []string              `json:"interested,omitempty"` // obligation: more people to tell of a change
	Notices         []string              `json:"notices,omitempty"`    // obligation: the drafts that told them
	Promise         string                `json:"promise,omitempty"`    // obligation: kept, renegotiated, broken
	LastFailure     string                `json:"last_failure,omitempty"`
	LastError       string                `json:"last_error,omitempty"`

	HealthDismissed []model.HealthDismissal `json:"health_dismissed,omitempty"`
	ImportedFrom    *model.ImportedFrom     `json:"imported_from,omitempty"`
	DuplicateOf     string                  `json:"duplicate_of,omitempty"`
	Prepared        *model.Prepared         `json:"prepared,omitempty"`
	Publish         []string                `json:"publish,omitempty"`    // publish sets (owner only)
	Threads         []string                `json:"threads,omitempty"`    // extra thread keys (model/thread.go)
	Pinned          string                  `json:"pinned,omitempty"`     // RFC 3339 time the owner pinned the item (pin)
	FormerIDs       []string                `json:"former_ids,omitempty"` // ids the document had before an id change
	Key             string                  `json:"key,omitempty"`        // thread: the pinned thread key
	TurnKind        string                  `json:"turn_kind,omitempty"`  // request: modify, ask, redirect, other
	Quote           string                  `json:"quote,omitempty"`      // request: owner-quoted text the turn is about
}

// registerAliases changes only the schema, never the JSON.
//   - int is int32: a count is a GraphQL Int and a TS number. Long is ids only,
//     and para has no integer ids.
//   - model.Meta is the flat Meta object.
//   - [][]string (graph cycles) would flatten in gat, so it is the JSON scalar.
//   - map[string]any (a proposed op's input) has no fixed shape: the JSON scalar.
//   - ops.ShowDetail is a union of the show Outputs (showDetail).
func registerAliases(reg huma.Registry) {
	reg.RegisterTypeAlias(reflect.TypeFor[int](), reflect.TypeFor[int32]())
	reg.RegisterTypeAlias(reflect.TypeFor[model.Meta](), reflect.TypeFor[Meta]())
	reg.RegisterTypeAlias(reflect.TypeFor[[][]string](), reflect.TypeFor[jsonValue]())
	reg.RegisterTypeAlias(reflect.TypeFor[map[string]any](), reflect.TypeFor[jsonValue]())
	reg.RegisterTypeAlias(reflect.TypeFor[ops.ShowDetail](), reflect.TypeFor[showDetail]())

	addEnum(reg, model.Types)
	addEnum(reg, model.Folders)
	addEnum(reg, model.GoalStatuses)
	addEnum(reg, model.ProjectStatuses)
	addEnum(reg, model.KPIDirections)
	addEnum(reg, model.CronMakesValues)
	addEnum(reg, model.Validations)
	addEnum(reg, model.PivotActions)
	addEnum(reg, model.AreaStatuses)
	addEnum(reg, model.AreaKinds)
	addEnum(reg, model.PointerKinds)
	addEnum(reg, model.TaskStatuses)
	addEnum(reg, model.TaskOutcomes)
	addEnum(reg, model.Promises)
	addEnum(reg, model.DeclinedBys)
	addEnum(reg, model.FrictionReasons)
	addEnum(reg, model.RFXDirections)
	addEnum(reg, model.RFXStatuses)
	addEnum(reg, model.Fidelities)
	addEnum(reg, model.ExceptionKinds)
	addEnum(reg, model.ExceptionStatuses)
	addEnum(reg, model.ConventionStatuses)
	addEnum(reg, model.JournalScopes)
	addEnum(reg, ops.JournalPeriods)
	addEnum(reg, ops.FeedbackKinds)
	addEnum(reg, ops.Lifecycles)
	addEnum(reg, ops.Turns)
	addEnum(reg, model.Mediums)
	addEnum(reg, model.GroupKinds)
	addEnum(reg, model.TargetStates)
	addEnum(reg, model.InteractionDirections)
	addEnum(reg, model.Visibilities)
	addEnum(reg, model.RequestStatuses)
	addEnum(reg, model.IncidentSeverities)
	addEnum(reg, model.IncidentStatuses)
	addEnum(reg, model.QuestionStatuses)
	addEnum(reg, model.QuestionKinds)
	addEnum(reg, model.MeetingStatuses)
	addEnum(reg, model.TaskKinds)
	addEnum(reg, model.ProposalStatuses)
	addEnum(reg, model.ItemKinds)
	addEnum(reg, model.ItemStates)
	addEnum(reg, model.TurnKinds)
	addEnum(reg, model.RequestResolutions)
	addEnum(reg, model.RequestTurnKinds)
	addEnum(reg, ops.RespondKinds)
	addEnum(reg, ops.RetryActions)
	addEnum(reg, model.ReceiptChecks)
	addEnum(reg, model.Confidences)
	addEnum(reg, model.TurnStates)
	addEnum(reg, model.Reversibles)
	addEnum(reg, model.PreparedKinds)
	addEnum(reg, []model.ChangeKind{model.ChangeAdded, model.ChangeChanged, model.ChangeRemoved})
	addEnum(reg, []model.Outcome{model.OutcomeLogged, model.OutcomeSynced, model.OutcomePending})
	addEnum(reg, ops.CopyFormats)
	addEnum(reg, export.Formats)
	addEnum(reg, ops.TriageTargets)
	addEnum(reg, model.Schedules)
	addEnum(reg, model.ProduceKinds)
	addEnum(reg, model.States)
	addEnum(reg, model.IODirections)
	addEnum(reg, model.IOStates)
	addEnum(reg, model.IOExpectsAll)
	addEnum(reg, model.Efforts)
	addEnum(reg, model.Attends)
	addEnum(reg, model.DocSystems)
	addEnum(reg, model.DocStatuses)
	addEnum(reg, model.CommentResolutions)
	addEnum(reg, model.SourceSystems)
	addEnum(reg, model.SourceVias)
	addEnum(reg, model.SourceStatuses)
	addEnum(reg, model.Priorities)
	addEnum(reg, model.IOTriages)
	addEnum(reg, model.IOVoices)
	addEnum(reg, model.AttemptResults)
	addEnum(reg, ops.ReplyOutcomes)
	addEnum(reg, ops.ReplyHeards)
	addEnum(reg, ops.AgendaKinds)
	addEnum(reg, ops.HistoryFilters)
	addEnum(reg, model.Levels)
	addEnum(reg, model.AssumptionStatuses)
	addEnum(reg, ops.ThreadTurnKinds)
	addEnum(reg, ops.AddTypes)
	addEnum(reg, ops.LinkRels)
	addEnum(reg, ops.WorkActions)
}
