package ops

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// reasonDeps is a vault at the current format, with people.
func reasonDeps(t *testing.T) Deps {
	t.Helper()
	d := newDeps(t)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, strconv.Itoa(upgrade.Latest())); err != nil {
		t.Fatal(err)
	}
	seedOrg(t, d)
	return d
}

// bareTask makes an open task whose project is gone: no reason. task_add
// refuses one, so it is written as a hand edit would be.
func bareTask(t *testing.T, d Deps, title string) string {
	t.Helper()
	doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderProjects, Title: title,
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, DoneWhen: "Done.", Project: model.LinkTo("project-gone")}})
	if err != nil {
		t.Fatal(err)
	}
	return doc.ID
}

func itemContext(t *testing.T, d Deps, id string) ItemContextOutput {
	t.Helper()
	return must(t, ItemContext, d, ItemContextInput{ID: id})
}

func reasonKinds(rs []Reason) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Kind+":"+r.Text)
	}
	return out
}

func TestContextCameFromIOAndRequestedBy(t *testing.T) {
	d := reasonDeps(t)
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	io := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Medium: model.MediumEmail, Text: "Please fix the login page",
		From: "person-bo-example", Ref: "https://mail.example.com/m/1", At: "2026-11-05T10:00:00Z"}).ID
	f := must(t, IOFile, d, IOFileInput{ID: io, To: TriageTask, Project: proj, DoneWhen: "Login works."})
	_, task, err := getTask(ctx, d.Store, f.Became)
	if err != nil {
		t.Fatal(err)
	}
	if task.RequestedBy != "[[person-bo-example]]" {
		t.Fatalf("requested_by = %q, want the sender", task.RequestedBy)
	}
	c := itemContext(t, d, f.Became)
	if len(c.CameFrom) != 1 {
		t.Fatalf("came_from = %+v", c.CameFrom)
	}
	cf := c.CameFrom[0]
	if cf.Kind != CameFromIO || cf.ID != io || cf.Medium != model.MediumEmail || cf.From != "Bo Example" || cf.FromID != "person-bo-example" ||
		cf.URL != "https://mail.example.com/m/1" || cf.Text != "Email from Bo Example, 2026-11-05" {
		t.Errorf("came_from = %+v", cf)
	}
	if !slices.Equal(c.For, []ForParty{{Role: ForRequestedBy, ID: "person-bo-example", Name: "Bo Example"}}) {
		t.Errorf("for = %+v", c.For)
	}
	want := []string{"goal:goal Portal (directly)", "owed:owed to Bo Example (directly)"}
	if got := reasonKinds(c.Reasons); !slices.Equal(got, want) {
		t.Errorf("reasons = %v, want %v", got, want)
	}

	// Filing into an existing task changes nothing on it; the filer's
	// requested_by wins on a new one.
	other := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Existing", DoneWhen: "Done.", RequestedBy: "VP eng"}).ID
	io2 := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Also this", From: "person-ada-example"}).ID
	must(t, IOFile, d, IOFileInput{ID: io2, Existing: other})
	if _, tk, _ := getTask(ctx, d.Store, other); tk.RequestedBy != "VP eng" {
		t.Errorf("existing requested_by = %q", tk.RequestedBy)
	}
	io3 := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Third", From: "person-ada-example"}).ID
	f3 := must(t, IOFile, d, IOFileInput{ID: io3, To: TriageTask, Project: proj, DoneWhen: "Done.", RequestedBy: "person-cy-example"})
	if _, tk, _ := getTask(ctx, d.Store, f3.Became); tk.RequestedBy != "[[person-cy-example]]" {
		t.Errorf("filer requested_by = %q", tk.RequestedBy)
	}
	// task_edit sets and clears it.
	e := must(t, TaskEdit, d, TaskEditInput{ID: other, RequestedBy: "person-ada-example"})
	if e.RequestedBy != "[[person-ada-example]]" {
		t.Errorf("task_edit requested_by = %q", e.RequestedBy)
	}
	if e := must(t, TaskEdit, d, TaskEditInput{ID: other, ClearRequestedBy: true}); e.RequestedBy != "" {
		t.Errorf("cleared = %q", e.RequestedBy)
	}
}

