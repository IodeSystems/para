package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/recur"
	"github.com/iodesystems/para/internal/upgrade"
)

// An obligation is what the owner owes someone or must attend: a meeting, or
// a commitment (upgrade step obligations made the older task kinds and asks
// to the owner obligations). Its lifecycle is open, later (its deferral), and
// closed with an outcome. A series is made one occurrence at a time: closing
// one makes the next (nextObligation). In a vault below the obligation format
// the same ops write the older types, so a para that has not been upgraded on
// another machine still reads the vault.

// The obligation ops are alias targets (opalias.go): each routes to itself.
var obligationRouted = func() bool {
	for _, op := range []Entry{ObligationAdd, ObligationSchedule, ObligationAttend} {
		verbRoutes[op.Name()] = func(_ context.Context, _ Deps, raw json.RawMessage) (verbRoute, error) {
			return verbRoute{op: op, input: raw}, nil
		}
	}
	return true
}()

// obligationsOn reports whether the vault is at the obligation format.
func obligationsOn(d Deps) bool {
	v, err := upgrade.Current(d.Store)
	return err == nil && v >= upgrade.ObligationFormat
}

// storedShape is a new document as the vault stores it: a meeting, a
// commitment, or an ask to the owner is an obligation at the obligation
// format, and stays in its older type below it.
func storedShape(d Deps, doc model.Document) (model.Document, error) {
	if !obligationsOn(d) || !upgrade.FoldsToObligation(doc) {
		return doc, nil
	}
	return upgrade.AsObligation(doc)
}

// getObligation reads a document that must be an obligation.
func getObligation(ctx context.Context, d Deps, id string) (model.Document, *model.Obligation, error) {
	doc, err := d.Store.Get(ctx, normID(id))
	if err != nil {
		return doc, nil, missing("obligation", id, err)
	}
	o, ok := doc.Meta.(*model.Obligation)
	if !ok {
		return doc, nil, fmt.Errorf("%s is a %s, not an obligation", doc.ID, typeWord(doc))
	}
	return doc, o, nil
}

// ObligationAddInput is the input of obligation_add.
type ObligationAddInput struct {
	Title string `json:"title"`
	// Kind is meeting or commitment. Empty is a meeting when with, attendees,
	// rfx, start, or a calendar uid is given, else a commitment.
	Kind model.TaskKind `json:"kind,omitempty"`
	// Schedule is once, at, series, or ongoing. Empty: series with recur,
	// ongoing with cadence, at for a meeting, else once.
	Schedule model.Schedule `json:"schedule,omitempty"`
	OwedTo   []string       `json:"owed_to,omitempty"` // person or group ids it is owed to
	OwedBy   []string       `json:"owed_by,omitempty"` // person or group ids that owe it, or agent or agent:<name>; empty: the owner
	// Interested are more person or group ids to tell when it changes
	// (obligation_change), beside owed_to, with, and attendees.
	Interested []string `json:"interested,omitempty"`
	// Parent is the open task or obligation this one came out of: a
	// follow-up of a meeting, or a promise made in it.
	Parent string `json:"parent,omitempty"`
	Project  string         `json:"project,omitempty"`
	Area     string         `json:"area,omitempty"`
	Due      string         `json:"due,omitempty"`   // YYYY-MM-DD
	Start    string         `json:"start,omitempty"` // RFC 3339 with a zone; a commitment may give a date
	End      string         `json:"end,omitempty"`
	Recur    string         `json:"recur,omitempty"`   // series: daily, weekly, monthly, quarterly, yearly, every <n> days
	Cadence  string         `json:"cadence,omitempty"` // ongoing: how often to review it, in the recur grammar
	DoneWhen string         `json:"done_when,omitempty"`
	Body     string         `json:"body,omitempty"`
	// With, Attendees, RFX, Scope, CalendarUID(s), and Optional are a
	// meeting's, as meeting_add takes them.
	With         []string              `json:"with,omitempty"`
	Attendees    []string              `json:"attendees,omitempty"`
	RFX          []string              `json:"rfx,omitempty"`
	Scope        []string              `json:"scope,omitempty"`
	CalendarUID  string                `json:"calendar_uid,omitempty"`
	CalendarUIDs []model.CalendarEvent `json:"calendar_uids,omitempty"`
	Optional     bool                  `json:"optional,omitempty"`
	ImportedFrom *model.ImportedFrom   `json:"imported_from,omitempty"`
	// Produce makes it a recurring artifact an agent owes: what the agent
	// produces each time it comes due (kind journal, report, message, doc, or
	// other), from which template, and for a message to whom. owed_by must
	// name an agent; empty owed_by is agent.
	Produce *ProduceInput `json:"produce,omitempty"`
}

