package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Risks and assumptions. goal_risk_set and assumption_set write them, one item
// at a time, matched by text. An agent writes freely: the receipt holds the why
// and the sources, and the owner's words in a chat count as a source.

// recheckDaysKey is the vault setting that makes an assumption stale.
const recheckDaysKey = "recheck.days"

// recheckDays returns recheck.days, or 0 when off.
func recheckDays(s store.Store) (int, error) {
	conf, err := s.Config(recheckDaysKey)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(conf))
	if err != nil || n <= 0 {
		return 0, nil
	}
	return n, nil
}

// recheckCutoff returns the date before which an assumption's check is stale,
// or "" when recheck.days is off.
func recheckCutoff(s store.Store) (string, error) {
	days, err := recheckDays(s)
	if err != nil {
		return "", err
	}
	return daysBefore(s.Now().Format(dateLayout), days), nil
}

// daysBefore returns the date n days before today (YYYY-MM-DD), or "" when n is
// 0 or today does not parse.
func daysBefore(today string, n int) string {
	t, err := time.Parse(dateLayout, today)
	if n <= 0 || err != nil {
		return ""
	}
	return t.AddDate(0, 0, -n).Format(dateLayout)
}

// fold puts text on one line with single spaces.
func fold(s string) string { return strings.Join(strings.Fields(s), " ") }

// AssumptionView is one assumption of a goal or a task. FromBody is a line of
// the ## Assumptions section: it reads as never checked until recheck_record
// writes it to the frontmatter. Stale is true when it was checked before the
// vault's recheck.days.
type AssumptionView struct {
	Text     string                 `json:"text"`
	Source   string                 `json:"source,omitempty"`
	Checked  model.Date             `json:"checked,omitempty"`
	By       string                 `json:"by,omitempty"`
	Status   model.AssumptionStatus `json:"status,omitempty"`
	Fact     string                 `json:"fact,omitempty"`
	FromBody bool                   `json:"from_body,omitempty"`
	Stale    bool                   `json:"stale,omitempty"`
}

// metaAssumptions returns the assumptions list of a goal or a task, or nil and
// false for another type.
func metaAssumptions(m model.Meta) (*[]model.Assumption, bool) {
	switch x := m.(type) {
	case *model.Goal:
		return &x.Assumptions, true
	case *model.Task:
		return &x.Assumptions, true
	}
	return nil, false
}

