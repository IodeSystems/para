package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Updates are free text the owner writes on a task. They are kept whole under
// this heading; an agent sorts them into typed items.
const (
	updatesHeading = "Updates"
	sortPrefix     = "Sort update: "
)

// openSort maps the id of each task with open "Sort update" work to the id of
// that request.
func openSort(ctx context.Context, s store.Store) (map[string]string, error) {
	docs, err := loadRequests(ctx, s)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if (r.Status != model.RequestQueued && r.Status != model.RequestClaimed) || (r.Kind != TaskUpdateKind && !strings.HasPrefix(doc.Title, sortPrefix)) {
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

// TaskUpdateInput is the input of task_update.
type TaskUpdateInput struct {
	ID   string `json:"id"`
	Text string `json:"text"` // free text, kept whole
}

// TaskUpdateOutput is the output of task_update. Saved is always true on
// success. Request is the agent work that sorts the text; it is empty, and Note
// says why, when nothing can run it.
type TaskUpdateOutput struct {
	ID       string              `json:"id"`
	Rev      string              `json:"rev"`
	Saved    bool                `json:"saved"`
	Request  string              `json:"request,omitempty"`
	Status   model.RequestStatus `json:"status,omitempty"`
	Appended bool                `json:"appended"`
	Note     string              `json:"note,omitempty"`
}

// updatesBody appends a dated entry under ## Updates, creating the section.
func updatesBody(body, date, text string) string {
	entry := "### " + date + "\n\n" + flattenHeadings(text)
	i := strings.Index("\n"+body, "\n## "+updatesHeading+"\n")
	if i < 0 {
		return appendSection(body, updatesHeading, entry)
	}
	// The section runs to the next "## " heading or the end.
	start := i + len("\n## "+updatesHeading+"\n")
	rest := body[start:]
	if j := strings.Index("\n"+rest, "\n## "); j >= 0 {
		return strings.TrimRight(body[:start]+rest[:j], "\n") + "\n\n" + entry + "\n\n" + rest[j:]
	}
	return strings.TrimRight(body, "\n") + "\n\n" + entry
}

const sortInstruction = "Sort the newest update on %s.\n\nRead the task and its ## Updates. Run similar on each candidate. " +
	"Make these changes with ops, grouped by kind: action items (task_add with parent), " +
	"open questions (question_add scoped to the task), questions answered (question_answer with the update as a source), " +
	"commitments (task_add with commitment, a due date, the owner as assignee), resolved policy (constraint_add or exception_add, " +
	"with a link to the task in text), follow-up tasks (task_add, depends_on when it waits). " +
	"Cite the owner's words as the source of each change. Say in your result what you left unsorted. Never rewrite the owner's text. " +
	"Take no external action: an outward action needs an outgoing draft the owner approved (io_add direction out, then io_approve)."

// TaskUpdate appends the owner's text under ## Updates, whole, and queues agent
// work "Sort update: <title>" linked to the task. One open run per task: another
// update adds a note to it. The text is saved even when nothing can run it.
var TaskUpdate = definePerson("task_update", "Add free text to a task under ## Updates, kept whole, and queue agent work to sort it into action items, open questions, answered questions, commitments, resolved policy, and follow-up tasks ; person only. If no runner is set the text is still saved and the output says so", func(ctx context.Context, d Deps, in TaskUpdateInput) (TaskUpdateOutput, error) {
	id := normID(in.ID)
	if id == "" {
		return TaskUpdateOutput{}, errors.New("id is empty")
	}
	text := strings.TrimSpace(strings.ReplaceAll(in.Text, "\r\n", "\n"))
	if text == "" {
		return TaskUpdateOutput{}, errors.New("text is empty")
	}
	if err := lintText("text", text); err != nil {
		return TaskUpdateOutput{}, err
	}
	doc, _, err := getTask(ctx, d.Store, id)
	if err != nil {
		return TaskUpdateOutput{}, err
	}
	date := d.Store.Now().Format(dateLayout)
	doc.Body = updatesBody(doc.Body, date, text)
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return TaskUpdateOutput{}, fmt.Errorf("task_update: %w", err)
	}
	out := TaskUpdateOutput{ID: id, Rev: doc.Rev, Saved: true}
	runner, err := d.Store.Config(runnerConfig)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(runner) == "" {
		out.Note = "Saved. No agent runner is set, so nothing sorts the update; set " + runnerConfig + "=INSTALL_ID in .para/vault.properties, or sort it by hand."
	}
	open, err := openSort(ctx, d.Store)
	if err != nil {
		return out, err
	}
	if rid := open[id]; rid != "" {
		o, err := appendHint(ctx, d, id, rid, "update dated "+date+" added to the task; sort it too")
		if err != nil {
			return out, err
		}
		out.Request, out.Status, out.Appended = o.Request, o.Status, true
		return out, nil
	}
	body := fmt.Sprintf(sortInstruction, id)
	r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: sortPrefix + doc.Title, Ask: body, Links: []string{id}, Kind: TaskUpdateKind})
	if err != nil {
		return out, fmt.Errorf("task_update: saved, but queueing the sort failed: %w", err)
	}
	out.Request, out.Status = r.ID, r.Status
	return out, nil
})