// ObligationAddOutput is the output of obligation_add. Type is obligation,
// or task in a vault below the obligation format.
type ObligationAddOutput struct {
	ID       string         `json:"id"`
	Type     model.Type     `json:"type"`
	Folder   model.Folder   `json:"folder"`
	Kind     model.TaskKind `json:"kind"`
	Schedule model.Schedule `json:"schedule,omitempty"`
	RFX      []string       `json:"rfx"` // requests whose meeting target changed
	Warnings []string       `json:"warnings"`
	Rev      string         `json:"rev"`
}

// ObligationAdd records an obligation.
var ObligationAdd = defineSafe("obligation_add", "Record an obligation: a meeting (with people at a time; the body gathers the questions of linked requests and of open questions in scope) or a commitment (something owed_to people, by due). schedule: once (by due), at (start and end with a zone), series (recur: each occurrence is its own item, the next made when one closes), or ongoing (no date, reviewed every cadence). A recurring artifact an agent owes (a report, a status message, a journal summary) is a commitment with owed_by agent or agent:<name> and produce {kind, template, to, medium, guide}: tick asks the agent for it each time it comes due, and the owner approves it. Close it with close (outcome done, skipped, dropped, declined)", obligationAdd)

func obligationAdd(ctx context.Context, d Deps, in ObligationAddInput) (ObligationAddOutput, error) {
	kind := in.Kind
	if kind == "" {
		kind = model.KindCommitment
		if len(in.With)+len(in.Attendees)+len(in.RFX)+len(in.CalendarUIDs) > 0 || in.Start != "" || in.CalendarUID != "" {
			kind = model.KindMeeting
		}
	}
	if !slices.Contains(model.ObligationKinds, kind) {
		return ObligationAddOutput{}, fmt.Errorf("kind %q: want meeting or commitment", kind)
	}
	sched, err := checkSchedule(in, kind)
	if err != nil {
		return ObligationAddOutput{}, err
	}
	on := obligationsOn(d)
	if !on && (sched == model.ScheduleOngoing || len(in.OwedBy) > 0) {
		return ObligationAddOutput{}, fmt.Errorf("this vault is below format %d: an ongoing obligation and owed_by need it; the owner runs `para vault upgrade`", upgrade.ObligationFormat)
	}
	owedTo, err := meetingLinks(ctx, d, "owed_to", in.OwedTo, model.TypePerson, model.TypeGroup)
	if err != nil {
		return ObligationAddOutput{}, err
	}
	if !on && in.Produce != nil {
		return ObligationAddOutput{}, fmt.Errorf("this vault is below format %d: a recurring artifact needs it; the owner runs `para vault upgrade`", upgrade.ObligationFormat)
	}
	owedBy, err := owers(ctx, d, in.OwedBy)
	if err != nil {
		return ObligationAddOutput{}, err
	}
	interested, err := meetingLinks(ctx, d, "interested", in.Interested, model.TypePerson, model.TypeGroup)
	if err != nil {
		return ObligationAddOutput{}, err
	}
	var parent parentRef
	if id := normID(in.Parent); id != "" {
		if parent, err = parentOf(ctx, d, id); err != nil {
			return ObligationAddOutput{}, err
		}
		if normID(in.Project) == "" && normID(in.Area) == "" {
			in.Project, in.Area = parent.project.Target(), parent.area.Target()
		}
	}
	if !on && (len(interested) > 0 || parent.link != "") {
		return ObligationAddOutput{}, fmt.Errorf("this vault is below format %d: interested and parent need it; the owner runs `para vault upgrade`", upgrade.ObligationFormat)
	}
	var produce *model.Produce
	agentOwed := slices.ContainsFunc(owedBy, model.IsAgentOwer)
	switch {
	case in.Produce != nil && kind != model.KindCommitment:
		return ObligationAddOutput{}, errors.New("produce: a meeting is attended, not produced; give kind commitment")
	case in.Produce != nil && len(owedBy) == 0:
		owedBy, agentOwed = []model.Link{model.OwedByAgent}, true
	case in.Produce != nil && !agentOwed:
		return ObligationAddOutput{}, errors.New("produce is for an obligation an agent owes: give owed_by agent or agent:<name>")
	case in.Produce == nil && agentOwed:
		return ObligationAddOutput{}, errors.New("owed_by an agent: give produce, what the agent produces each time ({kind, template})")
	}
	if in.Produce != nil {
		if produce, err = checkProduce(ctx, d, in.Produce); err != nil {
			return ObligationAddOutput{}, err
		}
	}
	due, err := parseDue(in.Due)
	if err != nil {
		return ObligationAddOutput{}, err
	}
	// A commitment series with no due is not due at once: its first
	// occurrence falls on the start's day, else one cadence from today.
	if kind == model.KindCommitment && sched == model.ScheduleSeries && due == "" {
		if day, ok := meetingDay(in.Start); ok {
			due = model.Date(day)
		} else if rule, err := recur.Parse(in.Recur); err == nil {
			today, _ := parseDay(d.Store.Now().Format(dateLayout))
			due = model.Date(rule.After(today).Format(dateLayout))
		}
	}
	doneWhen, err := checkDoneWhen(in.DoneWhen, false)
	if err != nil {
		return ObligationAddOutput{}, err
	}
	var project, area model.Link
	if p := normID(in.Project); p != "" {
		if _, _, err := getProject(ctx, d.Store, p); err != nil {
			return ObligationAddOutput{}, err
		}
		project = model.LinkTo(p)
	}
	if area, err = areaLink(ctx, d.Store, in.Area); err != nil {
		return ObligationAddOutput{}, err
	}
	pre := func(t *model.Task) {
		t.Project, t.Area, t.Due, t.Recur, t.DoneWhen = project, area, due, flatLine(in.Recur), doneWhen
		if t.Kind == model.KindCommitment && len(t.With) == 0 {
			t.With = owedTo
		}
	}
	post := func(o *model.Obligation) {
		o.OwedTo, o.OwedBy, o.Schedule, o.Cadence, o.Produce = owedTo, owedBy, sched, flatLine(in.Cadence), produce
		o.Interested, o.Parent = interested, parent.link
		if kind == model.KindCommitment {
			o.With = nil
		}
	}
	out := ObligationAddOutput{Kind: kind, RFX: []string{}, Warnings: []string{}}
	var doc model.Document
	if kind == model.KindMeeting {
		mo, saved, err := addMeetingDoc(ctx, d, MeetingAddInput{Title: in.Title, With: in.With, Attendees: in.Attendees, RFX: in.RFX, Scope: in.Scope,
			Start: in.Start, End: in.End, Body: in.Body, CalendarUID: in.CalendarUID, CalendarUIDs: in.CalendarUIDs, Optional: in.Optional, ImportedFrom: in.ImportedFrom}, pre, post)
		if err != nil {
			return out, err
		}
		doc, out.RFX, out.Warnings = saved, mo.RFX, mo.Warnings
	} else {
		if len(in.With)+len(in.Attendees)+len(in.RFX)+len(in.Scope)+len(in.CalendarUIDs) > 0 || in.CalendarUID != "" || in.Optional {
			return out, errors.New("with, attendees, rfx, scope, calendar_uid, and optional are a meeting's: give kind meeting, or owed_to for a commitment")
		}
		title, err := oneLine("title", in.Title)
		if err != nil {
			return out, err
		}
		if !on && project == "" && area == "" {
			return out, fmt.Errorf("this vault is below format %d, where a commitment is a task: give project or area", upgrade.ObligationFormat)
		}
		t := &model.Task{Common: model.Common{Type: model.TypeTask, ImportedFrom: in.ImportedFrom}, Status: model.TaskTodo, Kind: model.KindCommitment}
		if in.Start != "" || in.End != "" {
			if t.Scheduled, t.End, err = checkKindSlot(in.Start, in.End); err != nil {
				return out, err
			}
		}
		pre(t)
		if doc, err = storedShape(d, model.Document{Folder: model.FolderProjects, Title: title, Body: demoteH1(in.Body), Meta: t}); err != nil {
			return out, err
		}
		if o, ok := doc.Meta.(*model.Obligation); ok {
			post(o)
		}
		if doc, err = d.Store.Create(ctx, doc); err != nil {
			return out, err
		}
	}
	out.ID, out.Type, out.Folder, out.Rev = doc.ID, doc.Meta.Header().Type, doc.Folder, doc.Rev
	if o, ok := doc.Meta.(*model.Obligation); ok {
		out.Schedule = o.Schedule
	}
	if !on {
		out.Warnings = append(out.Warnings, fmt.Sprintf("this vault is below format %d: stored as a task of kind %s until the owner runs `para vault upgrade`", upgrade.ObligationFormat, kind))
	}
	if w := recurWarning(in.Recur); w != "" {
		out.Warnings = append(out.Warnings, w)
	}
	return out, nil
}

