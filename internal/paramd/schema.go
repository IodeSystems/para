package paramd

type kind int

const (
	kString kind = iota
	kDate
	kBool
	kEnum
	kList
	kLink         // "[[name]]"
	kRuleLink     // "[[name#^R4]]"
	kSourceMap    // source block
	kLinkList     // list of "[[name]]"
	kContacts     // list of contact maps
	kPointers     // list of {label, kind, target} maps
	kTargets      // list of rfx target maps
	kTimestamp    // RFC 3339 time; YAML may read it as a time or a string
	kCount        // integer, 1 or more
	kOwner        // "[[name]]" or the word self (the vault owner)
	kScope        // "[[name]]" or the word root
	kSources      // list of {ref, quote} maps
	kWhen         // a date YYYY-MM-DD, or an RFC 3339 time
	kMap          // a mapping; its parts are checked by the goal brief rules (model.CheckBrief)
	kMaps         // a list of mappings; its parts are checked by the goal brief rules
	kRisks        // list of risk maps (riskFields)
	kAssumptions  // list of assumption maps (assumptionFields)
	kImported     // {path, hash}: the file a document was imported from
	kCalendarUIDs // list of {uid, role} maps: calendar events of a meeting
)

type field struct {
	Name     string
	Kind     kind
	Enum     []string
	Required bool
}

var (
	projectStatus      = []string{"active", "paused", "done", "dropped"}
	taskStatus         = []string{"todo", "doing", "blocked", "done", "dropped"}
	taskOutcome        = []string{"done", "partial", "dropped", "duplicate", "superseded", "declined", "failed", "rerouted"}
	declinedBy         = []string{"owner", "other"}
	rfxDirection       = []string{"in", "out"}
	rfxStatus          = []string{"open", "answered", "closed", "declined"}
	fidelities         = []string{"exact", "ocr", "partial", "none"}
	exceptionKinds     = []string{"underspecified", "overly_broad", "bug", "unenforced", "conflict_with", "interpreted_as", "established_example", "wide_enough_for"}
	exceptionStatus    = []string{"open", "resolved", "accepted"}
	conventionState    = []string{"draft", "adopted", "retired"}
	mediums            = []string{"email", "slack", "jira", "confluence", "github", "teams", "phone", "meeting", "other"}
	groupKinds         = []string{"team", "department", "channel", "list", "other"}
	targetStates       = []string{"drafted", "sent", "replied", "no_reply", "requested", "scheduled"}
	interDirections    = []string{"in", "out", "observed"}
	visibilities       = []string{"public", "private"}
	journalScopes      = []string{"day", "week", "month", "project"}
	pointerKinds       = []string{"repo", "plan", "docs", "runbook", "tracker", "dashboard", "other"}
	areaStatus         = []string{"active", "paused"}
	areaKinds          = []string{"project", "concept"}
	severities         = []string{"sev1", "sev2", "sev3"}
	incidentStatus     = []string{"open", "mitigated", "resolved"}
	requestStatus      = []string{"proposed", "queued", "claimed", "done", "failed", "cancelled"}
	proposalStatus     = []string{"open", "sent_back", "closed", "superseded"}
	requestResolutions = []string{"done", "partial", "stuck", "blocked", "needs_clarification", "failed", "cancelled"}
	receiptChecks      = []string{"unchecked", "verified", "wrong"}
	questionStatus     = []string{"open", "answered", "dropped"}
	questionKinds      = []string{"question", "decision", "input"}
	taskKinds          = []string{"task", "meeting", "call", "work_with", "commitment"}
	meetingStatus      = []string{"requested", "scheduled", "held", "cancelled"}
	validationStates   = []string{"idea", "validated", "active", "pivoted", "stopped"}
	levels             = []string{"low", "medium", "high"}
	assumptionStatus   = []string{"holds", "changed", "unknown"}
	ioDirections       = []string{"in", "out"}
	ioStates           = []string{"unread", "read", "filed", "draft", "ready", "sent", "dropped"}
	ioExpects          = []string{"response", "none"}
	efforts            = []string{"xs", "s", "m", "l"}
	attends            = []string{"yes", "no", "maybe"}
	docSystems         = []string{"confluence", "jira", "github", "gdoc", "other"}
	docStatuses        = []string{"watching", "paused", "archived"}
	sourceSystems      = []string{"mail", "chat", "tracker", "wiki", "calendar", "other", "gmail", "gdocs", "gcal", "slack", "github", "jira", "confluence", "folder"}
	priorities         = []string{"high", "normal", "low"}
	ioTriages          = []string{"needs_you", "digest"}
	sourceVias         = []string{"mcp", "cli", "none"}
	sourceStatuses     = []string{"active", "paused"}
	frictionReasons    = []string{"wrong_owner", "no_owner", "policy_conflict", "policy_unclear", "no_response", "capacity", "needs_approval", "tool_gap", "other"}
)

