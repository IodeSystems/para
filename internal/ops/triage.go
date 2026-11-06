package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Triage rules are vault config, triage.rules in .para/triage.yaml: an
// ordered list the owner writes. They apply to incoming io when it is added
// (io_add, sweep_record) and over every open incoming message with
// triage_run. No model decides: a rule matches by fields and its actions are
// fixed.
//
//	rules:
//	  - name: release notes          # optional
//	    match:                       # every key given must match; a list is any of
//	      from: [group-vendors]      # sender person or group id; a group matches its members
//	      tag: newsletter            # a tag on the message or on its sender
//	      class: mail                # the source's class (via.source)
//	      vendor: outlook            # the source's vendor
//	      medium: email
//	      subject: "release notes"   # in the subject (or first line), any case
//	    actions: [digest, tag release]
//
// Actions: needs_you, digest, link <item id>, tag <tag>, mute (drop it with
// reason "muted by rule N"). Rules are read in order: tag and link add up
// over every matching rule; the first matching rule with needs_you, digest,
// or mute decides, and later ones do not. Each action that changes a message
// files a receipt (op triage_run) naming the rule; an action whose receipt
// was undone is not applied to that message again, so an undo stands.

const triageFile = "triage.yaml"

// triageOp is the op name on the receipts of triage writes.
const triageOp = "triage_run"

// strList is a YAML string or list of strings.
type strList []string

func (l *strList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*l = strList{n.Value}
		return nil
	}
	var s []string
	if err := n.Decode(&s); err != nil {
		return err
	}
	*l = s
	return nil
}