// checkSchedule is the schedule of a new obligation: the one given, checked
// against recur and cadence, or the one they imply.
func checkSchedule(in ObligationAddInput, kind model.TaskKind) (model.Schedule, error) {
	r, c := strings.TrimSpace(in.Recur), strings.TrimSpace(in.Cadence)
	s := in.Schedule
	if s == "" {
		switch {
		case r != "":
			s = model.ScheduleSeries
		case c != "":
			s = model.ScheduleOngoing
		case kind == model.KindMeeting || strings.Contains(in.Start, "T"):
			s = model.ScheduleAt
		default:
			s = model.ScheduleOnce
		}
	}
	switch {
	case !slices.Contains(model.Schedules, s):
		return "", fmt.Errorf("schedule %q: want once, at, series, or ongoing", s)
	case (s == model.ScheduleSeries) != (r != ""):
		return "", errors.New("recur goes with schedule series, and a series needs recur")
	case (s == model.ScheduleOngoing) != (c != ""):
		return "", errors.New("cadence goes with schedule ongoing, and an ongoing obligation needs cadence")
	case s == model.ScheduleOngoing && (in.Start != "" || in.Due != ""):
		return "", errors.New("an ongoing obligation has no date: give cadence, not start or due")
	}
	if c != "" {
		if _, err := recur.Parse(c); err != nil {
			return "", fmt.Errorf("cadence: %w", err)
		}
	}
	return s, nil
}

