package ops

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// The generic verbs: close, later, reopen, show, list, and send_to_agent take
// any id. Each finds the op of the item's type that does the work (a route)
// and runs it as the outermost call, so the gate, the person check, and the
// receipts are that op's own: a verb changes no gate class. On a person's
// surface a write of close, later, or reopen also files one receipt, so the
// web app can undo it (receipt_revert). The vault format does not change:
// each type keeps its own status values (outcomes map 1:1 to them).

// route is the op a verb runs for one input, and that op's input.
type verbRoute struct {
	op    Entry
	input json.RawMessage
	// receipt: on a person's surface, file one receipt for the write.
	receipt bool
	// evidence is close's evidence: the receipt keeps it as a source.
	evidence *model.Evidence
}

// verbRoutes holds each verb's router, by verb name. An alias checks the
// route before it runs it (opalias.go).
var verbRoutes = map[string]func(ctx context.Context, d Deps, raw json.RawMessage) (verbRoute, error){}

// defineVerb registers a generic verb of risk risk. It files no receipt and
// runs no gate itself: the routed op does.
func defineVerb[I any](name, desc string, risk Risk, resolve func(ctx context.Context, d Deps, in I) (verbRoute, error)) *Op[I, VerbOutput] {
	op := define(name, desc, func(ctx context.Context, d Deps, in I) (VerbOutput, error) {
		rt, err := resolve(ctx, d, in)
		if err != nil {
			return VerbOutput{}, err
		}
		return runRoute(ctx, d, rt)
	})
	op.risk, op.verb = risk, true
	verbRoutes[name] = func(ctx context.Context, d Deps, raw json.RawMessage) (verbRoute, error) {
		var in I
		if err := decodeStrict(raw, &in); err != nil {
			return verbRoute{}, err
		}
		return resolve(ctx, d, in)
	}
	return op
}

func decodeStrict(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// VerbOutput is what a write verb did: the op of the item's type that ran,
// that op's output, and on a person's surface the receipt that undoes it.
type VerbOutput struct {
	Op      string         `json:"op"`
	Receipt string         `json:"receipt,omitempty"`
	Result  map[string]any `json:"result"`
	raw     any
}

// newRoute builds a route to op with input as JSON.
func newRoute(op Entry, input any, receipt bool) (verbRoute, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return verbRoute{}, err
	}
	return verbRoute{op: op, input: raw, receipt: receipt}, nil
}

// recordsProposals reports whether an op files receipts for dossiers.
type proposalRecorder interface{ recordsProposals() bool }

func (o *Op[I, O]) recordsProposals() bool { return o.proposalReceipts }

// runRoute runs the routed op. A caller bound to scopes (the server) must be
// allowed the op itself. On an agent surface, or inside another write, the op
// runs as it would alone. On a person's surface a write files one receipt.
func runRoute(ctx context.Context, d Deps, rt verbRoute) (VerbOutput, error) {
	if d.Authorize != nil {
		if err := d.Authorize(rt.op.Name()); err != nil {
			return VerbOutput{}, err
		}
	}
	ctx = withEvidence(ctx, rt.evidence)
	out := VerbOutput{Op: rt.op.Name()}
	if !rt.receipt || rt.op.Risk() == RiskRead || agentSurface(d) || ctx.Value(recordingKey{}) != nil {
		res, err := rt.op.Invoke(ctx, d, rt.input)
		out.raw, out.Result = res, asMap(res)
		return out, err
	}
	rec := newRecorder(d.Store)
	if p, ok := rt.op.(proposalRecorder); ok {
		rec.proposals = p.recordsProposals()
	}
	inner := d
	inner.Store = rec
	res, err := rt.op.Invoke(context.WithValue(ctx, recordingKey{}, rt.op.Name()), inner, rt.input)
	out.raw, out.Result = res, asMap(res)
	id, ferr := rec.fileOneID(ctx, d, rt.op.Name(), writeMeta(ctx))
	if ferr != nil {
		return out, errors.Join(err, fmt.Errorf("%s: the write happened, but its receipt failed: %w", rt.op.Name(), ferr))
	}
	out.Receipt = id
	return out, err
}

// asMap is v as a JSON object; nil when v is not one.
func asMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// Lifecycles lists every Lifecycle; Turns every Turn.
var (
	Lifecycles = []Lifecycle{LifeOpen, LifeLater, LifeClosed}
	Turns      = []Turn{TurnOwner, TurnAgent, TurnWaiting, TurnNone}
)

