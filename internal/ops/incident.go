package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// An incident is the record of one outage or fault in a system you run: how
// bad, when, what happened (a timeline in the body), and the follow-up tasks
// that stop it happening again. Para is not a pager. Alerts stay in
// monitoring; an incident is written by a person, or by an agent who was
// told, after the fact or while it runs. At vault format 10 an incident is a
// task of kind incident (model.IncidentOf reads both forms): todo is open,
// doing mitigated, done resolved. These ops read and write either form.

const timelineHeading = "## Timeline"

// IncidentView is an incident without its body.
type IncidentView struct {
	ID       string                 `json:"id"`
	Folder   model.Folder           `json:"folder"`
	Title    string                 `json:"title"`
	Severity model.IncidentSeverity `json:"severity"`
	Status   model.IncidentStatus   `json:"status"`
	Started  string                 `json:"started"`
	Ended    string                 `json:"ended,omitempty"`
	Area     string                 `json:"area,omitempty"`
	Links    []model.Pointer        `json:"links"`
	Rev      string                 `json:"rev"`
}

func incidentView(d model.Document) (IncidentView, error) {
	m, ok := model.IncidentOf(d)
	if !ok {
		return IncidentView{}, fmt.Errorf("%s is a %s, not an incident", d.ID, d.Meta.Header().Type)
	}
	return IncidentView{ID: d.ID, Folder: d.Folder, Title: d.Title, Severity: m.Severity, Status: m.Status,
		Started: m.Started, Ended: m.Ended, Area: m.Area.Target(), Links: nonNilPointers(m.Links), Rev: d.Rev}, nil
}

func getIncident(ctx context.Context, s store.Store, id string) (model.Document, *model.Incident, error) {
	d, err := s.Get(ctx, normID(id))
	if err != nil {
		return d, nil, missing("incident", normID(id), err)
	}
	m, ok := model.IncidentOf(d)
	if !ok {
		return d, nil, fmt.Errorf("%s is a %s, not an incident", d.ID, d.Meta.Header().Type)
	}
	return d, m, nil
}

// incidentOpen reports whether an incident still needs attention.
func incidentOpen(s model.IncidentStatus) bool { return s != model.IncidentResolved }