// checkKindSlot checks a start (a date, or a time with a zone) and an end.
func checkKindSlot(start, end string) (string, string, error) {
	if start == "" {
		return "", "", errors.New("end needs start")
	}
	s, timed, err := checkWhen("start", start)
	if err != nil || end == "" {
		return s, "", err
	}
	if !timed {
		return "", "", errors.New("end: start is a date; give it a time to give an end")
	}
	return checkSlot(s, end)
}

// ObligationScheduleInput is the input of obligation_schedule.
type ObligationScheduleInput struct {
	ID string `json:"id"`
	// Start and End book or move a meeting (RFC 3339 with a zone); a
	// commitment takes a start date or time too. Due sets the date it is owed.
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	Due   string `json:"due,omitempty"`
	// CalendarUID(s) add a meeting's calendar events, as meeting_schedule.
	CalendarUID  string                `json:"calendar_uid,omitempty"`
	CalendarUIDs []model.CalendarEvent `json:"calendar_uids,omitempty"`
	// Reviewed marks an ongoing obligation reviewed today.
	Reviewed bool `json:"reviewed,omitempty"`
}

// ObligationScheduleOutput is the output of obligation_schedule.
type ObligationScheduleOutput struct {
	MeetingChangeOutput
	Due          model.Date `json:"due,omitempty"`
	LastReviewed model.Date `json:"last_reviewed,omitempty"`
}