// --- close ---

// CloseInput is the input of close. Outcome is one of the type's own close
// values; empty is the type's plain close (a task done, a meeting held, an
// ask closed). Fields are the extra inputs of the op that does it, such as
// link for a duplicate task or playbook for an inbound ask.
type CloseInput struct {
	ID       string          `json:"id"`
	Outcome  string          `json:"outcome,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Evidence *model.Evidence `json:"evidence,omitempty"`
	Fields   map[string]any  `json:"fields,omitempty"`
}

// CloseDescription is close's description.
const CloseDescription = "Close any item with an outcome. It runs the close op of the item's type, with that op's own gate, rules, and receipt: " +
	"task: (none) done = task_done, skipped = task_done skip (a recurring task), any task_resolve outcome (partial, dropped, duplicate, superseded, declined, failed, rerouted) = task_resolve; " +
	"meeting: (none) or done = meeting_hold (reason = notes), dropped = meeting_cancel, skipped = meeting_attend no, duplicate = task_resolve; " +
	"question: dropped = question_drop (answer with question_answer); goal: (none) or done, dropped = project_close; " +
	"ask: (none) or done = rfx_close, declined or answered = rfx_status; io: (none) or dropped = io_drop, sent = io_done (people only); " +
	"agent work: (none) or cancelled = request_cancel; incident: (none) or resolved = incident_status resolved. " +
	"reason goes where the op keeps one (note, notes, reason, or text); an op with none refuses it. " +
	"fields are the op's other inputs (task_resolve link, follow_ups, declined_by, source, tried, to; meeting_hold replies, answers, actions; project_close force; rfx_close playbook; io_done url, locator, text, at). " +
	"evidence, when given, must be one the owner can open: an http(s) URL, a vault id or path, or via {source, ref} with verify; free text alone is refused. " +
	"Outputs op (the op that ran) and result (its output); on a person's surface also receipt, which receipt_revert undoes"

// Close closes any item by its type's close op.
var Close = defineVerb("close", CloseDescription, RiskReview, closeRoute)

// put sets m[k] to v when v is not empty.
func put(m map[string]any, k string, v any) {
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return
		}
	case bool:
		if !x {
			return
		}
	}
	m[k] = v
}

func closeRoute(ctx context.Context, d Deps, in CloseInput) (verbRoute, error) {
	id := normID(in.ID)
	if id == "" {
		return verbRoute{}, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return verbRoute{}, linkErr("id", id, err)
	}
	outcome := strings.TrimSpace(in.Outcome)
	base := map[string]any{"id": doc.ID}
	var op Entry
	// reasonAs is the input field that keeps reason; "" refuses it.
	reasonAs := ""
	bad := func(want string) error {
		return fmt.Errorf("%s is a %s: outcome %q does not close it; want %s", doc.ID, typeWord(doc), outcome, want)
	}
	doc = incidentShape(doc) // a task of kind incident closes as an incident (incident_status resolved)
	switch m := doc.Meta.(type) {
	case *model.Task:
		if m.EffectiveKind() == model.KindMeeting {
			switch outcome {
			case "", "done":
				op, reasonAs = MeetingHold, "notes"
			case string(model.OutcomeDropped):
				op, reasonAs = MeetingCancel, "reason"
			case "skipped":
				op = MeetingAttend
				base["attend"] = string(model.AttendNo)
			case string(model.OutcomeDuplicate):
				op, reasonAs = TaskResolve, "note"
				base["outcome"] = outcome
			default:
				return verbRoute{}, bad("none or done (held), dropped (cancelled), skipped, or duplicate")
			}
			break
		}
		switch outcome {
		case "":
			op, reasonAs = TaskDone, "note"
		case "skipped":
			op, reasonAs = TaskDone, "note"
			base["skip"] = true
		default:
			if !slices.Contains(model.TaskOutcomes, model.TaskOutcome(outcome)) {
				return verbRoute{}, bad(fmt.Sprintf("none, skipped, or one of %v", model.TaskOutcomes))
			}
			op, reasonAs = TaskResolve, "note"
			if outcome == string(model.OutcomeDeclined) || outcome == string(model.OutcomeRerouted) {
				reasonAs = "reason"
			}
			base["outcome"] = outcome
		}
	case *model.Meeting:
		switch outcome {
		case "", "done":
			op, reasonAs = MeetingHold, "notes"
		case string(model.OutcomeDropped):
			op, reasonAs = MeetingCancel, "reason"
		default:
			return verbRoute{}, bad("none or done (held), or dropped (cancelled)")
		}
	case *model.Obligation:
		// A meeting is held (notes, replies, answers, actions) or cancelled
		// by its own ops; any other close sets the outcome.
		switch {
		case m.Kind == model.KindMeeting && (outcome == "" || outcome == "done"):
			op, reasonAs = MeetingHold, "notes"
		case m.Kind == model.KindMeeting && outcome == string(model.OutcomeDropped):
			op, reasonAs = MeetingCancel, "reason"
		case outcome == "" || slices.Contains(model.ObligationOutcomes, model.TaskOutcome(outcome)):
			op, reasonAs = obligationClose, "reason"
			put(base, "outcome", outcome)
		default:
			return verbRoute{}, bad(fmt.Sprintf("none or one of %v", model.ObligationOutcomes))
		}
	case *model.Question:
		switch outcome {
		case string(model.QuestionDropped):
			op, reasonAs = QuestionDrop, "reason"
		case string(model.QuestionAnswered):
			return verbRoute{}, fmt.Errorf("%s: answer it with question_answer (an answer needs its text and sources), not close", doc.ID)
		default:
			return verbRoute{}, bad("dropped (answer with question_answer)")
		}
	case *model.Project:
		switch outcome {
		case "", "done":
			op = ProjectClose
		case string(model.ProjectDropped):
			op = ProjectClose
			base["dropped"] = true
		default:
			return verbRoute{}, bad("none or done, or dropped")
		}
	case *model.RFX:
		switch outcome {
		case "", "done", string(model.RFXClosed):
			op = RFXClose
		case string(model.RFXDeclined), string(model.RFXAnswered):
			op = RFXStatus
			base["status"] = outcome
		default:
			return verbRoute{}, bad("none or done (closed), declined, or answered")
		}
	case *model.IO:
		switch outcome {
		case "", string(model.IODropped):
			op, reasonAs = IODrop, "reason"
		case string(model.IOSent):
			op = IODone
			if in.Evidence != nil {
				if model.IsWebURL(in.Evidence.Locator) {
					base["url"] = in.Evidence.Locator
				} else {
					put(base, "locator", in.Evidence.Locator)
				}
			}
		default:
			return verbRoute{}, bad("none or dropped, or sent")
		}
		_ = m
	case *model.Request:
		switch {
		case outcome == "" && (m.Status == model.RequestDone || m.Status == model.RequestFailed):
			// Finished work closes when the owner has seen its result.
			op = RequestSeen
		case outcome == "", outcome == string(model.RequestCancelled):
			op = RequestCancel
		default:
			return verbRoute{}, bad("none or cancelled (end agent work with request_done or request_fail)")
		}
	case *model.Incident:
		switch outcome {
		case "", "done", string(model.IncidentResolved):
			op, reasonAs = IncidentStatus, "text"
			base["status"] = string(model.IncidentResolved)
		default:
			return verbRoute{}, bad("none or resolved")
		}
	default:
		return verbRoute{}, fmt.Errorf("%s is a %s: it has no close (archive it with doc_archive)", doc.ID, typeWord(doc))
	}
	if r := strings.TrimSpace(in.Reason); r != "" {
		if reasonAs == "" {
			return verbRoute{}, fmt.Errorf("%s keeps no reason for a %s: leave reason out, or say it in why", op.Name(), typeWord(doc))
		}
		base[reasonAs] = in.Reason
	}
	for k, v := range in.Fields {
		if _, taken := base[k]; taken {
			return verbRoute{}, fmt.Errorf("fields.%s: close sets %s itself (from id, outcome, reason, or evidence)", k, k)
		}
		base[k] = v
	}
	if in.Evidence != nil {
		if err := checkEvidence(ctx, d, *in.Evidence); err != nil {
			return verbRoute{}, err
		}
	}
	rt, err := newRoute(op, base, true)
	rt.evidence = in.Evidence
	return rt, err
}

// typeWord is the word for a document's type the owner sees.
func typeWord(doc model.Document) string {
	switch t := doc.Meta.(type) {
	case *model.Obligation:
		return string(t.Kind)
	case *model.Task:
		if k := t.EffectiveKind(); k != model.KindTask {
			return string(k)
		}
	case *model.Project:
		return "goal"
	case *model.RFX:
		return "ask"
	case *model.Request:
		return "agent work"
	}
	return string(doc.Meta.Header().Type)
}

// withEvidence adds close's evidence to the write's sources, so the receipt
// keeps it.
func withEvidence(ctx context.Context, ev *model.Evidence) context.Context {
	if ev == nil {
		return ctx
	}
	m := writeMeta(ctx)
	if l := strings.TrimSpace(ev.Locator); l != "" && !slices.Contains(m.Sources, l) {
		m.Sources = append(m.Sources, l)
	}
	if ev.Via != nil && strings.TrimSpace(ev.Via.Ref) != "" {
		m.Sources = append(m.Sources, ev.Via.Source+":"+ev.Via.Ref)
	}
	if v := strings.TrimSpace(ev.Verify); v != "" && m.Verify == "" {
		m.Verify = v
	}
	return WithWrite(ctx, m)
}

// --- later ---

// LaterInput is the input of later: defer's input, plus id for one item.
type LaterInput struct {
	ID     string    `json:"id,omitempty"`
	IDs    []string  `json:"ids,omitempty"`
	Items  []ItemRef `json:"items,omitempty"`
	Tag    string    `json:"tag,omitempty"`
	Scope  string    `json:"scope,omitempty"`
	Until  string    `json:"until,omitempty"`
	After  string    `json:"after,omitempty"`
	When   string    `json:"when,omitempty"`
	Place  string    `json:"place,omitempty"`
	New    bool      `json:"new,omitempty"`
	Reason string    `json:"reason,omitempty"`
}

// Later puts items off: defer, with defer's rules.
var Later = defineVerb("later", "Put items off until a date (until), after an item is done (after), when a condition holds (when, makes a task), or someday: id or ids, a tag, or a scope (a goal or area). It runs defer, with defer's rules. Outputs op and result (defer's output); on a person's surface also receipt",
	RiskReview, func(_ context.Context, _ Deps, in LaterInput) (verbRoute, error) {
		ids := in.IDs
		if id := strings.TrimSpace(in.ID); id != "" {
			ids = append([]string{id}, ids...)
		}
		return newRoute(Defer, DeferInput{IDs: ids, Items: in.Items, Tag: in.Tag, Until: in.Until, Reason: in.Reason, Scope: in.Scope,
			After: in.After, When: in.When, Place: in.Place, New: in.New}, true)
	})

// --- reopen ---

// ReopenInput is the input of reopen: one id or ids. Items and tag bring
// back what was put off, as undefer does.
type ReopenInput struct {
	ID     string    `json:"id,omitempty"`
	IDs    []string  `json:"ids,omitempty"`
	Items  []ItemRef `json:"items,omitempty"`
	Tag    string    `json:"tag,omitempty"`
	Reason string    `json:"reason,omitempty"` // why it is open again; kept under ## Outcome of a closed item
}

// Reopen opens closed items again, or brings back items put off (undefer,
// people only).
var Reopen = defineVerb("reopen", "Open a closed item again (task, meeting, question, goal, ask, io item not sent, incident, exception), with a receipt: its status goes back to open and reason is kept under ## Outcome. "+
	"An item put off (later) comes back now instead: that is undefer, people only. Give closed items and put-off items in separate calls. Agent work is not reopened: send new work with send_to_agent. Outputs op and result; on a person's surface also receipt",
	RiskReview, reopenRoute)

func reopenRoute(ctx context.Context, d Deps, in ReopenInput) (verbRoute, error) {
	ids := slices.Clone(in.IDs)
	if id := strings.TrimSpace(in.ID); id != "" {
		ids = append([]string{id}, ids...)
	}
	if len(ids) == 0 && len(in.Items) == 0 && strings.TrimSpace(in.Tag) == "" {
		return verbRoute{}, errors.New("nothing to reopen: give id, ids, items, or a tag")
	}
	var closed, later []string
	for _, id := range ids {
		id = normID(id)
		if id == "" {
			continue
		}
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			return verbRoute{}, linkErr("id", id, err)
		}
		switch df := deferralOf(doc.Meta); {
		case lifeOf(doc.Meta) == LifeClosed:
			closed = append(closed, doc.ID)
		case df == nil || df.DeferUntil == "" && len(df.DeferredAfter) == 0:
			return verbRoute{}, fmt.Errorf("%s is open and not put off: nothing to reopen", doc.ID)
		default:
			later = append(later, id)
		}
	}
	if len(closed) > 0 {
		if len(later) > 0 || len(in.Items) > 0 || strings.TrimSpace(in.Tag) != "" {
			return verbRoute{}, fmt.Errorf("%s closed and %s not: reopen closed items and bring back put-off items in separate calls", strings.Join(closed, ", "), strings.Join(append(later, refStrings(in.Items)...), ", "))
		}
		return newRoute(reopenClosed, reopenClosedInput{IDs: closed, Reason: in.Reason}, true)
	}
	return newRoute(Undefer, UndeferInput{IDs: later, Items: in.Items, Tag: in.Tag}, true)
}

func refStrings(refs []ItemRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.String()
	}
	return out
}

// reopenClosedInput is the input reopen gives its own write.
type reopenClosedInput struct {
	IDs    []string `json:"ids"`
	Reason string   `json:"reason,omitempty"`
}

// ReopenOutput is what reopen of closed items changed.
type ReopenOutput struct {
	Changed  []ReopenedItem `json:"changed"`
	Warnings []string       `json:"warnings"`
}

// ReopenedItem is one item opened again: its status before and now.
type ReopenedItem struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	Rev  string `json:"rev"`
}

// reopenClosed is reopen's own write. It is not listed: reopen routes to it,
// so it has reopen's name, and a staged item of it runs reopen again.
var reopenClosed = func() *Op[reopenClosedInput, ReopenOutput] {
	op := &Op[reopenClosedInput, ReopenOutput]{name: "reopen", desc: "reopen closed items", risk: RiskReview, oneReceipt: true,
		fn: func(ctx context.Context, d Deps, in reopenClosedInput) (ReopenOutput, error) {
			reason := flatLine(in.Reason)
			if err := lintText("reason", reason); err != nil {
				return ReopenOutput{}, err
			}
			out := ReopenOutput{Changed: []ReopenedItem{}, Warnings: []string{}}
			for _, id := range in.IDs {
				it, warn, err := reopenOne(ctx, d, normID(id), reason)
				if err != nil {
					return out, err
				}
				out.Changed = append(out.Changed, it)
				if warn != "" {
					out.Warnings = append(out.Warnings, warn)
				}
			}
			return out, nil
		}}
	return op
}()

// reopenOne opens one closed item: its status goes back to the type's open
// value, the fields of how it ended are cleared, and a line under ## Outcome
// says when and why.
func reopenOne(ctx context.Context, d Deps, id, reason string) (ReopenedItem, string, error) {
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return ReopenedItem{}, "", linkErr("id", id, err)
	}
	if lifeOf(doc.Meta) != LifeClosed {
		return ReopenedItem{}, "", fmt.Errorf("%s is open: nothing to reopen", doc.ID)
	}
	var from, to, warn string
	move := model.Folder("")
	switch m := doc.Meta.(type) {
	case *model.Task:
		from, to = string(m.Status), string(model.TaskTodo)
		if m.Kind == model.KindIncident {
			from, to = string(model.IncidentStatusOf(m.Status)), string(model.IncidentOpen)
		}
		if mt, ok := readMeeting(doc); ok {
			from = string(mt.Status)
			to = string(model.MeetingRequested)
			if m.Scheduled != "" {
				to = string(model.MeetingScheduled)
			}
		}
		m.Status, m.Outcome = model.TaskTodo, ""
		m.DeclinedBy, m.DeclinedPerson, m.RetryAfter, m.ReroutedTo = "", "", "", ""
		if next, err := successor(ctx, d, doc.ID); err == nil && next != "" {
			warn = fmt.Sprintf("%s recurs: its next occurrence %s stays open", doc.ID, next)
		}
	case *model.Question:
		from, to = string(m.Status), string(model.QuestionOpen)
		m.Status, m.Choice, m.AnsweredBy, m.Answered = model.QuestionOpen, "", "", ""
		if from == string(model.QuestionAnswered) {
			warn = doc.ID + " was answered: the answer text stays in its body until a new answer replaces it"
		}
	case *model.Project:
		from, to = string(m.Status), string(model.ProjectActive)
		m.Status = model.ProjectActive
		if doc.Folder == model.FolderArchive {
			move = model.FolderProjects
		}
	case *model.RFX:
		from, to = string(m.Status), string(model.RFXOpen)
		m.Status = model.RFXOpen
	case *model.IO:
		if m.State == model.IOSent {
			return ReopenedItem{}, "", fmt.Errorf("%s was sent: an act outside the vault is not undone; write a new draft", doc.ID)
		}
		from = string(m.State)
		m.State = model.IORead
		if m.Direction == model.IOOut {
			m.State = model.IODraft
		}
		to, m.Reason = string(m.State), ""
	case *model.Obligation:
		from, to = string(m.Outcome), string(model.StateOpen)
		m.State, m.Outcome, m.Reason, m.Closed = model.StateOpen, "", "", ""
		if next, err := successor(ctx, d, doc.ID); err == nil && next != "" {
			warn = fmt.Sprintf("%s recurs: its next occurrence %s stays open", doc.ID, next)
		}
	case *model.Incident:
		from, to = string(m.Status), string(model.IncidentOpen)
		m.Status = model.IncidentOpen
	case *model.PolicyException:
		from, to = string(m.Status), string(model.ExceptionOpen)
		m.Status = model.ExceptionOpen
	case *model.Request:
		return ReopenedItem{}, "", fmt.Errorf("%s is agent work that ended: send new work with send_to_agent", doc.ID)
	default:
		return ReopenedItem{}, "", fmt.Errorf("%s is a %s: it is not reopened here", doc.ID, typeWord(doc))
	}
	line := "Reopened " + d.Store.Now().Format(dateLayout) + "."
	if reason != "" {
		line = "Reopened " + d.Store.Now().Format(dateLayout) + ": " + reason
	}
	doc.Body = appendToSection(doc.Body, outcomeHeading, line)
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return ReopenedItem{}, "", err
	}
	if move != "" {
		if saved, err = d.Store.Move(ctx, saved.ID, move); err != nil {
			return ReopenedItem{}, "", err
		}
	}
	if err := d.Store.Record(ctx, "reopen", saved.ID, from, to, ""); err != nil {
		return ReopenedItem{}, "", err
	}
	return ReopenedItem{ID: saved.ID, From: from, To: to, Rev: saved.Rev}, warn, nil
}

// --- send_to_agent ---

// SendToAgentInput is the input of send_to_agent.
type SendToAgentInput struct {
	ID          string   `json:"id"`
	Note        string   `json:"note,omitempty"`        // what to do about the item
	Attachments []string `json:"attachments,omitempty"` // vault paths (attachment_add); people only
	Done        bool     `json:"done,omitempty"`        // ask the agent to close the item after; people only
	Title       string   `json:"title,omitempty"`       // a short name for the work; an agent must give one
	FileNotes   bool     `json:"file_notes,omitempty"`  // a meeting: file its notes and close it as held; people only
}

// SendToAgent queues agent work about an item: agent_send on a person's
// surface, request_add linked to the item on an agent's.
var SendToAgent = defineVerb("send_to_agent", "Send an item to an agent with a note: queues agent work linked to the item. On a person's surface it runs agent_send (note is the owner's words; attachments and done as agent_send takes them); on an agent's it runs request_add with title, the note as the ask, and the item in links (an agent's request is proposed until the owner accepts it). file_notes (people only), on a meeting, asks the agent to file its notes and close it as held (prompt ingest_source). Outputs op and result",
	RiskSafe, func(ctx context.Context, d Deps, in SendToAgentInput) (verbRoute, error) {
		if !agentSurface(d) {
			return newRoute(AgentSend, AgentSendInput{ID: in.ID, Text: in.Note, Attachments: in.Attachments, Done: in.Done, FileNotes: in.FileNotes}, false)
		}
		if len(in.Attachments) > 0 || in.Done || in.FileNotes {
			return verbRoute{}, errors.New("attachments, done, and file_notes are the owner's: leave them out")
		}
		id := normID(in.ID)
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			return verbRoute{}, linkErr("id", id, err)
		}
		add := RequestAddInput{Title: in.Title, Ask: in.Note, Kind: FromAgentKind}
		if slices.Contains(linkTypes, doc.Meta.Header().Type) {
			add.Links = []string{doc.ID}
		} else {
			add.Context = []string{doc.ID}
		}
		return newRoute(RequestAdd, add, false)
	})

// --- show ---

// ShowInput is the input of show.
type ShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// ShowOutput is one item: its lifecycle, whether it needs the owner, and the
// output of its type's show op (detail), when the type has one.
type ShowOutput struct {
	ID       string         `json:"id"`
	Type     model.Type     `json:"type"`
	Title    string         `json:"title"`
	Status   string         `json:"status,omitempty"`
	Life     Lifecycle      `json:"life"`
	Turn     Turn           `json:"turn"`
	NeedsYou bool           `json:"needs_you"`
	Reason   string         `json:"reason"`
	Op       string         `json:"op,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
	raw      any
}

