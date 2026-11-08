package ops

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// TaskView is a task without its body. Project, Area, and DependsOn hold ids.
type TaskView struct {
	ID       string           `json:"id"`
	Folder   model.Folder     `json:"folder"`
	Title    string           `json:"title"`
	Status   model.TaskStatus `json:"status"`
	Project  string           `json:"project"`
	Area     string           `json:"area,omitempty"`
	Created  model.Date       `json:"created"`
	Updated  model.Date       `json:"updated,omitempty"`
	Due      model.Date       `json:"due,omitempty"`
	Recur    string           `json:"recur,omitempty"`
	Assignee string           `json:"assignee,omitempty"` // empty means self
	// RequestedBy is who the task is for: a person or group link, or free text.
	RequestedBy string `json:"requested_by,omitempty"`
	Previous    string `json:"previous,omitempty"`  // the task this one recurred from
	Incident    string `json:"incident,omitempty"`  // the incident this task follows up
	DoneWhen    string `json:"done_when,omitempty"` // how we know it is done
	// DoURL, DoCommand, and Steps say how the owner does it: the page, the
	// exact command, a short markdown list.
	DoURL     string `json:"do_url,omitempty"`
	DoCommand string `json:"do_command,omitempty"`
	Steps     string `json:"steps,omitempty"`
	// Parent is the task this one is an action item of. The parent does not wait
	// on it. Items counts this task's open action items; Commitment is true for
	// a task tagged commitment.
	Parent     string `json:"parent,omitempty"`
	Items      int    `json:"items"`
	Commitment bool   `json:"commitment,omitempty"`
	// Kind is task, meeting, call, work_with, or commitment (the older
	// commitment tag reads as commitment). Scheduled is a date or a start with
	// its zone; End is the end of a timed one; With names the people.
	Kind      model.TaskKind `json:"kind"`
	Scheduled string         `json:"scheduled,omitempty"`
	End       string         `json:"end,omitempty"`
	With      []string       `json:"with"`
	// Optional and Attend are a meeting's attendance: whether the owner may
	// skip it, and whether they go (yes, no, maybe; empty is undecided).
	Optional bool         `json:"optional,omitempty"`
	Attend   model.Attend `json:"attend,omitempty"`
	// Attempts counts the call attempts in ## Attempts; LastAttempt is the
	// result of the latest (no_answer, left_message, reached). Stuck is true
	// for an open call at the attempt cap.
	Attempts    int    `json:"attempts,omitempty"`
	LastAttempt string `json:"last_attempt,omitempty"`
	Stuck       bool   `json:"stuck,omitempty"`
	// Outcome is how a closed task ended: done, partial, dropped, duplicate, or
	// superseded. Empty on an open task and on one closed before outcomes.
	Outcome model.TaskOutcome `json:"outcome,omitempty"`
	// DeclinedBy, DeclinedPerson, RetryAfter, and ReroutedTo are the
	// outcome fields of a declined, failed, or rerouted task.
	DeclinedBy     model.DeclinedBy `json:"declined_by,omitempty"`
	DeclinedPerson string           `json:"declined_person,omitempty"`
	RetryAfter     model.Date       `json:"retry_after,omitempty"`
	ReroutedTo     string           `json:"rerouted_to,omitempty"`
	DependsOn      []string         `json:"depends_on"`
	BlockedBy      []string         `json:"blocked_by"`
	Rev            string           `json:"rev"`
	// NeedsYou is ops.NeedsYou for the row: open, not put off, not an
	// agent's turn, and the owner's turn. Reason says why, in words.
	NeedsYou bool   `json:"needs_you"`
	Reason   string `json:"reason,omitempty"`
	// Deferred is true while a person has put the task off (itself, or its
	// goal). DeferUntil is the date or "someday". It still blocks its dependents.
	Deferred bool `json:"deferred"`
	// Returns is the follow-up date of a blocked task (its defer_until):
	// next and the agenda list it again on that day.
	Returns        string `json:"returns,omitempty"`
	DeferUntil     string `json:"defer_until,omitempty"`
	DeferredReason string `json:"deferred_reason,omitempty"`
	// WaitsOn is the part of DeferredAfter still open: the item is under Later
	// until they resolve. ReadyWhy is set for 7 days once they have, "ready: X done".
	WaitsOn      []string `json:"waits_on"`
	ReadyWhy     string   `json:"ready_why,omitempty"`
	WaitsOnNames []string `json:"waits_on_names,omitempty" nameof:"WaitsOn"`
	// Names are for people: the title of what the id fields point at, "" when
	// unknown. DependsOnNames has one entry per DependsOn.
	ProjectName    string   `json:"project_name,omitempty" nameof:"Project"`
	AreaName       string   `json:"area_name,omitempty" nameof:"Area"`
	AssigneeName   string   `json:"assignee_name,omitempty" nameof:"Assignee"`
	IncidentName   string   `json:"incident_name,omitempty" nameof:"Incident"`
	ParentName     string   `json:"parent_name,omitempty" nameof:"Parent"`
	DependsOnNames []string `json:"depends_on_names,omitempty" nameof:"DependsOn"`
	WithNames      []string `json:"with_names,omitempty" nameof:"With"`
}

