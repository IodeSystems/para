package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Holding a meeting records what came of it. Replies go through rfx_reply and
// answers through question_answer, so a "no" still becomes a constraint and an
// answer still frees what waited on it. Each action item becomes a task with an
// assignee and a link to the meeting. Para does not record or transcribe: the
// notes are what the person or agent gives it, in their own words.

// HoldAction is one action item of a held meeting.
type HoldAction struct {
	Title    string `json:"title"`
	Assignee string `json:"assignee,omitempty"` // self (default), a person id, or agent:<name>
	Due      string `json:"due,omitempty"`      // YYYY-MM-DD
	Project  string `json:"project,omitempty"`  // default: the project of the hold, else of a linked request
	Area     string `json:"area,omitempty"`
	Body     string `json:"body,omitempty"`
	DoneWhen string `json:"done_when,omitempty"` // how we know the task is done; required from an agent
}

// MeetingHoldInput is the input of meeting_hold.
type MeetingHoldInput struct {
	ID      string                `json:"id"`
	Notes   string                `json:"notes,omitempty"`   // what was said, in your words; never a transcript
	Start   string                `json:"start,omitempty"`   // only for a meeting that was never scheduled: RFC 3339 with a zone
	End     string                `json:"end,omitempty"`     // as start
	Replies []RFXReplyInput       `json:"replies,omitempty"` // answers to linked requests, as rfx_reply takes them
	Answers []QuestionAnswerInput `json:"answers,omitempty"` // answers to open questions, as question_answer takes them
	Actions []HoldAction          `json:"actions,omitempty"` // action items: each becomes a task
	Project string                `json:"project,omitempty"` // default project of the actions
	Area    string                `json:"area,omitempty"`    // default area of the actions
}

// MeetingHoldOutput is the output of meeting_hold. On an error part-way, the
// ids recorded so far are in the error text; run the rest by hand.
type MeetingHoldOutput struct {
	ID           string              `json:"id"`
	Status       model.MeetingStatus `json:"status"`
	Replied      []string            `json:"replied"`           // requests that got a reply
	Constraints  []string            `json:"constraints"`       // made by a "no"
	Interactions []string            `json:"interactions"`      // logged by the replies
	Answered     []string            `json:"answered"`          // questions answered
	Tasks        []string            `json:"tasks"`             // action items
	Requests     []string            `json:"requests"`          // agent requests queued for tasks
	Waiting      []string            `json:"waiting"`           // requests asked in the meeting and still unanswered
	Next         string              `json:"next,omitempty"`    // a recurring meeting: its next occurrence
	Request      string              `json:"request,omitempty"` // agent request queued for the next occurrence
	Warnings     []string            `json:"warnings"`
	Rev          string              `json:"rev"`
}

// lintText refuses text a document would reject, before anything is written.
func lintText(field, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	err := precheck(model.Document{ID: "check", Folder: model.FolderResources, Title: "Check", Body: text,
		Meta: &model.Note{Common: model.Common{Type: model.TypeNote}}})
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

// demoteH1 trims a body passed to an add op and turns each "# " line outside
// a code fence into "## ": the file has its own "# Title", and lint refuses a
// second level-1 heading.
func demoteH1(body string) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n")
	fence := false
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
			continue
		}
		if !fence && (t == "#" || strings.HasPrefix(t, "# ")) {
			lines[i] = "#" + t
		}
	}
	return strings.Join(lines, "\n")
}

// flattenHeadings turns a heading line in free text into bold text, so notes
// cannot start a section of the meeting.
func flattenHeadings(text string) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	for i, l := range lines {
		if t := strings.TrimLeft(l, " "); strings.HasPrefix(t, "#") {
			lines[i] = "**" + strings.TrimSpace(strings.TrimLeft(t, "#")) + "**"
		}
	}
	return strings.Join(lines, "\n")
}

