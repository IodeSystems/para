package ops

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// grill_context gathers what an interviewer needs to grill a goal: the gaps
// and doubts the vault can show, in value order, each with what the vault
// already knows and the question to ask only when the vault and its sources
// cannot answer it. It writes nothing.

// Kinds of GrillProbe, in the order they rank.
const (
	ProbeBriefGap    = "brief_gap"
	ProbeQuestion    = "open_question"
	ProbeNoOwner     = "no_owner"
	ProbeRule        = "rule"
	ProbeOutside     = "outside_dependency"
	ProbeAssumption  = "assumption"
	ProbeCaseAgainst = "case_against"
)

// ProbeKinds lists every probe kind, in rank order.
var ProbeKinds = []string{ProbeBriefGap, ProbeQuestion, ProbeNoOwner, ProbeRule, ProbeOutside, ProbeAssumption, ProbeCaseAgainst}

// GrillProbe is one thing to settle about a goal.
type GrillProbe struct {
	Rank    int      `json:"rank"` // 1 is first
	Kind    string   `json:"kind"`
	About   string   `json:"about"` // the document it is about
	Title   string   `json:"title"`
	Known   string   `json:"known"`             // what the vault says now
	Sources []string `json:"sources,omitempty"` // where that comes from
	Ask     string   `json:"ask"`               // ask the owner this only if the vault and sources cannot answer it
}

// GrillContextInput is the input of grill_context.
type GrillContextInput struct {
	ID string `json:"id" query:"id" required:"true"` // the goal
}

// GrillContextOutput is the output of grill_context.
type GrillContextOutput struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	Owner    string           `json:"owner"`
	DoneWhen string           `json:"done_when,omitempty"`
	Probes   []GrillProbe     `json:"probes"`
	Rules    []RuleInEffect   `json:"rules"`
	Risks    []model.GoalRisk `json:"risks"`
	// RecheckDays is the vault's recheck.days; 0 is off.
	RecheckDays int `json:"recheck_days,omitempty"`
}

var policyLinkRe = regexp.MustCompile(`\[\[([a-z0-9][a-z0-9.-]*)(?:[#|][^\]]*)?\]\]`)

// GrillContext gathers the probes of a goal.
var GrillContext = defineRead("grill_context", "For a goal, list what an interviewer must settle, in value order: brief gaps (no done_when, risks, or signals), open questions, asks and blockers with no owner or route, rules and policies in effect, dependencies outside the owner's control, stale or unchecked assumptions, and the case against. Each probe says what the vault knows, its sources, and the question to ask only if the vault and sources cannot answer", func(ctx context.Context, d Deps, in GrillContextInput) (GrillContextOutput, error) {
	doc, p, err := getGoal(ctx, d.Store, normID(in.ID))
	if err != nil {
		return GrillContextOutput{}, err
	}
	today := d.Store.Now().Format(dateLayout)
	days, err := recheckDays(d.Store)
	if err != nil {
		return GrillContextOutput{}, err
	}
	g := &griller{goal: doc, p: p}
	tasks, err := goalDocs(ctx, d.Store, doc.ID)
	if err != nil {
		return GrillContextOutput{}, err
	}
	g.briefGaps()
	qs, err := openQuestions(ctx, d.Store, func(x qrow) bool { return x.project == doc.ID })
	if err != nil {
		return GrillContextOutput{}, err
	}
	for _, q := range qs {
		known := "open " + string(q.Kind)
		if q.Context != "" {
			known += ": " + q.Context
		}
		if q.Blocking > 0 {
			known += fmt.Sprintf("; blocks %d items", q.Blocking)
		}
		var src []string
		for _, s := range q.ProposedSources {
			src = append(src, s.Ref)
		}
		g.add(ProbeQuestion, q.ID, q.Title, known, src, q.Title)
	}
	g.noOwner(tasks)
	scopes, err := scopeChain(ctx, d.Store, doc.ID)
	if err != nil {
		return GrillContextOutput{}, err
	}
	rules, err := rulesFor(ctx, d.Store, scopes)
	if err != nil {
		return GrillContextOutput{}, err
	}
	if err := g.rules(ctx, d.Store, rules, tasks); err != nil {
		return GrillContextOutput{}, err
	}
	g.outside(ctx, d.Store, tasks)
	cutoff := daysBefore(today, days)
	for _, x := range append([]model.Document{doc}, tasks...) {
		for _, a := range assumptionsOf(x, cutoff) {
			switch {
			case a.Checked == "":
				g.add(ProbeAssumption, x.ID, x.Title, fmt.Sprintf("assumption never checked: %q", a.Text), nonEmpty(a.Source), fmt.Sprintf("Does this still hold, and how do you know: %s?", a.Text))
			case a.Stale:
				g.add(ProbeAssumption, x.ID, x.Title, fmt.Sprintf("assumption %q last checked %s (%s)", a.Text, a.Checked, a.Status), nonEmpty(a.Source), fmt.Sprintf("Does this still hold: %s?", a.Text))
			}
		}
	}
	g.caseAgainst()
	slices.SortStableFunc(g.out, func(a, b GrillProbe) int {
		return slices.Index(ProbeKinds, a.Kind) - slices.Index(ProbeKinds, b.Kind)
	})
	for i := range g.out {
		g.out[i].Rank = i + 1
	}
	if rules == nil {
		rules = []RuleInEffect{}
	}
	risks := p.Risks
	if risks == nil {
		risks = []model.GoalRisk{}
	}
	return GrillContextOutput{ID: doc.ID, Title: doc.Title, Owner: p.Owner, DoneWhen: p.DoneWhen, Probes: g.out, Rules: rules, Risks: risks, RecheckDays: days}, nil
})