func TestContextWhyChain(t *testing.T) {
	d := reasonDeps(t)
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Infra"}).ID
	goal := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Launch", Owner: "Jordan Example", Due: "2026-12-01"}).ID
	parent := must(t, TaskAdd, d, TaskAddInput{Project: goal, Title: "Ship it", DoneWhen: "Shipped."}).ID
	kid := bareTask(t, d, "Write the notes")
	doc, tk, _ := getTask(ctx, d.Store, kid)
	tk.Parent = model.LinkTo(parent)
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	want := []string{"goal:goal Launch, due 2026-12-01, through: action item of Ship it → goal Launch"}
	if got := reasonKinds(itemContext(t, d, kid).Reasons); !slices.Equal(got, want) {
		t.Errorf("parent chain = %v, want %v", got, want)
	}

	// Indirect through depends_on: the blocker unblocks a task of the goal.
	blocker := bareTask(t, d, "Get the cert")
	must(t, TaskDependsAdd, d, TaskDependsInput{ID: parent, DependsOn: []string{blocker}})
	c := itemContext(t, d, blocker)
	if len(c.Reasons) != 1 || c.Reasons[0].Kind != ReasonGoal || c.Reasons[0].Ref != goal ||
		len(c.Reasons[0].Via) != 1 || c.Reasons[0].Via[0] != (ReasonStep{How: StepUnblocks, ID: parent, Title: "Ship it"}) ||
		c.Reasons[0].Text != "goal Launch, due 2026-12-01, through: unblocks Ship it → goal Launch" {
		t.Errorf("depends_on chain = %+v", c.Reasons)
	}

	// Upkeep: a task under an area.
	up := must(t, TaskAdd, d, TaskAddInput{Area: area, Title: "Rotate keys", DoneWhen: "Rotated."}).ID
	if got := reasonKinds(itemContext(t, d, up).Reasons); !slices.Equal(got, []string{"upkeep:keeps up area Infra (directly)"}) {
		t.Errorf("upkeep = %v", got)
	}

	// None.
	lone := bareTask(t, d, "Something")
	c = itemContext(t, d, lone)
	if len(c.Reasons) != 1 || c.Reasons[0].Kind != ReasonNone || c.Reasons[0].Text != NoReasonText || len(c.For) != 0 || len(c.CameFrom) != 0 {
		t.Errorf("none = %+v", c)
	}
}

func TestContextObligationOwedTo(t *testing.T) {
	d := reasonDeps(t)
	o := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the report", Kind: model.KindCommitment, OwedTo: []string{"person-ada-example"}, Due: "2026-11-20"}).ID
	c := itemContext(t, d, o)
	if !slices.Equal(c.For, []ForParty{{Role: ForOwedTo, ID: "person-ada-example", Name: "Ada Example"}}) {
		t.Errorf("for = %+v", c.For)
	}
	if got := reasonKinds(c.Reasons); !slices.Equal(got, []string{"owed:owed to Ada Example (directly)"}) {
		t.Errorf("reasons = %v", got)
	}
}