func taskView(d model.Document) (TaskView, error) {
	d, _ = meetingAsTask(d) // an older `type: meeting` document reads as a task of kind meeting
	t, ok := d.Meta.(*model.Task)
	if !ok {
		return TaskView{}, fmt.Errorf("%s is a %s, not a task", d.ID, d.Meta.Header().Type)
	}
	v := TaskView{
		ID: d.ID, Folder: d.Folder, Title: d.Title, Status: t.Status,
		Project: t.Project.Target(), Area: t.Area.Target(), Created: t.Created, Updated: t.Updated, Due: t.Due, Recur: t.Recur, Assignee: t.Assignee, RequestedBy: t.RequestedBy, Previous: t.Previous.Target(), Incident: t.Incident.Target(), Parent: t.Parent.Target(), Commitment: t.EffectiveKind() == model.KindCommitment, Kind: t.EffectiveKind(), Scheduled: t.Scheduled, End: t.End, With: targets(t.With), DoneWhen: t.DoneWhen, DoURL: t.DoURL, DoCommand: t.DoCommand, Steps: t.Steps, Outcome: t.Outcome, DeclinedBy: t.DeclinedBy, DeclinedPerson: t.DeclinedPerson.Target(), RetryAfter: t.RetryAfter, ReroutedTo: t.ReroutedTo, DependsOn: []string{}, BlockedBy: []string{}, Rev: d.Rev,
		DeferUntil: t.DeferUntil, DeferredReason: t.DeferredReason, WaitsOn: []string{}, Optional: t.Optional, Attend: t.Attend,
	}
	for _, l := range t.DependsOn {
		v.DependsOn = append(v.DependsOn, l.Target())
	}
	v.BlockedBy = append(v.BlockedBy, t.BlockedBy...)
	if v.Kind == model.KindCall {
		v.Attempts, v.LastAttempt = callAttempts(d.Body)
		v.Stuck = v.Attempts >= CallAttemptCap && !taskTerminal(t.Status) && v.LastAttempt != CallReached
	}
	return v, nil
}

func taskTerminal(s model.TaskStatus) bool { return s == model.TaskDone || s == model.TaskDropped }

// getTask reads a document that must be a task.
func getTask(ctx context.Context, s store.Store, id string) (model.Document, *model.Task, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return d, nil, missing("task", id, err)
	}
	t, ok := d.Meta.(*model.Task)
	if !ok {
		return d, nil, fmt.Errorf("%s is a %s, not a task", id, d.Meta.Header().Type)
	}
	return d, t, nil
}

// listTasks returns tasks sorted by id, filtered by project id and status
// when not empty.
func listTasks(ctx context.Context, s store.Store, project string, status model.TaskStatus) ([]TaskView, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeTask})
	if err != nil {
		return nil, err
	}
	// Older `type: meeting` documents are tasks of kind meeting, and an
	// obligation a task of its kind.
	for _, typ := range []model.Type{model.TypeMeeting, model.TypeObligation} {
		old, err := s.List(ctx, store.Filter{Type: typ})
		if err != nil {
			return nil, err
		}
		refs = append(refs, old...)
	}
	all := make([]TaskView, 0, len(refs))
	items := map[string]int{} // parent id -> open action items
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if model.IsIncident(doc) {
			continue // an incident reads as an incident (incident_list), not as a task
		}
		v, err := taskView(doc)
		if err != nil {
			return nil, err
		}
		if v.Parent != "" && !taskTerminal(v.Status) {
			items[v.Parent]++
		}
		all = append(all, v)
	}
	out := []TaskView{}
	for _, v := range all {
		v.Items = items[v.ID]
		if (project == "" || v.Project == project) && (status == "" || v.Status == status) {
			out = append(out, v)
		}
	}
	return out, nil
}

// TaskAddInput is the input of task_add.
type TaskAddInput struct {
	Project  string `json:"project,omitempty"` // project id; a task needs a project or an area
	Area     string `json:"area,omitempty"`    // area id
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Due      string `json:"due,omitempty"`      // YYYY-MM-DD
	Recur    string `json:"recur,omitempty"`    // daily, weekly, monthly, quarterly, yearly, or every <n> days
	Assignee string `json:"assignee,omitempty"` // self (default), a person id, or agent:<name>
	// RequestedBy is who the task is for: a person or group id, or free text
	// (a role, an address). Optional.
	RequestedBy string   `json:"requested_by,omitempty"`
	Incident    string   `json:"incident,omitempty"`   // incident id this task follows up; with no project or area the task goes in the incident's area
	DoneWhen    string   `json:"done_when,omitempty"`  // how we know it is done; required from an agent
	DependsOn   []string `json:"depends_on,omitempty"` // task, obligation, rfx, question, goal, or agent work ids
	// DoURL is the page where the owner does it, DoCommand the exact command
	// to run (no secret in it), Steps a short markdown list. An agent that
	// adds a task for the owner gives at least one when it can find out how.
	DoURL     string `json:"do_url,omitempty"`
	DoCommand string `json:"do_command,omitempty"`
	Steps     string `json:"steps,omitempty"`
	// Effort is about how long it takes the owner: xs (under 15 min), s
	// (under an hour), m (half a day), l (a day or more). The value order uses it.
	Effort model.Effort `json:"effort,omitempty"`
	// Parent is the id of an open task or obligation this one is an action
	// item of: prep for a meeting or a promise, or what came out of it. The
	// parent does not wait on it. With no project or area the task takes the
	// parent's.
	Parent string `json:"parent,omitempty"`
	// Commitment is kind commitment: something owed to someone. It needs a due
	// date; the owner is the assignee (default self).
	Commitment bool `json:"commitment,omitempty"`
	// Kind is task (default), meeting, call, work_with, or commitment. A
	// meeting, call, or work_with is with someone (With) and needs no project
	// or area for a meeting or call; work_with needs done_when.
	Kind model.TaskKind `json:"kind,omitempty"`
	// Scheduled is when it happens: a date, or a start time with a zone. End is
	// the end time, with a timed Scheduled. They are not Due.
	Scheduled string   `json:"scheduled,omitempty"`
	End       string   `json:"end,omitempty"`
	With      []string `json:"with,omitempty"` // person or group ids
	// CalendarUID is the UID of the calendar event of a meeting or a call, so
	// calendar_compare matches it exactly (role primary); CalendarUIDs are
	// more events of it, each with a role in free text.
	CalendarUID  string                `json:"calendar_uid,omitempty"`
	CalendarUIDs []model.CalendarEvent `json:"calendar_uids,omitempty"`
	// Severity is kind incident: sev1 (the worst), sev2, or sev3. An
	// incident takes title, severity, area, and body only; it starts now and
	// is what incident_add makes.
	Severity model.IncidentSeverity `json:"severity,omitempty"`
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
}

