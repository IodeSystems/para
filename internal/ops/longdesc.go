package ops

// shortDesc replaces the description of an op that an agent's core tool list
// shows: one short text for the tool schema, where each byte costs context on
// every session. define keeps the text the op was defined with as its long
// description, which ops_describe {op} returns in full (LongDescription). To
// shorten another op, add it here; its definition does not change.
var shortDesc = map[string]string{
	"io_add":     "Capture an incoming message (direction in) or draft one to send (direction out). In: expects response when the sender waits for the owner. Out: to and expects; fill why, impact, assessments. Voice, via.thread, variants: ops_describe {op: io_add}",
	"close":      "Close any item with an outcome; it runs the close op of the item's type, with that op's own gate, rules, and receipt. Outcomes per type, reason, fields, evidence: ops_describe {op: close}. Closing a goal, obligation, or question also outputs hanging: ask the owner what becomes of each item, never drop them yourself",
	"io_file":    "File an incoming io item into a new task, Ask, question, note, interaction, or project, or link an existing document with existing; the item leaves the inbox. Fields per kind: ops_describe {op: io_file}",
	"io_approve": "Approve an outgoing draft for sending, with expect_hash (the draft_hash the owner saw). From an agent only with owner_said: the owner's words that approve this draft, verbatim; never approve without them. Then io_send_check, then io_send. Variants, several at once: ops_describe {op: io_approve}",
	"later":      "Put items off until a date (until), an obligation's start or end, after an item is done, when a condition holds, or someday. Takes id, ids, a tag, or a scope. Runs defer with defer's rules. Every option: ops_describe {op: later}",
	"io_send":    "Record that you sent an outgoing draft, with its url or locator. From an agent it is refused unless the owner approved the draft (io_approve) and io_send_check passed within 30 minutes. Observed sends, actions, variants: ops_describe {op: io_send}",
}

// Longer is implemented by an Entry that keeps a longer description than
// Description.
type Longer interface {
	LongDescription() string
}

// LongDescription is the full description of e: the text it was defined with,
// or its Description when it has no shorter one.
func LongDescription(e Entry) string {
	if l, ok := e.(Longer); ok {
		if d := l.LongDescription(); d != "" {
			return d
		}
	}
	return e.Description()
}

// LongDescription implements Longer.
func (o *Op[I, O]) LongDescription() string { return o.long }
