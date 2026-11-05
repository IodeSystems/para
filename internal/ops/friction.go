package ops

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	evlog "github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/export"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// The friction report counts what slows Asks down, by domain, policy, or reason
// code. It never groups by person and never ranks people: a person who is
// asked often is a sign of a missing owner or playbook, not a score.

// Friction groupings.
const (
	FrictionByDomain = "domain"
	FrictionByPolicy = "policy"
	FrictionByReason = "reason"
)

// FrictionInput is the input of report_friction.
type FrictionInput struct {
	Since string `json:"since,omitempty" query:"since"` // YYYY-MM-DD; Asks created from that day
	By    string `json:"by,omitempty" query:"by"`       // domain, policy, or reason (default)
	To    string `json:"to,omitempty" query:"to"`       // transport, as for export; empty or stdout shows it here
}

// FrictionRow is one group. Asks are the evidence: every number comes from
// these Asks.
type FrictionRow struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Name is the title of the domain or policy; for a reason it is the reason in words.
	Name               string   `json:"name,omitempty" nameof:"Key"`
	Asks               []string `json:"asks"`
	AskNames           []string `json:"ask_names,omitempty" nameof:"Asks"`
	Stalls             int      `json:"stalls"`                      // targets that went unanswered
	Redirects          int      `json:"redirects"`                   // targets added after the first
	HopsPerAsk         float64  `json:"hops_per_ask"`                // targets per Ask
	FirstAnswerDays    *float64 `json:"first_answer_days,omitempty"` // median, over Asks that were answered
	ResolutionDays     *float64 `json:"resolution_days,omitempty"`   // median, over Asks that were closed
	StalledDays        int      `json:"stalled_days"`                // days since the latest send, over Asks stalled now
	PolicyConflicts    int      `json:"policy_conflicts"`            // targets with reason policy_conflict
	ExceptionsHit      int      `json:"exceptions_hit"`              // targets that link a policy exception
	PlaybookCandidates int      `json:"playbook_candidates"`         // Asks that repeat another and have no playbook
}

// FrictionRepeat is a question asked more than once with no playbook.
type FrictionRepeat struct {
	Title    string   `json:"title"`
	Asks     []string `json:"asks"`
	AskNames []string `json:"ask_names,omitempty" nameof:"Asks"`
}

// FrictionOutput is the output of report_friction.
type FrictionOutput struct {
	Since        string           `json:"since,omitempty"`
	By           string           `json:"by"`
	Asks         int              `json:"asks"`          // Asks in the window
	WithFriction int              `json:"with_friction"` // Asks with a stall or redirect
	Rows         []FrictionRow    `json:"rows"`
	Repeats      []FrictionRepeat `json:"repeats"`
	Text         string           `json:"text,omitempty"`
	// Destination, Marking, and Ext are set when To names a transport: the CLI runs it.
	To          string `json:"to,omitempty"`
	Destination string `json:"destination,omitempty"`
	Marking     string `json:"marking,omitempty"`
	Ext         string `json:"ext,omitempty"`
}

// Rendered returns the plain text of the report, for a surface that runs the transport.
func (o FrictionOutput) Rendered() export.Rendered {
	return export.Rendered{Format: export.Plain, Ext: "txt", Data: []byte(o.Text)}
}

type askFacts struct {
	doc        model.Document
	m          *model.RFX
	first, res *time.Duration
	created    time.Time
	stallDays  int
}

func median(ds []float64) *float64 {
	if len(ds) == 0 {
		return nil
	}
	slices.Sort(ds)
	v := ds[len(ds)/2]
	if len(ds)%2 == 0 {
		v = (ds[len(ds)/2-1] + ds[len(ds)/2]) / 2
	}
	v = math.Round(v*10) / 10
	return &v
}