// TaskAddOutput is the output of task_add.
type TaskAddOutput struct {
	ID      string       `json:"id"`
	Folder  model.Folder `json:"folder"`
	Rev     string       `json:"rev"`
	Request string       `json:"request,omitempty"` // request made for a recurring agent task
	// Warnings: an unknown recur is stored, and lint warns.
	Warnings []string `json:"warnings"`
}

// TaskAdd creates a todo task in a project, an area, or both. Tasks live in
// projects/. A dependency may be any task or rfx, in any project or area.
var TaskAdd = defineSafe("task_add", "Create a task in a project or an area (at least one). A task for the owner says how to do it: do_url (the page where it is done), do_command (the exact command), or steps (1 to 5 lines); prepare drafts, config, and the command first. A secret never goes in them: say where the owner pastes it", func(ctx context.Context, d Deps, in TaskAddInput) (TaskAddOutput, error) {
	if in.Kind == model.KindIncident {
		return addIncidentTask(ctx, d, in)
	}
	if in.Severity != "" {
		return TaskAddOutput{}, errors.New("severity is for kind incident")
	}
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return TaskAddOutput{}, errors.New("title is empty")
	}
	due, err := parseDue(in.Due)
	if err != nil {
		return TaskAddOutput{}, err
	}
	if err := checkEffort(in.Effort); err != nil {
		return TaskAddOutput{}, err
	}
	doneWhen, err := checkDoneWhen(in.DoneWhen, doneWhenNeeded(d))
	if err != nil {
		return TaskAddOutput{}, fmt.Errorf("%w (every task an agent writes says how we know it is done)", err)
	}
	how, err := checkHow(in.DoURL, in.DoCommand, in.Steps)
	if err != nil {
		return TaskAddOutput{}, err
	}
	in.Project = normID(in.Project)
	var parent model.Link
	if id := normID(in.Parent); id != "" {
		p, err := parentOf(ctx, d, id)
		if err != nil {
			return TaskAddOutput{}, err
		}
		parent = p.link
		if in.Project == "" && normID(in.Area) == "" {
			in.Project, in.Area = p.project.Target(), p.area.Target()
		}
	}
	kind, err := kindOf(in)
	if err != nil {
		return TaskAddOutput{}, err
	}
	scheduled, end, with, err := checkKindFields(ctx, d, kind, in, due)
	if err != nil {
		return TaskAddOutput{}, err
	}
	uids, err := calendarEvents(in.CalendarUID, in.CalendarUIDs)
	if err != nil {
		return TaskAddOutput{}, err
	}
	if len(uids) > 0 && kind != model.KindMeeting && kind != model.KindCall {
		return TaskAddOutput{}, errors.New("calendar_uid: only a meeting or a call has a calendar event")
	}
	var incident model.Link
	if id := normID(in.Incident); id != "" {
		idoc, im, err := getIncident(ctx, d.Store, id)
		if err != nil {
			return TaskAddOutput{}, linkErr("incident", id, err)
		}
		incident = model.LinkTo(idoc.ID)
		if in.Project == "" && normID(in.Area) == "" {
			in.Area = im.Area.Target()
		}
	}
	if in.Project == "" && normID(in.Area) == "" && kind != model.KindMeeting && kind != model.KindCall {
		return TaskAddOutput{}, errors.New("a task needs a project or an area")
	}
	var project model.Link
	if in.Project != "" {
		proj, _, err := getProject(ctx, d.Store, in.Project)
		if err != nil {
			return TaskAddOutput{}, err
		}
		if proj.Folder == model.FolderArchive {
			return TaskAddOutput{}, fmt.Errorf("project %s is archived", in.Project)
		}
		project = model.LinkTo(in.Project)
	}
	area, err := areaLink(ctx, d.Store, in.Area)
	if err != nil {
		return TaskAddOutput{}, err
	}
	assignee, err := resolveAssignee(ctx, d, in.Assignee)
	if err != nil {
		return TaskAddOutput{}, err
	}
	requestedBy, err := ioParty(ctx, d, "requested_by", in.RequestedBy)
	if err != nil {
		return TaskAddOutput{}, err
	}
	var deps []model.Link
	for _, id := range in.DependsOn {
		l, err := dependencyLink(ctx, d.Store, id)
		if err != nil {
			return TaskAddOutput{}, err
		}
		if !slices.Contains(deps, l) {
			deps = append(deps, l)
		}
	}
	doc, err := storedShape(d, model.Document{
		Folder: model.FolderProjects,
		Title:  title,
		Body:   demoteH1(in.Body),
		Meta: &model.Task{
			Common:      model.Common{Type: model.TypeTask},
			Status:      model.TaskTodo,
			Parent:      parent,
			Project:     project,
			Area:        area,
			Due:         due,
			Recur:       strings.Join(strings.Fields(in.Recur), " "),
			Assignee:    assignee,
			RequestedBy: requestedBy,
			Incident:    incident,
			DoneWhen:    doneWhen,
			DoURL:       how.url,
			DoCommand:   how.command,
			Steps:       how.steps,
			DependsOn:   deps,
			Effort:      in.Effort,
			Kind:        kind,
			Occasion:    model.Occasion{Scheduled: scheduled, End: end, With: with, CalendarUIDs: uids},
		},
	})
	if err != nil {
		return TaskAddOutput{}, err
	}
	folded := doc.Meta.Header().Type == model.TypeObligation
	if doc, err = d.Store.Create(ctx, doc); err != nil {
		return TaskAddOutput{}, err
	}
	out := TaskAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev, Warnings: []string{}}
	if folded {
		out.Warnings = append(out.Warnings, "kind "+string(kind)+" is an obligation now: stored as one in obligations/; this is an alias of obligation_add, use it")
	}
	if w := recurWarning(in.Recur); w != "" {
		out.Warnings = append(out.Warnings, w)
	}
	if doneWhenNeeded(d) && (kind == "" || kind == model.KindTask) && assignee == "" && how.empty() {
		out.Warnings = append(out.Warnings, "say how to do it: add do_url (the page where it is done), do_command (the exact command), or steps (1 to 5 lines) with task_edit "+doc.ID+", and prepare what you can first")
	}
	if (kind == "" || kind == model.KindTask) && readsLikeWaiting(doc.Title) {
		out.Warnings = append(out.Warnings, waitingWarning(doc.ID))
	}
	out.Request, out.Warnings = agentRequest(ctx, d, doc, out.Warnings)
	return out, nil
})