// showOps is the show op of each type that has one. A meeting task shows as
// a task (task_show); meeting_show reads the older meeting documents too.
func showOps() map[model.Type]Entry {
	return map[model.Type]Entry{
		model.TypeTask: TaskShow, model.TypeQuestion: QuestionShow, model.TypeProject: ProjectShow, model.TypeRFX: RFXShow,
		model.TypeIO: IOShow, model.TypeRequest: RequestShow, model.TypeArea: AreaShow, model.TypeIncident: IncidentShow,
		model.TypeMeeting: MeetingShow, model.TypeReceipt: ReceiptShow, model.TypeSource: SourceShow, model.TypeTrackedDoc: DocTrackedShow,
		model.TypePolicyException: ExceptionShow, model.TypeConvention: ConventionShow, model.TypeConstraint: ConstraintShow,
		model.TypeInteraction: InteractionShow, model.TypeNote: NoteShow, model.TypePolicy: PolicyShow,
		model.TypePerson: OrgPersonShow, model.TypeTeam: OrgTeamShow, model.TypeRole: OrgRoleShow, model.TypeDomain: OrgDomainShow,
	}
}

// Show shows any item.
var Show = defineRead("show", "Show any item by id: type, title, status, life (open, later, closed), whether it needs the owner now (needs_you) and why, and detail: the output of its type's show op (op names it)", func(ctx context.Context, d Deps, in ShowInput) (ShowOutput, error) {
	rt, err := showRoute(ctx, d, in)
	if err != nil {
		return ShowOutput{}, err
	}
	id := normID(in.ID)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return ShowOutput{}, linkErr("id", id, err)
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return ShowOutput{}, err
	}
	n := sn.Need(doc.ID)
	out := ShowOutput{ID: doc.ID, Type: doc.Meta.Header().Type, Title: doc.Title, Status: statusOf(incidentShape(doc).Meta), Life: n.Life, Turn: n.Turn, NeedsYou: n.Yes, Reason: n.Reason}
	if rt.op == nil {
		return out, nil
	}
	res, err := rt.op.Invoke(ctx, d, rt.input)
	if err != nil {
		return out, err
	}
	out.Op, out.Detail, out.raw = rt.op.Name(), asMap(res), res
	return out, nil
})

