package ops

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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
	// pre, when set, runs before op and refuses the route with its error
	// (close sent on an agent's surface: io_send_check).
	pre func(ctx context.Context, d Deps) error
}

// verbRoutes holds each verb's router, by verb name. bulk routes each item
// with it.
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
	// Hanging are the open items that hang on what close closed (a goal, an
	// obligation, a question), with HangingNote: the owner decides on them.
	Hanging     []ThemeItem `json:"hanging,omitempty"`
	HangingNote string      `json:"hanging_note,omitempty"`
	raw         any
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
	if rt.pre != nil {
		if err := rt.pre(ctx, d); err != nil {
			return out, err
		}
	}
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
	// Answer, Choice, and Sources are an answer: a question's
	// (question_answer) or an ask's reply (rfx_reply, answer is its text).
	Answer  string               `json:"answer,omitempty"`
	Choice  string               `json:"choice,omitempty"`
	Sources []model.AnswerSource `json:"sources,omitempty"`
}

// CloseDescription is close's description.
const CloseDescription = "Close any item with an outcome. It runs the close op of the item's type, with that op's own gate, rules, and receipt: " +
	"task: (none) or done = done (a recurring task makes the next one), skipped = skip it once (a recurring task), any other outcome (partial, dropped, duplicate, superseded, declined, failed, rerouted) = resolve with that outcome; " +
	"meeting: (none) or done = held (reason = notes), dropped = cancelled, skipped = obligation_attend no, duplicate = resolve as duplicate; " +
	"question: dropped, answered = question_answer (answer or choice; reason is why; sources); goal: (none) or done, dropped; " +
	"ask: (none) or done = closed, declined or answered = set that status, no, partial, or answered with answer or fields.from = rfx_reply (fields heard, url, via, date); io: (none) or dropped = dropped, sent = marked sent (people) or, from an agent, io_send after a passing io_send_check (fields url, locator, tool); " +
	"agent work: (none) or cancelled; incident: (none) or resolved = incident_status resolved. " +
	"reason goes where the op keeps one (note, notes, reason, or text); an op with none refuses it. " +
	"fields are the op's other inputs (task resolve: link, follow_ups, declined_by, source, tried, to; meeting held: replies, answers, actions; goal: force; ask closed: playbook; io sent: url, locator, text, at). " +
	"evidence, when given, must be one the owner can open: an http(s) URL, a vault id or path, an absolute path or ~/path to a file on this machine, or via {source, ref} with verify; free text alone is refused. " +
	"Outputs op (the op that ran) and result (its output); on a person's surface also receipt, which receipt_revert undoes. " +
	"Closing a goal, obligation, or question also outputs hanging: the open items that still hang on it (as theme_items lists them); ask the owner what becomes of each, never drop them yourself"