// ObligationSchedule sets when an obligation falls due.
var ObligationSchedule = defineSafe("obligation_schedule", "Set when an obligation falls due: start and end book or move a meeting (linked requests move to scheduled, as meeting_schedule); due or start set a commitment's date; reviewed marks an ongoing one reviewed today", func(ctx context.Context, d Deps, in ObligationScheduleInput) (ObligationScheduleOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return ObligationScheduleOutput{}, linkErr("id", normID(in.ID), err)
	}
	if _, ok := readMeeting(doc); ok {
		if in.Due != "" || in.Reviewed {
			return ObligationScheduleOutput{}, errors.New("a meeting takes start and end: due and reviewed are for a commitment")
		}
		mo, err := meetingSchedule(ctx, d, MeetingScheduleInput{ID: doc.ID, Start: in.Start, End: in.End, CalendarUID: in.CalendarUID, CalendarUIDs: in.CalendarUIDs})
		return ObligationScheduleOutput{MeetingChangeOutput: mo}, err
	}
	if in.CalendarUID != "" || len(in.CalendarUIDs) > 0 {
		return ObligationScheduleOutput{}, errors.New("calendar_uid: only a meeting has a calendar event")
	}
	var due *model.Date
	var occ *model.Occasion
	var open bool
	switch m := doc.Meta.(type) {
	case *model.Obligation:
		due, occ, open = &m.Due, &m.Occasion, m.Open()
		if m.Schedule == model.ScheduleOngoing && (in.Due != "" || in.Start != "" || in.End != "") {
			return ObligationScheduleOutput{}, fmt.Errorf("%s is ongoing: it has no date; give reviewed", doc.ID)
		}
		if in.Reviewed {
			if m.Schedule != model.ScheduleOngoing {
				return ObligationScheduleOutput{}, fmt.Errorf("%s is not ongoing: reviewed is for an ongoing obligation", doc.ID)
			}
			m.LastReviewed = model.Date(d.Store.Now().Format(dateLayout))
		}
	case *model.Task:
		if m.EffectiveKind() != model.KindCommitment {
			return ObligationScheduleOutput{}, fmt.Errorf("%s is a %s, not an obligation", doc.ID, typeWord(doc))
		}
		due, occ, open = &m.Due, &m.Occasion, !taskTerminal(m.Status)
	default:
		return ObligationScheduleOutput{}, fmt.Errorf("%s is a %s, not an obligation", doc.ID, typeWord(doc))
	}
	if !open {
		return ObligationScheduleOutput{}, fmt.Errorf("%s is closed: reopen it first", doc.ID)
	}
	if in.Reviewed && doc.Meta.Header().Type != model.TypeObligation {
		return ObligationScheduleOutput{}, fmt.Errorf("%s is not ongoing: reviewed is for an ongoing obligation", doc.ID)
	}
	if in.Due != "" {
		if *due, err = parseDue(in.Due); err != nil {
			return ObligationScheduleOutput{}, err
		}
	}
	if in.Start != "" || in.End != "" {
		if occ.Scheduled, occ.End, err = checkKindSlot(in.Start, in.End); err != nil {
			return ObligationScheduleOutput{}, err
		}
	}
	if in.Due == "" && in.Start == "" && in.End == "" && !in.Reviewed {
		return ObligationScheduleOutput{}, errors.New("give start, due, or reviewed")
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return ObligationScheduleOutput{}, err
	}
	out := ObligationScheduleOutput{MeetingChangeOutput: MeetingChangeOutput{ID: doc.ID, Start: occ.Scheduled, End: occ.End, RFX: []string{}, Warnings: []string{}, Rev: saved.Rev}, Due: *due}
	if o, ok := doc.Meta.(*model.Obligation); ok {
		out.LastReviewed = o.LastReviewed
	}
	return out, nil
})

// obligationCloseInput is the input close gives its write for an obligation.
type obligationCloseInput struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome,omitempty"` // empty is done
	Reason  string `json:"reason,omitempty"`  // kept as reason and under ## Outcome
}

// ObligationCloseOutput is what closing an obligation did. Next is the next
// occurrence of a series.
type ObligationCloseOutput struct {
	ID       string            `json:"id"`
	State    model.State       `json:"state"`
	Outcome  model.TaskOutcome `json:"outcome"`
	Next     string            `json:"next,omitempty"`
	Warnings []string          `json:"warnings"`
	Rev      string            `json:"rev"`
}