func normTitle(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// ReportFriction builds the friction report from Asks and events.
var ReportFriction = defineRead("report_friction", "Read the friction report of Asks (it files nothing; to file feedback about para use feedback_add): time to first answer and resolution, redirects and hops per Ask, stalled time, repeat Asks a playbook could answer, policy conflicts and exceptions, grouped by domain, policy, or reason code, never by person. Each row lists its Asks as evidence", func(ctx context.Context, d Deps, in FrictionInput) (FrictionOutput, error) {
	by := in.By
	if by == "" {
		by = FrictionByReason
	}
	if !slices.Contains([]string{FrictionByDomain, FrictionByPolicy, FrictionByReason}, by) {
		return FrictionOutput{}, fmt.Errorf("by %q: want domain, policy, or reason; the report has no per-person view", by)
	}
	var since time.Time
	if in.Since != "" {
		if err := checkDate("since", in.Since); err != nil {
			return FrictionOutput{}, err
		}
		since, _ = time.Parse("2006-01-02", in.Since)
	}
	out := FrictionOutput{Since: in.Since, By: by, Rows: []FrictionRow{}, Repeats: []FrictionRepeat{}}
	now := d.Store.Now()
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeRFX})
	if err != nil {
		return out, err
	}
	evs, err := d.Store.Events(ctx, math.MaxInt32)
	if err != nil {
		return out, err
	}
	type tl struct {
		created, answered, resolved time.Time
	}
	times := map[string]*tl{}
	for _, e := range evs {
		t := times[e.Doc]
		if t == nil {
			t = &tl{}
			times[e.Doc] = t
		}
		switch {
		case e.Op == evlog.OpCreate && t.created.IsZero():
			t.created = e.TS
		case e.Op == evlog.OpUpdate && (e.To == string(model.RFXAnswered)) && t.answered.IsZero():
			t.answered = e.TS
		case e.Op == evlog.OpUpdate && (e.To == string(model.RFXClosed) || e.To == string(model.RFXDeclined)) && t.resolved.IsZero():
			t.resolved = e.TS
		}
	}
	var asks []*askFacts
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return out, err
		}
		m, ok := doc.Meta.(*model.RFX)
		if !ok || m.Direction != model.RFXOut {
			continue
		}
		f := &askFacts{doc: doc, m: m}
		t := times[doc.ID]
		if t != nil {
			f.created = t.created
		}
		if f.created.IsZero() {
			f.created, _ = time.Parse("2006-01-02", string(m.Created))
		}
		if !since.IsZero() && f.created.Before(since) {
			continue
		}
		if t != nil && !f.created.IsZero() {
			if !t.answered.IsZero() {
				v := t.answered.Sub(f.created)
				f.first = &v
			}
			if !t.resolved.IsZero() {
				v := t.resolved.Sub(f.created)
				f.res = &v
				if f.first == nil {
					f.first = &v
				}
			}
		}
		if st, _, days := stalled(m, now); st {
			f.stallDays = days
		}
		asks = append(asks, f)
	}
	out.Asks = len(asks)
	title := map[string]string{}
	for _, f := range asks {
		title[f.doc.ID] = f.doc.Title
	}
	titles := func(ids []string) []string {
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = title[id]
		}
		return out
	}
	policyOf := func(about model.Link) string {
		if about == "" {
			return ""
		}
		doc, err := d.Store.Get(ctx, about.Target())
		if err != nil {
			return ""
		}
		switch x := doc.Meta.(type) {
		case *model.Policy:
			return doc.ID
		case *model.PolicyException:
			return x.Policy.Target()
		}
		return ""
	}
	aboutType := func(about model.Link) model.Type {
		if about == "" {
			return ""
		}
		doc, err := d.Store.Get(ctx, about.Target())
		if err != nil {
			return ""
		}
		return doc.Meta.Header().Type
	}
	// repeats: the same question asked more than once, none with a playbook.
	byTitle := map[string][]*askFacts{}
	for _, f := range asks {
		k := normTitle(f.doc.Title)
		byTitle[k] = append(byTitle[k], f)
	}
	repeat := map[string]bool{}
	for _, group := range byTitle {
		if len(group) < 2 || slices.ContainsFunc(group, func(f *askFacts) bool { return f.m.Playbook != "" }) {
			continue
		}
		rp := FrictionRepeat{Title: group[0].doc.Title}
		for _, f := range group {
			rp.Asks = append(rp.Asks, f.doc.ID)
			repeat[f.doc.ID] = true
		}
		slices.Sort(rp.Asks)
		out.Repeats = append(out.Repeats, rp)
	}
	for i := range out.Repeats {
		out.Repeats[i].AskNames = titles(out.Repeats[i].Asks)
	}
	slices.SortFunc(out.Repeats, func(a, b FrictionRepeat) int {
		return cmp.Or(cmp.Compare(len(b.Asks), len(a.Asks)), cmp.Compare(a.Title, b.Title))
	})
	type acc struct {
		row          FrictionRow
		first, res   []float64
		targets, ask int
	}
	groups := map[string]*acc{}
	touch := func(key, label string) *acc {
		a := groups[key]
		if a == nil {
			a = &acc{row: FrictionRow{Key: key, Label: label, Asks: []string{}}}
			groups[key] = a
		}
		return a
	}
	for _, f := range asks {
		hasFriction := false
		for _, t := range f.m.Targets {
			if t.Reason != "" {
				hasFriction = true
			}
		}
		if hasFriction {
			out.WithFriction++
		}
		keys := map[string]string{} // key -> label
		switch by {
		case FrictionByReason:
			for _, t := range f.m.Targets {
				if t.Reason != "" {
					keys[string(t.Reason)] = strings.ReplaceAll(string(t.Reason), "_", " ")
				}
			}
		case FrictionByDomain:
			if hasFriction {
				if dd, err := d.Store.Get(ctx, normID(f.m.Route)); err == nil && dd.Meta.Header().Type == model.TypeDomain {
					keys[dd.ID] = dd.Title
				} else {
					keys["(none)"] = "no domain"
				}
			}
		case FrictionByPolicy:
			for _, t := range f.m.Targets {
				if p := policyOf(t.About); t.Reason != "" && p != "" {
					keys[p] = p
					if pd, err := d.Store.Get(ctx, p); err == nil {
						keys[p] = pd.Title
					}
				}
			}
			if hasFriction && len(keys) == 0 {
				keys["(none)"] = "no policy"
			}
		}
		for key, label := range keys {
			a := touch(key, label)
			a.row.Asks = append(a.row.Asks, f.doc.ID)
			a.ask++
			a.targets += len(f.m.Targets)
			for i, t := range f.m.Targets {
				if i > 0 {
					a.row.Redirects++
				}
				if t.State == model.TargetNoReply {
					a.row.Stalls++
				}
				if t.Reason == model.FrictionPolicyConflict {
					a.row.PolicyConflicts++
				}
				if aboutType(t.About) == model.TypePolicyException {
					a.row.ExceptionsHit++
				}
			}
			if f.first != nil {
				a.first = append(a.first, f.first.Hours()/24)
			}
			if f.res != nil {
				a.res = append(a.res, f.res.Hours()/24)
			}
			a.row.StalledDays += f.stallDays
			if repeat[f.doc.ID] {
				a.row.PlaybookCandidates++
			}
		}
	}
	for _, a := range groups {
		a.row.HopsPerAsk = math.Round(float64(a.targets)/float64(max(a.ask, 1))*10) / 10
		a.row.FirstAnswerDays, a.row.ResolutionDays = median(a.first), median(a.res)
		slices.Sort(a.row.Asks)
		a.row.AskNames = titles(a.row.Asks)
		out.Rows = append(out.Rows, a.row)
	}
	slices.SortFunc(out.Rows, func(a, b FrictionRow) int {
		return cmp.Or(cmp.Compare(len(b.Asks), len(a.Asks)), cmp.Compare(a.Key, b.Key))
	})
	out.Text = frictionText(out)
	if in.To != "" && in.To != "stdout" {
		target, err := export.ParseTarget(in.To)
		if err != nil {
			return out, err
		}
		dest := target.Dest()
		dec, err := d.Store.Egress(ctx, "friction-report", dest)
		if err != nil {
			return out, err
		}
		if !dec.Allowed {
			return FrictionOutput{}, fmt.Errorf("transport %s is not in this vault's egress.export", target.Kind)
		}
		out.To, out.Destination, out.Marking, out.Ext = target.String(), dest, dec.Marking, "txt"
	}
	return out, nil
})

