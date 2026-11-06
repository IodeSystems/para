package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/iodesystems/para/internal/model"
)

// The op alias table. An old op name stays callable from MCP and the CLI for
// one release after the generic verbs (verbs.go): the alias maps its input to
// the verb's, and the verb routes to the op of the item's type, which is the
// old op itself (or, for a person's op, the op it ran). Before it runs, the
// alias checks that route: when the verb would run another op, or the same op
// with another input, the old op runs as before. So an alias changes neither
// what an op does nor its gate class. The output is the old op's, with
// alias_of naming the verb. The old ops stay registered: the server, the
// dossier runner, and receipts use them by name.

// aliasDef is one row of the table.
type aliasDef struct {
	name string // the old op
	to   string // the verb
	// in maps the old input to the verb's input.
	in func(m map[string]any) (map[string]any, error)
	// want is the op the old op runs for this input, and its input; nil is
	// the old op itself with the same input.
	want func(ctx context.Context, d Deps, raw json.RawMessage) (Entry, any, error)
	// out maps the routed result to the old op's output; nil is the routed
	// op's output as it is.
	out func(rt verbRoute, raw any) any
}

// rename moves m[from] to the verb's field to when it is set.
func rename(m, out map[string]any, from, to string) {
	if v, ok := m[from]; ok {
		out[to] = v
		delete(m, from)
	}
}

// asFields puts what is left of m under fields.
func asFields(m, out map[string]any) map[string]any {
	delete(m, "id")
	if len(m) > 0 {
		out["fields"] = m
	}
	return out
}

func idOnly(m map[string]any) map[string]any { return map[string]any{"id": m["id"]} }

// closeAlias maps an old close op whose reason field is reasonKey (or "")
// and whose outcome is fixed (or "").
func closeAlias(name, outcome, reasonKey string) aliasDef {
	return aliasDef{name: name, to: "close", in: func(m map[string]any) (map[string]any, error) {
		out := idOnly(m)
		if outcome != "" {
			out["outcome"] = outcome
		}
		if reasonKey != "" {
			rename(m, out, reasonKey, "reason")
		}
		return asFields(m, out), nil
	}}
}

