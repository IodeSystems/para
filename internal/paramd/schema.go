package paramd

type kind int

const (
	kString kind = iota
	kDate
	kBool
	kEnum
	kList
	kLink      // "[[name]]"
	kRuleLink  // "[[name#^R4]]"
	kSourceMap // source block
	kLinkList  // list of "[[name]]"
	kContacts  // list of contact maps
	kPointers  // list of {label, kind, target} maps
	kTargets   // list of rfx target maps
	kTimestamp // RFC 3339 time; YAML may read it as a time or a string
)

type field struct {
	Name     string
	Kind     kind
	Enum     []string
	Required bool
}

var (
	projectStatus   = []string{"active", "paused", "done", "dropped"}
	taskStatus      = []string{"todo", "doing", "blocked", "done", "dropped"}
	rfxDirection    = []string{"in", "out"}
	rfxStatus       = []string{"open", "answered", "closed", "declined"}
	fidelities      = []string{"exact", "ocr", "partial", "none"}
	exceptionKinds  = []string{"underspecified", "overly_broad", "bug", "unenforced", "conflict_with", "interpreted_as"}
	exceptionStatus = []string{"open", "resolved", "accepted"}
	conventionState = []string{"draft", "adopted", "retired"}
	mediums         = []string{"email", "slack", "jira", "confluence", "github", "teams", "phone", "meeting", "other"}
	groupKinds      = []string{"team", "department", "channel", "list", "other"}
	targetStates    = []string{"drafted", "sent", "replied", "no_reply"}
	interDirections = []string{"in", "out", "observed"}
	visibilities    = []string{"public", "private"}
	journalScopes   = []string{"day", "week", "month", "project"}
	pointerKinds    = []string{"repo", "plan", "docs", "runbook", "tracker", "dashboard", "other"}
	areaStatus      = []string{"active", "paused"}
	requestStatus   = []string{"proposed", "queued", "claimed", "done", "failed", "cancelled"}
)

// Types lists every valid value of the frontmatter "type" field.
var Types = []string{
	"project", "task", "rfx", "constraint", "policy", "transcript",
	"policy_exception", "person", "team", "role", "domain", "convention", "journal", "inbox",
	"group", "interaction", "request", "area", "note",
}

// commonFields apply to every type.
var commonFields = []field{
	{Name: "type", Kind: kEnum, Enum: Types, Required: true},
	{Name: "created", Kind: kDate, Required: true},
	{Name: "updated", Kind: kDate},
	{Name: "tags", Kind: kList},
}