// showRouted registers show's router before any init runs (opalias.go reads
// verbRoutes in its init).
var showRouted = func() bool {
	verbRoutes[Show.Name()] = func(ctx context.Context, d Deps, raw json.RawMessage) (verbRoute, error) {
		var in ShowInput
		if err := decodeStrict(raw, &in); err != nil {
			return verbRoute{}, err
		}
		return showRoute(ctx, d, in)
	}
	return true
}()

// showRoute is the show op of the item's type; op is nil for a type with
// none.
func showRoute(ctx context.Context, d Deps, in ShowInput) (verbRoute, error) {
	id := normID(in.ID)
	if id == "" {
		return verbRoute{}, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return verbRoute{}, linkErr("id", id, err)
	}
	op, ok := showOps()[incidentShape(doc).Meta.Header().Type] // a task of kind incident shows as an incident
	if doc.Meta.Header().Type == model.TypeObligation {
		op, ok = TaskShow, true // a meeting or work owed shows as a task of its kind
	}
	if !ok {
		return verbRoute{}, nil
	}
	return newRoute(op, map[string]any{"id": doc.ID}, false)
}

// statusOf is the type's own status value, or "".
func statusOf(m model.Meta) string {
	switch x := m.(type) {
	case *model.Task:
		return string(x.Status)
	case *model.Meeting:
		return string(x.Status)
	case *model.Obligation:
		return string(cmp.Or(x.State, model.StateOpen))
	case *model.Question:
		return string(x.Status)
	case *model.Project:
		return string(x.Status)
	case *model.RFX:
		return string(x.Status)
	case *model.Request:
		return string(x.Status)
	case *model.IO:
		return string(x.State)
	case *model.Incident:
		return string(x.Status)
	case *model.PolicyException:
		return string(x.Status)
	case *model.Area:
		return string(x.Status)
	case *model.Proposal:
		return string(x.Status)
	}
	return ""
}

