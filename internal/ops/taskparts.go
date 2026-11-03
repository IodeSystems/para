package ops

import (
	"context"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// PartRef is a short reference to a document that belongs to a task.
type PartRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`             // task, constraint, or exception
	Status string `json:"status,omitempty"` // task status, or exception status
	Due    string `json:"due,omitempty"`
	Owner  string `json:"owner,omitempty"` // assignee of a commitment; empty is the owner
}

// TaskParts are the sections of a task page. They are computed from links, not
// stored: action items are tasks with this task as parent; commitments are
// those tagged commitment; answered questions are questions scoped to it;
// policy is a constraint or exception that links it; follow-ups are tasks that
// wait on it or link it in their body.
type TaskParts struct {
	ActionItems []PartRef      `json:"action_items"`
	Commitments []PartRef      `json:"commitments"`
	Answered    []QuestionView `json:"answered"`
	Policy      []PartRef      `json:"policy"`
	FollowUps   []PartRef      `json:"follow_ups"`
}

func bodyLinks(body, id string) bool {
	return strings.Contains(body, "[["+id+"]]") || strings.Contains(body, "[["+id+"|")
}

// taskParts reads the vault once and sorts what belongs to the task id.
func taskParts(ctx context.Context, s store.Store, id string) (TaskParts, error) {
	out := TaskParts{ActionItems: []PartRef{}, Commitments: []PartRef{}, Answered: []QuestionView{}, Policy: []PartRef{}, FollowUps: []PartRef{}}
	refs, err := s.List(ctx, store.Filter{})
	if err != nil {
		return out, err
	}
	for _, r := range refs {
		switch r.Type {
		case model.TypeTask, model.TypeConstraint, model.TypePolicyException, model.TypeQuestion:
		default:
			continue
		}
		if r.ID == id {
			continue
		}
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return out, err
		}
		switch m := doc.Meta.(type) {
		case *model.Task:
			ref := PartRef{ID: doc.ID, Title: doc.Title, Kind: "task", Status: string(m.Status), Due: string(m.Due), Owner: m.Assignee}
			switch {
			case m.Parent.Target() == id && m.EffectiveKind() == model.KindCommitment:
				out.Commitments = append(out.Commitments, ref)
			case m.Parent.Target() == id:
				out.ActionItems = append(out.ActionItems, ref)
			case slices.ContainsFunc(m.DependsOn, func(l model.Link) bool { return l.Target() == id }) || bodyLinks(doc.Body, id):
				out.FollowUps = append(out.FollowUps, ref)
			}
		case *model.Question:
			if m.Status == model.QuestionAnswered && scopeID(m) == id {
				v, err := questionView(doc)
				if err != nil {
					return out, err
				}
				out.Answered = append(out.Answered, v)
			}
		case *model.Constraint:
			if slices.Contains(m.AppliesIn, id) || bodyLinks(doc.Body, id) {
				out.Policy = append(out.Policy, PartRef{ID: doc.ID, Title: doc.Title, Kind: "constraint"})
			}
		case *model.PolicyException:
			if bodyLinks(doc.Body, id) {
				out.Policy = append(out.Policy, PartRef{ID: doc.ID, Title: doc.Title, Kind: "exception", Status: string(m.Status)})
			}
		}
	}
	return out, nil
}

// updatesText returns the text under ## Updates of a body.
func updatesText(body string) string {
	i := strings.Index("\n"+body, "\n## "+updatesHeading+"\n")
	if i < 0 {
		return ""
	}
	rest := body[i+len("\n## "+updatesHeading+"\n")-1:]
	if j := strings.Index("\n"+rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// openChildren lists the ids of open action items of a task, and its open
// questions.
func openChildren(ctx context.Context, s store.Store, id string) (items, questions []string, err error) {
	refs, err := s.List(ctx, store.Filter{})
	if err != nil {
		return nil, nil, err
	}
	for _, r := range refs {
		if r.Type != model.TypeTask && r.Type != model.TypeQuestion {
			continue
		}
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, nil, err
		}
		switch m := doc.Meta.(type) {
		case *model.Task:
			if m.Parent.Target() == id && !taskTerminal(m.Status) {
				items = append(items, doc.ID)
			}
		case *model.Question:
			if m.Status == model.QuestionOpen && scopeID(m) == id {
				questions = append(questions, doc.ID)
			}
		}
	}
	return items, questions, nil
}