// addIncidentTask is task_add of kind incident: incident_add with the
// fields an incident takes. Below vault format 10 it writes an incident file.
func addIncidentTask(ctx context.Context, d Deps, in TaskAddInput) (TaskAddOutput, error) {
	rest := in
	rest.Kind, rest.Title, rest.Severity, rest.Area, rest.Body, rest.ImportedFrom = "", "", "", "", "", nil
	if !reflect.ValueOf(rest).IsZero() {
		return TaskAddOutput{}, errors.New("kind incident takes title, severity, area, and body only (incident_status, incident_note, and incident_link_add do the rest)")
	}
	r, err := addIncident(ctx, d, IncidentAddInput{Title: in.Title, Severity: in.Severity, Area: in.Area, Body: in.Body, ImportedFrom: in.ImportedFrom})
	if err != nil {
		return TaskAddOutput{}, err
	}
	return TaskAddOutput{ID: r.ID, Folder: r.Folder, Rev: r.Rev, Warnings: []string{}}, nil
}

// TaskListInput is the input of task_list.
type TaskListInput struct {
	Project string           `json:"project,omitempty" query:"project"`
	Area    string           `json:"area,omitempty" query:"area"` // tasks with this area of their own
	Status  model.TaskStatus `json:"status,omitempty" query:"status"`
	// Open keeps the tasks that are todo, doing, or blocked. It cannot be
	// combined with status.
	Open bool `json:"open,omitempty" query:"open"`
	// Assignee keeps tasks assigned to this person id or agent:<name>; self
	// keeps the tasks with no assignee.
	Assignee string `json:"assignee,omitempty" query:"assignee"`
	Incident string `json:"incident,omitempty" query:"incident"` // follow-ups of this incident
	// Deferred includes the tasks a person put off (Later). They are left out
	// by default; each is marked.
	Deferred bool `json:"deferred,omitempty" query:"deferred"`
	// Outcome keeps closed tasks with this outcome, such as failed.
	Outcome model.TaskOutcome `json:"outcome,omitempty" query:"outcome"`
}

// TaskListOutput is the output of task_list.
type TaskListOutput struct {
	Tasks []TaskView `json:"tasks"`
	// DeferredHidden counts the tasks that match the filters but are left out
	// because a person put them off (Later). Pass deferred to list them.
	DeferredHidden int `json:"deferred_hidden,omitempty"`
}

// TaskList lists tasks.
var TaskList = defineRead("task_list", "List tasks; open: true lists every task that is todo, doing, or blocked in one call; outcome (such as failed or declined) lists closed tasks that ended so", func(ctx context.Context, d Deps, in TaskListInput) (TaskListOutput, error) {
	if in.Status != "" && !slices.Contains(model.TaskStatuses, in.Status) {
		return TaskListOutput{}, fmt.Errorf("unknown status %q", in.Status)
	}
	if in.Open && in.Status != "" {
		return TaskListOutput{}, errors.New("open and status do not combine: open is todo, doing, or blocked")
	}
	if in.Outcome != "" && !slices.Contains(model.TaskOutcomes, in.Outcome) {
		return TaskListOutput{}, fmt.Errorf("unknown outcome %q: want one of %v", in.Outcome, model.TaskOutcomes)
	}
	tasks, err := listTasks(ctx, d.Store, in.Project, in.Status)
	if err != nil {
		return TaskListOutput{}, err
	}
	if in.Open {
		tasks = slices.DeleteFunc(tasks, func(t TaskView) bool {
			return t.Status != model.TaskTodo && t.Status != model.TaskDoing && t.Status != model.TaskBlocked
		})
	}
	if in.Outcome != "" {
		tasks = slices.DeleteFunc(tasks, func(t TaskView) bool { return t.Outcome != in.Outcome })
	}
	if area := normID(in.Area); area != "" {
		tasks = slices.DeleteFunc(tasks, func(t TaskView) bool { return t.Area != area })
	}
	if inc := normID(in.Incident); inc != "" {
		tasks = slices.DeleteFunc(tasks, func(t TaskView) bool { return t.Incident != inc })
	}
	if want := strings.TrimSpace(in.Assignee); want != "" {
		if strings.EqualFold(want, SelfAssignee) {
			want = ""
		} else if want, err = resolveAssignee(ctx, d, want); err != nil {
			return TaskListOutput{}, err
		}
		tasks = slices.DeleteFunc(tasks, func(t TaskView) bool { return t.Assignee != want })
	}
	if tasks, err = markDeferred(ctx, d.Store, tasks, true); err != nil {
		return TaskListOutput{}, err
	}
	// A blocked task stays listed while it waits for its follow-up date: only
	// next and the agenda leave it out until then.
	today := d.Store.Now().Format(dateLayout)
	hidden := 0
	tasks = slices.DeleteFunc(tasks, func(t TaskView) bool {
		drop := t.Deferred && !in.Deferred && t.Status != model.TaskBlocked
		if drop {
			hidden++
		}
		return drop
	})
	for i := range tasks {
		if t := &tasks[i]; t.Status == model.TaskBlocked && t.DeferUntil != "" && t.DeferUntil != model.SomedayWord && t.DeferUntil > today {
			t.Returns = t.DeferUntil
		}
	}
	return TaskListOutput{Tasks: tasks, DeferredHidden: hidden}, nil
})

