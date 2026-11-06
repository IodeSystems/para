package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/recur"
)

// A recurring task has a `recur` value (daily, weekly, monthly, every <n>
// days). Marking it done creates the next task, with the due date advanced and
// `previous` linking back. There is no daemon: nothing happens until a person
// or an agent calls task_done (or task_done with skip).

// recurWarning explains an unknown recur value; empty when it is empty or valid.
func recurWarning(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, err := recur.Parse(s); err != nil {
		return err.Error() + ": stored as written, and para will not create the next task"
	}
	return ""
}

const outcomeHeading = "## Outcome"

// opTaskClose is the event of a task closed, with the outcome in to. The
// outcome report (report_outcomes) counts them by outcome and actor.
const opTaskClose = "task_close"

func recordClose(ctx context.Context, d Deps, id, outcome string) error {
	return d.Store.Record(ctx, opTaskClose, id, "", outcome, "")
}

// TaskDoneInput is the input of task_done.
type TaskDoneInput struct {
	ID   string `json:"id"`
	Skip bool   `json:"skip,omitempty"` // a recurring task: advance without doing it (this one is dropped)
	Note string `json:"note,omitempty"` // how it ended; stored under ## Outcome
}

// TaskDoneOutput is the output of task_done. Next is the id of the next
// occurrence of a recurring task; Request is the request made for it when it
// is assigned to an agent.
type TaskDoneOutput struct {
	ID        string           `json:"id"`
	Status    model.TaskStatus `json:"status"`
	BlockedBy []string         `json:"blocked_by"`
	Rev       string           `json:"rev"`
	Next      string           `json:"next,omitempty"`
	Request   string           `json:"request,omitempty"`
	// DoneWhen is the condition the task was finished against, if it had one.
	DoneWhen string   `json:"done_when,omitempty"`
	Warnings []string `json:"warnings"`
}

// TaskDone marks a task done and clears its blockers. A recurring task also
// gets its next occurrence. It is safe to repeat on a recurring task: the next
// occurrence is made once. With Skip the task is dropped, not done, and the
// next occurrence is made all the same.
var TaskDone = defineSafe("task_done", "Mark a task done; a recurring task also gets its next occurrence (skip advances without doing)", func(ctx context.Context, d Deps, in TaskDoneInput) (TaskDoneOutput, error) {
	return finishTask(ctx, d, in, "")
})

// finishTask closes a task done, or dropped with Skip. A non-empty outcome is
// stored in the frontmatter.
func finishTask(ctx context.Context, d Deps, in TaskDoneInput, outcome model.TaskOutcome) (TaskDoneOutput, error) {
	id := normID(in.ID)
	doc, t, err := getTask(ctx, d.Store, id)
	if err != nil {
		return TaskDoneOutput{}, err
	}
	if err := closeGuard(doc, t, outcome); err != nil {
		return TaskDoneOutput{}, err
	}
	rule, recurring := recurRule(t)
	if in.Skip && !recurring {
		return TaskDoneOutput{}, fmt.Errorf("%s does not recur: skip is for a recurring task (recur: %s)", id, recur.Forms)
	}
	out := TaskDoneOutput{ID: id, Warnings: []string{}}
	if w := recurWarning(t.Recur); w != "" {
		out.Warnings = append(out.Warnings, w)
	}
	if taskTerminal(t.Status) {
		// A repeat is a success only for a recurring task that has a next one.
		next, err := successor(ctx, d, id)
		if !recurring || next == "" || err != nil {
			if err != nil {
				return TaskDoneOutput{}, err
			}
			return TaskDoneOutput{}, fmt.Errorf("%s is already %s", id, t.Status)
		}
		out.Status, out.BlockedBy, out.Rev, out.Next = t.Status, append([]string{}, t.BlockedBy...), doc.Rev, next
		return out, nil
	}
	if recurring {
		// Make the next task first: if the next step fails, this task is still
		// open, and a retry finds the successor instead of making another.
		next, req, warns, err := makeNext(ctx, d, doc, t, rule)
		if err != nil {
			return TaskDoneOutput{}, err
		}
		out.Next, out.Request = next, req
		out.Warnings = append(out.Warnings, warns...)
	}
	t.Status = model.TaskDone
	if in.Skip {
		t.Status = model.TaskDropped
	}
	t.BlockedBy = nil
	t.Outcome = outcome
	if note := strings.TrimSpace(in.Note); note != "" {
		doc.Body = appendToSection(doc.Body, outcomeHeading, note)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskDoneOutput{}, err
	}
	out.Status, out.BlockedBy, out.Rev, out.DoneWhen = t.Status, []string{}, saved.Rev, t.DoneWhen
	closed := firstOf(string(outcome), string(model.OutcomeDone))
	if in.Skip {
		closed = string(model.OutcomeDropped)
	}
	if err := recordClose(ctx, d, id, closed); err != nil {
		return out, err
	}
	return out, nil
}