// loadIncidents returns every incident, open ones first, worst severity first,
// then latest start.
func loadIncidents(ctx context.Context, s store.Store) ([]IncidentView, error) {
	refs, err := refsOf(ctx, s, store.Filter{Type: model.TypeIncident}) // and the tasks of kind incident
	if err != nil {
		return nil, err
	}
	out := []IncidentView{}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		v, err := incidentView(doc)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	slices.SortStableFunc(out, func(a, b IncidentView) int {
		return cmp.Or(
			cmp.Compare(b2i(!incidentOpen(a.Status)), b2i(!incidentOpen(b.Status))),
			cmp.Compare(slices.Index(model.IncidentSeverities, a.Severity), slices.Index(model.IncidentSeverities, b.Severity)),
			cmp.Compare(b.Started, a.Started),
			cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// followUps returns the tasks that follow up an incident, by id.
func followUps(ctx context.Context, s store.Store, incident string) ([]TaskView, error) {
	all, err := listTasks(ctx, s, "", "")
	if err != nil {
		return nil, err
	}
	out := []TaskView{}
	for _, t := range all {
		if t.Incident == incident {
			out = append(out, t)
		}
	}
	return out, nil
}

// parseTime parses an RFC 3339 time with a zone.
func parseTime(field, v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
	if err != nil {
		return t, fmt.Errorf("%s %q: want RFC 3339 with a zone, such as 2026-11-01T09:30:00Z", field, v)
	}
	return t, nil
}

// addTimeline appends one timestamped line to the Timeline section of a body.
// A body with no Timeline section gets one at the end.
func addTimeline(body string, at time.Time, text string) string {
	line := "- " + at.UTC().Format(time.RFC3339) + " " + strings.Join(strings.Fields(text), " ")
	lines := strings.Split(body, "\n")
	start := slices.IndexFunc(lines, func(l string) bool { return strings.TrimRight(l, " \t") == timelineHeading })
	if start < 0 {
		if strings.TrimSpace(body) != "" {
			body = strings.TrimRight(body, "\n") + "\n\n"
		}
		return body + timelineHeading + "\n\n" + line
	}
	last := start
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "# ") || strings.HasPrefix(lines[i], "## ") {
			break
		}
		if strings.TrimSpace(lines[i]) != "" {
			last = i
		}
	}
	if last == start { // empty section: leave a blank line after the heading
		lines = slices.Insert(lines, start+1, "", line)
		return strings.Join(lines, "\n")
	}
	return strings.Join(slices.Insert(lines, last+1, line), "\n")
}

// IncidentAddInput is the input of incident_add.
type IncidentAddInput struct {
	Title    string                 `json:"title"`
	Severity model.IncidentSeverity `json:"severity"`          // sev1, sev2, or sev3
	Area     string                 `json:"area,omitempty"`    // the system's area id
	Started  string                 `json:"started,omitempty"` // RFC 3339; default now
	Body     string                 `json:"body,omitempty"`    // what is known, before the timeline
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
}

// IncidentAddOutput is the output of incident_add.
type IncidentAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// IncidentAdd opens an incident.
var IncidentAdd = defineSafe("incident_add", "Open an incident record for a system you run (para is not a pager: alerts stay in monitoring). At vault format 10 it is a task of kind incident: task_add {kind: incident, title, severity, area, body} does the same", addIncident)

// addIncident is incident_add, and task_add of kind incident.
func addIncident(ctx context.Context, d Deps, in IncidentAddInput) (IncidentAddOutput, error) {
	title, err := oneLine("title", in.Title)
	if err != nil {
		return IncidentAddOutput{}, err
	}
	if !slices.Contains(model.IncidentSeverities, in.Severity) {
		return IncidentAddOutput{}, fmt.Errorf("severity %q must be one of %v", in.Severity, model.IncidentSeverities)
	}
	started := d.Store.Now()
	if strings.TrimSpace(in.Started) != "" {
		if started, err = parseTime("started", in.Started); err != nil {
			return IncidentAddOutput{}, err
		}
	}
	area, err := areaLink(ctx, d.Store, in.Area)
	if err != nil {
		return IncidentAddOutput{}, err
	}
	body := addTimeline(strings.TrimSpace(in.Body), started, fmt.Sprintf("opened %s", in.Severity))
	// At the small types format an incident is a task of kind incident.
	doc, err := smallShape(d, model.Document{
		Folder: model.FolderIncidents, Title: title, Body: body,
		Meta: &model.Incident{
			Common:   model.Common{Type: model.TypeIncident},
			Severity: in.Severity, Status: model.IncidentOpen,
			Started: started.UTC().Format(time.RFC3339), Area: area,
		},
	})
	if err != nil {
		return IncidentAddOutput{}, err
	}
	doc, err = d.Store.Create(ctx, doc)
	if err != nil {
		return IncidentAddOutput{}, err
	}
	return IncidentAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
}

// IncidentListInput is the input of incident_list.
type IncidentListInput struct {
	Status model.IncidentStatus `json:"status,omitempty" query:"status"`
	Area   string               `json:"area,omitempty" query:"area"`
	All    bool                 `json:"all,omitempty" query:"all"` // include resolved incidents
}

// IncidentListOutput is the output of incident_list.
type IncidentListOutput struct {
	Incidents []IncidentView `json:"incidents"`
}

// IncidentList lists open and mitigated incidents, worst first. All, or a
// Status, includes resolved ones.
var IncidentList = defineRead("incident_list", "List incidents: open and mitigated by default, worst first", func(ctx context.Context, d Deps, in IncidentListInput) (IncidentListOutput, error) {
	if in.Status != "" && !slices.Contains(model.IncidentStatuses, in.Status) {
		return IncidentListOutput{}, fmt.Errorf("status %q must be one of %v", in.Status, model.IncidentStatuses)
	}
	all, err := loadIncidents(ctx, d.Store)
	if err != nil {
		return IncidentListOutput{}, err
	}
	area := normID(in.Area)
	out := IncidentListOutput{Incidents: []IncidentView{}}
	for _, v := range all {
		switch {
		case in.Status != "" && v.Status != in.Status,
			in.Status == "" && !in.All && !incidentOpen(v.Status),
			area != "" && v.Area != area:
			continue
		}
		out.Incidents = append(out.Incidents, v)
	}
	return out, nil
})

// IncidentShowInput is the input of incident_show.
type IncidentShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// IncidentShowOutput is the output of incident_show.
type IncidentShowOutput struct {
	Incident  IncidentView `json:"incident"`
	Body      string       `json:"body"`
	FollowUps []TaskView   `json:"follow_ups"`
}

// IncidentShow returns one incident with its timeline and follow-up tasks.
var IncidentShow = defineRead("incident_show", "Show an incident with its timeline and follow-up tasks", func(ctx context.Context, d Deps, in IncidentShowInput) (IncidentShowOutput, error) {
	doc, _, err := getIncident(ctx, d.Store, in.ID)
	if err != nil {
		return IncidentShowOutput{}, err
	}
	v, err := incidentView(doc)
	if err != nil {
		return IncidentShowOutput{}, err
	}
	fu, err := followUps(ctx, d.Store, doc.ID)
	return IncidentShowOutput{Incident: v, Body: doc.Body, FollowUps: fu}, err
})

// IncidentNoteInput is the input of incident_note.
type IncidentNoteInput struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	At   string `json:"at,omitempty"` // RFC 3339; default now
}

// IncidentChangeOutput is the output of incident_note and incident_status.
type IncidentChangeOutput struct {
	ID        string               `json:"id"`
	Status    model.IncidentStatus `json:"status"`
	Ended     string               `json:"ended,omitempty"`
	FollowUps int                  `json:"follow_ups"`
	Warnings  []string             `json:"warnings"`
	Rev       string               `json:"rev"`
}

// IncidentNote appends a timestamped line to the timeline. It works on a
// resolved incident too: the record may be written up afterward.
var IncidentNote = defineSafe("incident_note", "Append a timestamped line to an incident timeline", func(ctx context.Context, d Deps, in IncidentNoteInput) (IncidentChangeOutput, error) {
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" {
		return IncidentChangeOutput{}, errors.New("text is empty")
	}
	at := d.Store.Now()
	if strings.TrimSpace(in.At) != "" {
		var err error
		if at, err = parseTime("at", in.At); err != nil {
			return IncidentChangeOutput{}, err
		}
	}
	doc, m, err := getIncident(ctx, d.Store, in.ID)
	if err != nil {
		return IncidentChangeOutput{}, err
	}
	doc.Body = addTimeline(doc.Body, at, text)
	return saveIncident(ctx, d, doc, m, nil)
})

