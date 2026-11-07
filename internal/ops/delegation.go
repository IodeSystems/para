package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Delegation: whose words an outgoing draft goes out as. A draft is as_me
// (the default): the owner approves it (io_approve) and it goes out in the
// owner's name, with no stamp. Or it is for_me: an agent sends it without
// the owner's review, and its text ends with a stamp that says so. An agent
// may send a for_me draft without approval only when a rule of the owner's
// .para/delegation.yaml matches it at send time, the text ends with the
// stamp, and io_send_check passed within the window. With no rule that
// matches, a for_me draft waits for the owner like any draft. When the owner
// approves a for_me draft it becomes as_me: the stamp comes off, and the
// approval binds the text the owner approved.
//
// The file is the owner's, like triage.yaml: no op writes it, and an agent
// may only propose a rule to the owner.
//
//	stamp: "— sent by {agent} for {owner}"   # optional: the vault default
//	agents:                                  # optional: per agent
//	  worker-a: {stamp: "— {agent}, an assistant of {owner}"}
//	rules:                                   # every key given must match; a list is any of
//	  - name: scheduling                     # optional
//	    medium: [slack, email]
//	    class: chat                          # the source's class (via.source)
//	    vendor: examplechat
//	    to: [group-team, "*@example.test"]   # every recipient: a person or group id
//	                                         # (a group matches its members), or an address pattern
//	    kind: [scheduling, acknowledgement]  # the draft's kind, a free label the agent sets
//	    max_chars: 400                       # the text without the stamp
//	    agent: [worker-a]                    # the agents that may use the rule
//	    stamp: "— {agent} for {owner} ({rule})"  # optional: the stamp for this rule
//
// The stamp of a draft is, first found: the matching rule's stamp, the
// agent's stamp, the file's stamp, the vault setting io.stamp, the built-in
// default. Placeholders: {agent} (the agent's name), {owner} (the vault
// setting owner, a person id read as their name; else "the owner"), {date}
// (the day the draft was made), {rule} (the rule's label; empty with none).

const delegationFile = "delegation.yaml"

// StampKey is the vault setting of the default stamp of a for_me draft.
const StampKey = "io.stamp"

// DefaultStamp is the stamp when neither delegation.yaml nor io.stamp sets one.
const DefaultStamp = "— sent by {agent} for {owner}; not reviewed by {owner}"

var stampPlaceholder = regexp.MustCompile(`\{[^{}]*\}`)

// stampFields are the placeholders a stamp may hold.
var stampFields = []string{"{agent}", "{owner}", "{date}", "{rule}"}

// checkStamp checks a stamp template: one line, known placeholders only.
func checkStamp(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "\r\n") {
		return "", errors.New("a stamp is one line")
	}
	for _, p := range stampPlaceholder.FindAllString(s, -1) {
		if !slices.Contains(stampFields, p) {
			return "", fmt.Errorf("stamp placeholder %s: want %s", p, strings.Join(stampFields, ", "))
		}
	}
	return s, nil
}