func recurRule(t *model.Task) (recur.Rule, bool) {
	if strings.TrimSpace(t.Recur) == "" {
		return recur.Rule{}, false
	}
	r, err := recur.Parse(t.Recur)
	return r, err == nil
}

// successor returns the id of the task that recurred from id, or "".
func successor(ctx context.Context, d Deps, id string) (string, error) {
	all, err := listTasks(ctx, d.Store, "", "")
	if err != nil {
		return "", err
	}
	for _, t := range all {
		if t.Previous == id {
			return t.ID, nil
		}
	}
	return "", nil
}

// makeNext creates the next occurrence of a recurring task, or returns the one
// that exists. The due date moves one step from the old due date (from today
// when there is none), then on to the first step that is not in the past. A
// project that was closed gets no next task: a warning says so.
func makeNext(ctx context.Context, d Deps, doc model.Document, t *model.Task, rule recur.Rule) (id, request string, warns []string, err error) {
	if existing, err := successor(ctx, d, doc.ID); err != nil || existing != "" {
		return existing, "", nil, err
	}
	if p := t.Project.Target(); p != "" {
		proj, _, err := getProject(ctx, d.Store, p)
		if err != nil {
			return "", "", nil, fmt.Errorf("next task: %w", err)
		}
		if proj.Folder == model.FolderArchive {
			return "", "", []string{"project " + p + " is closed: no next task was made"}, nil
		}
	}
	today := d.Store.Now().Local()
	from := today
	switch {
	case t.Due != "":
		if from, err = parseDay(string(t.Due)); err != nil {
			return "", "", nil, fmt.Errorf("next task: %w", err)
		}
	case t.Scheduled != "":
		if from, err = scheduledDay(t.Scheduled); err != nil {
			return "", "", nil, fmt.Errorf("next task: %w", err)
		}
	}
	step := rule.Next(from, today)
	days := int(step.Sub(day(from)).Round(24*time.Hour) / (24 * time.Hour))
	var due model.Date
	if t.Due != "" || t.Scheduled == "" {
		due = model.Date(step.Format(recur.Layout))
	}
	sched, end, err := shiftSlot(t.Scheduled, t.End, days)
	if err != nil {
		return "", "", nil, fmt.Errorf("next task: %w", err)
	}
	var rfx []model.Link
	if t.EffectiveKind() == model.KindMeeting {
		rfx = t.RFX
	}
	next, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderProjects,
		Title:  doc.Title,
		Body:   doc.Body,
		Meta: &model.Task{
			Common:   model.Common{Type: model.TypeTask},
			Status:   model.TaskTodo,
			Project:  t.Project,
			Area:     t.Area,
			Due:      due,
			Recur:    t.Recur,
			Assignee: t.Assignee,
			DoneWhen: t.DoneWhen,
			Previous: model.LinkTo(doc.ID),

			Kind:     t.Kind,
			Occasion: nextOccasion(t.Occasion, rfx, sched, end),
		},
	})
	if err != nil {
		return "", "", nil, err
	}
	request, warns = agentRequest(ctx, d, next, nil)
	return next.ID, request, warns, nil
}

// nextOccasion is the occasion of the next occurrence. Optional is the
// series' and carries on; attend is one occurrence's answer, so the next
// starts from the default.
func nextOccasion(o model.Occasion, rfx []model.Link, sched, end string) model.Occasion {
	return model.Occasion{With: o.With, Attendees: o.Attendees, RFX: rfx, Scheduled: sched, End: end, CalendarUIDs: o.UIDs(),
		Optional: o.Optional, Attend: o.AttendDefault, AttendDefault: o.AttendDefault}
}

// slotZone returns the location to read an RFC 3339 time in. A time is stored
// with an offset only, so when this machine's zone has that offset at that
// instant the machine's zone is used, and the wall-clock time then survives a
// clock change. Otherwise the offset as written is kept.
func slotZone(t time.Time) *time.Location {
	if _, off := t.In(time.Local).Zone(); off == func() int { _, o := t.Zone(); return o }() {
		return time.Local
	}
	return t.Location()
}