func aliasTable() []aliasDef {
	defs := []aliasDef{
		{name: "task_done", to: "close", in: func(m map[string]any) (map[string]any, error) {
			out := idOnly(m)
			if skip, _ := m["skip"].(bool); skip {
				out["outcome"] = "skipped"
			}
			delete(m, "skip")
			rename(m, out, "note", "reason")
			return asFields(m, out), nil
		}},
		{name: "task_resolve", to: "close", in: func(m map[string]any) (map[string]any, error) {
			out := idOnly(m)
			o, _ := m["outcome"].(string)
			if o == "" {
				o = string(model.OutcomeDone)
			}
			out["outcome"] = o
			delete(m, "outcome")
			if o == string(model.OutcomeDeclined) || o == string(model.OutcomeRerouted) {
				rename(m, out, "reason", "reason")
			} else {
				rename(m, out, "note", "reason")
			}
			return asFields(m, out), nil
		}, want: func(_ context.Context, _ Deps, raw json.RawMessage) (Entry, any, error) {
			var in TaskResolveInput
			if err := decodeStrict(raw, &in); err != nil {
				return nil, nil, err
			}
			if in.Outcome == "" {
				in.Outcome = model.OutcomeDone // task_resolve's default
			}
			return TaskResolve, in, nil
		}},
		closeAlias("meeting_hold", "", "notes"),
		closeAlias("meeting_cancel", string(model.OutcomeDropped), "reason"),
		closeAlias("question_drop", string(model.QuestionDropped), "reason"),
		{name: "project_close", to: "close", in: func(m map[string]any) (map[string]any, error) {
			out := idOnly(m)
			if dropped, _ := m["dropped"].(bool); dropped {
				out["outcome"] = string(model.ProjectDropped)
			}
			delete(m, "dropped")
			return asFields(m, out), nil
		}},
		closeAlias("rfx_close", "", ""),
		closeAlias("io_drop", string(model.IODropped), "reason"),
		closeAlias("io_done", string(model.IOSent), ""),
		closeAlias("request_cancel", "", ""),
		{name: "item_close", to: "close", in: func(m map[string]any) (map[string]any, error) { return asFields(m, idOnly(m)), nil },
			want: itemCloseWant, out: func(rt verbRoute, raw any) any {
				res := asMap(raw)
				id, _ := res["id"].(string)
				rev, _ := res["rev"].(string)
				return ItemCloseOutput{ID: id, By: rt.op.Name(), Rev: rev}
			}},
		{name: "defer", to: "later", in: func(m map[string]any) (map[string]any, error) { return m, nil }},
		{name: "stack_later", to: "later", in: func(m map[string]any) (map[string]any, error) {
			rename(m, m, "questions", "ids")
			return m, nil
		}, want: func(_ context.Context, _ Deps, raw json.RawMessage) (Entry, any, error) {
			var in StackLaterInput
			if err := decodeStrict(raw, &in); err != nil {
				return nil, nil, err
			}
			if len(in.Items) == 0 && len(in.Questions) == 0 {
				return nil, nil, fmt.Errorf("nothing to put off")
			}
			return Defer, DeferInput{IDs: in.Questions, Items: in.Items, Until: in.Until, Reason: in.Reason}, nil
		}},
		{name: "undefer", to: "reopen", in: func(m map[string]any) (map[string]any, error) { return m, nil }},
		{name: "stack_return", to: "reopen", in: func(m map[string]any) (map[string]any, error) {
			rename(m, m, "questions", "ids")
			return m, nil
		}, want: func(_ context.Context, _ Deps, raw json.RawMessage) (Entry, any, error) {
			var in StackReturnInput
			if err := decodeStrict(raw, &in); err != nil {
				return nil, nil, err
			}
			if len(in.Items) == 0 && len(in.Questions) == 0 {
				return nil, nil, fmt.Errorf("nothing to bring back")
			}
			return Undefer, UndeferInput{IDs: in.Questions, Items: in.Items}, nil
		}},
		{name: "agent_send", to: "send_to_agent", in: func(m map[string]any) (map[string]any, error) {
			rename(m, m, "text", "note")
			return m, nil
		}},
	}
	for _, name := range showAliases {
		defs = append(defs, aliasDef{name: name, to: "show", in: func(m map[string]any) (map[string]any, error) { return m, nil }})
	}
	// The meeting ops are obligation ops now. meeting_add and meeting_schedule
	// keep their own outputs, so they run as before (on the same code as
	// obligation_add and obligation_schedule); attend and rsvp run
	// obligation_attend, rsvp after its person check.
	same := func(m map[string]any) (map[string]any, error) { return m, nil }
	attend := func(_ context.Context, _ Deps, raw json.RawMessage) (Entry, any, error) {
		return ObligationAttend, raw, nil
	}
	defs = append(defs,
		aliasDef{name: "meeting_add", to: ObligationAdd.Name(), in: same},
		aliasDef{name: "meeting_schedule", to: ObligationSchedule.Name(), in: same},
		aliasDef{name: "meeting_attend", to: ObligationAttend.Name(), in: same, want: attend},
		aliasDef{name: "meeting_rsvp", to: ObligationAttend.Name(), in: same, want: attend},
		aliasDef{name: "meeting_show", to: "show", in: same},
	)
	// The add ops of exceptions, conventions, and constraints are annotate
	// now. They keep their inputs and outputs and run as before (each old op
	// writes an annotation itself at the policy docs format, the same one the
	// upgrade step makes of such a file), so they never route.
	for _, name := range []string{"exception_add", "convention_add", "constraint_add"} {
		defs = append(defs, aliasDef{name: name, to: Annotate.Name(), in: same})
	}
	// The add ops of teams, roles, domains, interactions, and incidents are
	// the ops of the types that hold them now. They keep their inputs and
	// outputs and run as before (at the small types format each writes the
	// same new form the upgrade step makes of such a file: a group of that
	// kind, a closed io item, a task of kind incident), so they never route.
	for _, a := range [][2]string{{"org_team_add", "org_group_add"}, {"org_role_add", "org_group_add"}, {"org_domain_add", "org_group_add"},
		{"interaction_add", "io_add"}, {"incident_add", "task_add"}} {
		defs = append(defs, aliasDef{name: a[0], to: a[1], in: same})
	}
	// incident_status resolved is close; the other statuses run as before.
	defs = append(defs, aliasDef{name: "incident_status", to: "close", in: func(m map[string]any) (map[string]any, error) {
		if m["status"] != string(model.IncidentResolved) {
			return nil, fmt.Errorf("status %v is not a close", m["status"])
		}
		delete(m, "status")
		out := idOnly(m)
		rename(m, out, "text", "reason")
		return asFields(m, out), nil
	}})
	return defs
}