// TaskShowInput is the input of task_show.
type TaskShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// TaskShowOutput is the output of task_show.
type TaskShowOutput struct {
	Task TaskView `json:"task"`
	Body string   `json:"body"`
	// Questions are the open questions scoped to this task.
	Questions []QuestionView `json:"questions"`
	// Parts are the sections computed from links. Updates is the owner's text
	// under ## Updates, as written.
	Parts   TaskParts `json:"parts"`
	Updates string    `json:"updates,omitempty"`
	// Rules are the conventions and constraints in effect (rules_in_effect).
	Rules []RuleRef `json:"rules"`
	// Assumptions are the task's assumptions, with ## Assumptions body lines
	// read as never checked.
	Assumptions []AssumptionView `json:"assumptions"`
	// Produce is set on an obligation an agent owes: what it produces, and
	// the last artifact and its state.
	Produce *ProduceView `json:"produce,omitempty"`
}

// TaskShow returns one task.
var TaskShow = defineRead("task_show", "Show a task", func(ctx context.Context, d Deps, in TaskShowInput) (TaskShowOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return TaskShowOutput{}, err
	}
	switch doc.Meta.(type) {
	case *model.Task, *model.Meeting, *model.Obligation:
	default:
		return TaskShowOutput{}, fmt.Errorf("%s is a %s, not a task", doc.ID, doc.Meta.Header().Type)
	}
	v, err := taskView(doc)
	if err != nil {
		return TaskShowOutput{}, err
	}
	qs, err := openQuestions(ctx, d.Store, func(x qrow) bool { return x.v.Scope == doc.ID })
	if err != nil {
		return TaskShowOutput{}, err
	}
	parts, err := taskParts(ctx, d.Store, doc.ID)
	if err != nil {
		return TaskShowOutput{}, err
	}
	for _, a := range parts.ActionItems {
		if a.Status != string(model.TaskDone) && a.Status != string(model.TaskDropped) {
			v.Items++
		}
	}
	for _, a := range parts.Commitments {
		if a.Status != string(model.TaskDone) && a.Status != string(model.TaskDropped) {
			v.Items++
		}
	}
	rules, err := rulesShort(ctx, d.Store, doc.ID)
	if err != nil {
		return TaskShowOutput{}, err
	}
	cutoff, err := recheckCutoff(d.Store)
	if err != nil {
		return TaskShowOutput{}, err
	}
	as := assumptionsOf(doc, cutoff)
	if as == nil {
		as = []AssumptionView{}
	}
	pv, err := produceView(ctx, d, doc)
	if err != nil {
		return TaskShowOutput{}, err
	}
	return TaskShowOutput{Task: v, Body: doc.Body, Questions: qs, Parts: parts, Updates: updatesText(doc.Body), Rules: rules, Assumptions: as, Produce: pv}, nil
})

// TaskChangeOutput is the output of task_start, task_done, and task_block.
type TaskChangeOutput struct {
	ID        string           `json:"id"`
	Status    model.TaskStatus `json:"status"`
	BlockedBy []string         `json:"blocked_by"`
	Rev       string           `json:"rev"`
}

// transition applies fn to a task, then saves it. fn may refuse.
func transition(ctx context.Context, d Deps, id string, fn func(doc model.Document, t *model.Task) error) (TaskChangeOutput, error) {
	doc, t, err := getTask(ctx, d.Store, id)
	if err != nil {
		return TaskChangeOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskChangeOutput{}, fmt.Errorf("%s is already %s", id, t.Status)
	}
	if err := fn(doc, t); err != nil {
		return TaskChangeOutput{}, err
	}
	// Unblocked: the follow-up date the block set goes with it. A deferral
	// the owner made with defer stays.
	if t.Status != model.TaskBlocked && blockDeferral(t) {
		t.DeferUntil, t.DeferredReason = "", ""
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskChangeOutput{}, err
	}
	return TaskChangeOutput{ID: id, Status: t.Status, BlockedBy: append([]string{}, t.BlockedBy...), Rev: saved.Rev}, nil
}

// blockMarker starts the deferred_reason of a defer_until that task_block
// until set, so unblocking clears that one and no other.
const blockMarker = "blocked: "

// blockDeferral reports whether the task's deferral came from task_block.
func blockDeferral(t *model.Task) bool {
	return t.DeferUntil != "" && strings.HasPrefix(t.DeferredReason, blockMarker)
}

