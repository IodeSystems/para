package today

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

func must[I, O any](t *testing.T, op *ops.Op[I, O], d ops.Deps, in I) O {
	t.Helper()
	out, err := op.Run(context.Background(), d, in)
	if err != nil {
		t.Fatalf("%s: %v", op.Name(), err)
	}
	return out
}

// fixture is one synthetic vault with an item of every kind in every state
// the predicate tells apart, dated from the real clock: Today reads the
// files with the machine's time.
func fixture(t *testing.T) (registry.Install, ops.Deps) {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, "vaults", "acme")
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "acme", tpl, vault.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	props := filepath.Join(root, ".para", "vault.properties")
	b, err := os.ReadFile(props)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	if err := os.WriteFile(props, append(b, "gate.require=none\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	in := registry.Install{Home: home}
	if err := in.Add("acme", root); err != nil {
		t.Fatal(err)
	}
	st, err := local.Open(root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "m1", StateDir: filepath.Join(home, "state")})
	if err != nil {
		t.Fatal(err)
	}
	return in, ops.Deps{Store: st}
}

func day(n int) string { return time.Now().AddDate(0, 0, n).Format(dateLayout) }

// agenda ∪ stack ∪ Today (the needs-you parts) is the NeedsYou set, no part
// lists anything outside it, and the counts agree.
func TestViewsAreTheNeedsYouSet(t *testing.T) {
	in, d := fixture(t)
	ada := must(t, ops.OrgPersonAdd, d, ops.OrgAddInput{Name: "Ada Example", Org: "acme.example"}).ID
	proj := must(t, ops.ProjectAdd, d, ops.ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	task := func(title string, f func(*ops.TaskAddInput)) string {
		in := ops.TaskAddInput{Project: proj, Title: title, DoneWhen: "Done."}
		if f != nil {
			f(&in)
		}
		return must(t, ops.TaskAdd, d, in).ID
	}
	task("Due today", func(i *ops.TaskAddInput) { i.Due = day(0) })
	task("No date", nil)
	task("Overdue", func(i *ops.TaskAddInput) { i.Due = day(-3) })
	done := task("Done", func(i *ops.TaskAddInput) { i.Due = day(0) })
	must(t, ops.TaskDone, d, ops.TaskDoneInput{ID: done})
	later := task("Put off", func(i *ops.TaskAddInput) { i.Due = day(0) })
	task("For Ada", func(i *ops.TaskAddInput) { i.Due, i.Assignee = day(0), ada })
	blocked := task("Blocked", nil)
	must(t, ops.TaskBlock, d, ops.TaskBlockInput{ID: blocked, Reason: "Waiting for access."})
	q := must(t, ops.QuestionAdd, d, ops.QuestionAddInput{Text: "Which region?", Project: proj}).ID
	task("Waits on the question", func(i *ops.TaskAddInput) { i.Due, i.DependsOn = day(0), []string{q} })
	must(t, ops.QuestionAdd, d, ops.QuestionAddInput{Text: "Which vendor?", Project: proj})
	qLater := must(t, ops.QuestionAdd, d, ops.QuestionAddInput{Text: "Which logo?", Project: proj}).ID
	qSent := must(t, ops.QuestionAdd, d, ops.QuestionAddInput{Text: "Which budget?", Project: proj}).ID
	tSent := task("Sent to an agent", func(i *ops.TaskAddInput) { i.Due = day(0) })
	must(t, ops.Defer, d, ops.DeferInput{IDs: []string{later, qLater}, Until: day(5)})
	must(t, ops.RFXAdd, d, ops.RFXAddInput{Direction: model.RFXIn, Ask: "Review the numbers", Route: ada,
		Targets: []ops.TargetInput{{To: ada, Medium: "email"}}})
	must(t, ops.RFXAdd, d, ops.RFXAddInput{Direction: model.RFXOut, Ask: "Send the list", Route: ada, NextNudge: day(0),
		Targets: []ops.TargetInput{{To: ada, Medium: "email"}}})
	must(t, ops.RFXAdd, d, ops.RFXAddInput{Direction: model.RFXOut, Ask: "Send the keys", Route: ada, NextNudge: day(4),
		Targets: []ops.TargetInput{{To: ada, Medium: "email"}}})
	must(t, ops.IOAdd, d, ops.IOAddInput{Direction: model.IOIn, Text: "Invoice attached", From: ada})
	must(t, ops.IOAdd, d, ops.IOAddInput{Direction: model.IOOut, Text: "Thanks, paid.", To: []string{ada}, Expects: model.ExpectsNone})
	must(t, ops.IncidentAdd, d, ops.IncidentAddInput{Title: "Checkout down", Severity: "sev2"})
	must(t, ops.AreaAdd, d, ops.AreaAddInput{Title: "Finance", Cadence: "weekly"})
	soon := time.Now().Add(2 * time.Hour).Truncate(time.Minute)
	at := func(t time.Time) string { return t.Format(time.RFC3339) }
	must(t, ops.MeetingAdd, d, ops.MeetingAddInput{Title: "Required sync", With: []string{ada}, Start: at(soon), End: at(soon.Add(30 * time.Minute))})
	must(t, ops.MeetingAdd, d, ops.MeetingAddInput{Title: "Optional sync", With: []string{ada}, Start: at(soon), End: at(soon.Add(30 * time.Minute)), Optional: true})
	must(t, ops.MeetingAdd, d, ops.MeetingAddInput{Title: "Next week", With: []string{ada}, Start: at(soon.AddDate(0, 0, 6)), End: at(soon.AddDate(0, 0, 6).Add(time.Hour))})
	for _, id := range []string{qSent, tSent} {
		must(t, ops.AgentSend, d, ops.AgentSendInput{ID: id, Text: "Please handle this."})
	}
	// Agent work that ended needing the owner.
	r := must(t, ops.RequestAdd, d, ops.RequestAddInput{Ask: "Draft the report"}).ID
	if got := must(t, ops.RequestNext, d, ops.RequestNextInput{}); got.Request == nil {
		t.Fatal("nothing to claim")
	}
	// request_next takes the oldest: claim until ours is claimed, then end it.
	for i := 0; i < 3; i++ {
		_, rr, err := getRequest(d, r)
		if err != nil {
			t.Fatal(err)
		}
		if rr.Status == model.RequestClaimed {
			break
		}
		must(t, ops.RequestNext, d, ops.RequestNextInput{})
	}
	must(t, ops.RequestDone, d, ops.RequestDoneInput{ID: r, Resolution: model.ResolutionNeedsClarification, Result: "Which quarter?"})

	sn, err := ops.NewSnapshot(context.Background(), d.Store)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, id := range sn.Set() {
		set[id] = true
	}
	for _, id := range []string{done, later, qLater, qSent, tSent} {
		if set[id] {
			t.Errorf("%s needs the owner: %+v", id, sn.Need(id))
		}
	}

	rep, err := Read(context.Background(), in, "user", "cli", day(0))
	if err != nil || rep.Failed() != nil {
		t.Fatalf("today: %v %v", err, rep.Failed())
	}
	vt := rep.Vaults[0]
	union := map[string]bool{}
	part := func(name string, ids []string) {
		t.Helper()
		for _, id := range ids {
			if !set[id] {
				t.Errorf("%s lists %s, which does not need the owner: %+v", name, id, sn.Need(id))
			}
			union[id] = true
		}
	}
	groupIDs := func(g Group) []string {
		var out []string
		for _, it := range g.Items {
			out = append(out, it.ID)
		}
		return out
	}
	for name, g := range map[string]Group{"incidents": vt.Incidents, "agenda": vt.Agenda, "overdue": vt.Overdue, "due_soon": vt.DueSoon,
		"blocked": vt.Blocked, "nudges": vt.Nudges, "due": vt.Due, "work": vt.Work} {
		part("today "+name, groupIDs(g))
	}
	var ag []string
	agenda := must(t, ops.Agenda, d, ops.AgendaInput{Days: 1})
	for _, it := range agenda.Items {
		if it.Kind == ops.AgendaGoal {
			continue // a goal's watch date, listed by name
		}
		if it.NeedsYou != set[it.ID] {
			t.Errorf("agenda %s: needs_you %v, set %v", it.ID, it.NeedsYou, set[it.ID])
		}
		if it.NeedsYou {
			ag = append(ag, it.ID)
		}
	}
	part("agenda", ag)
	stack := must(t, ops.ProposalStack, d, ops.ProposalStackInput{NeedsYou: true})
	var st []string
	for _, r := range stack.Rows {
		st = append(st, r.Item)
	}
	for _, r := range stack.Requests {
		st = append(st, r.Request)
	}
	part("stack", st)
	var inbox []string
	for _, it := range must(t, ops.IOList, d, ops.IOListInput{Direction: model.IOIn}).Items {
		if it.NeedsYou {
			inbox = append(inbox, it.ID)
		}
	}
	part("inbox", inbox)
	var missing []string
	for id := range set {
		if !union[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("needs the owner but no view lists it: %v", missing)
	}
	// Counts: Today's is the set; the stack's is the part of it the stack holds.
	if vt.NeedsYou != len(set) {
		t.Errorf("today needs_you = %d, set = %d", vt.NeedsYou, len(set))
	}
	if stack.NeedsYou != len(st) {
		t.Errorf("stack needs_you = %d, rows = %d", stack.NeedsYou, len(st))
	}
	if len(set) < 12 {
		t.Errorf("fixture too small: %d items need the owner: %v", len(set), sn.Set())
	}
	if slices.ContainsFunc(agenda.Optional, func(it ops.AgendaItem) bool { return set[it.ID] }) {
		t.Error("an optional meeting the owner has not said they go to needs them")
	}
}

func getRequest(d ops.Deps, id string) (model.Document, *model.Request, error) {
	doc, err := d.Store.Get(context.Background(), id)
	if err != nil {
		return doc, nil, err
	}
	r, _ := doc.Meta.(*model.Request)
	return doc, r, nil
}