// showAliases are the show ops that take only an id and are the show op of
// their type (showOps). meeting_show is an alias of show too, but show shows
// a meeting with task_show, so meeting_show runs as before.
var showAliases = []string{"task_show", "question_show", "project_show", "rfx_show", "io_show", "request_show", "area_show",
	"incident_show", "receipt_show", "source_show", "exception_show", "convention_show", "constraint_show", "interaction_show",
	"note_show", "policy_show", "org_person_show", "org_team_show", "org_role_show", "org_domain_show"}

// itemCloseWant is what item_close runs for an id (composer.go).
func itemCloseWant(ctx context.Context, d Deps, raw json.RawMessage) (Entry, any, error) {
	var in ItemCloseInput
	if err := decodeStrict(raw, &in); err != nil {
		return nil, nil, err
	}
	id := normID(in.ID)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := readMeeting(doc); ok {
		return MeetingHold, MeetingHoldInput{ID: id}, nil
	}
	switch m := doc.Meta.(type) {
	case *model.Obligation:
		return obligationClose, obligationCloseInput{ID: id}, nil
	case *model.Task:
		return TaskDone, TaskDoneInput{ID: id}, nil
	case *model.RFX:
		if m.Direction == model.RFXOut {
			return RFXClose, RFXCloseInput{ID: id}, nil
		}
	}
	return nil, nil, fmt.Errorf("%s: item_close refuses it", id)
}

// aliases holds the alias entries by old name.
var aliases = map[string]*aliasEntry{}

func init() {
	for _, def := range aliasTable() {
		old, ok := registry[def.name]
		verb, vok := registry[def.to]
		if !ok || !vok || verbRoutes[def.to] == nil {
			panic("ops: alias " + def.name + " -> " + def.to + ": no such op")
		}
		if _, dup := aliases[def.name]; dup {
			panic("ops: duplicate alias " + def.name)
		}
		aliases[def.name] = &aliasEntry{def: def, old: old, verb: verb}
	}
}

// AliasOf is the verb an old op name is an alias of, or "".
func AliasOf(name string) string {
	if a, ok := aliases[name]; ok {
		return a.def.to
	}
	return ""
}

// Alias is the alias entry of an old op name, or nil.
func Alias(name string) Entry {
	if a, ok := aliases[name]; ok {
		return a
	}
	return nil
}