type griller struct {
	goal model.Document
	p    *model.Goal
	out  []GrillProbe
}

func (g *griller) add(kind, about, title, known string, sources []string, ask string) {
	if sources == nil {
		sources = []string{}
	}
	g.out = append(g.out, GrillProbe{Kind: kind, About: about, Title: title, Known: known, Sources: sources, Ask: ask})
}

func nonEmpty(s ...string) []string {
	out := []string{}
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}

// goalDocs returns the open tasks of a goal and its open obligations (read
// as tasks), sorted by id.
func goalDocs(ctx context.Context, s store.Store, goal string) ([]model.Document, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeTask})
	if err != nil {
		return nil, err
	}
	obls, err := s.List(ctx, store.Filter{Type: model.TypeObligation})
	if err != nil {
		return nil, err
	}
	var out []model.Document
	for _, r := range append(refs, obls...) {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		doc = obligationShape(doc)
		if t, ok := doc.Meta.(*model.Task); ok && t.Goal.Target() == goal && !taskTerminal(t.Status) {
			out = append(out, doc)
		}
	}
	slices.SortFunc(out, func(a, b model.Document) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (g *griller) briefGaps() {
	id, t, p := g.goal.ID, g.goal.Title, g.p
	if strings.TrimSpace(p.DoneWhen) == "" {
		g.add(ProbeBriefGap, id, t, "no done_when", nil, "How will you know this goal is done?")
	}
	if len(p.Risks) == 0 {
		g.add(ProbeBriefGap, id, t, "no risks recorded", nil, "What could stop this goal or make it not worth it? For each: how likely, how bad, who owns it, and what reduces it?")
	}
	if len(p.Signals) == 0 {
		g.add(ProbeBriefGap, id, t, "no signals: nothing says how to tell it is working", nil, "What would you read, how often, and what value do you expect, to know it is working?")
	}
	for _, u := range p.Unknowns {
		if u.Link == "" {
			g.add(ProbeBriefGap, id, t, fmt.Sprintf("unknown %q has no question or task", u.Text), nil, fmt.Sprintf("Who can answer %q, and by when?", u.Text))
		}
	}
}

// noOwner lists risks with no owner, high-impact risks with no mitigation,
// asks with no one to ask or no route, and blocked tasks with no reason.
func (g *griller) noOwner(tasks []model.Document) {
	for _, r := range g.p.Risks {
		if strings.TrimSpace(r.Owner) == "" {
			g.add(ProbeNoOwner, g.goal.ID, g.goal.Title, fmt.Sprintf("risk %q has no owner", r.Text), nonEmpty(r.Source), fmt.Sprintf("Who handles the risk %q?", r.Text))
		}
		if r.Impact == model.LevelHigh && strings.TrimSpace(r.Mitigation) == "" {
			g.add(ProbeNoOwner, g.goal.ID, g.goal.Title, fmt.Sprintf("risk %q has high impact and no mitigation", r.Text), nonEmpty(r.Source), fmt.Sprintf("What reduces the risk %q, or is it accepted?", r.Text))
		}
	}
	for _, x := range tasks {
		t := x.Meta.(*model.Task)
		if t.Status == model.TaskBlocked && len(t.BlockedBy) == 0 {
			g.add(ProbeNoOwner, x.ID, x.Title, "blocked with no reason", nil, fmt.Sprintf("What blocks %q, who said so, and who can clear it?", x.Title))
		}
	}
}

// asks returns the open outbound asks of the goal: those with project set to
// it, and those its open tasks wait on.
func (g *griller) asks(ctx context.Context, s store.Store, tasks []model.Document) []model.Document {
	want := map[string]bool{}
	for _, x := range tasks {
		for _, dep := range targets(x.Meta.(*model.Task).DependsOn) {
			want[dep] = true
		}
	}
	refs, err := s.List(ctx, store.Filter{Type: model.TypeRFX})
	if err != nil {
		return nil
	}
	var out []model.Document
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			continue
		}
		m, ok := doc.Meta.(*model.RFX)
		if !ok || m.Status != model.RFXOpen || m.Direction != model.RFXOut {
			continue
		}
		if m.Goal.Target() == g.goal.ID || want[doc.ID] {
			out = append(out, doc)
		}
	}
	slices.SortFunc(out, func(a, b model.Document) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// rules lists each constraint in effect, and each policy or exception that the
// goal or its open tasks link in their bodies.
func (g *griller) rules(ctx context.Context, s store.Store, rules []RuleInEffect, tasks []model.Document) error {
	rx, err := newRuleIndex(ctx, s)
	if err != nil {
		return err
	}
	for _, r := range rules {
		if r.Type != model.TypeConstraint {
			continue
		}
		doc, err := rx.get(r.ID)
		if err != nil {
			return err
		}
		c := doc.Meta.(*model.Constraint)
		known := fmt.Sprintf("constraint in effect via %s: %s (by %s, review by %s)", r.Via, c.Why, c.By, c.ReviewBy)
		g.add(ProbeRule, doc.ID, doc.Title, known, nonEmpty(c.Source.URL), fmt.Sprintf("Does %q still bind this goal, and is an exception needed?", doc.Title))
	}
	seen := map[string]bool{}
	for _, x := range append([]model.Document{g.goal}, tasks...) {
		for _, m := range policyLinkRe.FindAllStringSubmatch(x.Body, -1) {
			id := m[1]
			if seen[id] {
				continue
			}
			seen[id] = true
			doc, err := rx.get(id)
			if err != nil {
				continue
			}
			switch pm := doc.Meta.(type) {
			case *model.Policy:
				known := fmt.Sprintf("policy linked from %s; verified %t", x.ID, pm.Verified)
				g.add(ProbeRule, doc.ID, doc.Title, known, nonEmpty(pm.Source.URL), fmt.Sprintf("Which rules of %q bind this goal?", doc.Title))
			case *model.PolicyException:
				known := fmt.Sprintf("policy exception linked from %s: %s, %s, by %s", x.ID, pm.Kind, pm.Status, pm.By)
				if pm.ReviewBy != "" {
					known += ", review by " + string(pm.ReviewBy)
				}
				g.add(ProbeRule, doc.ID, doc.Title, known, nil, fmt.Sprintf("Does the exception %q still hold for this goal?", doc.Title))
			}
		}
	}
	return nil
}

// outside lists what the goal waits on that the owner does not control: asks
// to other people, tasks assigned to others, and blocked tasks.
func (g *griller) outside(ctx context.Context, s store.Store, tasks []model.Document) {
	for _, a := range g.asks(ctx, s, tasks) {
		m := a.Meta.(*model.RFX)
		if len(m.Targets) == 0 || strings.TrimSpace(m.Route) == "" {
			g.add(ProbeNoOwner, a.ID, a.Title, "open ask with no one to ask or no route", nil, fmt.Sprintf("Who should be asked %q, and through what route?", a.Title))
			continue
		}
		var to []string
		for _, t := range m.Targets {
			to = append(to, t.To.Target()+" by "+string(t.Medium)+" ("+string(t.State)+")")
		}
		known := "waiting on " + strings.Join(to, "; ")
		if m.NextNudge != "" {
			known += "; next follow-up " + string(m.NextNudge)
		}
		g.add(ProbeOutside, a.ID, a.Title, known, nil, fmt.Sprintf("What is the fallback if %q gets no answer, and by when?", a.Title))
	}
	for _, x := range tasks {
		t := x.Meta.(*model.Task)
		if IsOther(t.Assignee) {
			known := "assigned to " + t.Assignee
			if t.Due != "" {
				known += ", due " + string(t.Due)
			}
			g.add(ProbeOutside, x.ID, x.Title, known, nil, fmt.Sprintf("What happens to the goal if %s does not deliver %q?", t.Assignee, x.Title))
		}
		if t.Status == model.TaskBlocked && len(t.BlockedBy) > 0 {
			g.add(ProbeOutside, x.ID, x.Title, "blocked: "+strings.Join(t.BlockedBy, "; "), nil, fmt.Sprintf("Who can unblock %q, and by when?", x.Title))
		}
	}
}

// caseAgainst lists what the brief says against the goal, and asks for it when
// the brief has no do-nothing option or no pivot.
func (g *griller) caseAgainst() {
	id, t, p := g.goal.ID, g.goal.Title, g.p
	i := slices.IndexFunc(p.Alternatives, func(o model.OptionDetail) bool { return strings.EqualFold(strings.TrimSpace(o.Name), model.DoNothing) })
	if i < 0 {
		g.add(ProbeCaseAgainst, id, t, "the brief has no do-nothing option", nil, "What happens if you do nothing? What is the cheapest alternative?")
	} else {
		o := p.Alternatives[i]
		var known, src []string
		for _, c := range append(append([]model.Claim{}, o.Pros...), o.Cons...) {
			known = append(known, c.Text)
			src = append(src, c.Source)
		}
		k := "do nothing: " + strings.Join(known, "; ")
		if len(known) == 0 {
			k = "do nothing is listed with no pros or cons"
		}
		g.add(ProbeCaseAgainst, id, t, k, nonEmpty(src...), "What is the strongest reason to stop this goal now?")
	}
	if p.Pivot == nil {
		g.add(ProbeCaseAgainst, id, t, "no pivot: nothing says when to stop or change", nil, "Under what condition, and by what date, would you stop or change this goal?")
	}
}