// TriageMatch is what a rule matches. Empty keys match anything.
type TriageMatch struct {
	From    strList `yaml:"from,omitempty" json:"from,omitempty"`
	Tag     strList `yaml:"tag,omitempty" json:"tag,omitempty"`
	Class   strList `yaml:"class,omitempty" json:"class,omitempty"`
	Vendor  strList `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Medium  strList `yaml:"medium,omitempty" json:"medium,omitempty"`
	Subject string  `yaml:"subject,omitempty" json:"subject,omitempty"`
}

// TriageRule is one rule of triage.rules.
type TriageRule struct {
	Name    string      `yaml:"name,omitempty" json:"name,omitempty"`
	Match   TriageMatch `yaml:"match" json:"match"`
	Actions []string    `yaml:"actions" json:"actions"`
}

// triageConfig is .para/triage.yaml.
type triageConfig struct {
	Rules []TriageRule `yaml:"rules"`
}

// triageAction is one parsed action.
type triageAction struct {
	kind, arg string
}

func (a triageAction) String() string {
	if a.arg == "" {
		return a.kind
	}
	return a.kind + " " + a.arg
}

// verdict reports whether the action decides needs_you.
func (a triageAction) verdict() bool {
	return a.kind == "needs_you" || a.kind == "digest" || a.kind == "mute"
}

// rule is a checked rule: its number (1-based), name, match, and actions.
type rule struct {
	n       int
	name    string
	match   TriageMatch
	actions []triageAction
}

// label is "rule N" or "rule N (name)".
func (r rule) label() string {
	if r.name == "" {
		return fmt.Sprintf("rule %d", r.n)
	}
	return fmt.Sprintf("rule %d (%s)", r.n, r.name)
}

// loadTriageRules reads and checks triage.rules; no file is no rules.
func loadTriageRules(s store.Store) ([]rule, error) {
	b, err := s.ConfigFile(triageFile)
	if err != nil || b == nil {
		return nil, err
	}
	var cfg triageConfig
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf(".para/%s: %w", triageFile, err)
	}
	out := make([]rule, 0, len(cfg.Rules))
	for i, tr := range cfg.Rules {
		r, err := checkRule(i+1, tr)
		if err != nil {
			return nil, fmt.Errorf(".para/%s: rule %d: %w", triageFile, i+1, err)
		}
		out = append(out, r)
	}
	return out, nil
}

func checkRule(n int, tr TriageRule) (rule, error) {
	r := rule{n: n, name: strings.TrimSpace(tr.Name), match: tr.Match}
	if len(tr.Actions) == 0 {
		return r, errors.New("no actions: give needs_you, digest, link <id>, tag <tag>, or mute")
	}
	for _, raw := range tr.Actions {
		kind, arg, _ := strings.Cut(strings.TrimSpace(raw), " ")
		arg = strings.TrimSpace(arg)
		switch kind {
		case "needs_you", "digest", "mute":
			if arg != "" {
				return r, fmt.Errorf("action %q takes no argument", raw)
			}
		case "link":
			if arg = normID(arg); arg == "" {
				return r, fmt.Errorf("action %q: give the id of the item to link", raw)
			}
		case "tag":
			if arg = normTag(arg); arg == "" {
				return r, fmt.Errorf("action %q: give a tag", raw)
			}
		default:
			return r, fmt.Errorf("action %q: want needs_you, digest, link <id>, tag <tag>, or mute", raw)
		}
		r.actions = append(r.actions, triageAction{kind, arg})
	}
	norm := func(l strList, f func(string) string) strList {
		out := make(strList, 0, len(l))
		for _, s := range l {
			if s = f(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	r.match.From = norm(r.match.From, func(s string) string { return strings.ToLower(normID(s)) })
	r.match.Tag = norm(r.match.Tag, normTag)
	r.match.Vendor = norm(r.match.Vendor, normVendor)
	r.match.Class = norm(r.match.Class, func(s string) string {
		c := model.SourceSystem(strings.ToLower(strings.TrimSpace(s)))
		if k, ok := legacySystems[c]; ok {
			c = k.class
		}
		return string(c)
	})
	for _, c := range r.match.Class {
		if !slices.Contains(model.SourceClasses, model.SourceSystem(c)) {
			return r, fmt.Errorf("match class %q: want one of %v", c, model.SourceClasses)
		}
	}
	r.match.Medium = norm(r.match.Medium, func(s string) string { return strings.ToLower(strings.TrimSpace(s)) })
	for _, m := range r.match.Medium {
		if err := checkMedium(model.Medium(m)); err != nil {
			return r, fmt.Errorf("match %w", err)
		}
	}
	r.match.Subject = strings.ToLower(strings.TrimSpace(r.match.Subject))
	return r, nil
}

// triageFacts is what rules match on for one incoming message.
type triageFacts struct {
	sender  string   // person or group id, or ""
	parties []string // the sender and the groups it is in
	tags    []string // the message's tags and the sender's
	class   string
	vendor  string
	medium  string
	subject string // lower case
}

func (r rule) matches(f triageFacts) bool {
	m := r.match
	anyIn := func(want strList, have []string) bool {
		return len(want) == 0 || slices.ContainsFunc(want, func(w string) bool { return slices.Contains(have, w) })
	}
	return anyIn(m.From, f.parties) && anyIn(m.Tag, f.tags) && anyIn(m.Class, []string{f.class}) &&
		anyIn(m.Vendor, []string{f.vendor}) && anyIn(m.Medium, []string{f.medium}) &&
		(m.Subject == "" || strings.Contains(f.subject, m.Subject))
}

// triager applies rules: it holds the rules, the sources, and the receipts
// of triage writes, read once.
type triager struct {
	d        Deps
	rules    []rule
	sources  []model.Document
	undone   map[string][]string // doc -> the why of each undone triage receipt
	receipts bool
}

func newTriager(ctx context.Context, d Deps) (*triager, error) {
	rules, err := loadTriageRules(d.Store)
	if err != nil {
		return nil, err
	}
	t := &triager{d: d, rules: rules}
	if len(rules) == 0 {
		return t, nil
	}
	if t.sources, err = loadSources(ctx, d.Store); err != nil {
		return nil, err
	}
	return t, nil
}

// facts reads what rules match on.
func (t *triager) facts(ctx context.Context, doc model.Document, m *model.IO) triageFacts {
	f := triageFacts{tags: append([]string{}, m.Tags...), medium: string(m.Medium), subject: strings.ToLower(firstOf(m.Subject, doc.Title))}
	if strings.HasPrefix(m.From, "[[") {
		f.sender = model.Link(m.From).Target()
		f.parties = []string{f.sender}
		if sd, err := t.d.Store.Get(ctx, f.sender); err == nil {
			f.tags = append(f.tags, sd.Meta.Header().Tags...)
			if p, ok := sd.Meta.(*model.Person); ok {
				for _, g := range p.Groups {
					f.parties = append(f.parties, g.Target())
				}
			}
		}
	} else if from := strings.ToLower(normID(m.From)); from != "" {
		f.parties = []string{from} // an address or a name not in the directory
	}
	if src := ioSource(t.sources, m.Via); src != nil {
		k := sourceKindOf(src.Meta.(*model.SourceDoc))
		f.class, f.vendor = string(k.class), k.vendor
	}
	return f
}

// TriageAct is one action of a rule on one message.
type TriageAct struct {
	ID     string `json:"id"`
	Rule   int    `json:"rule"`
	Name   string `json:"name,omitempty"`
	Action string `json:"action"`
	// Receipt is the receipt of the write; empty in a dry run or when
	// skipped.
	Receipt string `json:"receipt,omitempty"`
	// Skipped says why the action was not applied: already so, or its
	// receipt was undone.
	Skipped string `json:"skipped,omitempty"`
}

// planned is one action to apply, with the rule that gives it.
type planned struct {
	r rule
	a triageAction
}

// plan lists the actions the rules give a message, in rule order.
func (t *triager) plan(f triageFacts) []planned {
	var out []planned
	decided := false
	for _, r := range t.rules {
		if !r.matches(f) {
			continue
		}
		for _, a := range r.actions {
			if a.verdict() {
				if decided {
					continue
				}
				decided = true
			}
			out = append(out, planned{r, a})
		}
	}
	return out
}

// key names an action of a rule on receipts.
func (p planned) key() string { return fmt.Sprintf("triage rule %d: %s", p.r.n, p.a) }

func (p planned) why() string {
	w := p.key()
	if p.r.name != "" {
		w += " (" + p.r.name + ")"
	}
	return w + ". Undo the receipt to put it back; the rule does not apply it to this message again."
}

// wasUndone reports whether a receipt of this action on doc was undone.
func (t *triager) wasUndone(ctx context.Context, doc string, p planned) (bool, error) {
	if !t.receipts {
		t.undone = map[string][]string{}
		all, err := listReceipts(ctx, t.d.Store)
		if err != nil {
			return false, err
		}
		for _, r := range all {
			if r.Op == triageOp && r.Undone != "" {
				t.undone[r.Doc] = append(t.undone[r.Doc], r.Why)
			}
		}
		t.receipts = true
	}
	k := p.key()
	return slices.ContainsFunc(t.undone[doc], func(why string) bool {
		return strings.HasPrefix(why, k+".") || strings.HasPrefix(why, k+" (")
	}), nil
}

// change applies one action to m; false when it changes nothing.
func (p planned) change(doc *model.Document, m *model.IO) bool {
	by := p.r.label()
	switch p.a.kind {
	case "needs_you", "digest":
		v := model.IOTriage(p.a.kind)
		if m.Triage == v && m.TriageBy == by {
			return false
		}
		m.Triage, m.TriageBy = v, by
	case "mute":
		if !m.State.Open() {
			return false
		}
		m.State, m.Reason = model.IODropped, "muted by "+by
	case "tag":
		if slices.Contains(m.Tags, p.a.arg) {
			return false
		}
		m.Tags = append(m.Tags, p.a.arg)
	case "link":
		l := model.LinkTo(p.a.arg)
		if slices.Contains(m.Links, l) {
			return false
		}
		m.Links = append(m.Links, l)
	}
	return true
}

// apply runs the rules on one incoming message. dry plans only.
func (t *triager) apply(ctx context.Context, id string, dry bool) ([]TriageAct, error) {
	if len(t.rules) == 0 {
		return nil, nil
	}
	doc, err := t.d.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	m, ok := doc.Meta.(*model.IO)
	if !ok || m.Direction != model.IOIn || !m.State.Open() {
		return nil, nil
	}
	var out []TriageAct
	for _, p := range t.plan(t.facts(ctx, doc, m)) {
		act := TriageAct{ID: doc.ID, Rule: p.r.n, Name: p.r.name, Action: p.a.String()}
		if p.a.kind == "link" {
			if _, err := t.d.Store.Get(ctx, p.a.arg); err != nil {
				act.Skipped = "no item " + p.a.arg
				out = append(out, act)
				continue
			}
		}
		undone, err := t.wasUndone(ctx, doc.ID, p)
		if err != nil {
			return out, err
		}
		cur, err := t.d.Store.Get(ctx, doc.ID)
		if err != nil {
			return out, err
		}
		cm := cur.Meta.(*model.IO)
		switch {
		case undone:
			act.Skipped = "its receipt was undone"
		case !p.change(&cur, cm):
			act.Skipped = "already so"
		case dry:
		default:
			if act.Receipt, err = t.write(ctx, cur, p); err != nil {
				return out, fmt.Errorf("%s %s: %w", doc.ID, p.key(), err)
			}
		}
		out = append(out, act)
	}
	return out, nil
}

// write saves one changed message and files its receipt, on every surface.
func (t *triager) write(ctx context.Context, doc model.Document, p planned) (string, error) {
	rec := newRecorder(t.d.Store)
	if _, err := rec.Update(ctx, doc); err != nil {
		return "", err
	}
	return rec.fileOneID(ctx, t.d, triageOp, WriteMeta{Why: p.why(), Confidence: model.ConfidenceHigh})
}

// triageNew applies the rules to a message just added, and returns what
// they did and any problem as a warning: a rule never fails the add.
func triageNew(ctx context.Context, d Deps, ids ...string) ([]TriageAct, []string) {
	t, err := newTriager(ctx, d)
	if err != nil {
		return nil, []string{"triage rules not applied: " + err.Error()}
	}
	var acts []TriageAct
	var warn []string
	for _, id := range ids {
		a, err := t.apply(ctx, id, false)
		acts = append(acts, a...)
		if err != nil {
			warn = append(warn, "triage of "+id+": "+err.Error())
		}
	}
	return acts, warn
}

// TriageRunInput is the input of triage_run.
type TriageRunInput struct {
	// IDs are the messages to triage; empty: every open incoming message.
	IDs []string `json:"ids,omitempty"`
	// DryRun lists what the rules would do and changes nothing.
	DryRun bool `json:"dry_run,omitempty"`
}

// TriageRunOutput is the output of triage_run.
type TriageRunOutput struct {
	Rules   int         `json:"rules"`   // rules in .para/triage.yaml
	Checked int         `json:"checked"` // open incoming messages read
	Acts    []TriageAct `json:"acts"`
	DryRun  bool        `json:"dry_run,omitempty"`
}

// TriageRun applies the triage rules over open incoming io.
var TriageRun = defineSafe("triage_run", "Apply the owner's triage rules (.para/triage.yaml, triage.rules) to open incoming io, or to ids: needs_you, digest, link <id>, tag <tag>, or mute (dropped with reason \"muted by rule N\"). Rules are read in order; tag and link add up, the first needs_you, digest, or mute decides. Each change files a receipt naming the rule; an action whose receipt was undone is not applied again. dry_run lists what would happen. No model decides", func(ctx context.Context, d Deps, in TriageRunInput) (TriageRunOutput, error) {
	t, err := newTriager(ctx, d)
	if err != nil {
		return TriageRunOutput{}, err
	}
	out := TriageRunOutput{Rules: len(t.rules), Acts: []TriageAct{}, DryRun: in.DryRun}
	ids := make([]string, 0, len(in.IDs))
	for _, id := range in.IDs {
		ids = append(ids, normID(id))
	}
	if len(ids) == 0 {
		refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeIO})
		if err != nil {
			return out, err
		}
		for _, r := range refs {
			ids = append(ids, r.ID)
		}
	}
	for _, id := range ids {
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			return out, err
		}
		m, ok := doc.Meta.(*model.IO)
		if !ok {
			return out, fmt.Errorf("%s is %s, not io", doc.ID, doc.Meta.Header().Type)
		}
		if m.Direction != model.IOIn || !m.State.Open() {
			continue
		}
		out.Checked++
		acts, err := t.apply(ctx, id, in.DryRun)
		out.Acts = append(out.Acts, acts...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
})