// AliasNames lists the aliased op names, sorted.
func AliasNames() []string {
	out := make([]string, 0, len(aliases))
	for n := range aliases {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// aliasEntry is an old op name served through a verb.
type aliasEntry struct {
	def  aliasDef
	old  Entry
	verb Entry
}

func (a *aliasEntry) Name() string { return a.old.Name() }
func (a *aliasEntry) Description() string {
	return "Alias of " + a.def.to + " (this name goes in a later release; call " + a.def.to + "). " + a.old.Description()
}
func (a *aliasEntry) PersonOnly() bool        { return a.old.PersonOnly() }
func (a *aliasEntry) Risk() Risk              { return a.old.Risk() }
func (a *aliasEntry) Undoable() bool          { return a.old.Undoable() }
func (a *aliasEntry) InputType() reflect.Type { return a.old.InputType() }

// OutputType is the old op's output with alias_of. The server's routes of
// old names serve the old op, so there alias_of is empty and left out.
func (a *aliasEntry) OutputType() reflect.Type {
	t := a.old.OutputType()
	return reflect.StructOf([]reflect.StructField{
		{Name: t.Name(), Type: t, Anonymous: true},
		{Name: "AliasOf", Type: reflect.TypeFor[string](), Tag: `json:"alias_of,omitempty"`},
	})
}

// Invoke runs the old op through its verb, or the old op itself when the verb
// would not run the same thing. The person check comes first, as in the old
// op.
func (a *aliasEntry) Invoke(ctx context.Context, d Deps, input json.RawMessage) (any, error) {
	if a.old.PersonOnly() && !slices.Contains(PersonSurfaces, d.Store.Info().Surface) {
		return nil, fmt.Errorf("%s: %w", a.old.Name(), ErrPersonOnly)
	}
	res, err := a.run(ctx, d, input)
	return AliasOutput{v: res, of: a.def.to}, err
}

// Routed reports whether input would run through the verb (true) or as the
// old op (false). Tests use it.
func (a *aliasEntry) Routed(ctx context.Context, d Deps, input json.RawMessage) bool {
	_, ok := a.route(ctx, d, input)
	return ok
}

func (a *aliasEntry) route(ctx context.Context, d Deps, input json.RawMessage) (verbRoute, bool) {
	if len(bytes.TrimSpace(input)) == 0 {
		input = json.RawMessage("{}")
	}
	probe := reflect.New(a.old.InputType()).Interface()
	if decodeStrict(input, probe) != nil {
		return verbRoute{}, false
	}
	var m map[string]any
	if json.Unmarshal(input, &m) != nil || m == nil {
		return verbRoute{}, false
	}
	vin, err := a.def.in(m)
	if err != nil {
		return verbRoute{}, false
	}
	vraw, err := json.Marshal(vin)
	if err != nil {
		return verbRoute{}, false
	}
	rt, err := verbRoutes[a.def.to](ctx, d, vraw)
	if err != nil || rt.op == nil {
		return verbRoute{}, false
	}
	want, wantIn := a.old, any(input)
	if a.def.want != nil {
		if want, wantIn, err = a.def.want(ctx, d, input); err != nil {
			return verbRoute{}, false
		}
	}
	if rt.op.Name() != want.Name() || !sameInput(rt.op, rt.input, wantIn) {
		return verbRoute{}, false
	}
	return rt, true
}

func (a *aliasEntry) run(ctx context.Context, d Deps, input json.RawMessage) (any, error) {
	rt, ok := a.route(ctx, d, input)
	if !ok {
		return a.old.Invoke(ctx, d, input)
	}
	var raw any
	var err error
	if a.verb.Risk() == RiskRead {
		raw, err = rt.op.Invoke(ctx, d, rt.input)
	} else {
		var vo VerbOutput
		vo, err = runRoute(ctx, d, rt)
		raw = vo.raw
	}
	if err != nil {
		return raw, err
	}
	if a.def.out != nil {
		return a.def.out(rt, raw), nil
	}
	return raw, nil
}

// sameInput reports whether a and b are the same input of op.
func sameInput(op Entry, a json.RawMessage, b any) bool {
	norm := func(v any) ([]byte, bool) {
		raw, ok := v.(json.RawMessage)
		if !ok {
			var err error
			if raw, err = json.Marshal(v); err != nil {
				return nil, false
			}
		}
		p := reflect.New(op.InputType()).Interface()
		if decodeStrict(raw, p) != nil {
			return nil, false
		}
		out, err := json.Marshal(p)
		return out, err == nil
	}
	x, ok1 := norm(a)
	y, ok2 := norm(b)
	return ok1 && ok2 && bytes.Equal(x, y)
}

// AliasOutput is the old op's output with alias_of: the verb to call instead.
type AliasOutput struct {
	v  any
	of string
}

// Value is the old op's output.
func (o AliasOutput) Value() any { return o.v }

// Of is the verb the call was an alias of.
func (o AliasOutput) Of() string { return o.of }

// MarshalJSON is the old output's object with alias_of added last, in the
// field order of OutputType.
func (o AliasOutput) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(o.v)
	if err != nil {
		return nil, err
	}
	of, _ := json.Marshal(o.of)
	b = bytes.TrimSpace(b)
	if len(b) < 2 || b[0] != '{' || b[len(b)-1] != '}' {
		return json.Marshal(map[string]json.RawMessage{"value": b, "alias_of": of})
	}
	sep := ","
	if bytes.Equal(b, []byte("{}")) {
		sep = ""
	}
	out := append(append([]byte{}, b[:len(b)-1]...), sep+`"alias_of":`...)
	return append(append(out, of...), '}'), nil
}

// Failed passes the old output's failure on.
func (o AliasOutput) Failed() error {
	if f, ok := o.v.(Failing); ok {
		return f.Failed()
	}
	return nil
}
