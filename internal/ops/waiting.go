package ops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Waiting is an Ask. Waiting on a person is an Ask (rfx out) with a target
// and a follow-up date, never a task: a task is work the owner does.
// task_add warns on a title that reads like waiting, health flags open
// ones (task_is_waiting), and task_to_asks turns one into Asks.

// waitingRe matches a title that reads like waiting on someone.
var waitingRe = regexp.MustCompile(`(?i)\b(wait(ing)?\s+(for|on|until)|awaiting|pending\s+(a\s+)?(reply|response|answer|approval|sign[- ]?off)|hear\s+back|follow[- ]?up\s+with\b.*\b(response|reply|answer))\b`)

// readsLikeWaiting reports whether a title reads like waiting on someone.
func readsLikeWaiting(title string) bool { return waitingRe.MatchString(title) }

// waitingWarning is the task_add warning for such a title.
func waitingWarning(id string) string {
	return fmt.Sprintf("the title reads like waiting on someone: waiting on a person is an Ask (rfx_add direction out, with a target and next_nudge), not a task. "+
		"Turn it into Asks with task_to_asks {id: %s, people: [...]}", id)
}

// TaskToAsksInput is the input of task_to_asks.
type TaskToAsksInput struct {
	ID     string   `json:"id"`
	People []string `json:"people"`          // person or group ids: one Ask each
	Route  string   `json:"route,omitempty"` // the domain; default each person's team, else role, else org
	// Medium is how each Ask goes; default each person's preferred contact,
	// else other.
	Medium    model.Medium `json:"medium,omitempty"`
	NextNudge string       `json:"next_nudge,omitempty"` // YYYY-MM-DD; default the task's due date
}

// TaskToAsksOutput is the output of task_to_asks.
type TaskToAsksOutput struct {
	ID       string   `json:"id"`
	Asks     []string `json:"asks"`
	Moved    []string `json:"moved"` // dependents that now wait on the Asks
	Warnings []string `json:"warnings"`
}

// TaskToAsks turns a task that waits on people into one Ask per person.
var TaskToAsks = withOneReceipt(define("task_to_asks", "Turn a task that is waiting on people into Asks: one rfx out per person (route from route, else the person's team, role, or org; target by medium, else the person's preferred contact), dependents of the task move to the Asks, and the task closes as superseded by them. One receipt; receipt_undo reverses all of it", func(ctx context.Context, d Deps, in TaskToAsksInput) (TaskToAsksOutput, error) {
	id := normID(in.ID)
	if len(in.People) == 0 {
		return TaskToAsksOutput{}, errors.New("people is empty: name who the task waits on, one Ask each")
	}
	doc, t, err := getTask(ctx, d.Store, id)
	if err != nil {
		return TaskToAsksOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskToAsksOutput{}, fmt.Errorf("%s is already %s", id, t.Status)
	}
	if k := t.EffectiveKind(); k != model.KindTask {
		return TaskToAsksOutput{}, fmt.Errorf("%s is a %s, not a plain task", id, k)
	}
	nudge := strings.TrimSpace(in.NextNudge)
	if nudge == "" {
		nudge = string(t.Due)
	}
	type ask struct {
		to     string
		route  string
		medium model.Medium
	}
	var plan []ask
	for _, p := range in.People {
		pid := normID(p)
		pd, err := d.Store.Get(ctx, pid)
		if err != nil {
			return TaskToAsksOutput{}, linkErr("people", pid, err)
		}
		a := ask{to: pid, route: strings.TrimSpace(in.Route), medium: in.Medium}
		switch m := pd.Meta.(type) {
		case *model.Person:
			if a.route == "" {
				a.route = firstOf(m.Team.Target(), m.Role.Target(), m.Org, pid)
			}
			if a.medium == "" {
				for _, c := range m.Contacts {
					if c.Preferred || a.medium == "" {
						a.medium = c.Method
					}
				}
			}
		case *model.Group:
			if a.route == "" {
				a.route = pid
			}
		default:
			return TaskToAsksOutput{}, fmt.Errorf("people: %s is a %s, not a person or group", pid, pd.Meta.Header().Type)
		}
		if a.medium == "" {
			a.medium = model.MediumOther
		}
		plan = append(plan, a)
	}
	out := TaskToAsksOutput{ID: id, Asks: []string{}, Moved: []string{}, Warnings: []string{}}
	body := strings.TrimSpace(doc.Body)
	for _, a := range plan {
		text := doc.Title
		if body != "" {
			text += "\n\n" + body
		}
		text += "\n\nFrom the task [[" + id + "]]."
		r, err := rfxAdd(ctx, d, RFXAddInput{Direction: model.RFXOut, Ask: text, Route: a.route, Goal: firstOf(t.Goal.Target(), t.Area.Target()),
			Targets: []TargetInput{{To: a.to, Medium: a.medium}}, NextNudge: nudge, SelfServe: "Moved from a task that waited on this person."})
		if err != nil {
			return out, fmt.Errorf("ask of %s: %w (made: %v)", a.to, err, out.Asks)
		}
		out.Asks = append(out.Asks, r.ID)
		out.Warnings = append(out.Warnings, r.Warnings...)
	}
	moved, err := relinkDependentsTo(ctx, d.Store, id, out.Asks)
	out.Moved = append(out.Moved, moved...)
	if err != nil {
		return out, err
	}
	note := "Waiting on people is an Ask: moved to " + wikilinks(out.Asks) + "."
	if _, err := TaskResolve.Run(ctx, d, TaskResolveInput{ID: id, Outcome: model.OutcomeSuperseded, Link: out.Asks[0], Note: note}); err != nil {
		return out, err
	}
	return out, nil
}))