// --- list ---

// ListInput is the input of list. Give type, needs_you, or both.
type ListInput struct {
	Type model.Type `json:"type,omitempty" query:"type"`
	// State is open, later, closed, or all; empty is open and later (not
	// closed).
	State    string `json:"state,omitempty" query:"state"`
	Tag      string `json:"tag,omitempty" query:"tag"`
	NeedsYou bool   `json:"needs_you,omitempty" query:"needs_you"`
	Limit    int    `json:"limit,omitempty" query:"limit"`
}

// ListRow is one item of list.
type ListRow struct {
	ID       string     `json:"id"`
	Type     model.Type `json:"type"`
	Title    string     `json:"title"`
	Status   string     `json:"status,omitempty"`
	Life     Lifecycle  `json:"life"`
	Turn     Turn       `json:"turn"`
	NeedsYou bool       `json:"needs_you"`
	Reason   string     `json:"reason"`
	// Request is the detail of a request row (type request): its kind, the
	// item it is about, the owner's words, who has it, how it ended, stale,
	// seen, and the receipts made under it.
	Request *SentItem `json:"request,omitempty"`
}

// ListOutput is the output of list.
type ListOutput struct {
	Rows  []ListRow `json:"rows"`
	Count int       `json:"count"` // rows that matched, before limit
}

