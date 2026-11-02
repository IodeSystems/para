package ops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

// A task has an assignee: empty (or "self") for the owner, a person id, or
// agent:<name>. Para stores it and never contacts the assignee.

// AgentPrefix starts the assignee of an agent.
const AgentPrefix = "agent:"

// SelfAssignee is the input word for the owner. It is stored as no assignee.
const SelfAssignee = "self"

var agentNameRe = regexp.MustCompile(`^[a-z0-9]+(?:[-_.][a-z0-9]+)*$`)

// IsOther reports whether a stored assignee is someone other than the owner.
func IsOther(assignee string) bool { return assignee != "" && assignee != SelfAssignee }

// resolveAssignee checks an assignee input and returns the stored form: empty
// for self, agent:<name>, or the id of an existing person.
func resolveAssignee(ctx context.Context, d Deps, raw string) (string, error) {
	s := strings.TrimSpace(raw)
	switch {
	case s == "" || strings.EqualFold(s, SelfAssignee):
		return "", nil
	case strings.HasPrefix(strings.ToLower(s), AgentPrefix):
		name := strings.ToLower(strings.TrimSpace(s[len(AgentPrefix):]))
		if !agentNameRe.MatchString(name) {
			return "", fmt.Errorf("assignee %q: want agent:<name> using letters, digits, and - _ separators", s)
		}
		return AgentPrefix + name, nil
	}
	id := normID(s)
	if _, _, err := getPerson(ctx, d, id); err != nil {
		return "", fmt.Errorf("assignee %s: want self, a person id, or agent:<name>: %w", id, err)
	}
	return id, nil
}

// TaskAssignInput is the input of task_assign.
type TaskAssignInput struct {
	ID       string `json:"id"`
	Assignee string `json:"assignee"` // self, a person id, or agent:<name>
}

// TaskAssignOutput is the output of task_assign.
type TaskAssignOutput struct {
	ID       string `json:"id"`
	Assignee string `json:"assignee,omitempty"` // empty means self
	Rev      string `json:"rev"`
}

// TaskAssign sets who an open task is assigned to. A task assigned to someone
// else leaves `next` and shows under `waiting`.
var TaskAssign = define("task_assign", "Assign a task to yourself, a person, or agent:<name>", func(ctx context.Context, d Deps, in TaskAssignInput) (TaskAssignOutput, error) {
	if strings.TrimSpace(in.Assignee) == "" {
		return TaskAssignOutput{}, errors.New("assignee is empty: say self, a person id, or agent:<name>")
	}
	assignee, err := resolveAssignee(ctx, d, in.Assignee)
	if err != nil {
		return TaskAssignOutput{}, err
	}
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskAssignOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskAssignOutput{}, fmt.Errorf("%s is already %s", doc.ID, t.Status)
	}
	t.Assignee = assignee
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskAssignOutput{}, err
	}
	return TaskAssignOutput{ID: doc.ID, Assignee: assignee, Rev: saved.Rev}, nil
})

// WaitingInput is the input of waiting.
type WaitingInput struct {
	Today string `json:"today,omitempty" query:"today"` // YYYY-MM-DD; empty means the local date
}

// WaitingOutput is the output of waiting: what the owner waits on. RFX holds
// open outbound requests (see rfx_waiting); Tasks holds open tasks assigned to
// someone else, earliest due first.
type WaitingOutput struct {
	Today string        `json:"today"`
	RFX   []WaitingItem `json:"rfx"`
	Tasks []TaskView    `json:"tasks"`
	Count int           `json:"count"`
}

// Waiting lists what you wait on: outbound RFX with a target to hear from, and
// tasks assigned to other people or agents.
var Waiting = defineRead("waiting", "List what you wait on: outbound requests and tasks assigned to others", func(ctx context.Context, d Deps, in WaitingInput) (WaitingOutput, error) {
	today := in.Today
	if today == "" {
		today = time.Now().Format(dateLayout)
	}
	rfx, err := RFXWaiting.Run(ctx, d, RFXWaitingInput{Direction: model.RFXOut, Today: today})
	if err != nil {
		return WaitingOutput{}, err
	}
	all, err := listTasks(ctx, d.Store, "", "")
	if err != nil {
		return WaitingOutput{}, err
	}
	out := WaitingOutput{Today: today, RFX: rfx.Items, Tasks: []TaskView{}}
	for _, t := range all {
		if IsOther(t.Assignee) && !taskTerminal(t.Status) {
			out.Tasks = append(out.Tasks, t)
		}
	}
	sortNext(out.Tasks)
	out.Count = len(out.RFX) + len(out.Tasks)
	return out, nil
})

// assignedTo lists the open tasks assigned to a person id.
func assignedTo(ctx context.Context, d Deps, person string) ([]TaskView, error) {
	all, err := listTasks(ctx, d.Store, "", "")
	if err != nil {
		return nil, err
	}
	out := []TaskView{}
	for _, t := range all {
		if t.Assignee == person && !taskTerminal(t.Status) {
			out = append(out, t)
		}
	}
	sortNext(out)
	return out, nil
}