// bodyAssumptions returns the list lines of the ## Assumptions section of a
// body, without the "- ".
func bodyAssumptions(body string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(body, "\n") {
		switch {
		case strings.TrimRight(l, " \t") == assumptionsHeading:
			in = true
		case strings.HasPrefix(l, "# ") || strings.HasPrefix(l, "## "):
			in = false
		case in && strings.HasPrefix(l, "- "):
			if t := fold(l[2:]); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// assumptionsOf returns the assumptions of a goal or a task: the frontmatter
// list, then the body lines that the list does not hold. cutoff is the date
// before which a check is stale; empty is off.
func assumptionsOf(doc model.Document, cutoff string) []AssumptionView {
	list, ok := metaAssumptions(doc.Meta)
	if !ok {
		return nil
	}
	out := []AssumptionView{}
	for _, a := range *list {
		out = append(out, AssumptionView{Text: a.Text, Source: a.Source, Checked: a.Checked, By: a.By, Status: a.Status, Fact: a.Fact,
			Stale: cutoff != "" && a.Checked != "" && string(a.Checked) < cutoff})
	}
	for _, t := range bodyAssumptions(doc.Body) {
		if !slices.ContainsFunc(*list, func(a model.Assumption) bool { return model.SameText(a.Text, t) }) {
			out = append(out, AssumptionView{Text: t, FromBody: true})
		}
	}
	return out
}

// GoalRiskSetInput is the input of goal_risk_set. Text names the risk; an
// existing risk with the same text is updated, field by field.
type GoalRiskSetInput struct {
	ID         string      `json:"id"`   // the goal
	Text       string      `json:"text"` // what can go wrong
	Likelihood model.Level `json:"likelihood,omitempty"`
	Impact     model.Level `json:"impact,omitempty"`
	Owner      string      `json:"owner,omitempty"`      // who handles it: a link such as [[person-...]], or a name
	Mitigation string      `json:"mitigation,omitempty"` // what reduces it
	Source     string      `json:"source,omitempty"`     // where it was said: a URL, a document id, or the owner in chat; required for a new risk
	Remove     bool        `json:"remove,omitempty"`     // remove the risk with this text
}

// GoalRiskSetOutput is the output of goal_risk_set.
type GoalRiskSetOutput struct {
	ID    string           `json:"id"`
	Risks []model.GoalRisk `json:"risks"`
	Rev   string           `json:"rev"`
}

// GoalRiskSet adds, updates, or removes one risk of a goal's brief.
var GoalRiskSet = defineSafe("goal_risk_set", "Add or update one risk of a goal's brief, matched by text: likelihood and impact (low, medium, high), owner, mitigation, and source (required for a new risk; the owner's chat words count). remove drops it", func(ctx context.Context, d Deps, in GoalRiskSetInput) (GoalRiskSetOutput, error) {
	doc, p, err := getGoal(ctx, d.Store, normID(in.ID))
	if err != nil {
		return GoalRiskSetOutput{}, err
	}
	text := fold(in.Text)
	if text == "" {
		return GoalRiskSetOutput{}, errors.New("text is empty")
	}
	r := model.GoalRisk{Text: text, Likelihood: in.Likelihood, Impact: in.Impact, Owner: fold(in.Owner), Mitigation: fold(in.Mitigation), Source: fold(in.Source)}
	for _, f := range [][2]string{{"text", r.Text}, {"owner", r.Owner}, {"mitigation", r.Mitigation}, {"source", r.Source}} {
		if err := lintText(f[0], f[1]); err != nil {
			return GoalRiskSetOutput{}, err
		}
	}
	i := slices.IndexFunc(p.Risks, func(x model.GoalRisk) bool { return model.SameText(x.Text, text) })
	switch {
	case in.Remove && i < 0:
		return GoalRiskSetOutput{}, fmt.Errorf("%s has no risk %q", doc.ID, text)
	case in.Remove:
		p.Risks = slices.Delete(p.Risks, i, i+1)
	case i < 0:
		if r.Source == "" {
			return GoalRiskSetOutput{}, errors.New("source is required for a new risk: a URL, a document id, or the owner's words in chat")
		}
		p.Risks = append(p.Risks, r)
	default:
		old := &p.Risks[i]
		old.Likelihood = cmpOr(r.Likelihood, old.Likelihood)
		old.Impact = cmpOr(r.Impact, old.Impact)
		old.Owner = cmpOr(r.Owner, old.Owner)
		old.Mitigation = cmpOr(r.Mitigation, old.Mitigation)
		old.Source = cmpOr(r.Source, old.Source)
	}
	if err := model.CheckRisks(p.Risks); err != nil {
		return GoalRiskSetOutput{}, err
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return GoalRiskSetOutput{}, err
	}
	risks := p.Risks
	if risks == nil {
		risks = []model.GoalRisk{}
	}
	return GoalRiskSetOutput{ID: doc.ID, Risks: risks, Rev: saved.Rev}, nil
})

// cmpOr returns a when it is not empty, else b.
func cmpOr[T ~string](a, b T) T {
	if a != "" {
		return a
	}
	return b
}

// AssumptionSetInput is the input of assumption_set. Text names the
// assumption; an existing one with the same text is updated, field by field.
type AssumptionSetInput struct {
	ID      string                 `json:"id"`               // a goal or a task
	Text    string                 `json:"text"`             // what is taken as true
	Source  string                 `json:"source,omitempty"` // where it comes from; required for a new assumption
	Status  model.AssumptionStatus `json:"status,omitempty"` // holds, changed, or unknown: what a check found
	Checked string                 `json:"checked,omitempty"`
	By      string                 `json:"by,omitempty"`   // who checked; default the caller when status is set
	Fact    string                 `json:"fact,omitempty"` // what is true now, with status changed
	Remove  bool                   `json:"remove,omitempty"`
}

// AssumptionSetOutput is the output of assumption_set.
type AssumptionSetOutput struct {
	ID          string             `json:"id"`
	Assumptions []model.Assumption `json:"assumptions"`
	Rev         string             `json:"rev"`
}

// AssumptionSet adds, updates, or removes one assumption of a goal or a task.
var AssumptionSet = defineSafe("assumption_set", "Add or update one assumption of a goal or a task, matched by text: source (required for a new one; the owner's chat words count), and what a check found (status holds, changed, or unknown; checked date, default today; by, default you; fact when changed). remove drops it. Lines under ## Assumptions in the body are read as never checked; to check one, use recheck_record", func(ctx context.Context, d Deps, in AssumptionSetInput) (AssumptionSetOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return AssumptionSetOutput{}, err
	}
	list, ok := metaAssumptions(doc.Meta)
	if !ok {
		return AssumptionSetOutput{}, fmt.Errorf("%s is a %s: want a goal or a task", doc.ID, doc.Meta.Header().Type)
	}
	text := fold(in.Text)
	if text == "" {
		return AssumptionSetOutput{}, errors.New("text is empty")
	}
	a := model.Assumption{Text: text, Source: fold(in.Source), Status: in.Status, By: fold(in.By), Fact: fold(in.Fact), Checked: model.Date(in.Checked)}
	for _, f := range [][2]string{{"text", a.Text}, {"source", a.Source}, {"by", a.By}, {"fact", a.Fact}} {
		if err := lintText(f[0], f[1]); err != nil {
			return AssumptionSetOutput{}, err
		}
	}
	if a.Checked != "" {
		if err := checkDate("checked", string(a.Checked)); err != nil {
			return AssumptionSetOutput{}, err
		}
	}
	if a.Status != "" {
		if a.Checked == "" {
			a.Checked = model.Date(d.Store.Now().Format(dateLayout))
		}
		if a.By == "" {
			a.By = d.Store.Info().Actor
		}
	}
	if a.Status == model.AssumptionChanged && a.Fact == "" {
		return AssumptionSetOutput{}, errors.New("status changed needs fact: what is true now")
	}
	i := slices.IndexFunc(*list, func(x model.Assumption) bool { return model.SameText(x.Text, text) })
	switch {
	case in.Remove && i < 0:
		return AssumptionSetOutput{}, fmt.Errorf("%s has no assumption %q in its frontmatter", doc.ID, text)
	case in.Remove:
		*list = slices.Delete(*list, i, i+1)
	case i < 0:
		if a.Source == "" {
			return AssumptionSetOutput{}, errors.New("source is required for a new assumption: a URL, a document id, or the owner's words in chat")
		}
		*list = append(*list, a)
	default:
		old := &(*list)[i]
		old.Source = cmpOr(a.Source, old.Source)
		old.Status = cmpOr(a.Status, old.Status)
		old.Checked = cmpOr(a.Checked, old.Checked)
		old.By = cmpOr(a.By, old.By)
		old.Fact = cmpOr(a.Fact, old.Fact)
		if old.Status != model.AssumptionChanged {
			old.Fact = ""
		}
	}
	if err := model.CheckAssumptions(*list); err != nil {
		return AssumptionSetOutput{}, err
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return AssumptionSetOutput{}, err
	}
	out := *list
	if out == nil {
		out = []model.Assumption{}
	}
	return AssumptionSetOutput{ID: doc.ID, Assumptions: out, Rev: saved.Rev}, nil
})