// listStates are the values of list's state.
var listStates = []string{"", string(LifeOpen), string(LifeLater), string(LifeClosed), "all"}

// List lists items of any type with their lifecycle and needs_you.
var List = defineRead("list", "List items of a type (task, question, project, rfx, io, request, area, incident, ...) with life (open, later, closed), whether each needs the owner now (needs_you) and why. state: open, later, closed, or all; default open and later. needs_you: only what needs the owner now (with no type: every type). tag: a topic. Closed goals live in archive/ and are listed with state closed or all",
	func(ctx context.Context, d Deps, in ListInput) (ListOutput, error) {
		state := strings.TrimSpace(in.State)
		if !slices.Contains(listStates, state) {
			return ListOutput{}, fmt.Errorf("state %q: want open, later, closed, or all", state)
		}
		if in.Type == "" && !in.NeedsYou {
			return ListOutput{}, errors.New("give type, needs_you, or both")
		}
		if in.Type != "" && !slices.Contains(model.Types, in.Type) {
			return ListOutput{}, fmt.Errorf("type %q: want one of %v", in.Type, model.Types)
		}
		sn, err := NewSnapshot(ctx, d.Store)
		if err != nil {
			return ListOutput{}, err
		}
		// An older small type lists its new form too (refsOf), as that type.
		refs, err := refsOf(ctx, d.Store, store.Filter{Type: in.Type, All: state == string(LifeClosed) || state == "all"})
		if err != nil {
			return ListOutput{}, err
		}
		tag := normTag(in.Tag)
		var receipts map[string][]string
		if in.Type == model.TypeRequest {
			if receipts, err = requestReceipts(ctx, d); err != nil {
				return ListOutput{}, err
			}
		}
		out := ListOutput{Rows: []ListRow{}}
		for _, r := range refs {
			if r.Type == model.TypeReceipt && in.Type != model.TypeReceipt {
				continue
			}
			n := sn.Need(r.ID)
			switch {
			case in.NeedsYou && !n.Yes:
				continue
			case state == "" && n.Life == LifeClosed, state != "" && state != "all" && string(n.Life) != state:
				continue
			}
			doc, err := sn.Get(r.ID)
			if err != nil {
				continue
			}
			if tag != "" && !slices.Contains(normTags(doc.Meta.Header().Tags), tag) {
				continue
			}
			out.Count++
			if in.Limit > 0 && len(out.Rows) >= in.Limit {
				continue
			}
			row := ListRow{ID: doc.ID, Type: r.Type, Title: doc.Title, Status: statusOf(incidentShape(doc).Meta), Life: n.Life, Turn: n.Turn, NeedsYou: n.Yes, Reason: n.Reason}
			if m, ok := doc.Meta.(*model.Request); ok && in.Type == model.TypeRequest {
				it := askedItem(ctx, d, doc, m, sn.Now)
				it.Receipts, it.NeedsYou = receipts[doc.ID], n.Yes
				row.Request = &it
			}
			out.Rows = append(out.Rows, row)
		}
		return out, nil
	})