// typeFields are the per-type fields, on top of commonFields.
var typeFields = map[string][]field{
	"project": {
		{Name: "status", Kind: kEnum, Enum: projectStatus, Required: true},
		{Name: "owner", Kind: kString, Required: true},
		{Name: "area", Kind: kLink},
		{Name: "links", Kind: kPointers},
	},
	"area": {
		{Name: "status", Kind: kEnum, Enum: areaStatus, Required: true},
		{Name: "links", Kind: kPointers},
	},
	"note": {
		{Name: "area", Kind: kLink},
		{Name: "project", Kind: kLink},
	},
	"task": {
		{Name: "status", Kind: kEnum, Enum: taskStatus, Required: true},
		{Name: "project", Kind: kLink}, // project or area: at least one (lint)
		{Name: "area", Kind: kLink},
		{Name: "due", Kind: kDate},
		{Name: "recur", Kind: kString},
		{Name: "blocked_by", Kind: kList},
		{Name: "depends_on", Kind: kList},
	},
	"rfx": {
		{Name: "direction", Kind: kEnum, Enum: rfxDirection, Required: true},
		{Name: "status", Kind: kEnum, Enum: rfxStatus, Required: true},
		{Name: "route", Kind: kString, Required: true},
		{Name: "self_serve_check", Kind: kString, Required: true},
		{Name: "project", Kind: kLink},
		{Name: "targets", Kind: kTargets},
	},
	"constraint": {
		{Name: "why", Kind: kString, Required: true},
		{Name: "by", Kind: kString, Required: true},
		{Name: "source", Kind: kSourceMap, Required: true},
		{Name: "applies_in", Kind: kList, Required: true},
		{Name: "not_in", Kind: kList, Required: true},
		{Name: "review_by", Kind: kDate, Required: true},
	},
	"policy": {
		{Name: "source", Kind: kSourceMap, Required: true},
		{Name: "verified", Kind: kBool, Required: true},
	},
	"transcript": {
		{Name: "of", Kind: kLink, Required: true},
		{Name: "source", Kind: kSourceMap, Required: true},
	},
	"policy_exception": {
		{Name: "policy", Kind: kLink, Required: true},
		{Name: "rule", Kind: kRuleLink},
		{Name: "kind", Kind: kEnum, Enum: exceptionKinds, Required: true},
		{Name: "status", Kind: kEnum, Enum: exceptionStatus, Required: true},
		{Name: "by", Kind: kString, Required: true},
		{Name: "with", Kind: kLink}, // required when kind is conflict_with
		{Name: "review_by", Kind: kDate},
	},
	"person": {
		{Name: "org", Kind: kString, Required: true},
		{Name: "role", Kind: kLink},
		{Name: "team", Kind: kLink},
		{Name: "aliases", Kind: kList},
		{Name: "contacts", Kind: kContacts},
		{Name: "groups", Kind: kLinkList},
	},
	"group": {
		{Name: "org", Kind: kString, Required: true},
		{Name: "kind", Kind: kEnum, Enum: groupKinds, Required: true},
	},
	"interaction": {
		{Name: "date", Kind: kDate, Required: true},
		{Name: "with", Kind: kLinkList, Required: true},
		{Name: "medium", Kind: kEnum, Enum: mediums, Required: true},
		{Name: "direction", Kind: kEnum, Enum: interDirections, Required: true},
		{Name: "visibility", Kind: kEnum, Enum: visibilities, Required: true},
		{Name: "url", Kind: kString},
	},
	"request": {
		{Name: "status", Kind: kEnum, Enum: requestStatus, Required: true},
		{Name: "runner", Kind: kString, Required: true},
		{Name: "queued_at", Kind: kTimestamp, Required: true},
		{Name: "claimed_by", Kind: kString},
		{Name: "lease_until", Kind: kTimestamp},
		{Name: "links", Kind: kLinkList},
		{Name: "result_links", Kind: kLinkList},
	},
	"team": {
		{Name: "org", Kind: kString, Required: true},
	},
	"role": {
		{Name: "org", Kind: kString, Required: true},
	},
	"domain": {
		{Name: "owner", Kind: kLink, Required: true},
	},
	"convention": {
		{Name: "status", Kind: kEnum, Enum: conventionState, Required: true},
		{Name: "applies_in", Kind: kList, Required: true},
	},
	"journal": {
		{Name: "date", Kind: kDate, Required: true},
		{Name: "scope", Kind: kEnum, Enum: journalScopes, Required: true},
	},
	"inbox": {},
}

// Item fields of a contact and of an rfx target.
var (
	contactFields = []field{
		{Name: "method", Kind: kEnum, Enum: mediums, Required: true},
		{Name: "value", Kind: kString, Required: true},
		{Name: "label", Kind: kString},
		{Name: "preferred", Kind: kBool},
	}
	pointerFields = []field{
		{Name: "label", Kind: kString, Required: true},
		{Name: "kind", Kind: kEnum, Enum: pointerKinds, Required: true},
		{Name: "target", Kind: kString, Required: true},
	}
	targetFields = []field{
		{Name: "to", Kind: kLink, Required: true},
		{Name: "medium", Kind: kEnum, Enum: mediums, Required: true},
		{Name: "value", Kind: kString},
		{Name: "state", Kind: kEnum, Enum: targetStates, Required: true},
		{Name: "ref", Kind: kString},
		{Name: "sent", Kind: kDate},
	}
)

// FieldEnum returns the allowed values of an enum field of a type, or nil.
// model's enums are tested against it.
func FieldEnum(typ, name string) []string {
	for _, fd := range typeFields[typ] {
		if fd.Name == name {
			return fd.Enum
		}
	}
	return nil
}

// ItemFieldEnum returns the allowed values of an enum field inside the items
// of a list field (contacts, targets), or nil.
func ItemFieldEnum(typ, list, name string) []string {
	var items []field
	for _, fd := range typeFields[typ] {
		if fd.Name == list {
			switch fd.Kind {
			case kPointers:
				items = pointerFields
			case kContacts:
				items = contactFields
			case kTargets:
				items = targetFields
			}
		}
	}
	for _, fd := range items {
		if fd.Name == name {
			return fd.Enum
		}
	}
	return nil
}

// Fidelities lists the allowed source.fidelity values.
func Fidelities() []string { return fidelities }
