package ops

import (
	"sort"
)

// Op names an agent can no longer call (0.5.0). An internal op is still
// defined and keeps its name: the generic verbs route to it (verbs.go,
// verbwrite.go), the CLI and the web app call it by reference, receipts and
// staged items name it, and it runs with its own gate, risk class, and
// receipts. No agent surface lists it or runs it by name: All, op_run,
// ops_describe, all-mode MCP, ops stats, and the API leave it out, except
// the web routes the app reads (server/routes_*.go). A removed name has no op
// at all. Both say what to use instead.

const removedVersion = "0.5.0"

// internalTable maps each internal op to what an agent uses instead.
var internalTable = map[string]string{
	// show routes to the show op of the item's type.
	"task_show": "show", "question_show": "show", "project_show": "show", "rfx_show": "show", "io_show": "show",
	"request_show": "show", "area_show": "show", "incident_show": "show", "receipt_show": "show", "source_show": "show",
	"exception_show": "show", "convention_show": "show", "constraint_show": "show", "interaction_show": "show",
	"note_show": "show", "policy_show": "show", "org_person_show": "show", "org_team_show": "show",
	"org_role_show": "show", "org_domain_show": "show", "meeting_show": "show",
	// edit routes to the edit op of the item's type. io_edit stays a web
	// route: the app's Edit of an io item.
	"task_edit": "edit", "project_edit": "edit", "question_edit": "edit", "note_edit": "edit",
	"org_person_edit": "edit", "io_edit": "edit",
	"area_add": "add {type: area}",
	// close routes to the op of the item's type and outcome.
	"task_done": "close", "task_resolve": "close {id, outcome}", "meeting_hold": "close", "meeting_cancel": "close {id, outcome: dropped}",
	"question_drop": "close {id, outcome: dropped}", "project_close": "close {id, outcome}", "rfx_close": "close",
	"rfx_status": "close {id, outcome: declined|answered|closed}, or reopen", "io_drop": "close {id, outcome: dropped}",
	"io_done": "close {id, outcome: sent}", "request_cancel": "close", "meeting_attend": "obligation_attend",
	"defer": "later", "undefer": "reopen", "agent_send": "send_to_agent", "request_respond": "thread_post",
	// Ops no verb routes to, kept for their CLI command (and meeting_rsvp
	// for the app's Attend and Skip).
	"item_close": "close", "stack_later": "later", "stack_return": "reopen",
	"meeting_add": "add {type: obligation}", "meeting_schedule": "obligation_schedule", "meeting_rsvp": "obligation_attend",
	"exception_add": "annotate", "convention_add": "annotate", "constraint_add": "annotate",
	"org_team_add": "add {type: group}", "org_role_add": "add {type: group}", "org_domain_add": "add {type: group}",
	"interaction_add": "add {type: io}", "incident_add": "add {type: task, fields: {kind: incident}}",
}

// removedTable maps an op name removed with no op left to what to use
// instead (free text).
var removedTable = map[string]string{}

func init() {
	for name := range internalTable {
		if _, ok := registry[name]; !ok {
			panic("ops: internal op " + name + ": no such op")
		}
	}
	for name := range removedTable {
		if _, ok := registry[name]; ok {
			panic("ops: removed op " + name + " is still defined")
		}
	}
}

// RemovedIn reports that an op name was removed from the agent surfaces, in
// which version, and what replaces it: a removed name or an internal op.
func RemovedIn(name string) (version, replacement string, ok bool) {
	if replacement, ok = internalTable[name]; ok {
		return removedVersion, replacement, true
	}
	if replacement, ok = removedTable[name]; ok {
		return removedVersion, replacement, true
	}
	return "", "", false
}

// IsInternal reports whether name is an internal op.
func IsInternal(name string) bool {
	_, ok := internalTable[name]
	return ok
}

// Internal returns the internal ops, sorted by name.
func Internal() []Entry {
	out := make([]Entry, 0, len(internalTable))
	for name := range internalTable {
		out = append(out, registry[name])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Lookup finds a defined op by name, listed or internal. It is for records
// that name an op (receipts, staged items, retries) and for tests; an agent
// surface resolves names from ForAgents.
func Lookup(name string) (Entry, bool) {
	e, ok := registry[name]
	return e, ok
}

// SetRemovedForTest adds a removed-op entry and returns the func that
// removes it. Tests only.
func SetRemovedForTest(name, replacement string) (restore func()) {
	removedTable[name] = replacement
	return func() { delete(removedTable, name) }
}