// MeetingHold records a meeting as held.
var MeetingHold = defineSafe("meeting_hold", "Record a held meeting: notes, replies to its requests (rfx_reply), answers to questions (question_answer), and action items as tasks linked to the meeting", func(ctx context.Context, d Deps, in MeetingHoldInput) (MeetingHoldOutput, error) {
	doc, m, err := getMeeting(ctx, d.Store, in.ID)
	if err != nil {
		return MeetingHoldOutput{}, err
	}
	if !meetingOpen(m.Status) {
		return MeetingHoldOutput{}, fmt.Errorf("%s is already %s", doc.ID, m.Status)
	}
	switch {
	case in.Start != "" || in.End != "":
		if m.Status == model.MeetingScheduled {
			return MeetingHoldOutput{}, errors.New("start and end: the meeting is already scheduled; use meeting_schedule to change the time")
		}
		if m.Start, m.End, err = checkSlot(in.Start, in.End); err != nil {
			return MeetingHoldOutput{}, err
		}
	case m.Status == model.MeetingRequested:
		return MeetingHoldOutput{}, errors.New("this meeting was never scheduled: pass start and end (when it was held)")
	}
	notes := flattenHeadings(in.Notes)
	if err := lintText("notes", notes); err != nil {
		return MeetingHoldOutput{}, err
	}

	// Check everything that can be checked before the first write.
	replyQuestion := map[string]bool{}
	for i, r := range in.Replies {
		id := normID(r.ID)
		if id == "" {
			return MeetingHoldOutput{}, fmt.Errorf("replies[%d]: id (the request) is empty", i)
		}
		_, rm, err := getRFX(ctx, d, id)
		if err != nil {
			return MeetingHoldOutput{}, fmt.Errorf("replies[%d]: %w", i, err)
		}
		if q := rm.Question.Target(); q != "" {
			replyQuestion[q] = true
		}
		if _, err := replySources(ctx, d, rm, r); err != nil {
			return MeetingHoldOutput{}, fmt.Errorf("replies[%d]: %w", i, err)
		}
		for f, v := range map[string]string{"text": r.Text, "why": r.Why} {
			if err := lintText(fmt.Sprintf("replies[%d].%s", i, f), v); err != nil {
				return MeetingHoldOutput{}, err
			}
		}
	}
	answered := map[string]bool{}
	for i, a := range in.Answers {
		id := normID(a.ID)
		_, q, err := getQuestion(ctx, d.Store, id)
		if err != nil {
			return MeetingHoldOutput{}, fmt.Errorf("answers[%d]: %w", i, err)
		}
		switch {
		case q.Status != model.QuestionOpen:
			return MeetingHoldOutput{}, fmt.Errorf("answers[%d]: %s is already %s", i, id, q.Status)
		case replyQuestion[id]:
			return MeetingHoldOutput{}, fmt.Errorf("answers[%d]: %s is answered by the reply to its request; do not answer it twice", i, id)
		case answered[id]:
			return MeetingHoldOutput{}, fmt.Errorf("answers[%d]: %s is answered twice", i, id)
		}
		answered[id] = true
		if _, err := answerSources(ctx, d, a.Sources, agentSurface(d)); err != nil {
			return MeetingHoldOutput{}, fmt.Errorf("answers[%d]: %w", i, err)
		}
		for f, v := range map[string]string{"text": a.Text, "why": a.Why} {
			if err := lintText(fmt.Sprintf("answers[%d].%s", i, f), v); err != nil {
				return MeetingHoldOutput{}, err
			}
		}
	}
	project, area := normID(in.Project), normID(in.Area)
	if project == "" && area == "" {
		for _, rl := range m.RFX {
			if _, rm, err := getRFX(ctx, d, rl.Target()); err == nil && rm.Project != "" {
				project = rm.Project.Target()
				break
			}
		}
	}
	tasks := make([]TaskAddInput, 0, len(in.Actions))
	for i, a := range in.Actions {
		ti := TaskAddInput{Title: a.Title, Assignee: a.Assignee, Due: a.Due, Project: a.Project, Area: a.Area, DoneWhen: a.DoneWhen}
		if normID(ti.Project) == "" && normID(ti.Area) == "" {
			ti.Project, ti.Area = project, area
		}
		if normID(ti.Project) == "" && normID(ti.Area) == "" {
			return MeetingHoldOutput{}, fmt.Errorf("actions[%d]: a task needs a project or an area: pass one on the action, or project or area for the whole hold", i)
		}
		if strings.TrimSpace(ti.Title) == "" {
			return MeetingHoldOutput{}, fmt.Errorf("actions[%d]: title is empty", i)
		}
		if _, err := checkDoneWhen(a.DoneWhen, doneWhenNeeded(d)); err != nil {
			return MeetingHoldOutput{}, fmt.Errorf("actions[%d]: %w", i, err)
		}
		if _, err := resolveAssignee(ctx, d, a.Assignee); err != nil {
			return MeetingHoldOutput{}, fmt.Errorf("actions[%d]: %w", i, err)
		}
		if _, err := parseDue(a.Due); err != nil {
			return MeetingHoldOutput{}, fmt.Errorf("actions[%d]: %w", i, err)
		}
		if normID(ti.Project) != "" {
			if _, _, err := getProject(ctx, d.Store, normID(ti.Project)); err != nil {
				return MeetingHoldOutput{}, fmt.Errorf("actions[%d]: project: %w", i, err)
			}
		}
		if normID(ti.Area) != "" {
			if _, err := areaLink(ctx, d.Store, ti.Area); err != nil {
				return MeetingHoldOutput{}, fmt.Errorf("actions[%d]: %w", i, err)
			}
		}
		body := fmt.Sprintf("From meeting [[%s]].", doc.ID)
		if b := strings.TrimSpace(a.Body); b != "" {
			if err := lintText(fmt.Sprintf("actions[%d].body", i), b); err != nil {
				return MeetingHoldOutput{}, err
			}
			body += "\n\n" + b
		}
		ti.Body = body
		tasks = append(tasks, ti)
	}

	out := MeetingHoldOutput{ID: doc.ID, Replied: []string{}, Constraints: []string{}, Interactions: []string{},
		Answered: []string{}, Tasks: []string{}, Requests: []string{}, Waiting: []string{}, Warnings: []string{}}
	partial := func(what string, err error) (MeetingHoldOutput, error) {
		return out, fmt.Errorf("%s: %w; recorded so far: replied %v, answered %v, tasks %v; the meeting is not marked held", what, err, out.Replied, out.Answered, out.Tasks)
	}
	var lines []string
	for i, r := range in.Replies {
		o, err := RFXReply.Run(ctx, d, r)
		if err != nil {
			return partial(fmt.Sprintf("replies[%d]", i), err)
		}
		out.Replied = append(out.Replied, o.ID)
		out.Interactions = append(out.Interactions, o.Interaction)
		line := fmt.Sprintf("- Replied: [[%s]] ([[%s]]", o.ID, o.Interaction)
		if o.Constraint != "" {
			out.Constraints = append(out.Constraints, o.Constraint)
			line += ", [[" + o.Constraint + "]]"
		}
		lines = append(lines, line+")")
		switch {
		case o.Question != "":
			out.Answered = append(out.Answered, o.Question)
			lines = append(lines, "- Answered: [["+o.Question+"]]")
		}
		out.Warnings = append(out.Warnings, o.Warnings...)
		// The person answered in the meeting: their meeting target is replied too.
		if _, _, err := syncTargets(ctx, d, []model.Link{model.LinkTo(o.ID)}, []model.Link{model.LinkTo(o.From)}, false, func(old model.TargetState) (model.TargetState, bool) {
			if old == model.TargetScheduled || old == model.TargetRequested {
				return model.TargetReplied, true
			}
			return old, false
		}); err != nil {
			return partial(fmt.Sprintf("replies[%d] meeting target", i), err)
		}
	}
	for i, a := range in.Answers {
		if strings.TrimSpace(a.Source) == "" && !agentSurface(d) {
			a.Source = "[[" + doc.ID + "]]"
		}
		o, err := QuestionAnswer.Run(ctx, d, a)
		if err != nil {
			return partial(fmt.Sprintf("answers[%d]", i), err)
		}
		out.Answered = append(out.Answered, o.ID)
		lines = append(lines, "- Answered: [["+o.ID+"]]")
	}
	for i, ti := range tasks {
		o, err := TaskAdd.Run(ctx, d, ti)
		if err != nil {
			return partial(fmt.Sprintf("actions[%d]", i), err)
		}
		out.Tasks = append(out.Tasks, o.ID)
		if o.Request != "" {
			out.Requests = append(out.Requests, o.Request)
		}
		out.Warnings = append(out.Warnings, o.Warnings...)
		who := "you"
		if a := strings.TrimSpace(in.Actions[i].Assignee); a != "" && !strings.EqualFold(a, SelfAssignee) {
			who = normID(a)
		}
		lines = append(lines, fmt.Sprintf("- Action: [[%s]] for %s", o.ID, who))
	}

	// Mark the meeting held, with the notes and what came of it.
	doc, m, err = getMeeting(ctx, d.Store, doc.ID)
	if err != nil {
		return partial("meeting", err)
	}
	if in.Start != "" {
		if m.Start, m.End, err = checkSlot(in.Start, in.End); err != nil {
			return partial("meeting", err)
		}
	}
	m.Status = model.MeetingHeld
	m.sync()
	if next, req, warns, err := nextMeeting(ctx, d, doc, m); err != nil {
		return partial("next meeting", err)
	} else {
		out.Next, out.Request = next, req
		out.Warnings = append(out.Warnings, warns...)
	}
	block := "- " + d.Store.Now().Format(dateLayout) + ": Held."
	if notes != "" {
		block += "\n\n" + notes
	}
	if len(lines) > 0 {
		block += "\n\n" + strings.Join(lines, "\n")
	}
	doc.Body = appendToSection(doc.Body, notesHeading, block)
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return partial("meeting", err)
	}
	out.Status, out.Rev = m.Status, saved.Rev

	// A request asked in the meeting and not answered in it is now a wait on
	// the people, like any request that was sent.
	changed, warns, err := syncTargets(ctx, d, m.RFX, m.With, false, func(old model.TargetState) (model.TargetState, bool) {
		if old == model.TargetScheduled || old == model.TargetRequested {
			return model.TargetSent, true
		}
		return old, false
	})
	out.Waiting = append(out.Waiting, changed...)
	out.Warnings = append(out.Warnings, warns...)
	slices.Sort(out.Waiting)
	if err != nil {
		return out, fmt.Errorf("meeting %s is held but its requests were not all updated: %w", doc.ID, err)
	}
	return out, nil
})
