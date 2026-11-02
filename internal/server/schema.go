package server

import (
	"reflect"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/iodesystems/parasolve/internal/export"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
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

// Meta is the frontmatter of every document type, flattened into one object:
// gat has no sealed union. `type` says which fields apply. A field that holds
// different enums per type is a plain string. `with` is a link on an exception
// and a list of links on an interaction, so it is the JSON scalar. `links` is
// a list of wikilinks on a request and a list of {label, kind, target}
// pointers on an area and a project, so it is the JSON scalar too.
type Meta struct {
	Type           model.Type       `json:"type"`
	Created        string           `json:"created"`
	Updated        string           `json:"updated,omitempty"`
	Tags           []string         `json:"tags,omitempty"`
	Status         string           `json:"status,omitempty"`
	Owner          string           `json:"owner,omitempty"`
	Project        string           `json:"project,omitempty"`
	Area           string           `json:"area,omitempty"`
	Due            string           `json:"due,omitempty"`
	Recur          string           `json:"recur,omitempty"`
	BlockedBy      []string         `json:"blocked_by,omitempty"`
	DependsOn      []string         `json:"depends_on,omitempty"`
	Direction      string           `json:"direction,omitempty"`
	Route          string           `json:"route,omitempty"`
	SelfServeCheck string           `json:"self_serve_check,omitempty"`
	Targets        []model.Target   `json:"targets,omitempty"`
	Blocks         []string         `json:"blocks,omitempty"`
	NextNudge      string           `json:"next_nudge,omitempty"`
	Playbook       string           `json:"playbook,omitempty"`
	Why            string           `json:"why,omitempty"`
	By             string           `json:"by,omitempty"`
	Source         *model.Source    `json:"source,omitempty"`
	AppliesIn      []string         `json:"applies_in,omitempty"`
	NotIn          []string         `json:"not_in,omitempty"`
	ReviewBy       string           `json:"review_by,omitempty"`
	Of             string           `json:"of,omitempty"`
	Org            string           `json:"org,omitempty"`
	Role           string           `json:"role,omitempty"`
	Team           string           `json:"team,omitempty"`
	Aliases        []string         `json:"aliases,omitempty"`
	Contacts       []model.Contact  `json:"contacts,omitempty"`
	Groups         []string         `json:"groups,omitempty"`
	Kind           string           `json:"kind,omitempty"`
	Date           string           `json:"date,omitempty"`
	Medium         model.Medium     `json:"medium,omitempty"`
	URL            string           `json:"url,omitempty"`
	Visibility     model.Visibility `json:"visibility,omitempty"`
	With           *jsonValue       `json:"with,omitempty"`
	Scope          string           `json:"scope,omitempty"`
	Policy         string           `json:"policy,omitempty"`
	Rule           string           `json:"rule,omitempty"`
	ReviewedBy     string           `json:"reviewed_by,omitempty"`
	Verified       *bool            `json:"verified,omitempty"`
	Runner         string           `json:"runner,omitempty"`
	QueuedAt       string           `json:"queued_at,omitempty"`
	ClaimedBy      string           `json:"claimed_by,omitempty"`
	LeaseUntil     string           `json:"lease_until,omitempty"`
	Links          *jsonValue       `json:"links,omitempty"`
	ResultLinks    []string         `json:"result_links,omitempty"`
}

// registerAliases changes only the schema, never the JSON.
//   - int is int32: a count is a GraphQL Int and a TS number. Long is ids only,
//     and para has no integer ids.
//   - model.Meta is the flat Meta object.
//   - [][]string (graph cycles) would flatten in gat, so it is the JSON scalar.
func registerAliases(reg huma.Registry) {
	reg.RegisterTypeAlias(reflect.TypeFor[int](), reflect.TypeFor[int32]())
	reg.RegisterTypeAlias(reflect.TypeFor[model.Meta](), reflect.TypeFor[Meta]())
	reg.RegisterTypeAlias(reflect.TypeFor[[][]string](), reflect.TypeFor[jsonValue]())

	addEnum(reg, model.Types)
	addEnum(reg, model.Folders)
	addEnum(reg, model.ProjectStatuses)
	addEnum(reg, model.AreaStatuses)
	addEnum(reg, model.PointerKinds)
	addEnum(reg, model.TaskStatuses)
	addEnum(reg, model.RFXDirections)
	addEnum(reg, model.RFXStatuses)
	addEnum(reg, model.Fidelities)
	addEnum(reg, model.ExceptionKinds)
	addEnum(reg, model.ExceptionStatuses)
	addEnum(reg, model.ConventionStatuses)
	addEnum(reg, model.JournalScopes)
	addEnum(reg, model.Mediums)
	addEnum(reg, model.GroupKinds)
	addEnum(reg, model.TargetStates)
	addEnum(reg, model.InteractionDirections)
	addEnum(reg, model.Visibilities)
	addEnum(reg, model.RequestStatuses)
	addEnum(reg, []model.ChangeKind{model.ChangeAdded, model.ChangeChanged, model.ChangeRemoved})
	addEnum(reg, []model.Outcome{model.OutcomeLogged, model.OutcomeSynced, model.OutcomePending})
	addEnum(reg, ops.CopyFormats)
	addEnum(reg, export.Formats)
	addEnum(reg, ops.TriageTargets)
	addEnum(reg, ops.ReplyOutcomes)
}
