package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Titles of agent work made by investigate. A question gets the evidence title.
const (
	findEvidencePrefix = "Find evidence: "
	findContextPrefix  = "Find context: "
	hintsHeading       = "Hints"
)

// investigable are the types a person can send an agent to find context for.
var investigable = []model.Type{
	model.TypeTask, model.TypeMeeting, model.TypeQuestion, model.TypeRFX, model.TypeProject, model.TypeRequest,
}

// isFindTitle reports whether a request title was made by investigate.
func isFindTitle(title string) bool {
	return strings.HasPrefix(title, findEvidencePrefix) || strings.HasPrefix(title, findContextPrefix)
}

// investigating maps the id of each item that queued or claimed agent work
// looks into to the id of that request. It reads the requests once.
func investigating(ctx context.Context, s store.Store) (map[string]string, error) {
	docs, err := loadRequests(ctx, s)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if !openFind(doc.Title, r) {
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

// openFind reports whether a request is open agent work made by investigate.
func openFind(title string, r *model.Request) bool {
	return (r.Status == model.RequestQueued || r.Status == model.RequestClaimed) && isFindTitle(title)
}

// InvestigateInput is the input of investigate.
type InvestigateInput struct {
	ID     string `json:"id"`
	Hint   string `json:"hint,omitempty"`   // where to look, in words: "check the wiki", "search #platform"
	Runner string `json:"runner,omitempty"` // install id that may claim; default as request_add
}

// InvestigateOutput is the output of investigate. Appended is true when the
// item already had open agent work and the hint went into it.
type InvestigateOutput struct {
	ID       string              `json:"id"`
	Request  string              `json:"request"`
	Status   model.RequestStatus `json:"status"`
	Runner   string              `json:"runner"`
	Appended bool                `json:"appended"`
}

func findInstruction(id string, question bool) string {
	s := "Find context for " + id + ".\n\nRead the item. Gather context and sources with your own tools; " +
		"stay read-only outside para. "
	if question {
		s += "Propose an answer with question_answer and at least one source (a URL or a document id, with a quote). " +
			"Do not decide for the owner. "
	} else {
		s += "File a proposal that adds the context and sources to the item. "
	}
	return s + "No source, no item: a guess is not a source. If you find nothing, end the work and say so."
}

// Investigate queues agent work that finds context for an item. A question
// gets a proposed answer with sources; any other item gets a proposal that adds
// context and sources. The owner reviews it as always. One open run per item: a
// second call adds its hint to the open request.
var Investigate = definePerson("investigate", "Send an item (task, meeting, question, ask, goal, or agent work) to an agent to find context: queues agent work linked to the item, with an optional hint on where to look; if the item already has open work, the hint is added to it. The agent's result is a proposal for the owner to review", func(ctx context.Context, d Deps, in InvestigateInput) (InvestigateOutput, error) {
	id := normID(in.ID)
	if id == "" {
		return InvestigateOutput{}, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return InvestigateOutput{}, fmt.Errorf("id %s: %w", id, err)
	}
	typ := doc.Meta.Header().Type
	if !slices.Contains(investigable, typ) {
		return InvestigateOutput{}, fmt.Errorf("%s is a %s: want one of %v", doc.ID, typ, investigable)
	}
	if q, ok := doc.Meta.(*model.Question); ok && q.Status != model.QuestionOpen {
		return InvestigateOutput{}, fmt.Errorf("%s is already %s", doc.ID, q.Status)
	}
	if _, ok := doc.Meta.(*model.Request); ok && isFindTitle(doc.Title) {
		return InvestigateOutput{}, fmt.Errorf("%s is itself a search for context", doc.ID)
	}
	hint := strings.Join(strings.Fields(in.Hint), " ")
	if err := lintText("hint", hint); err != nil {
		return InvestigateOutput{}, err
	}
	looking, err := investigating(ctx, d.Store)
	if err != nil {
		return InvestigateOutput{}, err
	}
	if rid := looking[doc.ID]; rid != "" {
		return appendHint(ctx, d, doc.ID, rid, hint)
	}
	body := findInstruction(doc.ID, typ == model.TypeQuestion)
	if hint != "" {
		body = appendSection(body, hintsHeading, "- "+hint)
	}
	prefix := findContextPrefix
	if typ == model.TypeQuestion {
		prefix = findEvidencePrefix
	}
	r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: prefix + doc.Title, Ask: body, Links: []string{doc.ID}, Runner: in.Runner})
	if err != nil {
		return InvestigateOutput{}, err
	}
	return InvestigateOutput{ID: doc.ID, Request: r.ID, Status: r.Status, Runner: r.Runner}, nil
})

// appendHint adds a hint to the open request of an item.
func appendHint(ctx context.Context, d Deps, id, rid, hint string) (InvestigateOutput, error) {
	doc, r, err := getRequest(ctx, d.Store, rid)
	if err != nil {
		return InvestigateOutput{}, err
	}
	if hint != "" {
		line := "- " + hint
		if strings.Contains(doc.Body, "\n## "+hintsHeading+"\n") {
			doc.Body += "\n" + line
		} else {
			doc.Body = appendSection(doc.Body, hintsHeading, line)
		}
		if doc, err = d.Store.Update(ctx, doc); err != nil {
			return InvestigateOutput{}, fmt.Errorf("investigate: %w", err)
		}
	}
	return InvestigateOutput{ID: id, Request: doc.ID, Status: r.Status, Runner: r.Runner, Appended: true}, nil
}

// queueInvestigation queues find work linked to a proposal, for an investigation
// requested on one of its items. The text is the hint. The agent answers in a
// revision, with sources. One open find per proposal: a second request adds its
// hint to the open one.
func queueInvestigation(ctx context.Context, d Deps, p *proposal, item, text string) (string, error) {
	hint := strings.Join(strings.Fields(text), " ")
	if err := lintText("hint", hint); err != nil {
		return "", err
	}
	hint = "item " + item + ": " + hint
	looking, err := investigating(ctx, d.Store)
	if err != nil {
		return "", err
	}
	if rid := looking[p.doc.ID]; rid != "" {
		out, err := appendHint(ctx, d, p.doc.ID, rid, hint)
		return out.Request, err
	}
	body := fmt.Sprintf("Find context for %s.\n\nThe owner asked for an investigation of an item of this proposal; the hint says which. "+
		"Gather context and sources with your own tools; stay read-only outside para. "+
		"Answer in a revision: call proposal_revise with an answer for the open request and the findings in the item. "+
		"No source, no finding: a guess is not a source. If you find nothing, answer declined and say so.", p.doc.ID)
	body = appendSection(body, hintsHeading, "- "+hint)
	out, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: findContextPrefix + p.doc.Title, Ask: body, Links: []string{p.doc.ID}})
	if err != nil {
		return "", err
	}
	return out.ID, nil
}