// Types lists every valid value of the frontmatter "type" field.
var Types = []string{
	"project", "task", "rfx", "constraint", "policy", "transcript",
	"policy_exception", "person", "team", "role", "domain", "convention", "journal", "inbox", "io", "tracked_doc", "source",
	"group", "interaction", "request", "area", "note", "incident", "proposal", "question", "meeting", "receipt",
}

// commonFields apply to every type.
var commonFields = []field{
	{Name: "type", Kind: kEnum, Enum: Types, Required: true},
	{Name: "created", Kind: kDate, Required: true},
	{Name: "updated", Kind: kDate},
	{Name: "tags", Kind: kList},
	{Name: "imported_from", Kind: kImported},
	{Name: "duplicate_of", Kind: kLink}, // doc_merge: the primary this document was merged into
}

// typeFields are the per-type fields, on top of commonFields.
var typeFields = map[string][]field{
	"project": {
		{Name: "status", Kind: kEnum, Enum: projectStatus, Required: true},
		{Name: "owner", Kind: kString, Required: true},
		{Name: "area", Kind: kLink},         // an area of kind project (vault check)
		{Name: "concepts", Kind: kLinkList}, // areas of kind concept (vault check)
		{Name: "done_when", Kind: kString},
		{Name: "links", Kind: kPointers},
		// The goal brief; all optional.
		{Name: "why", Kind: kMap},
		{Name: "confidence", Kind: kMap},
		{Name: "alternatives", Kind: kMaps},
		{Name: "unknowns", Kind: kMaps},
		{Name: "signals", Kind: kMaps},
		{Name: "risks", Kind: kRisks},
		{Name: "assumptions", Kind: kAssumptions},
		{Name: "pivot", Kind: kMap},
		{Name: "extensions", Kind: kList},
		{Name: "ecosystem", Kind: kMap},
		{Name: "rank", Kind: kCount},
		{Name: "validation", Kind: kEnum, Enum: validationStates},
	},
	"area": {
		{Name: "status", Kind: kEnum, Enum: areaStatus, Required: true},
		{Name: "kind", Kind: kEnum, Enum: areaKinds}, // missing reads as project
		{Name: "cadence", Kind: kString},             // recur grammar; another value is a warning
		{Name: "last_reviewed", Kind: kDate},
		{Name: "concepts", Kind: kLinkList}, // kind project only; areas of kind concept (vault check)
		{Name: "links", Kind: kPointers},
	},
	"incident": {
		{Name: "severity", Kind: kEnum, Enum: severities, Required: true},
		{Name: "status", Kind: kEnum, Enum: incidentStatus, Required: true},
		{Name: "started", Kind: kTimestamp, Required: true},
		{Name: "ended", Kind: kTimestamp},
		{Name: "area", Kind: kLink},
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
		{Name: "recur", Kind: kString}, // daily, weekly, monthly, quarterly, yearly, every <n> days; another value is a warning
		{Name: "previous", Kind: kLink},
		{Name: "incident", Kind: kLink},
		{Name: "parent", Kind: kLink}, // the task this is an action item of; not a dependency
		{Name: "assignee", Kind: kString},
		{Name: "done_when", Kind: kString},
		{Name: "do_url", Kind: kString},     // where the owner does it
		{Name: "do_command", Kind: kString}, // the exact command to run; no secrets
		{Name: "steps", Kind: kString},      // short markdown list
		{Name: "blocked_by", Kind: kList},
		{Name: "depends_on", Kind: kList},
		{Name: "outcome", Kind: kEnum, Enum: taskOutcome},
		{Name: "declined_by", Kind: kEnum, Enum: declinedBy},
		{Name: "declined_person", Kind: kLink},
		{Name: "retry_after", Kind: kDate},
		{Name: "rerouted_to", Kind: kString}, // a link to a vault item, or a URL
		{Name: "kind", Kind: kEnum, Enum: taskKinds},
		{Name: "scheduled", Kind: kWhen}, // a date, or an RFC 3339 start with a zone
		{Name: "end", Kind: kTimestamp},
		{Name: "with", Kind: kLinkList},
		{Name: "attendees", Kind: kList},             // kind meeting: invitees not in the directory, as text
		{Name: "rfx", Kind: kLinkList},               // kind meeting: the requests it settles
		{Name: "calendar_uid", Kind: kString},        // older single form of calendar_uids; read as one entry
		{Name: "calendar_uids", Kind: kCalendarUIDs}, // kind meeting or call: [{uid, role}] of its calendar events
		{Name: "optional", Kind: kBool},              // kind meeting: the owner may skip it
		{Name: "attend", Kind: kEnum, Enum: attends}, // kind meeting: yes, no, maybe; empty is undecided
		{Name: "attend_default", Kind: kEnum, Enum: attends},
		{Name: "assumptions", Kind: kAssumptions},
		{Name: "effort", Kind: kEnum, Enum: efforts},
	},
	"rfx": {
		{Name: "direction", Kind: kEnum, Enum: rfxDirection, Required: true},
		{Name: "status", Kind: kEnum, Enum: rfxStatus, Required: true},
		{Name: "route", Kind: kString, Required: true},
		{Name: "self_serve_check", Kind: kString, Required: true},
		{Name: "project", Kind: kLink},
		{Name: "targets", Kind: kTargets},
		{Name: "question", Kind: kLink},
		{Name: "effort", Kind: kEnum, Enum: efforts},
	},
	"question": {
		{Name: "status", Kind: kEnum, Enum: questionStatus, Required: true},
		{Name: "review_state", Kind: kEnum, Enum: []string{"skipped", "sent_back"}},
		{Name: "review_note", Kind: kString},
		{Name: "review_refs", Kind: kLinkList},
		{Name: "review_request", Kind: kLink},
		{Name: "review_at", Kind: kString},
		{Name: "review_batch", Kind: kString},
		{Name: "kind", Kind: kEnum, Enum: questionKinds},
		{Name: "scope", Kind: kScope, Required: true}, // a project, task, or area link, or root
		{Name: "options", Kind: kList},
		{Name: "recommended", Kind: kString},
		{Name: "rfx", Kind: kLink},
		{Name: "choice", Kind: kString},
		{Name: "answered_by", Kind: kString},
		{Name: "answered", Kind: kDate},
		{Name: "depends_on", Kind: kList},
		{Name: "proposed_answer", Kind: kString},
		{Name: "proposed_choice", Kind: kString},
		{Name: "proposed_by", Kind: kString},
		{Name: "proposed_sources", Kind: kSources},
		{Name: "proposed_why", Kind: kString},
		{Name: "proposed", Kind: kDate},
		{Name: "rejected_reason", Kind: kString},
		{Name: "rejected_by", Kind: kString},
		{Name: "effort", Kind: kEnum, Enum: efforts},
	},
	"meeting": {
		{Name: "status", Kind: kEnum, Enum: meetingStatus, Required: true},
		{Name: "start", Kind: kTimestamp}, // required once scheduled (lint)
		{Name: "end", Kind: kTimestamp},
		{Name: "with", Kind: kLinkList, Required: true},
		{Name: "rfx", Kind: kLinkList},
	},
	"constraint": {
		{Name: "why", Kind: kString, Required: true},
		{Name: "by", Kind: kString, Required: true},
		{Name: "source", Kind: kSourceMap, Required: true},
		{Name: "applies_in", Kind: kList, Required: true},
		{Name: "not_in", Kind: kList, Required: true},
		{Name: "review_by", Kind: kDate, Required: true},
		{Name: "refines", Kind: kLink}, // a constraint (vault check)
	},
	"policy": {
		{Name: "source", Kind: kSourceMap, Required: true},
		{Name: "verified", Kind: kBool, Required: true},
	},
	"transcript": {
		{Name: "of", Kind: kLink, Required: true},
		{Name: "source", Kind: kSourceMap, Required: true},
		{Name: "version", Kind: kCount}, // a tracked_doc version: Lint transcript
		{Name: "fetched_by", Kind: kString},
		{Name: "proposal", Kind: kLink},
		{Name: "item", Kind: kString},
	},
	"policy_exception": {
		{Name: "policy", Kind: kLink}, // required unless policy_text is set (lint)
		{Name: "policy_text", Kind: kString},
		{Name: "rule", Kind: kRuleLink},
		{Name: "kind", Kind: kEnum, Enum: exceptionKinds, Required: true},
		{Name: "status", Kind: kEnum, Enum: exceptionStatus, Required: true},
		{Name: "by", Kind: kString, Required: true},
		{Name: "with", Kind: kLink}, // required when kind is conflict_with
		{Name: "review_by", Kind: kDate},
	},
	"person": {
		{Name: "org", Kind: kString},
		{Name: "role", Kind: kLink},
		{Name: "role_text", Kind: kString},
		{Name: "team", Kind: kLink},
		{Name: "aliases", Kind: kList},
		{Name: "contacts", Kind: kContacts},
		{Name: "groups", Kind: kLinkList},
		{Name: "away_until", Kind: kDate},
		{Name: "quiet_days", Kind: kCount},
		{Name: "priority", Kind: kEnum, Enum: priorities},
	},
	"group": {
		{Name: "org", Kind: kString, Required: true},
		{Name: "kind", Kind: kEnum, Enum: groupKinds, Required: true},
		{Name: "priority", Kind: kEnum, Enum: priorities},
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
		{Name: "claimed_at", Kind: kTimestamp},
		{Name: "active_at", Kind: kTimestamp},
		{Name: "links", Kind: kLinkList},
		{Name: "result_links", Kind: kLinkList},
		{Name: "context", Kind: kLinkList},
		{Name: "required", Kind: kLinkList},
		{Name: "followed", Kind: kLinkList},
		{Name: "resolution", Kind: kEnum, Enum: requestResolutions},
		{Name: "requires_source", Kind: kLink},
		{Name: "seen", Kind: kTimestamp},
	},
	"proposal": {
		{Name: "source", Kind: kLink},
		{Name: "status", Kind: kEnum, Enum: proposalStatus, Required: true},
		{Name: "revision", Kind: kCount, Required: true},
		{Name: "supersedes", Kind: kLink},
		{Name: "source_path", Kind: kString},
		{Name: "proposed_by", Kind: kString, Required: true},
	},
	"receipt": {
		{Name: "actor", Kind: kString, Required: true},
		{Name: "op", Kind: kString, Required: true},
		{Name: "doc", Kind: kString, Required: true},
		{Name: "check", Kind: kEnum, Enum: receiptChecks, Required: true},
	},
	"team": {
		{Name: "org", Kind: kString, Required: true},
	},
	"role": {
		{Name: "org", Kind: kString, Required: true},
	},
	"domain": {
		{Name: "owner", Kind: kOwner, Required: true},
	},
	"convention": {
		{Name: "status", Kind: kEnum, Enum: conventionState, Required: true},
		{Name: "applies_in", Kind: kList, Required: true},
		{Name: "refines", Kind: kLink}, // a convention (vault check)
	},
	"journal": {
		{Name: "date", Kind: kDate, Required: true},
		{Name: "scope", Kind: kEnum, Enum: journalScopes, Required: true},
	},
	"inbox": {},
	"io": {
		{Name: "direction", Kind: kEnum, Enum: ioDirections, Required: true},
		{Name: "medium", Kind: kEnum, Enum: mediums, Required: true},
		{Name: "state", Kind: kEnum, Enum: ioStates, Required: true}, // per direction: Lint io
		{Name: "from", Kind: kString},
		{Name: "to", Kind: kList},
		{Name: "at", Kind: kTimestamp},
		{Name: "subject", Kind: kString},
		{Name: "ref", Kind: kString},
		{Name: "via", Kind: kMap}, // {source, ref}: Lint io
		{Name: "verify", Kind: kString},
		{Name: "expects", Kind: kEnum, Enum: ioExpects},
		{Name: "route", Kind: kString},
		{Name: "became", Kind: kLink},
		{Name: "thread", Kind: kLink},
		{Name: "reason", Kind: kString},
		{Name: "effort", Kind: kEnum, Enum: efforts},
		{Name: "approved_hash", Kind: kString},
		{Name: "triage", Kind: kEnum, Enum: ioTriages}, // set by a triage rule
		{Name: "triage_by", Kind: kString},
		{Name: "links", Kind: kLinkList},
	},
	"tracked_doc": {
		{Name: "url", Kind: kString, Required: true},
		{Name: "via", Kind: kMap}, // {source, ref}: Lint trackedDoc
		{Name: "system", Kind: kEnum, Enum: docSystems, Required: true},
		{Name: "status", Kind: kEnum, Enum: docStatuses, Required: true},
		{Name: "owner", Kind: kLink},
		{Name: "watchers", Kind: kLinkList},
		{Name: "area", Kind: kLink},
		{Name: "project", Kind: kLink},
		{Name: "route", Kind: kString},
		{Name: "latest", Kind: kCount, Required: true},
		{Name: "last_seen", Kind: kCount},
		{Name: "seen_at", Kind: kTimestamp},
		{Name: "checked", Kind: kTimestamp},
		{Name: "comments", Kind: kMaps}, // Lint trackedDoc
	},
	"source": {
		{Name: "system", Kind: kEnum, Enum: sourceSystems, Required: true}, // a class, or an older alias
		{Name: "vendor", Kind: kString},
		{Name: "status", Kind: kEnum, Enum: sourceStatuses, Required: true},
		{Name: "priority", Kind: kEnum, Enum: priorities},
		{Name: "via", Kind: kEnum, Enum: sourceVias, Required: true},
		{Name: "server", Kind: kString},
		{Name: "ref_url", Kind: kString}, // link template with {ref}
		{Name: "tools", Kind: kList},
		{Name: "fetch", Kind: kMap}, // steps: Lint source
		{Name: "scan_in", Kind: kMap},
		{Name: "scan_out", Kind: kMap},
		{Name: "state", Kind: kMap},
		{Name: "reached_by", Kind: kList},
		{Name: "cadence", Kind: kString}, // recur grammar: Lint source
		{Name: "last_scan", Kind: kTimestamp},
		{Name: "cursor", Kind: kString},
	},
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
	riskFields = []field{
		{Name: "text", Kind: kString, Required: true},
		{Name: "likelihood", Kind: kEnum, Enum: levels},
		{Name: "impact", Kind: kEnum, Enum: levels},
		{Name: "owner", Kind: kString},
		{Name: "mitigation", Kind: kString},
		{Name: "source", Kind: kString},
	}
	assumptionFields = []field{
		{Name: "text", Kind: kString, Required: true},
		{Name: "source", Kind: kString},
		{Name: "checked", Kind: kDate},
		{Name: "by", Kind: kString},
		{Name: "status", Kind: kEnum, Enum: assumptionStatus},
		{Name: "fact", Kind: kString},
	}
	sourceFields = []field{
		{Name: "ref", Kind: kString, Required: true},
		{Name: "quote", Kind: kString},
	}
	targetFields = []field{
		{Name: "to", Kind: kLink, Required: true},
		{Name: "medium", Kind: kEnum, Enum: mediums, Required: true},
		{Name: "value", Kind: kString},
		{Name: "state", Kind: kEnum, Enum: targetStates, Required: true},
		{Name: "ref", Kind: kString},
		{Name: "sent", Kind: kDate},
		{Name: "added", Kind: kDate},
		{Name: "reason", Kind: kEnum, Enum: frictionReasons},
		{Name: "why", Kind: kString},
		{Name: "about", Kind: kLink},
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
			case kRisks:
				items = riskFields
			case kAssumptions:
				items = assumptionFields
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

// FieldInfo describes one frontmatter field a person may edit.
type FieldInfo struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"` // string, date, bool, enum, list, link, links, ...
	Enum     []string `json:"enum,omitempty"`
	Required bool     `json:"required,omitempty"`
}

var kindNames = map[kind]string{
	kString: "string", kDate: "date", kBool: "bool", kEnum: "enum", kList: "list", kLink: "link",
	kRuleLink: "rule_link", kSourceMap: "source", kLinkList: "links", kContacts: "contacts",
	kPointers: "pointers", kTargets: "targets", kTimestamp: "timestamp", kCount: "count",
	kScope: "scope", kSources: "sources", kWhen: "date_or_time", kMap: "map", kMaps: "maps",
	kRisks: "risks", kAssumptions: "assumptions", kImported: "imported", kCalendarUIDs: "calendar_uids",
}

// editableTypes are the types doc_edit changes. A proposal, receipt, question,
// and note keep their own ops; a note goes through note_edit.
var editableTypes = []string{
	"task", "project", "area", "rfx", "meeting", "journal", "incident", "request", "person", "group", "team",
	"role", "domain", "convention", "constraint", "policy", "policy_exception", "interaction", "inbox", "transcript", "tracked_doc", "source",
}

// editLocked are fields a lifecycle op, a recurrence chain, or a fetched source
// owns, per type. status is locked for every type.
var editLocked = map[string][]string{
	"request":     {"queued_at", "claimed_by", "lease_until", "claimed_at", "active_at", "resolution", "result_links", "followed"},
	"task":        {"previous", "rfx"},
	"rfx":         {"targets", "question"},
	"policy":      {"source", "verified"},
	"transcript":  {"source", "of", "version", "fetched_by", "proposal", "item"},
	"tracked_doc": {"latest", "last_seen", "seen_at", "checked", "comments"},
	"source":      {"last_scan", "cursor"},
	"constraint":  {"source"},
}

// EditableTypes lists the types doc_edit accepts.
func EditableTypes() []string { return append([]string(nil), editableTypes...) }

// EditableFields returns the declared fields of a type that doc_edit may set:
// not type, created, updated, or status, and not a locked field. It returns nil
// for a type doc_edit does not edit.
func EditableFields(typ string) []FieldInfo {
	ok := false
	for _, t := range editableTypes {
		ok = ok || t == typ
	}
	if !ok {
		return nil
	}
	var out []FieldInfo
next:
	for _, fd := range typeFields[typ] {
		if fd.Name == "status" {
			continue
		}
		for _, l := range editLocked[typ] {
			if l == fd.Name {
				continue next
			}
		}
		out = append(out, FieldInfo{Name: fd.Name, Kind: kindNames[fd.Kind], Enum: fd.Enum, Required: fd.Required})
	}
	return out
}

// TypeFields returns every declared field of a type, in schema order.
func TypeFields(typ string) []FieldInfo {
	var out []FieldInfo
	for _, fd := range typeFields[typ] {
		out = append(out, FieldInfo{Name: fd.Name, Kind: kindNames[fd.Kind], Enum: fd.Enum, Required: fd.Required})
	}
	return out
}