// TaskStartInput is the input of task_start.
type TaskStartInput struct {
	ID    string `json:"id"`
	Force bool   `json:"force,omitempty"` // start with unfinished dependencies
}

// TaskStart moves a todo or blocked task to doing and clears its blockers.
// Unfinished dependencies refuse the start unless Force.
var TaskStart = defineSafe("task_start", "Start a task", func(ctx context.Context, d Deps, in TaskStartInput) (TaskChangeOutput, error) {
	return transition(ctx, d, in.ID, func(_ model.Document, t *model.Task) error {
		if t.Status == model.TaskDoing {
			return fmt.Errorf("%s is already doing", in.ID)
		}
		if !in.Force {
			var open []string
			for _, l := range model.HardDeps(t) {
				dep, err := d.Store.Get(ctx, l.Target())
				if err != nil || !resolved(dep) {
					open = append(open, l.Target())
				}
			}
			if len(open) > 0 {
				return fmt.Errorf("%s waits on %s (use force)", in.ID, strings.Join(open, " "))
			}
		}
		t.Status = model.TaskDoing
		t.BlockedBy = nil
		return nil
	})
})

// TaskBlockInput is the input of task_block.
type TaskBlockInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason"` // why: who said it, and where it applies
	// Until is the follow-up date, YYYY-MM-DD: it sets defer_until, so the
	// blocked task leaves next and the agenda until then and comes back on
	// the day. task_list still lists it, with "returns <date>".
	Until string `json:"until,omitempty"`
}

// TaskBlock marks a task blocked and records the reason. Blocking a blocked
// task adds a reason.
var TaskBlock = defineSafe("task_block", "Block a task with a reason; until (YYYY-MM-DD) is the follow-up date: it sets defer_until, so the task leaves next and the agenda until then and comes back on the day (task_list still lists it). The due date never does this", func(ctx context.Context, d Deps, in TaskBlockInput) (TaskChangeOutput, error) {
	reason := strings.Join(strings.Fields(in.Reason), " ")
	if reason == "" {
		return TaskChangeOutput{}, errors.New("reason is empty")
	}
	until, err := parseDate("until", in.Until)
	if err != nil {
		return TaskChangeOutput{}, err
	}
	return transition(ctx, d, in.ID, func(_ model.Document, t *model.Task) error {
		if until != "" {
			if t.DeferUntil != "" && !blockDeferral(t) {
				return fmt.Errorf("%s is put off until %s by defer: undefer it first, or block it without until", in.ID, t.DeferUntil)
			}
			t.DeferUntil, t.DeferredReason = string(until), blockMarker+reason
		}
		t.Status = model.TaskBlocked
		if !slices.Contains(t.BlockedBy, reason) {
			t.BlockedBy = append(t.BlockedBy, reason)
		}
		return nil
	})
})

// parseDue checks a YYYY-MM-DD date. Empty gives no date.
func parseDue(s string) (model.Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", fmt.Errorf("due %q: want a date YYYY-MM-DD", s)
	}
	return model.Date(s), nil
}

// parseDate checks an optional YYYY-MM-DD date. Empty gives no date.
func parseDate(name, s string) (model.Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", fmt.Errorf("%s %q: want a date YYYY-MM-DD", name, s)
	}
	return model.Date(s), nil
}

// TaskDueInput is the input of task_due.
type TaskDueInput struct {
	ID    string `json:"id"`
	Date  string `json:"date,omitempty"`  // YYYY-MM-DD; empty with Clear removes the date
	Clear bool   `json:"clear,omitempty"` // remove the due date
	Recur string `json:"recur,omitempty"` // daily, weekly, monthly, quarterly, yearly, or every <n> days
}

// TaskDueOutput is the output of task_due.
type TaskDueOutput struct {
	ID    string     `json:"id"`
	Due   model.Date `json:"due,omitempty"`
	Recur string     `json:"recur,omitempty"`
	Rev   string     `json:"rev"`
}

// TaskDue sets or clears the due date of an open task, and optionally its
// recurrence. The recurrence acts when the task is marked done.
var TaskDue = defineSafe("task_due", "Set or clear the due date of a task", func(ctx context.Context, d Deps, in TaskDueInput) (TaskDueOutput, error) {
	date, err := parseDue(in.Date)
	if err != nil {
		return TaskDueOutput{}, err
	}
	if (date == "") == !in.Clear {
		return TaskDueOutput{}, errors.New("give a date, or clear to remove it")
	}
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskDueOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskDueOutput{}, fmt.Errorf("%s is already %s", doc.ID, t.Status)
	}
	t.Due = date
	if recur := strings.Join(strings.Fields(in.Recur), " "); recur != "" {
		t.Recur = recur
	}
	if in.Clear {
		t.Recur = ""
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskDueOutput{}, err
	}
	return TaskDueOutput{ID: doc.ID, Due: t.Due, Recur: t.Recur, Rev: saved.Rev}, nil
})

// dependencyLink checks that id names a task, obligation, rfx, question, goal,
// or agent work, in any project or area.
func dependencyLink(ctx context.Context, s store.Store, raw string) (model.Link, error) {
	id := normID(raw)
	dep, err := s.Get(ctx, id)
	if err != nil {
		return "", linkErr("depends_on", id, err)
	}
	if typ := dep.Meta.Header().Type; typ != model.TypeTask && typ != model.TypeObligation && typ != model.TypeRFX && typ != model.TypeQuestion && typ != model.TypeProject && typ != model.TypeRequest {
		return "", fmt.Errorf("depends_on %s is a %s: want a task, obligation, rfx, question, goal, or agent work", id, typ)
	}
	return model.LinkTo(id), nil
}