// obligationClose is close's write for an obligation. It is not listed: close
// routes to it, so it has close's name.
var obligationClose = &Op[obligationCloseInput, ObligationCloseOutput]{name: "close", desc: "close an obligation", risk: RiskSafe, oneReceipt: true,
	fn: func(ctx context.Context, d Deps, in obligationCloseInput) (ObligationCloseOutput, error) {
		doc, o, err := getObligation(ctx, d, in.ID)
		if err != nil {
			return ObligationCloseOutput{}, err
		}
		outcome := model.TaskOutcome(cmp.Or(strings.TrimSpace(in.Outcome), string(model.OutcomeDone)))
		if !slices.Contains(model.ObligationOutcomes, outcome) {
			return ObligationCloseOutput{}, fmt.Errorf("outcome %q: want one of %v", outcome, model.ObligationOutcomes)
		}
		if !o.Open() {
			return ObligationCloseOutput{}, fmt.Errorf("%s is already closed (%s)", doc.ID, o.Outcome)
		}
		if why := messageUnsent(ctx, d, o); why != "" && outcome == model.OutcomeDone {
			return ObligationCloseOutput{}, fmt.Errorf("%s: %s", doc.ID, why)
		}
		reason := flatLine(in.Reason)
		if err := lintText("reason", reason); err != nil {
			return ObligationCloseOutput{}, err
		}
		out := ObligationCloseOutput{ID: doc.ID, State: model.StateClosed, Outcome: outcome, Warnings: []string{}}
		if rule, ok := recurRule(o.Recur); ok && o.Schedule == model.ScheduleSeries {
			// The next occurrence first: a retry finds it and makes no second.
			if out.Next, err = nextObligation(ctx, d, doc, o, rule); err != nil {
				return out, err
			}
		}
		o.State, o.Outcome, o.Reason, o.Closed = model.StateClosed, outcome, reason, model.Date(d.Store.Now().Format(dateLayout))
		o.Promise = promiseNow(storeGet(ctx, d.Store), o, string(o.Closed))
		if reason != "" {
			doc.Body = appendToSection(doc.Body, outcomeHeading, reason)
		}
		saved, err := d.Store.Update(ctx, doc)
		if err != nil {
			return out, err
		}
		out.Rev = saved.Rev
		return out, recordClose(ctx, d, doc.ID, string(outcome))
	}}

// nextObligation makes the next occurrence of a series, or returns the one
// that exists: one recur step on from this one's due date or start, with the
// people, attendance default, and calendar events carried on, occurrence_of
// the first of the series, and previous this one.
func nextObligation(ctx context.Context, d Deps, doc model.Document, o *model.Obligation, rule recur.Rule) (string, error) {
	if next, err := successor(ctx, d, doc.ID); err != nil || next != "" {
		return next, err
	}
	due, sched, end, err := nextSlot(rule, o.Due, o.Scheduled, o.End, d.Store.Now().Local())
	if err != nil {
		return "", fmt.Errorf("next occurrence: %w", err)
	}
	var rfx []model.Link
	if o.Kind == model.KindMeeting {
		rfx = o.RFX
	}
	next := &model.Obligation{Common: model.Common{Type: model.TypeObligation, Tags: slices.Clone(o.Tags)}, Kind: o.Kind, State: model.StateOpen,
		Schedule: o.Schedule, OwedTo: o.OwedTo, OwedBy: o.OwedBy, Project: o.Project, Area: o.Area, Due: due, Recur: o.Recur,
		OccurrenceOf: cmp.Or(o.OccurrenceOf, model.LinkTo(doc.ID)), Previous: model.LinkTo(doc.ID),
		Occasion: nextOccasion(o.Occasion, rfx, sched, end), DoneWhen: o.DoneWhen, Effort: o.Effort, Interested: o.Interested}
	if o.Produce != nil {
		p := *o.Produce
		p.To = slices.Clone(o.Produce.To)
		next.Produce = &p
	}
	saved, err := d.Store.Create(ctx, model.Document{Folder: model.FolderObligations, Title: doc.Title, Body: doc.Body, Meta: next})
	if err != nil {
		return "", err
	}
	return saved.ID, nil
}

// ongoingTurn is the owner's turn on an ongoing obligation: its review is due
// every cadence from the last one; never reviewed is due now.
func ongoingTurn(o *model.Obligation) Need {
	rule, err := recur.Parse(o.Cadence)
	if err != nil {
		return Need{Turn: TurnNone, Reason: "ongoing; cadence para cannot read"}
	}
	if o.LastReviewed == "" {
		return Need{Turn: TurnOwner, Reason: "review " + o.Cadence + "; never reviewed"}
	}
	last, err := parseDay(string(o.LastReviewed))
	if err != nil {
		return Need{Turn: TurnOwner, Reason: "review " + o.Cadence}
	}
	return Need{Turn: TurnOwner, On: rule.After(last).Format(dateLayout), Reason: "review " + o.Cadence}
}
