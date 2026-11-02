package ops

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/iodesystems/parasolve/internal/model"
)

// A proposal item's summary is one plain line for a person: what approving it
// records. It is made from the item's own fields, never from the evidence. The
// full input stays on the item.

var mediumWords = map[model.Medium]string{
	model.MediumEmail: "Email", model.MediumSlack: "Slack message", model.MediumJira: "Jira ticket",
	model.MediumConfluence: "Confluence page", model.MediumGithub: "GitHub message", model.MediumTeams: "Teams message",
	model.MediumPhone: "Phone call", model.MediumMeeting: "Meeting", model.MediumOther: "Message",
}

// itemSummarizer builds summaries. name resolves an id to a title; who resolves
// an address or handle to a person's name ("" when unknown).
type itemSummarizer struct {
	name func(string) string
	who  func(string) string
	now  time.Time
}

func newItemSummarizer(ctx context.Context, d Deps) itemSummarizer {
	n := &namer{ctx: ctx, s: d.Store}
	return itemSummarizer{
		name: n.name,
		who: func(value string) string {
			out, err := who(ctx, d, WhoInput{Query: value})
			if err != nil || out.Person == nil {
				return ""
			}
			return out.Person.Name
		},
		now: d.Store.Now(),
	}
}

// opWords turns an op name into words: task_depends_add is "Task depends add".
func opWords(op string) string {
	s := strings.ReplaceAll(op, "_", " ")
	if s == "" {
		return "Change"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (z itemSummarizer) of(it model.ProposalItem) string {
	if it.Kind == model.ItemExternal {
		return z.external(it)
	}
	in := it.Input
	str := func(k string) string {
		v, _ := in[k].(string)
		return strings.TrimSpace(v)
	}
	// ref names the document an id field points at, else the id itself.
	ref := func(k string) string {
		id := str(k)
		if n := z.name(id); n != "" {
			return n
		}
		return id
	}
	first := func(s string) string { l, _, _ := strings.Cut(strings.TrimSpace(s), "\n"); return l }
	with := func(base string, parts ...string) string {
		for _, p := range parts {
			if p != "" {
				base += ", " + p
			}
		}
		return base
	}
	prefixed := func(p, v string) string {
		if v == "" {
			return ""
		}
		return p + v
	}
	person := func(k string) string {
		if str(k) == "" || str(k) == "self" {
			return ""
		}
		return prefixed("for ", ref(k))
	}
	switch it.Op {
	case "task_add":
		return with("Add task: "+str("title"), person("assignee"), prefixed("due ", z.date(str("due"))), prefixed("in ", firstNonEmpty(ref("project"), ref("area"))))
	case "task_done":
		return "Mark done: " + ref("id")
	case "task_due":
		if str("date") == "" {
			return "Remove the due date of: " + ref("id")
		}
		return "Set the due date of " + ref("id") + " to " + z.date(str("date"))
	case "task_assign":
		return "Give " + ref("id") + " to " + ref("assignee")
	case "task_block":
		return with("Mark blocked: "+ref("id"), prefixed("because ", first(str("reason"))))
	case "task_start":
		return "Start: " + ref("id")
	case "rfx_add":
		return with("Add request: "+first(str("ask")), prefixed("route ", str("route")))
	case "rfx_close":
		return "Close request: " + ref("id")
	case "rfx_status":
		return "Mark request " + ref("id") + " as " + str("status")
	case "inbox_add":
		return "Add to the inbox: " + first(str("text"))
	case "note_add":
		return "Add note: " + str("title")
	case "question_add":
		return "Ask a question: " + first(str("text"))
	case "project_add":
		return "Add project: " + str("title")
	case "area_add":
		return "Add area: " + str("title")
	case "incident_add":
		return with("Open "+str("severity")+" incident: "+str("title"), prefixed("in ", ref("area")))
	case "meeting_add":
		if str("start") != "" {
			return with("Book a meeting: "+str("title"), "with "+z.people(in["with"]))
		}
		return with("Ask for a meeting: "+str("title"), "with "+z.people(in["with"]))
	case "meeting_schedule":
		return "Set the time of " + ref("id") + " to " + str("start")
	case "meeting_cancel":
		return "Cancel meeting: " + ref("id")
	case "meeting_hold":
		return with("Record the meeting as held: "+ref("id"), prefixed("notes: ", first(str("notes"))))
	case "interaction_add":
		return "Record " + mediumWordsOr(model.Medium(str("medium"))) + " with " + z.people(in["with"]) + ": " + first(str("summary"))
	}
	return opWords(it.Op)
}

func mediumWordsOr(m model.Medium) string {
	if w, ok := mediumWords[m]; ok {
		return strings.ToLower(w)
	}
	return "contact"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// people names the persons in an input list of ids.
func (z itemSummarizer) people(v any) string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		id, _ := x.(string)
		if n := z.name(id); n != "" {
			id = n
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return "nobody"
	}
	return strings.Join(out, ", ")
}

func (z itemSummarizer) external(it model.ProposalItem) string {
	w := mediumWords[it.Medium]
	if w == "" {
		w = "Message"
	}
	to := z.who(it.Target)
	if to == "" {
		to = nameFromAddress(it.Target)
	}
	return w + " to " + to
}

// nameFromAddress reads a person's name from first.last@host, and gives the
// address back when the local part is not a name.
func nameFromAddress(addr string) string {
	local, _, ok := strings.Cut(addr, "@")
	if !ok {
		return addr
	}
	parts := strings.FieldsFunc(local, func(r rune) bool { return r == '.' || r == '_' || r == '-' })
	if len(parts) < 2 {
		return addr
	}
	for i, p := range parts {
		for _, r := range p {
			if !unicode.IsLetter(r) {
				return addr
			}
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, " ")
}

// date writes YYYY-MM-DD as "Nov 3", with the year when it is not this year.
func (z itemSummarizer) date(s string) string {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return s
	}
	if t.Year() != z.now.Year() {
		return t.Format("Jan 2, 2006")
	}
	return t.Format("Jan 2")
}