// Close closes any item by its type's close op. Closing a theme lists the
// open items that hang on it (hanging), so an agent asks the owner about them.
var Close = func() *Op[CloseInput, VerbOutput] {
	op := defineVerb("close", CloseDescription, RiskReview, closeRoute)
	inner := op.fn
	op.fn = func(ctx context.Context, d Deps, in CloseInput) (VerbOutput, error) {
		out, err := inner(ctx, d, in)
		if err == nil {
			if out.Hanging = hanging(ctx, d, in.ID); len(out.Hanging) > 0 {
				out.HangingNote = HangingNote
			}
		}
		return out, err
	}
	return op
}()

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
	// answered: the op takes answer, choice, and sources (a question or an ask).
	answered := false
	var pre func(ctx context.Context, d Deps) error
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
			op, reasonAs, answered = QuestionAnswer, "why", true
			put(base, "text", in.Answer)
			put(base, "choice", in.Choice)
		default:
			return verbRoute{}, bad("dropped, or answered (answer or choice, and sources)")
		}
	case *model.Goal:
		switch outcome {
		case "", "done":
			op = GoalClose
		case string(model.GoalDropped):
			op = GoalClose
			base["dropped"] = true
		default:
			return verbRoute{}, bad("none or done, or dropped")
		}
	case *model.Project:
		if outcome != "" && outcome != string(model.ProjectRetired) {
			return verbRoute{}, bad("retired (a project has no done: its goals do)")
		}
		op = ProjectRetire
	case *model.RFX:
		_, from := in.Fields["from"]
		switch {
		case outcome == "", outcome == "done", outcome == string(model.RFXClosed):
			op = RFXClose
		case outcome == string(ReplyNo), outcome == string(ReplyPartial), outcome == string(ReplyAnswered) && (in.Answer != "" || from || len(in.Sources) > 0):
			// A reply with its text or its source: rfx_reply. With no
			// answer, the one quote of the sources is the reply's text.
			op, reasonAs, answered = RFXReply, "why", true
			base["outcome"] = outcome
			text := in.Answer
			if text == "" && len(in.Sources) == 1 {
				text = in.Sources[0].Quote
			}
			put(base, "text", text)
		case outcome == string(model.RFXDeclined), outcome == string(model.RFXAnswered):
			op = RFXStatus
			base["status"] = outcome
		default:
			return verbRoute{}, bad("none or done (closed), declined, answered (with answer or fields.from: rfx_reply), no, or partial")
		}
	case *model.IO:
		switch outcome {
		case "", string(model.IODropped):
			op, reasonAs = IODrop, "reason"
		case string(model.IOSent):
			op = IODone
			if agentSurface(d) {
				// An agent records its own send: io_send, after a passing
				// io_send_check.
				op, pre = IOSend, sendCheck(doc.ID)
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
	if answered && len(in.Sources) == 0 && in.Evidence != nil && strings.TrimSpace(in.Evidence.Locator) != "" {
		// The evidence of an answer is its source.
		in.Sources = []model.AnswerSource{{Ref: in.Evidence.Locator, Quote: in.Evidence.Quote}}
	}
	if op == QuestionAnswer && len(in.Sources) == 0 && agentSurface(d) {
		return verbRoute{}, fmt.Errorf("%w. The call that works: close {id: %s, outcome: answered, %s, sources: [{ref: %s, quote: \"<the words that give the answer>\"}]}",
			ErrNoSource, doc.ID, answerArg(in), sourceHint(ctx, d, closeWords(ctx, in)...))
	}
	if !answered && in.Answer == "" && in.Choice == "" && len(in.Sources) > 0 {
		// Sources on another close are its evidence.
		if len(in.Sources) > 1 || in.Evidence != nil {
			return verbRoute{}, fmt.Errorf("a %s %s keeps one evidence, not sources: close {id: %s, outcome: %s, evidence: {quote: %q, locator: %q}}",
				typeWord(doc), cmp.Or(outcome, "close"), doc.ID, cmp.Or(outcome, "done"), in.Sources[0].Quote, in.Sources[0].Ref)
		}
		in.Evidence = &model.Evidence{Quote: in.Sources[0].Quote, Locator: in.Sources[0].Ref}
		in.Sources = nil
	}
	if (op == IODone || op == IOSend) && in.Evidence != nil {
		if model.IsWebURL(in.Evidence.Locator) {
			base["url"] = in.Evidence.Locator
		} else {
			put(base, "locator", in.Evidence.Locator)
		}
	}
	if !answered && (in.Answer != "" || in.Choice != "" || len(in.Sources) > 0) {
		return verbRoute{}, fmt.Errorf("answer, choice, and sources answer a question or an ask (outcome answered), not a %s %s", typeWord(doc), cmp.Or(outcome, "close"))
	}
	if in.Choice != "" && op != QuestionAnswer {
		return verbRoute{}, errors.New("choice is a question's: an ask's reply takes answer")
	}
	if len(in.Sources) > 0 {
		base["sources"] = in.Sources
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
	rt.evidence, rt.pre = in.Evidence, pre
	return rt, err
}

// sendCheck is io_send_check of id, as a route's pre: a held or failed check
// refuses the send with its reason.
func sendCheck(id string) func(ctx context.Context, d Deps) error {
	return func(ctx context.Context, d Deps) error {
		if d.Authorize != nil {
			if err := d.Authorize(IOSendCheck.Name()); err != nil {
				return err
			}
		}
		out, err := IOSendCheck.Run(ctx, d, IOSendCheckInput{ID: id})
		switch {
		case err != nil:
			return err
		case out.Held:
			return fmt.Errorf("%s: io_send_check held the draft: %s; do not send it, tell the owner", id, out.Reason)
		case !out.OK:
			return fmt.Errorf("%s: io_send_check did not pass: %s", id, cmp.Or(out.Reason, out.Next))
		}
		return nil
	}
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
	case *model.Goal:
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
	// UntilStart and UntilEnd are an obligation: back when it starts, or the
	// day after it ends, following it when it moves (defer's until_start).
	UntilStart string `json:"until_start,omitempty"`
	UntilEnd   string `json:"until_end,omitempty"`
}

// Later puts items off: defer, with defer's rules.
var Later = defineVerb("later", "Put items off until a date (until), until an obligation starts or ends (until_start, until_end: an obligation id; the date follows the obligation when it moves, and the items come back with a note when it is cancelled), after an item is done (after), when a condition holds (when: reuses the open task of the same condition, or makes one; new always makes a new one), or someday: id or ids, a tag, or a scope (a goal or area). Outputs op and result; on a person's surface also receipt",
	RiskReview, func(_ context.Context, _ Deps, in LaterInput) (verbRoute, error) {
		ids := in.IDs
		if id := strings.TrimSpace(in.ID); id != "" {
			ids = append([]string{id}, ids...)
		}
		return newRoute(Defer, DeferInput{IDs: ids, Items: in.Items, Tag: in.Tag, Until: in.Until, Reason: in.Reason, Scope: in.Scope,
			After: in.After, When: in.When, Place: in.Place, New: in.New, UntilStart: in.UntilStart, UntilEnd: in.UntilEnd}, true)
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
	"An item put off (later) comes back now with reopen instead (people only). Give closed items and put-off items in separate calls. Agent work is not reopened: send new work with send_to_agent. Outputs op and result; on a person's surface also receipt",
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
	case *model.Goal:
		from, to = string(m.Status), string(model.GoalActive)
		m.Status = model.GoalActive
		if doc.Folder == model.FolderArchive {
			move = model.FolderGoals
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
var SendToAgent = defineVerb("send_to_agent", "Send an item to an agent with a note: queues agent work linked to the item. On a person's surface it takes the owner's words as the note, with attachments and done; on an agent's it runs request_add with title, the note as the ask, and the item in links (an agent's request is proposed until the owner accepts it). file_notes (people only), on a meeting, asks the agent to file its notes and close it as held (prompt ingest_source). Outputs op and result",
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
	ID       string     `json:"id"`
	Type     model.Type `json:"type"`
	Title    string     `json:"title"`
	Status   string     `json:"status,omitempty"`
	Life     Lifecycle  `json:"life"`
	Turn     Turn       `json:"turn"`
	NeedsYou bool       `json:"needs_you"`
	Reason   string     `json:"reason"`
	Op       string     `json:"op,omitempty"`
	Detail   ShowDetail `json:"detail,omitempty"`
	// Notes are the item's ## Notes, and NotesHash their hash: edit {id,
	// notes, notes_hash} replaces them (notes_edit).
	Notes     string `json:"notes,omitempty"`
	NotesHash string `json:"notes_hash,omitempty"`
	raw       any
}

// showOps is the show op of each type that has one. A meeting task shows as
// a task (task_show); meeting_show reads the older meeting documents too.
func showOps() map[model.Type]Entry {
	return map[model.Type]Entry{
		model.TypeTask: TaskShow, model.TypeQuestion: QuestionShow, model.TypeGoal: GoalShow, model.TypeProject: ProjectShow, model.TypeRFX: RFXShow,
		model.TypeIO: IOShow, model.TypeRequest: RequestShow, model.TypeArea: AreaShow, model.TypeIncident: IncidentShow,
		model.TypeMeeting: MeetingShow, model.TypeReceipt: ReceiptShow, model.TypeSource: SourceShow, model.TypeTrackedDoc: DocTrackedShow,
		model.TypePolicyException: ExceptionShow, model.TypeConvention: ConventionShow, model.TypeConstraint: ConstraintShow,
		model.TypeInteraction: InteractionShow, model.TypeNote: NoteShow, model.TypePolicy: PolicyShow,
		model.TypePerson: OrgPersonShow, model.TypeTeam: OrgTeamShow, model.TypeRole: OrgRoleShow, model.TypeDomain: OrgDomainShow,
		model.TypeGroup: OrgGroupShow,
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
	if !slices.Contains(noRewrite, out.Type) {
		out.Notes = headingText(doc.Body, notesHeading)
		out.NotesHash = notesHash(out.Notes)
	}
	if rt.op == nil {
		return out, nil
	}
	res, err := rt.op.Invoke(ctx, d, rt.input)
	if err != nil {
		return out, err
	}
	out.Op, out.Detail, out.raw = rt.op.Name(), showDetail(rt.op.Name(), res), res
	return out, nil
})

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
	case *model.Goal:
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

// ListInput is the input of list. With no type, needs_you is on. ask is rfx.
type ListInput struct {
	Type model.Type `json:"type,omitempty" query:"type"`
	// State is open, later, closed, or all; empty is open and later (not
	// closed).
	State string `json:"state,omitempty" query:"state"`
	// Status is the type's own status (goal parked: the icebox); with no
	// state it lists every life. A project lists status active by default.
	Status   string `json:"status,omitempty" query:"status"`
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
	// Rank and Group are the row's place in the order op's ranking (rank 1
	// first), on a needs_you list; a row the ranking leaves out has none.
	Rank  int    `json:"rank,omitempty"`
	Group string `json:"group,omitempty"`
	// Request is the detail of a request row (type request): its kind, the
	// item it is about, the owner's words, who has it, how it ended, stale,
	// seen, and the receipts made under it.
	Request *SentItem `json:"request,omitempty"`
	// Obligation is the detail of an obligation row: its kind, schedule,
	// date, start, who it is owed to or with, and how the promise stands.
	Obligation *ObligationRow `json:"obligation,omitempty"`
	// Project is the summary of a project row (projectsummary.go).
	Project *ProjectRow `json:"project,omitempty"`
	// Goal is the summary of a goal row (goalsummary.go).
	Goal *GoalRow `json:"goal,omitempty"`
}

// ObligationRow is the detail of an obligation in a list.
type ObligationRow struct {
	Kind     model.TaskKind `json:"kind"`
	Schedule model.Schedule `json:"schedule"`
	Date     string         `json:"date,omitempty"`
	Start    string         `json:"start,omitempty"`
	End      string         `json:"end,omitempty"`
	Recur    string         `json:"recur,omitempty"`
	Reply    bool           `json:"reply,omitempty"` // a reply owed to a message
	Agent    bool           `json:"agent,omitempty"` // an agent owes it (owed_by)
	OwedBy   []string       `json:"owed_by"`
	Parties  []string       `json:"parties"` // names
	Promise  model.Promise  `json:"promise,omitempty"`
	Outcome  string         `json:"outcome,omitempty"`
}

// ListOutput is the output of list.
type ListOutput struct {
	Rows  []ListRow `json:"rows"`
	Count int       `json:"count"` // rows that matched, before limit
}

// listStates are the values of list's state.
var listStates = []string{"", string(LifeOpen), string(LifeLater), string(LifeClosed), "all"}

// List lists items of any type with their lifecycle and needs_you.
var List = defineRead("list", "List items of a type (task, question, goal, project, rfx, io, request, area, incident, ...) with life (open, later, closed), whether each needs the owner now (needs_you) and why. state: open, later, closed, or all; default open and later. needs_you: only what needs the owner now (default with no type: every type), ranked as the order op ranks it (rank, group): use that order as it is. tag: a topic. Closed goals live in archive/ and are listed with state closed or all. project: summary per row, stalest first",
	func(ctx context.Context, d Deps, in ListInput) (ListOutput, error) {
		state := strings.TrimSpace(in.State)
		if !slices.Contains(listStates, state) {
			return ListOutput{}, fmt.Errorf("state %q: want open, later, closed, or all", state)
		}
		in.Type = model.Type(strings.TrimSpace(string(in.Type)))
		if in.Type == "ask" { // the UI says ask
			in.Type = model.TypeRFX
		}
		if in.Type == "" {
			in.NeedsYou = true
		}
		if in.Type != "" && !slices.Contains(model.Types, in.Type) {
			return ListOutput{}, fmt.Errorf("type %q: want one of %v", in.Type, model.Types)
		}
		status := strings.TrimSpace(in.Status)
		if status != "" && in.Type == "" {
			return ListOutput{}, errors.New("status needs type: a status is a type's own")
		}
		if in.Type == model.TypeProject && status == "" && state == "" {
			status = string(model.ProjectActive)
		}
		if status != "" && state == "" {
			state = "all"
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
			if status != "" && statusOf(incidentShape(doc).Meta) != status {
				continue
			}
			if tag != "" && !slices.Contains(normTags(doc.Meta.Header().Tags), tag) {
				continue
			}
			out.Count++
			if in.Limit > 0 && len(out.Rows) >= in.Limit && !in.NeedsYou && in.Type != model.TypeProject && in.Type != model.TypeGoal {
				continue
			}
			row := ListRow{ID: doc.ID, Type: r.Type, Title: doc.Title, Status: statusOf(incidentShape(doc).Meta), Life: n.Life, Turn: n.Turn, NeedsYou: n.Yes, Reason: n.Reason}
			if m, ok := doc.Meta.(*model.Request); ok && in.Type == model.TypeRequest {
				it := askedItem(ctx, d, doc, m, sn.Now)
				it.Receipts, it.NeedsYou = receipts[doc.ID], n.Yes
				row.Request = &it
			}
			if o, ok := doc.Meta.(*model.Obligation); ok {
				row.Obligation = obligationRow(sn, o)
			}
			out.Rows = append(out.Rows, row)
		}
		if in.Type == model.TypeProject {
			sums, err := projectRows(ctx, d, sn, out.Rows)
			if err != nil {
				return ListOutput{}, err
			}
			for i := range out.Rows {
				out.Rows[i].Project = sums[out.Rows[i].ID]
			}
			sortProjectRows(out.Rows)
			if in.Limit > 0 && len(out.Rows) > in.Limit {
				out.Rows = out.Rows[:in.Limit]
			}
		}
		if in.Type == model.TypeGoal {
			sums, err := goalRows(ctx, d, sn, out.Rows)
			if err != nil {
				return ListOutput{}, err
			}
			for i := range out.Rows {
				out.Rows[i].Goal = sums[out.Rows[i].ID]
			}
			sortGoalRows(out.Rows)
			if in.Limit > 0 && len(out.Rows) > in.Limit {
				out.Rows = out.Rows[:in.Limit]
			}
		}
		if in.NeedsYou {
			if err := rankRows(ctx, d, out.Rows); err != nil {
				return ListOutput{}, err
			}
			if in.Limit > 0 && len(out.Rows) > in.Limit {
				out.Rows = out.Rows[:in.Limit]
			}
		}
		return out, nil
	})

// rankRows orders needs_you rows as the order op ranks them (next follows
// the same ranking): ranked rows first, by rank; the rest keep their order.
func rankRows(ctx context.Context, d Deps, rows []ListRow) error {
	o, err := Order(ctx, d.Store)
	if err != nil {
		return err
	}
	pos := map[string]ValueRow{}
	rank := map[string]int{}
	for i, r := range o.Rows {
		if _, ok := rank[r.ID]; !ok {
			pos[r.ID], rank[r.ID] = r, i+1
		}
	}
	for i := range rows {
		if n, ok := rank[rows[i].ID]; ok {
			rows[i].Rank, rows[i].Group = n, pos[rows[i].ID].Group
		}
	}
	slices.SortStableFunc(rows, func(a, b ListRow) int {
		switch {
		case a.Rank == 0 && b.Rank == 0:
			return 0
		case a.Rank == 0:
			return 1
		case b.Rank == 0:
			return -1
		}
		return cmp.Compare(a.Rank, b.Rank)
	})
	return nil
}

// obligationRow is the list detail of an obligation.
func obligationRow(sn *Snapshot, o *model.Obligation) *ObligationRow {
	r := &ObligationRow{Kind: o.Kind, Schedule: o.Schedule, Date: obligationDate(o), Start: o.Scheduled, End: o.End, Recur: o.Recur,
		Reply: o.ReplyTo != "", Agent: o.Agent() != "", OwedBy: targets(o.OwedBy), Parties: []string{}, Promise: promiseNow(sn.Get, o, sn.Today), Outcome: string(o.Outcome)}
	if r.OwedBy == nil {
		r.OwedBy = []string{}
	}
	for _, p := range partiesOf(o) {
		name := p.Text
		if p.ID != "" {
			name = p.ID
			if pd, err := sn.Get(p.ID); err == nil && pd.Title != "" {
				name = pd.Title
			}
		}
		r.Parties = append(r.Parties, name)
	}
	return r
}

// closeWords are the caller's own words on a close: where a refusal looks
// for the source the caller had in hand.
func closeWords(ctx context.Context, in CloseInput) []string {
	m := writeMeta(ctx)
	words := append([]string{m.Input, in.Reason, m.Why, in.Answer, fmt.Sprint(in.Fields)}, m.Sources...)
	if in.Evidence != nil {
		words = append(words, in.Evidence.Locator)
	}
	return words
}

// answerArg is a question answer's own field in a call close shows back.
func answerArg(in CloseInput) string {
	if in.Choice != "" {
		return fmt.Sprintf("choice: %q", in.Choice)
	}
	return fmt.Sprintf("answer: %q", cmp.Or(in.Answer, "<the answer>"))
}

var (
	hintURLRe = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)
	hintIDRe  = regexp.MustCompile(`\b[a-z]+-[a-z0-9][a-z0-9-]*\b`)
)

// sourceHint is a source ref for a refusal to show back: the first URL in
// the caller's own words, else the first vault id there that exists, else a
// placeholder. It is a hint only: an answer cites its sources in sources.
func sourceHint(ctx context.Context, d Deps, texts ...string) string {
	all := strings.Join(texts, " ")
	if u := hintURLRe.FindString(all); u != "" {
		return fmt.Sprintf("%q", strings.TrimRight(u, ".,;:"))
	}
	for _, id := range hintIDRe.FindAllString(all, -1) {
		if _, err := d.Store.Get(ctx, id); err == nil {
			return fmt.Sprintf("%q", id)
		}
	}
	return `"<a URL, vault document id, or absolute path you read it in>"`
}