// scheduledDay returns the calendar day of a scheduled value: a date, or the
// day of an RFC 3339 start in its own zone, at midnight UTC.
func scheduledDay(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return day(t.In(slotZone(t))), nil
	}
	t, err := time.Parse(recur.Layout, s)
	if err != nil {
		return t, fmt.Errorf("scheduled %q: want a date or RFC 3339 with a zone", s)
	}
	return t, nil
}

func day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// shiftSlot moves scheduled and end forward by days calendar days. A timed
// start keeps its wall-clock time and zone; end keeps the duration.
func shiftSlot(scheduled, end string, days int) (string, string, error) {
	if scheduled == "" {
		return "", "", nil
	}
	st, err := time.Parse(time.RFC3339, scheduled)
	if err != nil {
		d, derr := time.Parse(recur.Layout, scheduled)
		if derr != nil {
			return "", "", fmt.Errorf("scheduled %q: want a date or RFC 3339 with a zone", scheduled)
		}
		return d.AddDate(0, 0, days).Format(recur.Layout), "", nil
	}
	loc := slotZone(st)
	l := st.In(loc)
	ns := time.Date(l.Year(), l.Month(), l.Day()+days, l.Hour(), l.Minute(), l.Second(), l.Nanosecond(), loc)
	if end == "" {
		return ns.Format(time.RFC3339), "", nil
	}
	en, err := time.Parse(time.RFC3339, end)
	if err != nil {
		return "", "", fmt.Errorf("end %q: want RFC 3339 with a zone", end)
	}
	return ns.Format(time.RFC3339), ns.Add(en.Sub(st)).Format(time.RFC3339), nil
}

// nextMeeting makes the next occurrence of a recurring meeting that is being
// held or cancelled, through makeNext, so a task_done on the same task finds
// it and makes no second one. A meeting that does not recur, or an old-form
// meeting, gets none. Call it before the closing document is saved.
func nextMeeting(ctx context.Context, d Deps, doc model.Document, m *mtg) (id, request string, warns []string, err error) {
	if m.t == nil {
		return "", "", nil, nil
	}
	rule, ok := recurRule(m.t)
	if !ok {
		return "", "", nil, nil
	}
	return makeNext(ctx, d, doc, m.t, rule)
}

// agentRequest queues a request for a recurring task assigned to an agent,
// through request_add. A failure is a warning, not an error: the task exists.
func agentRequest(ctx context.Context, d Deps, doc model.Document, warns []string) (string, []string) {
	t, ok := doc.Meta.(*model.Task)
	if !ok || !strings.HasPrefix(t.Assignee, AgentPrefix) {
		return "", warns
	}
	if _, ok := recurRule(t); !ok {
		return "", warns
	}
	ask := fmt.Sprintf("%s\n\nRecurring task %s, assigned to %s, due %s.", doc.Title, doc.ID, t.Assignee, dashOr(string(t.Due)))
	out, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: doc.Title, Ask: ask, Links: []string{doc.ID}, Kind: RecurringKind})
	if err != nil {
		return "", append(warns, "task "+doc.ID+" is assigned to "+t.Assignee+" but no request was made: "+err.Error())
	}
	return out.ID, warns
}

func dashOr(s string) string {
	if s == "" {
		return "no date"
	}
	return s
}

func parseDay(s string) (time.Time, error) { return time.ParseInLocation(recur.Layout, s, time.Local) }

// closeGuard refuses to close a task the plain way when its kind has a closing
// flow of its own. An open meeting closes through meeting_hold (replies,
// answers, action items) or meeting_cancel. Work with someone and a commitment
// close with an outcome, so task_done is not enough: use task_resolve.
func closeGuard(doc model.Document, t *model.Task, outcome model.TaskOutcome) error {
	switch {
	case taskTerminal(t.Status):
		return nil
	case t.EffectiveKind() == model.KindMeeting && outcome == model.OutcomeDuplicate:
		return nil // a moved meeting merges into its successor (doc_merge)
	case t.EffectiveKind() == model.KindMeeting:
		return fmt.Errorf("%s is a meeting: close it with meeting_hold (what came of it), meeting_cancel, or doc_merge into the meeting that replaced it", doc.ID)
	case needsOutcome(t) && outcome == "":
		return fmt.Errorf("%s is a %s: close it with task_resolve and say how it ended (outcome)", doc.ID, t.EffectiveKind())
	}
	return nil
}