func days(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f", *v)
}

// frictionText is the report as plain text.
func frictionText(o FrictionOutput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Friction by %s", o.By)
	if o.Since != "" {
		fmt.Fprintf(&b, " since %s", o.Since)
	}
	fmt.Fprintf(&b, ": %s, %d with a stall or redirect\n", plural(o.Asks, "ask"), o.WithFriction)
	if len(o.Rows) == 0 {
		b.WriteString("Nothing to report.\n")
	}
	for _, r := range o.Rows {
		name := r.Name
		if name == "" {
			name = r.Label
		}
		fmt.Fprintf(&b, "\n%s (%s)\n", name, plural(len(r.Asks), "ask"))
		hops := "hops"
		if r.HopsPerAsk == 1 {
			hops = "hop"
		}
		fmt.Fprintf(&b, "  first answer %s d, resolution %s d, %.1f %s per ask, %s, %s, %s\n",
			days(r.FirstAnswerDays), days(r.ResolutionDays), r.HopsPerAsk, hops, plural(r.Redirects, "redirect"), plural(r.Stalls, "stall"), plural(r.StalledDays, "stalled day"))
		fmt.Fprintf(&b, "  %s, %s, %s without a playbook\n", plural(r.PolicyConflicts, "policy conflict"), plural(r.ExceptionsHit, "exception"), plural(r.PlaybookCandidates, "repeat ask"))
		n := r.AskNames
		if len(n) != len(r.Asks) {
			n = r.Asks
		}
		fmt.Fprintf(&b, "  asks: %s\n", strings.Join(n, "; "))
	}
	if len(o.Repeats) > 0 {
		b.WriteString("\nAsked more than once, no playbook:\n")
		for _, r := range o.Repeats {
			fmt.Fprintf(&b, "  %s (%s)\n", r.Title, plural(len(r.Asks), "time"))
		}
	}
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