// DelegationRule is one rule of delegation.yaml.
type DelegationRule struct {
	Name     string  `yaml:"name,omitempty" json:"name,omitempty"`
	Medium   strList `yaml:"medium,omitempty" json:"medium,omitempty"`
	Class    strList `yaml:"class,omitempty" json:"class,omitempty"`
	Vendor   strList `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	To       strList `yaml:"to,omitempty" json:"to,omitempty"`
	Kind     strList `yaml:"kind,omitempty" json:"kind,omitempty"`
	MaxChars int     `yaml:"max_chars,omitempty" json:"max_chars,omitempty"`
	Agent    strList `yaml:"agent,omitempty" json:"agent,omitempty"`
	Stamp    string  `yaml:"stamp,omitempty" json:"stamp,omitempty"`
}

type delegationAgent struct {
	Stamp string `yaml:"stamp,omitempty"`
}

// delegationConfig is .para/delegation.yaml.
type delegationConfig struct {
	Stamp  string                     `yaml:"stamp,omitempty"`
	Agents map[string]delegationAgent `yaml:"agents,omitempty"`
	Rules  []DelegationRule           `yaml:"rules"`
}

// drule is a checked delegation rule and its number (1-based).
type drule struct {
	n int
	DelegationRule
}

func (r drule) label() string {
	if r.Name == "" {
		return fmt.Sprintf("rule %d", r.n)
	}
	return fmt.Sprintf("rule %d (%s)", r.n, r.Name)
}

// delegation is the checked file: whether it exists, the stamps, the rules.
type delegation struct {
	file   bool
	stamp  string            // the file's default stamp
	agents map[string]string // agent name to its stamp
	rules  []drule
}

// normAgent is an agent name as rules compare it: no agent: prefix, lower case.
func normAgent(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), AgentPrefix))
}

// loadDelegation reads and checks .para/delegation.yaml; no file is no rules.
func loadDelegation(s store.Store) (*delegation, error) {
	out := &delegation{agents: map[string]string{}}
	b, err := s.ConfigFile(delegationFile)
	if err != nil || b == nil {
		return out, err
	}
	out.file = true
	var cfg delegationConfig
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf(".para/%s: %w", delegationFile, err)
	}
	if out.stamp, err = checkStamp(cfg.Stamp); err != nil {
		return nil, fmt.Errorf(".para/%s: %w", delegationFile, err)
	}
	for name, a := range cfg.Agents {
		st, err := checkStamp(a.Stamp)
		if err != nil {
			return nil, fmt.Errorf(".para/%s: agent %s: %w", delegationFile, name, err)
		}
		if n := normAgent(name); n != "" && st != "" {
			out.agents[n] = st
		}
	}
	for i, r := range cfg.Rules {
		cr, err := checkDelegationRule(i+1, r)
		if err != nil {
			return nil, fmt.Errorf(".para/%s: rule %d: %w", delegationFile, i+1, err)
		}
		out.rules = append(out.rules, cr)
	}
	return out, nil
}

func checkDelegationRule(n int, r DelegationRule) (drule, error) {
	out := drule{n: n, DelegationRule: r}
	out.Name = strings.TrimSpace(r.Name)
	var err error
	if out.Class, out.Vendor, out.Medium, err = checkChannel(r.Class, r.Vendor, r.Medium); err != nil {
		return out, err
	}
	out.To = normList(r.To, normParty)
	out.Kind = normList(r.Kind, normTag)
	out.Agent = normList(r.Agent, normAgent)
	if r.MaxChars < 0 {
		return out, errors.New("max_chars: want 0 (no limit) or more")
	}
	if out.Stamp, err = checkStamp(r.Stamp); err != nil {
		return out, err
	}
	return out, nil
}

// draftFacts is what delegation rules match on for one outgoing draft.
type draftFacts struct {
	medium, class, vendor string
	to                    [][]string // per recipient: the parties a rule may name
	kind                  string
	chars                 int // the text without the stamp
	agent                 string
}

func (dl *delegation) facts(ctx context.Context, s store.Store, sources []model.Document, doc model.Document, m *model.IO) draftFacts {
	f := draftFacts{medium: string(m.Medium), kind: m.Kind, agent: normAgent(m.Agent),
		chars: utf8.RuneCountInString(unstamped(doc.Body, m.Stamp))}
	f.class, f.vendor = channelFacts(sources, m.Via)
	for _, p := range m.To {
		_, parties, _ := partyFacts(ctx, s, p)
		f.to = append(f.to, parties)
	}
	return f
}

// toMatches reports whether every recipient is named by the rule: an id in
// its parties, or an address that matches a pattern.
func toMatches(want strList, to [][]string) bool {
	if len(want) == 0 {
		return true
	}
	if len(to) == 0 {
		return false
	}
	for _, parties := range to {
		ok := slices.ContainsFunc(want, func(w string) bool {
			return slices.ContainsFunc(parties, func(p string) bool {
				if w == p {
					return true
				}
				hit, _ := path.Match(w, p)
				return hit
			})
		})
		if !ok {
			return false
		}
	}
	return true
}

// miss says why the rule does not match the draft; "" when it matches.
func (r drule) miss(f draftFacts) string {
	list := func(l strList) string { return "[" + strings.Join(l, ", ") + "]" }
	switch {
	case !anyIn(r.Agent, []string{f.agent}):
		return fmt.Sprintf("agent %q not in %s", f.agent, list(r.Agent))
	case !anyIn(r.Medium, []string{f.medium}):
		return fmt.Sprintf("medium %s not in %s", f.medium, list(r.Medium))
	case !anyIn(r.Class, []string{f.class}):
		return fmt.Sprintf("source class %q not in %s (give via.source)", f.class, list(r.Class))
	case !anyIn(r.Vendor, []string{f.vendor}):
		return fmt.Sprintf("source vendor %q not in %s (give via.source)", f.vendor, list(r.Vendor))
	case !toMatches(r.To, f.to):
		return fmt.Sprintf("a recipient is not in to %s", list(r.To))
	case !anyIn(r.Kind, []string{f.kind}):
		return fmt.Sprintf("kind %q not in %s", f.kind, list(r.Kind))
	case r.MaxChars > 0 && f.chars > r.MaxChars:
		return fmt.Sprintf("%d characters, more than max_chars %d", f.chars, r.MaxChars)
	}
	return ""
}

// DelegationMiss is why one rule does not match a draft.
type DelegationMiss struct {
	Rule int    `json:"rule"`
	Name string `json:"name,omitempty"`
	Why  string `json:"why"`
}

// verdict is what the delegation file says of one for_me draft.
type verdict struct {
	file   bool
	rules  int
	rule   string // the label of the first matching rule; "" none
	stamp  string // the filled stamp its text must end with
	misses []DelegationMiss
}

// judge reads the delegation file and the draft: the first matching rule and
// the stamp. The draft's Agent and Stamp are read; nothing is changed.
func judge(ctx context.Context, d Deps, doc model.Document, m *model.IO) (verdict, error) {
	dl, err := loadDelegation(d.Store)
	if err != nil {
		return verdict{}, err
	}
	v := verdict{file: dl.file, rules: len(dl.rules), misses: []DelegationMiss{}}
	var hit *drule
	if len(dl.rules) > 0 {
		sources, err := loadSources(ctx, d.Store)
		if err != nil {
			return v, err
		}
		f := dl.facts(ctx, d.Store, sources, doc, m)
		for i := range dl.rules {
			r := &dl.rules[i]
			if why := r.miss(f); why != "" {
				v.misses = append(v.misses, DelegationMiss{Rule: r.n, Name: r.Name, Why: why})
				continue
			}
			hit = r
			break
		}
	}
	tpl := ""
	if hit != nil {
		v.rule, tpl = hit.label(), hit.Stamp
	}
	if tpl == "" {
		tpl = dl.agents[normAgent(m.Agent)]
	}
	if tpl == "" {
		tpl = dl.stamp
	}
	if tpl == "" {
		if tpl, err = d.Store.Config(StampKey); err != nil {
			return v, err
		}
	}
	if tpl = strings.TrimSpace(tpl); tpl == "" {
		tpl = DefaultStamp
	}
	agent := firstOf(normAgent(m.Agent), "an agent")
	v.stamp = strings.NewReplacer("{agent}", agent, "{owner}", ownerName(ctx, d), "{date}", string(m.Created), "{rule}", v.rule).Replace(tpl)
	return v, nil
}

// ownerName is the vault setting owner as a stamp says it: a person id is
// read as the person's name; unset or self is "the owner".
func ownerName(ctx context.Context, d Deps) string {
	o, _ := d.Store.Config("owner")
	o = strings.TrimSpace(o)
	if o == "" || strings.EqualFold(o, "self") {
		return "the owner"
	}
	if doc, err := d.Store.Get(ctx, normID(o)); err == nil {
		if _, ok := doc.Meta.(*model.Person); ok {
			return doc.Title
		}
	}
	return o
}

// unstamped is the body without the stamp at its end.
func unstamped(body, stamp string) string {
	if stamp == "" {
		return body
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(body, " \n"), stamp))
}

// stamped reports whether the body ends with the stamp.
func stamped(body, stamp string) bool {
	return stamp != "" && strings.HasSuffix(strings.TrimRight(body, " \n"), stamp)
}

// stampDraft puts the draft's stamp on it again: the old stamp comes off,
// the one the delegation file gives now goes at the end, and AllowedBy names
// the matching rule.
func stampDraft(ctx context.Context, d Deps, doc *model.Document, m *model.IO) (verdict, error) {
	body := unstamped(doc.Body, m.Stamp)
	m.Stamp = ""
	v, err := judge(ctx, d, model.Document{Body: body}, m)
	if err != nil {
		return v, err
	}
	doc.Body, m.Stamp, m.AllowedBy = strings.TrimSpace(body)+"\n\n"+v.stamp, v.stamp, v.rule
	return v, nil
}

// asMe makes a for_me draft as_me: the stamp comes off.
func asMe(doc *model.Document, m *model.IO) {
	if !m.ForMe() {
		return
	}
	doc.Body = unstamped(doc.Body, m.Stamp)
	m.Voice, m.Agent, m.Stamp, m.AllowedBy, m.DraftedSeen = "", "", "", "", ""
}

// forMe makes a draft for_me, for agent: stamped, with the newest incoming
// message of its conversation as the baseline of io_send_check. It returns
// notes for the caller.
func forMe(ctx context.Context, d Deps, doc *model.Document, m *model.IO, agent string) ([]string, error) {
	if m.Direction != model.IOOut {
		return nil, errors.New("voice for_me is for an outgoing draft")
	}
	if !m.ForMe() || normAgent(agent) != normAgent(m.Agent) {
		m.Voice, m.Agent = model.VoiceForMe, normAgent(agent)
		conv, err := conversation(ctx, d, *doc, m)
		if err != nil {
			return nil, err
		}
		m.DraftedSeen = newestAt(conv)
	}
	v, err := stampDraft(ctx, d, doc, m)
	if err != nil {
		return nil, err
	}
	return v.notes(), nil
}

// notes are the words for an agent about a for_me draft.
func (v verdict) notes() []string {
	switch {
	case !v.file:
		return []string{"voice for_me: the owner has no delegation rules (.para/" + delegationFile + "), so this draft waits for the owner's approval like any draft; when the owner approves it, it goes out as the owner's, without the stamp"}
	case v.rule == "":
		return []string{fmt.Sprintf("voice for_me: no delegation rule allows it (%s), so it waits for the owner's approval; delegation_run says why", plural(v.rules, "rule"))}
	}
	return []string{"voice for_me: " + v.rule + " lets you send it, stamped, without the owner's review: read the channel again, io_send_check, send the exact text with the stamp, then io_send. Never present it as the owner's own words"}
}

// draftAgent is the agent a for_me draft is for: on an agent surface the
// caller; from a person, the agent named.
func draftAgent(d Deps, named string) (string, error) {
	named = normAgent(named)
	if agentSurface(d) {
		self := normAgent(d.Store.Info().Actor)
		if named != "" && named != self {
			return "", fmt.Errorf("agent %q: you are %q; a for_me draft is sent by the agent that drafts it", named, self)
		}
		return self, nil
	}
	if named == "" {
		return "", errors.New("voice for_me needs agent: the name of the agent that sends it for you")
	}
	if !agentNameRe.MatchString(named) {
		return "", fmt.Errorf("agent %q: want a name of letters, digits, and - _ .", named)
	}
	return named, nil
}

// checkVoice checks a voice input; "" stays as it is.
func checkVoice(v model.IOVoice) error {
	if v != "" && !slices.Contains(model.IOVoices, v) {
		return fmt.Errorf("voice %q: want as_me (the owner approves it and it goes out as theirs) or for_me (an agent sends it, stamped, under a delegation rule)", v)
	}
	return nil
}

// delegationGate decides an agent's io_send of a for_me draft that the owner
// did not approve: a rule must match now, the text must end with the stamp,
// and a passing io_send_check must be within the window. It returns the
// label of the rule.
func delegationGate(ctx context.Context, d Deps, doc model.Document, m *model.IO) (string, error) {
	if m.Held != "" {
		return "", fmt.Errorf("%s is held: %s; the owner approves it first", doc.ID, m.Held)
	}
	if self := normAgent(d.Store.Info().Actor); self != m.Agent {
		return "", fmt.Errorf("%s is for agent %s, not %s: its stamp names %s", doc.ID, m.Agent, self, m.Agent)
	}
	v, err := judge(ctx, d, doc, m)
	if err != nil {
		return "", err
	}
	if v.rule == "" {
		return "", fmt.Errorf("%s is for_me, but no delegation rule allows it (%s): it waits for the owner's approval (io_approve); delegation_run says why", doc.ID, plural(v.rules, "rule"))
	}
	if !stamped(doc.Body, v.stamp) || m.Stamp != v.stamp {
		return "", fmt.Errorf("%s does not end with its stamp %q: io_edit with voice for_me to stamp it again, then io_send_check; never send it as the owner's own words", doc.ID, v.stamp)
	}
	if err := checkFresh(d, doc, m.CheckedAt, ""); err != nil {
		return "", err
	}
	return v.rule, nil
}

// DelegationRunInput is the input of delegation_run.
type DelegationRunInput struct {
	ID string `json:"id" query:"id" required:"true"`
	// Agent: for a draft that names no agent, the agent to judge it for;
	// default the caller on an agent surface.
	Agent string `json:"agent,omitempty" query:"agent"`
}

// DelegationRunOutput is the output of delegation_run.
type DelegationRunOutput struct {
	ID    string        `json:"id"`
	Voice model.IOVoice `json:"voice"`
	Agent string        `json:"agent,omitempty"`
	File  bool          `json:"file"`  // .para/delegation.yaml exists
	Rules int           `json:"rules"` // rules in it
	// Rule is the rule that would let the agent send it, stamped, without the
	// owner's review; empty: none.
	Rule string `json:"rule,omitempty"`
	// Stamp is the stamp the text must end with; Stamped: it does.
	Stamp   string           `json:"stamp"`
	Stamped bool             `json:"stamped"`
	Misses  []DelegationMiss `json:"misses"`
	Next    string           `json:"next"`
}

// DelegationRun is the dry run of the delegation rules on one draft.
var DelegationRun = defineRead("delegation_run", "Dry run of the owner's delegation rules (.para/delegation.yaml) on one outgoing draft: which rule would let an agent send it for the owner, stamped, without review (voice for_me), or why each rule does not match (misses), and the stamp its text must end with. An as_me draft is judged as if it were for_me. Changes nothing. The file is the owner's: propose a rule to the owner, never write it", func(ctx context.Context, d Deps, in DelegationRunInput) (DelegationRunOutput, error) {
	doc, m, err := getIO(ctx, d, in.ID)
	if err != nil {
		return DelegationRunOutput{}, err
	}
	if m.Direction != model.IOOut {
		return DelegationRunOutput{}, fmt.Errorf("%s is incoming: delegation rules are for outgoing drafts", doc.ID)
	}
	voice := firstOf(string(m.Voice), string(model.VoiceAsMe))
	if !m.ForMe() {
		c := *m
		m = &c
		m.Agent = normAgent(in.Agent)
		if m.Agent == "" && agentSurface(d) {
			m.Agent = normAgent(d.Store.Info().Actor)
		}
	}
	v, err := judge(ctx, d, doc, m)
	if err != nil {
		return DelegationRunOutput{}, err
	}
	out := DelegationRunOutput{ID: doc.ID, Voice: model.IOVoice(voice), Agent: m.Agent, File: v.file, Rules: v.rules, Rule: v.rule,
		Stamp: v.stamp, Stamped: stamped(doc.Body, v.stamp) && m.Stamp == v.stamp, Misses: v.misses}
	switch {
	case !v.file:
		out.Next = "no delegation rules: every draft waits for the owner's approval; the owner may write .para/" + delegationFile
	case v.rule == "":
		out.Next = "no rule matches: the draft waits for the owner's approval"
	case !m.ForMe() || !out.Stamped:
		out.Next = v.rule + " would allow it as for_me: io_edit with voice for_me stamps it"
	default:
		out.Next = v.rule + " allows it: io_send_check, send the exact text with the stamp, then io_send"
	}
	return out, nil
})