func TestHealthNoReasonUsesReasons(t *testing.T) {
	d := reasonDeps(t)
	lone := bareTask(t, d, "Lone work")
	owed := bareTask(t, d, "Owed work")
	must(t, TaskEdit, d, TaskEditInput{ID: owed, RequestedBy: "person-ada-example"})
	out, err := Health(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	flagged := map[string]string{}
	for _, g := range out.Groups {
		for _, f := range g.Findings {
			if f.Kind == HealthNoPath {
				flagged[f.ID] = f.Message
			}
		}
	}
	if flagged[lone] != NoReasonTask {
		t.Errorf("lone task: %q, want %q", flagged[lone], NoReasonTask)
	}
	if _, ok := flagged[owed]; ok {
		t.Errorf("a task with requested_by is flagged: %v", flagged)
	}
}

func TestOwnerMediumsNeedNoRef(t *testing.T) {
	d := reasonDeps(t)
	sw := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Medium: model.MediumSwivel, Text: "Carry the budget numbers over", From: "the finance tracker"})
	vb := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Medium: model.MediumVerbal, Text: "Dana asked for the slides", From: "person-ada-example"})
	if _, err := IOAdd.Run(ctx, d, IOAddInput{Direction: model.IOIn, Medium: model.MediumSlack, Text: "no link", From: "person-ada-example"}); err == nil {
		t.Error("slack without ref was taken")
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{sw.ID, vb.ID} {
		if n := sn.Need(id); !n.Yes || n.Reason != "to sort: your capture" {
			t.Errorf("%s: need = %+v, want the owner's own capture", id, n)
		}
	}
	// A dated conversation is a logged contact: closed.
	lg := must(t, InteractionAdd, d, InteractionAddInput{Summary: "Talked budgets with Ada", With: []string{"person-ada-example"},
		Medium: model.MediumVerbal, Direction: model.InteractionIn, Date: "2026-11-06"})
	doc, err := d.Store.Get(ctx, lg.ID)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := doc.Meta.(*model.IO)
	if !ok || !m.Logged() || m.State.Open() || m.Medium != model.MediumVerbal {
		t.Fatalf("logged = %+v", doc.Meta)
	}
	// Filed into a new task, the swivel item says where it came from and
	// sets no requested_by (from is a system).
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Budget", Owner: "Jordan Example"}).ID
	f := must(t, IOFile, d, IOFileInput{ID: sw.ID, To: TriageTask, Project: proj, DoneWhen: "Done."})
	c := itemContext(t, d, f.Became)
	if len(c.CameFrom) != 1 || !strings.HasPrefix(c.CameFrom[0].Text, "Carried over by hand from the finance tracker") || len(c.For) != 0 {
		t.Errorf("swivel context = %+v", c)
	}
	f2 := must(t, IOFile, d, IOFileInput{ID: vb.ID, To: TriageTask, Project: proj, DoneWhen: "Done."})
	if c := itemContext(t, d, f2.Became); len(c.CameFrom) != 1 || !strings.HasPrefix(c.CameFrom[0].Text, "Conversation with Ada Example") {
		t.Errorf("verbal context = %+v", c.CameFrom)
	}
}

func TestTickReasonCheck(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia", "request.surfaces=cli,pwa,tick", "gap.daily_cap=2")
	d := tv.open("ia", "cli")
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Home"}).ID
	// A complete task with a reason: no fill-gap and no reason check.
	must(t, TaskAdd, d, TaskAddInput{Area: area, Title: "Has a reason", DoneWhen: "Done.", Steps: "1. Do it.", Effort: model.EffortS})
	a := bareTask(t, d, "Alpha")
	b := bareTask(t, d, "Beta")
	c := bareTask(t, d, "Gamma")
	for _, id := range []string{a, b, c} {
		doc, tk, _ := getTask(ctx, d.Store, id)
		tk.Steps, tk.Effort = "1. Do it.", model.EffortS
		if _, err := d.Store.Update(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}
	out := tick(t, d)
	if len(out.Reasons) != 2 {
		t.Fatalf("reasons = %v, want the cap of 2", out.Reasons)
	}
	var linked []string
	for _, id := range out.Reasons {
		doc, r, err := getRequest(ctx, d.Store, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind != InvestigateKind || !strings.Contains(doc.Body, NoReasonHint) || len(r.Links) != 1 {
			t.Fatalf("%s: kind %q links %v body:\n%s", id, r.Kind, r.Links, doc.Body)
		}
		linked = append(linked, r.Links[0].Target())
	}
	slices.Sort(linked)
	if c := itemContext(t, d, linked[0]); c.Looking == "" || c.Reasons[0].Kind != ReasonNone {
		t.Errorf("context while open: %+v", c)
	}
	// The cap is per day: none more today.
	if again := tick(t, d); len(again.Reasons) != 0 {
		t.Errorf("same day: %v", again.Reasons)
	}
	// Next day: the third item; the first two have open work.
	tv.now = tv.now.AddDate(0, 0, 1)
	next := tick(t, d)
	if len(next.Reasons) != 1 {
		t.Fatalf("next day = %v", next.Reasons)
	}
	// The work ends with no reason found: never asked again.
	for _, id := range append(out.Reasons, next.Reasons...) {
		must(t, RequestCancel, d, RequestCancelInput{ID: id})
	}
	tv.now = tv.now.AddDate(0, 0, 1)
	if last := tick(t, d); len(last.Reasons) != 0 {
		t.Errorf("asked again: %v", last.Reasons)
	}
	if c := itemContext(t, d, a); c.Looking != "" {
		t.Errorf("looking after cancel = %q", c.Looking)
	}
}