// IncidentStatusInput is the input of incident_status.
type IncidentStatusInput struct {
	ID     string               `json:"id"`
	Status model.IncidentStatus `json:"status"`
	Text   string               `json:"text,omitempty"` // added to the timeline line
}

// IncidentStatus moves an incident to open, mitigated, or resolved, and adds a
// timeline line. Resolving sets the end time, and warns when no task follows
// the incident up. Reopening clears the end time.
var IncidentStatus = define("incident_status", "Set an incident to open, mitigated, or resolved; resolving warns when no follow-up task exists", func(ctx context.Context, d Deps, in IncidentStatusInput) (IncidentChangeOutput, error) {
	if !slices.Contains(model.IncidentStatuses, in.Status) {
		return IncidentChangeOutput{}, fmt.Errorf("status %q must be one of %v", in.Status, model.IncidentStatuses)
	}
	doc, m, err := getIncident(ctx, d.Store, in.ID)
	if err != nil {
		return IncidentChangeOutput{}, err
	}
	if m.Status == in.Status {
		return IncidentChangeOutput{}, fmt.Errorf("%s is already %s", doc.ID, m.Status)
	}
	now := d.Store.Now()
	m.Status = in.Status
	m.Ended = ""
	if in.Status == model.IncidentResolved {
		m.Ended = now.UTC().Format(time.RFC3339)
	}
	line := "status " + string(in.Status)
	if t := strings.Join(strings.Fields(in.Text), " "); t != "" {
		line += ": " + t
	}
	doc.Body = addTimeline(doc.Body, now, line)
	return saveIncident(ctx, d, doc, m, func(out *IncidentChangeOutput) {
		if in.Status == model.IncidentResolved && out.FollowUps == 0 {
			out.Warnings = append(out.Warnings, "no follow-up task: add one with `para task add --incident "+doc.ID+"`, or accept that nothing will change")
		}
	})
})

func saveIncident(ctx context.Context, d Deps, doc model.Document, m *model.Incident, after func(*IncidentChangeOutput)) (IncidentChangeOutput, error) {
	model.SetIncident(doc, m) // a task of kind incident holds the fields
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return IncidentChangeOutput{}, err
	}
	fu, err := followUps(ctx, d.Store, doc.ID)
	if err != nil {
		return IncidentChangeOutput{}, err
	}
	out := IncidentChangeOutput{ID: doc.ID, Status: m.Status, Ended: m.Ended, FollowUps: len(fu), Warnings: []string{}, Rev: saved.Rev}
	if after != nil {
		after(&out)
	}
	return out, nil
}

// The link ops of an incident share the area and project implementation.
var (
	IncidentLinkAdd    = defineSafe("incident_link_add", "Point an incident at a dashboard, runbook, or ticket that lives elsewhere", linkAdd(model.TypeIncident))
	IncidentLinkRemove = defineSafe("incident_link_remove", "Remove a link from an incident by label", linkRemove(model.TypeIncident))
)
