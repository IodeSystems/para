package upgrade

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// smallFixture is a synthetic vault at format 9 with a team, a role, a
// domain, interactions in each direction (one archived), incidents in each
// status, a follow-up task, and notes with and without a link.
func smallFixture(t *testing.T) (*fixture, store.Store) {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{t: t, root: filepath.Join(dir, "v"), home: filepath.Join(dir, "state")}
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(f.root, "acme", tpl, vault.InitOptions{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	s := f.open("one")
	ctx := context.Background()
	if err := s.SetConfig(ctx, ConfigKey, "9"); err != nil {
		t.Fatal(err)
	}
	c := func(typ model.Type) model.Common { return model.Common{Type: typ} }
	with := []model.Link{model.LinkTo("person-ada")}
	dash := []model.Pointer{{Label: "Dashboard", Kind: model.PointerDashboard, Target: "https://grafana.acme.example/d/1"}}
	for _, d := range []model.Document{
		{ID: "area-ops", Folder: model.FolderAreas, Title: "Ops", Body: "## Logs\n\nWhere logs go.", Meta: &model.Area{Common: c(model.TypeArea), Status: model.AreaActive}},
		{ID: "team-atlas", Folder: model.FolderOrg, Title: "Atlas", Body: "## Facts\n\n- Runs the portal (source: wiki)", Meta: &model.Team{Common: c(model.TypeTeam), Org: "acme.example"}},
		{ID: "role-lead", Folder: model.FolderOrg, Title: "Lead", Meta: &model.Role{Common: c(model.TypeRole), Org: "acme.example"}},
		{ID: "domain-logs", Folder: model.FolderOrg, Title: "Logs", Meta: &model.Domain{Common: c(model.TypeDomain), Owner: model.LinkTo("role-lead")}},
		{ID: "person-ada", Folder: model.FolderOrg, Title: "Ada", Meta: &model.Person{Common: c(model.TypePerson), Org: "acme.example",
			Role: model.LinkTo("role-lead"), Team: model.LinkTo("team-atlas")}},
		{ID: "int-in", Folder: model.FolderInteractions, Title: "Ada asked for the logs", Body: "She needs them by Friday.",
			Meta: &model.Interaction{Common: c(model.TypeInteraction), Date: "2026-11-01", With: with, Medium: model.MediumSlack,
				Direction: model.InteractionIn, Visibility: model.VisibilityPrivate, URL: "https://acme.example.slack.com/archives/C1/p1"}},
		{ID: "int-out", Folder: model.FolderInteractions, Title: "Sent Ada the logs",
			Meta: &model.Interaction{Common: c(model.TypeInteraction), Date: "2026-11-02", With: with, Medium: model.MediumEmail,
				Direction: model.InteractionOut, Visibility: model.VisibilityPrivate}},
		{ID: "int-seen", Folder: model.FolderInteractions, Title: "Atlas demo", Body: "Watched it.",
			Meta: &model.Interaction{Common: c(model.TypeInteraction), Date: "2026-10-30", With: []model.Link{model.LinkTo("team-atlas")}, Medium: model.MediumMeeting,
				Direction: model.InteractionObserved, Visibility: model.VisibilityPublic}},
		{ID: "inc-open", Folder: model.FolderIncidents, Title: "Logs down", Body: "## Timeline\n\n- 2026-11-01T09:00:00Z opened sev1",
			Meta: &model.Incident{Common: c(model.TypeIncident), Severity: model.Sev1, Status: model.IncidentOpen, Started: "2026-11-01T09:00:00Z", Area: model.LinkTo("area-ops"), Links: dash}},
		{ID: "inc-mit", Folder: model.FolderIncidents, Title: "Slow search",
			Meta: &model.Incident{Common: c(model.TypeIncident), Severity: model.Sev3, Status: model.IncidentMitigated, Started: "2026-10-20T09:00:00Z"}},
		{ID: "inc-done", Folder: model.FolderIncidents, Title: "Disk full",
			Meta: &model.Incident{Common: c(model.TypeIncident), Severity: model.Sev2, Status: model.IncidentResolved, Started: "2026-10-01T09:00:00Z", Ended: "2026-10-01T11:00:00Z"}},
		{ID: "fix-disk", Folder: model.FolderProjects, Title: "Add a disk alert",
			Meta: &model.Task{Common: c(model.TypeTask), Status: model.TaskTodo, Area: model.LinkTo("area-ops"), Incident: model.LinkTo("inc-done")}},
		{ID: "note-ops", Folder: model.FolderResources, Title: "Log paths", Body: "/var/log/atlas", Meta: &model.Note{Common: c(model.TypeNote), Area: model.LinkTo("area-ops")}},
		{ID: "note-free", Folder: model.FolderResources, Title: "Stray thought", Meta: &model.Note{Common: c(model.TypeNote)}},
	} {
		d.Meta.Header().Updated = "2026-11-03" // as a fold writes it, so an unfold gives the file back byte for byte
		if _, err := s.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.ID, err)
		}
	}
	// An unknown key a person added by hand stays through the fold.
	d, _ := s.Get(ctx, "inc-open")
	if d, err = d.WithFields(map[string]any{"x_pager": "PD-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	// An archived interaction folds too and stays in archive/.
	old := model.Document{ID: "int-old", Folder: model.FolderArchive, Title: "Old call",
		Meta: &model.Interaction{Common: c(model.TypeInteraction), Date: "2026-01-02", With: with, Medium: model.MediumPhone,
			Direction: model.InteractionOut, Visibility: model.VisibilityPrivate}}
	old.Meta.Header().Updated = "2026-11-03"
	if _, err := s.Create(ctx, old); err != nil {
		t.Fatal(err)
	}
	return f, s
}

var smallFolded = []string{"domain-logs", "inc-done", "inc-mit", "inc-open", "int-in", "int-old", "int-out", "int-seen", "role-lead", "team-atlas"}

var smallRels = map[string]string{
	"team-atlas": "org/team-atlas.md", "role-lead": "org/role-lead.md", "domain-logs": "org/domain-logs.md",
	"int-in": "interactions/int-in.md", "int-out": "interactions/int-out.md", "int-seen": "interactions/int-seen.md", "int-old": "archive/int-old.md",
	"inc-open": "incidents/inc-open.md", "inc-mit": "incidents/inc-mit.md", "inc-done": "incidents/inc-done.md",
	"person-ada": "org/person-ada.md", "fix-disk": "projects/fix-disk.md", "note-ops": "resources/note-ops.md",
}

func TestSmallTypesStep(t *testing.T) {
	f, s := smallFixture(t)
	ctx := context.Background()
	before := map[string]string{}
	for id, rel := range smallRels {
		before[id] = raw(t, f, rel)
	}
	// The optional notes step is listed, and does not hold the format back.
	st, err := StatusOf(ctx, s)
	st = older(st, GoalsFormat)
	if err == nil && len(st.Pending) == 2 {
		slices.Sort(st.Pending[0].Docs)
	}
	if err != nil || len(st.Pending) != 2 || st.Pending[0].ID != "small-types" || strings.Join(st.Pending[0].Docs, ",") != strings.Join(smallFolded, ",") ||
		st.Pending[1].ID != "notes-placement" || st.Pending[1].Auto {
		t.Fatalf("status = %+v, %v", st, err)
	}

	dry, err := Run(ctx, s, true)
	if err != nil {
		t.Fatal(err)
	}
	so := stepOut(dry, "small-types")
	got := slices.Clone(so.Docs)
	slices.Sort(got)
	if strings.Join(got, ",") != strings.Join(smallFolded, ",") || so.Batch != "" || len(so.Plan) != len(smallFolded) {
		t.Fatalf("dry run = %+v", dry)
	}
	for id, rel := range smallRels {
		if raw(t, f, rel) != before[id] {
			t.Fatalf("a dry run changed %s", id)
		}
	}

	res, err := Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	so = stepOut(res, "small-types")
	if res.FormatVersion != Latest() || so.Count != len(smallFolded) || so.Batch == "" {
		t.Fatalf("run = %+v", res)
	}
	get := func(id string) model.Document {
		t.Helper()
		d, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	for id, want := range map[string]model.GroupKind{"team-atlas": model.GroupTeam, "role-lead": model.GroupRole, "domain-logs": model.GroupDomain} {
		d := get(id)
		g, ok := d.Meta.(*model.Group)
		if !ok || g.Kind != want || d.Folder != model.FolderOrg || string(g.FoldedFrom) != strings.Split(id, "-")[0] {
			t.Fatalf("%s = %s %+v", id, d.Folder, d.Meta)
		}
	}
	if g := get("domain-logs").Meta.(*model.Group); g.Owner != model.LinkTo("role-lead") || g.Org != "" {
		t.Fatalf("domain group = %+v", g)
	}
	if !strings.Contains(raw(t, f, "org/team-atlas.md"), "Runs the portal") {
		t.Fatal("team lost its facts")
	}
	// Interactions are closed io items with their text.
	for id, want := range map[string]struct {
		dir      model.IODirection
		state    model.IOState
		observed bool
		folder   model.Folder
	}{
		"int-in": {model.IOIn, model.IOFiled, false, model.FolderIO}, "int-out": {model.IOOut, model.IOSent, false, model.FolderIO},
		"int-seen": {model.IOIn, model.IOFiled, true, model.FolderIO}, "int-old": {model.IOOut, model.IOSent, false, model.FolderArchive},
	} {
		d := get(id)
		m, ok := d.Meta.(*model.IO)
		if !ok || m.Direction != want.dir || m.State != want.state || m.Observed != want.observed || d.Folder != want.folder ||
			m.FoldedFrom != model.TypeInteraction || !m.Logged() || len(m.With) != 1 {
			t.Fatalf("%s = %s %+v", id, d.Folder, d.Meta)
		}
	}
	if d := get("int-in"); d.Title != "Ada asked for the logs" || d.Body != "She needs them by Friday." || d.Meta.(*model.IO).URL == "" {
		t.Fatalf("int-in text = %q %q", d.Title, d.Body)
	}
	// Incidents are tasks of kind incident; the status is held by the task status.
	for id, want := range map[string]model.TaskStatus{"inc-open": model.TaskTodo, "inc-mit": model.TaskDoing, "inc-done": model.TaskDone} {
		d := get(id)
		m, ok := d.Meta.(*model.Task)
		if !ok || m.Kind != model.KindIncident || m.Status != want || m.Severity == "" || m.Started == "" || d.Folder != model.FolderProjects || m.FoldedFrom != model.TypeIncident {
			t.Fatalf("%s = %s %+v", id, d.Folder, d.Meta)
		}
		if i, ok := model.IncidentOf(d); !ok || i.Status != model.IncidentStatusOf(want) {
			t.Fatalf("%s reads as incident %+v", id, i)
		}
	}
	text := raw(t, f, "projects/inc-open.md")
	for _, k := range []string{"x_pager: PD-1", "folded_from: incident", "severity: sev1", "Dashboard", "opened sev1"} {
		if !strings.Contains(text, k) {
			t.Fatalf("inc-open lost %q:\n%s", k, text)
		}
	}
	// What links to them is unchanged and still resolves.
	for _, id := range []string{"person-ada", "fix-disk", "note-ops"} {
		if raw(t, f, smallRels[id]) != before[id] {
			t.Fatalf("the step changed %s", id)
		}
	}
	findings, err := s.Lint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range findings {
		if fd.Severity == model.SeverityError {
			t.Fatalf("lint after the fold: %s", fd)
		}
	}
	// The optional notes step lists the note that links an item (not the one
	// that links nothing), and does not hold the format back.
	if st, _ := StatusOf(ctx, s); st.FormatVersion != Latest() || len(older(st, GoalsFormat).Pending) != 0 {
		t.Fatalf("status after = %+v", st)
	}
	if ids := notesOf(t, ctx, s); strings.Join(ids, ",") != "note-ops" {
		t.Fatalf("notes-placement selects %v", ids)
	}

	// A re-run, and a second machine, do nothing.
	if again, err := Run(ctx, s, false); err != nil || len(again.Steps) != 0 {
		t.Fatalf("re-run = %+v, %v", again, err)
	}
	two := f.open("two")
	if st, err := StatusOf(ctx, two); err != nil || st.FormatVersion != Latest() || len(older(st, GoalsFormat).Pending) != 0 {
		t.Fatalf("second machine = %+v, %v", st, err)
	}
	if r, err := Run(ctx, two, false); err != nil || len(r.Steps) != 0 {
		t.Fatalf("second machine run = %+v, %v", r, err)
	}
	if err := two.SetConfig(ctx, ConfigKey, "9"); err != nil {
		t.Fatal(err)
	}
	if r, err := Run(ctx, two, false); err != nil || changed(r) || r.FormatVersion != Latest() {
		t.Fatalf("second machine at 9 = %+v, %v", r, err)
	}

	// The kept keys give each original back byte for byte (what another
	// machine, which has no batch, would do).
	un := smallUnfold(t, ctx, s)
	if len(un) != len(smallFolded) {
		t.Fatalf("unfold made %d files", len(un))
	}
	for id, b := range un {
		if b != before[id] {
			t.Fatalf("unfold of %s:\n%s\nwant\n%s", id, b, before[id])
		}
	}

	// Undo puts every file back where it was, byte for byte.
	ur, err := Undo(ctx, s, so.Batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(ur.Docs) != len(smallFolded) || len(ur.Skipped) != 0 {
		t.Fatalf("undo = %+v", ur)
	}
	for id, rel := range smallRels {
		if got := raw(t, f, rel); got != before[id] {
			t.Fatalf("undo of %s:\n%s\nwant\n%s", id, got, before[id])
		}
	}
	if v, _ := s.Config(ConfigKey); v != "9" {
		t.Fatalf("format after undo = %q", v)
	}
}

func notesOf(t *testing.T, ctx context.Context, s store.Store) []string {
	t.Helper()
	for _, st := range Steps() {
		if st.ID == "notes-placement" {
			ids, _, err := matching(ctx, s, st, st.Select)
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(ids)
			return ids
		}
	}
	t.Fatal("no step notes-placement")
	return nil
}

// smallUnfold rebuilds each folded original from the kept keys. It renders
// them and writes nothing.
func smallUnfold(t *testing.T, ctx context.Context, s store.Store) map[string]string {
	t.Helper()
	out := map[string]string{}
	refs, err := s.List(ctx, store.Filter{All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		d, err := s.Get(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		var to model.Type
		set := map[string]any{"folded_from": nil}
		switch m := d.Meta.(type) {
		case *model.Group:
			to, set["kind"] = m.FoldedFrom, nil
		case *model.IO:
			to, set["state"], set["expects"], set["observed"] = m.FoldedFrom, nil, nil, nil
			if m.Observed {
				set["direction"] = string(model.InteractionObserved)
			}
		case *model.Task:
			to, set["kind"], set["status"] = m.FoldedFrom, nil, string(model.IncidentStatusOf(m.Status))
		}
		if to == "" {
			continue
		}
		folder := d.Folder
		u, err := model.Retype(d, to, set)
		if err != nil {
			t.Fatal(err)
		}
		if folder == model.FolderArchive {
			u.Folder = folder
		}
		b, err := model.Render(u)
		if err != nil {
			t.Fatal(err)
		}
		out[d.ID] = string(b)
	}
	return out
}

func TestSmallTypesStepRefusesAnOlderPara(t *testing.T) {
	f, s := smallFixture(t)
	ctx := context.Background()
	old, err := local.Open(f.root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "old",
		StateDir: filepath.Join(f.home, "old"), Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Record(ctx, OpSession, "", "v0.3.0", "9", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, s, false); err == nil || !strings.Contains(err.Error(), "format 9") {
		t.Fatalf("run with format 9 seen = %v", err)
	}
	if d, _ := s.Get(ctx, "team-atlas"); d.Meta.Header().Type != model.TypeTeam {
		t.Fatal("a refused run changed a file")
	}
}

// AsSmallType is what the older add ops write at format 10: the new form
// with no folded_from and no keys of the older type.
func TestAsSmallType(t *testing.T) {
	d := model.Document{ID: "domain-x", Folder: model.FolderOrg, Title: "X", Meta: &model.Domain{Common: model.Common{Type: model.TypeDomain}, Owner: "self"}}
	out, err := AsSmallType(d)
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := out.Meta.(*model.Group); !ok || g.Kind != model.GroupDomain || g.Owner != "self" || g.FoldedFrom != "" {
		t.Fatalf("domain = %+v", out.Meta)
	}
	b, _ := model.Render(out)
	if strings.Contains(string(b), "folded_from") {
		t.Fatalf("new group:\n%s", b)
	}
}
