package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// workPrefix starts the title of agent work made by task_delegate.
const workPrefix = "Work on: "

// openWork reports whether a request is open agent work made by task_delegate.
func openWork(title string, r *model.Request) bool {
	return (r.Status == model.RequestQueued || r.Status == model.RequestClaimed) && (r.Kind == DelegateKind || strings.HasPrefix(title, workPrefix))
}

// delegated maps the id of each task that has open delegated work to the id of
// that request.
func delegated(ctx context.Context, s store.Store) (map[string]string, error) {
	docs, err := loadRequests(ctx, s)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if !openWork(doc.Title, r) {
			continue
		}
		for _, l := range r.Links {
			if _, ok := out[l.Target()]; !ok {
				out[l.Target()] = doc.ID
			}
		}
	}
	return out, nil
}

// TaskDelegateInput is the input of task_delegate.
type TaskDelegateInput struct {
	ID    string `json:"id"`
	Agent string `json:"agent,omitempty"` // agent name, or agent:<name>; default the vault's request.runner
	Hint  string `json:"hint,omitempty"`  // where to start, in words
}

// TaskDelegateOutput is the output of task_delegate. Appended is true when the
// task already had open agent work and the hint went into it. Note is set when
// the vault names no agent.
type TaskDelegateOutput struct {
	ID       string              `json:"id"`
	Request  string              `json:"request"`
	Status   model.RequestStatus `json:"status"`
	Runner   string              `json:"runner"`
	Assignee string              `json:"assignee,omitempty"`
	Appended bool                `json:"appended"`
	Note     string              `json:"note,omitempty"`
}

func delegateInstruction(id, doneWhen string) string {
	s := "Work on " + id + ".\n\nRead the task. Do the work with your own tools. " +
		"Take no external action without an outgoing draft the owner approved: work run in the background cannot ask, so write the draft (io_add direction out) and name it in your result. " +
		"Report with a resolution and sources: end the work with request_done and say what you did, with the sources."
	if doneWhen != "" {
		s += "\n\n## Done when\n\n" + doneWhen
	}
	return s
}

const noRunnerNote = "No agent is named, so the task is not assigned. Name one with the agent input, or set " + runnerConfig + "=INSTALL_ID in .para/vault.properties; until a runner is set, only this install can claim the work."

// TaskDelegate gives an open task to an agent: assigns agent:<name> and queues
// agent work titled "Work on: <title>", linked to the task. One open run per
// task: a second call adds its hint to the open work. The agent reports with a
// resolution; the task closes only if its own task_done is allowed.
var TaskDelegate = definePerson("task_delegate", "Give a task to an agent: assigns agent:<name> and queues agent work linked to the task, with an optional hint and the task's done_when; if the task already has open work, the hint is added to it. The agent reports with a resolution and sources", func(ctx context.Context, d Deps, in TaskDelegateInput) (TaskDelegateOutput, error) {
	id := normID(in.ID)
	if id == "" {
		return TaskDelegateOutput{}, errors.New("id is empty")
	}
	doc, t, err := getTask(ctx, d.Store, id)
	if err != nil {
		return TaskDelegateOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskDelegateOutput{}, fmt.Errorf("%s is already %s", id, t.Status)
	}
	hint := strings.Join(strings.Fields(in.Hint), " ")
	if err := lintText("hint", hint); err != nil {
		return TaskDelegateOutput{}, err
	}
	name := strings.TrimSpace(in.Agent)
	if name == "" {
		if name, err = d.Store.Config(runnerConfig); err != nil {
			return TaskDelegateOutput{}, err
		}
		name = strings.TrimSpace(name)
	}
	out := TaskDelegateOutput{ID: id}
	if name != "" {
		if !strings.HasPrefix(strings.ToLower(name), AgentPrefix) {
			name = AgentPrefix + name
		}
		if out.Assignee, err = resolveAssignee(ctx, d, name); err != nil {
			return TaskDelegateOutput{}, err
		}
		if t.Assignee != out.Assignee {
			t.Assignee = out.Assignee
			if doc, err = d.Store.Update(ctx, doc); err != nil {
				return TaskDelegateOutput{}, err
			}
		}
	} else {
		out.Note = noRunnerNote
	}
	open, err := delegated(ctx, d.Store)
	if err != nil {
		return TaskDelegateOutput{}, err
	}
	if rid := open[id]; rid != "" {
		o, err := appendHint(ctx, d, id, rid, hint)
		if err != nil {
			return TaskDelegateOutput{}, err
		}
		out.Request, out.Status, out.Runner, out.Appended = o.Request, o.Status, o.Runner, true
		return out, nil
	}
	body := delegateInstruction(id, t.DoneWhen)
	if hint != "" {
		body = appendSection(body, hintsHeading, "- "+hint)
	}
	if out.Note != "" {
		body = appendSection(body, "Note", out.Note)
	}
	r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: workPrefix + doc.Title, Ask: body, Links: []string{id}, Kind: DelegateKind})
	if err != nil {
		return TaskDelegateOutput{}, err
	}
	out.Request, out.Status, out.Runner = r.ID, r.Status, r.Runner
	return out, nil
})