// dependencyPath returns a chain of dependencies from "from" to "to" (both
// ends included), or nil when "from" does not depend on "to", directly or not.
// A missing document ends its branch.
func dependencyPath(ctx context.Context, s store.Store, from, to string) ([]string, error) {
	seen := map[string]bool{}
	var walk func(id string) ([]string, error)
	walk = func(id string) ([]string, error) {
		if id == to {
			return []string{id}, nil
		}
		if seen[id] {
			return nil, nil
		}
		seen[id] = true
		doc, err := s.Get(ctx, id)
		if errors.Is(err, model.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		// Only hard dependencies can deadlock: an optional one never waits.
		for _, l := range model.HardDeps(doc.Meta) {
			path, err := walk(l.Target())
			if err != nil || path != nil {
				return append([]string{id}, path...), err
			}
		}
		return nil, nil
	}
	return walk(from)
}

// TaskDependsInput is the input of task_depends_add and task_depends_remove.
type TaskDependsInput struct {
	ID        string   `json:"id"`
	DependsOn []string `json:"depends_on"` // task, obligation, rfx, question, goal, or agent work ids, in any project or area
}

// TaskDependsOutput is the output of task_depends_add and task_depends_remove.
type TaskDependsOutput struct {
	ID        string   `json:"id"`
	DependsOn []string `json:"depends_on"` // all dependencies after the change
	Rev       string   `json:"rev"`
}

// TaskDependsAdd adds dependencies to an open task. An edge that would close a
// cycle is refused, and nothing is changed.
var TaskDependsAdd = defineSafe("task_depends_add", "Make a task wait on other tasks or rfx; a cycle is refused", func(ctx context.Context, d Deps, in TaskDependsInput) (TaskDependsOutput, error) {
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskDependsOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskDependsOutput{}, fmt.Errorf("%s is already %s", doc.ID, t.Status)
	}
	if len(in.DependsOn) == 0 {
		return TaskDependsOutput{}, errors.New("depends_on is empty")
	}
	for _, raw := range in.DependsOn {
		l, err := dependencyLink(ctx, d.Store, raw)
		if err != nil {
			return TaskDependsOutput{}, err
		}
		if slices.Contains(t.DependsOn, l) {
			continue
		}
		if l.Target() == doc.ID {
			return TaskDependsOutput{}, fmt.Errorf("%s cannot depend on itself", doc.ID)
		}
		// The new edge doc -> dep closes a cycle when dep already reaches doc.
		back, err := dependencyPath(ctx, d.Store, l.Target(), doc.ID)
		if err != nil {
			return TaskDependsOutput{}, err
		}
		if back != nil {
			return TaskDependsOutput{}, fmt.Errorf("%s -> %s would make a dependency cycle: %s -> %s",
				doc.ID, l.Target(), doc.ID, strings.Join(back, " -> "))
		}
		t.DependsOn = append(t.DependsOn, l)
		t.OptionalAfter = slices.DeleteFunc(t.OptionalAfter, func(x model.Link) bool { return x == l })
	}
	return saveDepends(ctx, d, doc, t)
})

// TaskDependsRemove removes dependencies from a task.
var TaskDependsRemove = defineSafe("task_depends_remove", "Stop a task waiting on other tasks or rfx", func(ctx context.Context, d Deps, in TaskDependsInput) (TaskDependsOutput, error) {
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskDependsOutput{}, err
	}
	if len(in.DependsOn) == 0 {
		return TaskDependsOutput{}, errors.New("depends_on is empty")
	}
	for _, raw := range in.DependsOn {
		l := model.LinkTo(normID(raw))
		if !slices.Contains(t.DependsOn, l) {
			return TaskDependsOutput{}, fmt.Errorf("%s does not depend on %s", doc.ID, normID(raw))
		}
		t.DependsOn = slices.DeleteFunc(t.DependsOn, func(x model.Link) bool { return x == l })
	}
	return saveDepends(ctx, d, doc, t)
})

func saveDepends(ctx context.Context, d Deps, doc model.Document, t *model.Task) (TaskDependsOutput, error) {
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskDependsOutput{}, err
	}
	return TaskDependsOutput{ID: doc.ID, DependsOn: targets(t.DependsOn), Rev: saved.Rev}, nil
}

// doneWhenNeeded is true for a caller that is not a person: an agent, or an
// item an agent proposed. A person may capture a task without a condition.
func doneWhenNeeded(d Deps) bool {
	return !slices.Contains(PersonSurfaces, d.Store.Info().Surface)
}

// TaskEditInput is the input of task_edit.
type TaskEditInput struct {
	ID       string       `json:"id"`
	DoneWhen string       `json:"done_when,omitempty"` // how we know the task is done
	Effort   model.Effort `json:"effort,omitempty"`    // xs, s, m, or l
	// DoURL, DoCommand, and Steps say how the owner does it. A value replaces
	// the old one; clear_how removes all three.
	DoURL     string `json:"do_url,omitempty"`
	DoCommand string `json:"do_command,omitempty"`
	Steps     string `json:"steps,omitempty"`
	ClearHow  bool   `json:"clear_how,omitempty"`
	// RequestedBy sets who the task is for: a person or group id, or free
	// text. ClearRequestedBy removes it.
	RequestedBy      string `json:"requested_by,omitempty"`
	ClearRequestedBy bool   `json:"clear_requested_by,omitempty"`
	// Parent sets the open task or obligation this one is an action item of
	// (prep for a meeting, or what came out of it). ClearParent removes it.
	Parent      string `json:"parent,omitempty"`
	ClearParent bool   `json:"clear_parent,omitempty"`
}

// TaskEditOutput is the output of task_edit.
type TaskEditOutput struct {
	ID        string       `json:"id"`
	DoneWhen  string       `json:"done_when"`
	Effort    model.Effort `json:"effort,omitempty"`
	DoURL     string       `json:"do_url,omitempty"`
	DoCommand string       `json:"do_command,omitempty"`
	Steps     string       `json:"steps,omitempty"`
	// RequestedBy is who the task is for, after the edit.
	RequestedBy string `json:"requested_by,omitempty"`
	Parent      string `json:"parent,omitempty"`
	Rev         string `json:"rev"`
}

// howFields are the checked do_url, do_command, and steps.
type howFields struct{ url, command, steps string }

func (h howFields) empty() bool { return h.url == "" && h.command == "" && h.steps == "" }

// checkHow checks how a task is done: do_url an http(s) address, do_command a
// command of up to 2000 characters (a short block is fine), steps a short
// markdown list. None may hold a secret: the owner pastes one where the tool asks.
func checkHow(doURL, command, steps string) (howFields, error) {
	var h howFields
	h.url = strings.TrimSpace(doURL)
	if h.url != "" {
		if !isWebURL(h.url) || strings.ContainsAny(h.url, " \t\n") {
			return h, errors.New("do_url: want one http(s) address")
		}
	}
	h.command = strings.TrimSpace(command)
	h.steps = strings.TrimSpace(steps)
	for _, f := range []struct{ name, v string }{{"do_command", h.command}, {"steps", h.steps}} {
		if len([]rune(f.v)) > 2000 {
			return h, fmt.Errorf("%s is over 2000 characters", f.name)
		}
		if err := lintText(f.name, f.v); err != nil {
			return h, err
		}
	}
	return h, nil
}

// TaskEdit sets the done condition or the effort of an open task.
var TaskEdit = defineSafe("task_edit", "Set how we know a task is done (done_when), how the owner does it (do_url: the page, do_command: the exact command, steps: 1 to 5 lines; no secrets), its effort (xs under 15 min, s under an hour, m half a day, l a day or more), who it is for (requested_by: a person or group id, or free text; clear_requested_by removes it), the task or obligation it is an action item of (parent: prep for a meeting or a promise; clear_parent removes it), or several. To change the title, body, or another field, use doc_edit {id, title}", func(ctx context.Context, d Deps, in TaskEditInput) (TaskEditOutput, error) {
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskEditOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskEditOutput{}, fmt.Errorf("%s is already %s", doc.ID, t.Status)
	}
	if err := checkEffort(in.Effort); err != nil {
		return TaskEditOutput{}, err
	}
	how, err := checkHow(in.DoURL, in.DoCommand, in.Steps)
	if err != nil {
		return TaskEditOutput{}, err
	}
	changesHow := !how.empty() || in.ClearHow
	if in.RequestedBy != "" && in.ClearRequestedBy {
		return TaskEditOutput{}, errors.New("give requested_by or clear_requested_by, not both")
	}
	changesFor := in.RequestedBy != "" || in.ClearRequestedBy
	if in.Parent != "" && in.ClearParent {
		return TaskEditOutput{}, errors.New("give parent or clear_parent, not both")
	}
	changesParent := in.Parent != "" || in.ClearParent
	if in.ClearParent {
		t.Parent = ""
	}
	if id := normID(in.Parent); id != "" {
		if id == doc.ID {
			return TaskEditOutput{}, errors.New("parent: a task is not its own action item")
		}
		p, err := parentOf(ctx, d, id)
		if err != nil {
			return TaskEditOutput{}, err
		}
		t.Parent = p.link
	}
	if in.ClearRequestedBy {
		t.RequestedBy = ""
	}
	if in.RequestedBy != "" {
		if t.RequestedBy, err = ioParty(ctx, d, "requested_by", in.RequestedBy); err != nil {
			return TaskEditOutput{}, err
		}
	}
	if in.ClearHow {
		t.DoURL, t.DoCommand, t.Steps = "", "", ""
	}
	if how.url != "" {
		t.DoURL = how.url
	}
	if how.command != "" {
		t.DoCommand = how.command
	}
	if how.steps != "" {
		t.Steps = how.steps
	}
	if (in.Effort == "" && !changesHow && !changesFor && !changesParent) || in.DoneWhen != "" {
		dw, err := checkDoneWhen(in.DoneWhen, true)
		if err != nil {
			return TaskEditOutput{}, err
		}
		t.DoneWhen = dw
	}
	if in.Effort != "" {
		t.Effort = in.Effort
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskEditOutput{}, err
	}
	return TaskEditOutput{ID: doc.ID, DoneWhen: t.DoneWhen, Effort: t.Effort, DoURL: t.DoURL, DoCommand: t.DoCommand, Steps: t.Steps, RequestedBy: t.RequestedBy, Parent: t.Parent.Target(), Rev: saved.Rev}, nil
})

// markDeferred sets Deferred, NeedsYou, and Reason on tasks, and drops the
// deferred ones unless keep is set.
func markDeferred(ctx context.Context, s store.Store, tasks []TaskView, keep bool) ([]TaskView, error) {
	sn, err := NewSnapshot(ctx, s)
	if err != nil {
		return nil, err
	}
	out := tasks[:0]
	for _, t := range tasks {
		need := sn.Need(t.ID)
		t.NeedsYou, t.Reason = need.Yes, need.Reason
		set := sn.later
		t.Deferred = set[t.ID].any() && !taskTerminal(t.Status)
		if !taskTerminal(t.Status) {
			if t.WaitsOn = set[t.ID].Waits; t.WaitsOn == nil {
				t.WaitsOn = []string{}
			}
			t.ReadyWhy = set[t.ID].Ready
		}
		if t.Deferred && !keep {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
