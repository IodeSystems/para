package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Titles of agent work made by investigate. A question gets the evidence title.
const (
	findEvidencePrefix = "Find evidence: "
	findContextPrefix  = "Find context: " // the old title, still read
	investigatePrefix  = "Investigate: "
	askPrefix          = "Ask: "
	hintsHeading       = "Hints"
)

// investigable are the types a person can send an agent to find context for.
var investigable = []model.Type{
	model.TypeTask, model.TypeMeeting, model.TypeQuestion, model.TypeRFX, model.TypeProject, model.TypeRequest,
}

// isFindTitle reports whether a request title was made by investigate.
func isFindTitle(title string) bool {
	return strings.HasPrefix(title, findEvidencePrefix) || strings.HasPrefix(title, findContextPrefix) || strings.HasPrefix(title, investigatePrefix) || strings.HasPrefix(title, askPrefix)
}

// replyNote is the owner-facing note of finished find work: the text of its
// last Result section, cut to a short length. Empty when the work is not a done
// find or has no result.
func replyNote(title, body string, r *model.Request) string {
	if r.Status != model.RequestDone || !(r.Kind == InvestigateKind || isFindTitle(title)) {
		return ""
	}
	var note string
	for _, sec := range strings.Split("\n"+body, "\n## ")[1:] {
		head, text, _ := strings.Cut(sec, "\n")
		if strings.HasPrefix(head, "Result") {
			note = strings.TrimSpace(text)
		}
	}
	if rs := []rune(note); len(rs) > 400 {
		note = string(rs[:400]) + "..."
	}
	return note
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
	return (r.Status == model.RequestQueued || r.Status == model.RequestClaimed) && (r.Kind == InvestigateKind || isFindTitle(title))
}

// InvestigateInput is the input of investigate.
type InvestigateInput struct {
	ID     string `json:"id"`
	Hint   string `json:"hint,omitempty"`   // the owner's text: a question, a correction, or where to look. Empty: investigate
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

func findInstruction(id string, question, asked bool) string {
	if asked {
		return "Ask about " + id + ".\n\nThe owner wrote text about this item (see Ask below); the item may be wrong, mistyped, or missing context. " +
			"Read the item and the text. Gather context and sources with your own tools; stay read-only outside para. " +
			"Answer the text in words in your result, or fix it: rewrite it (doc_edit), retype it (doc_retype, task_to_question), " +
			"split it (task_add with parent), drop or duplicate it (task_resolve), or add context. " +
			"Every change leaves a receipt; do not decide for the owner. " +
			"Every claim cites a source (a URL or a document id, with a quote): a guess is not a source. " +
			"If you find nothing, end the work and say so."
	}
	s := "Investigate " + id + ".\n\nRead the item. Gather context and sources with your own tools; " +
		"stay read-only outside para. "
	if question {
		s += "Answer with question_answer and at least one source (a URL or a document id, with a quote). " +
			"Cite every source. "
	} else {
		s += "Add the context and sources to the item. "
	}
	return s + "No source, no item: a guess is not a source. If you find nothing, end the work and say so."
}

// Investigate ("Ask agent") queues agent work for an item. With text, the agent answers it or proposes a fix; without, it finds context. A question
// gets an answer with sources; any other item gets context and sources added. One open run per item: a
// second call adds its hint to the open request.
var Investigate = definePerson("investigate", "Ask an agent about an item (task, meeting, question, ask, goal, or agent work): queues agent work linked to the item. With text the agent answers in words or fixes the item (rewrite, retype, split, drop, add context); with no text it finds context and sources. If the item already has open work, the text is added to it. Changes leave receipts", func(ctx context.Context, d Deps, in InvestigateInput) (InvestigateOutput, error) {
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
	if r, ok := doc.Meta.(*model.Request); ok && (r.Kind == InvestigateKind || isFindTitle(doc.Title)) {
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
	body := findInstruction(doc.ID, typ == model.TypeQuestion, hint != "")
	prefix := investigatePrefix
	switch {
	case hint != "":
		body = appendSection(body, "Ask", hint)
		prefix = askPrefix
	case typ == model.TypeQuestion:
		prefix = findEvidencePrefix
	}
	r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: prefix + doc.Title, Ask: body, Links: []string{doc.ID}, Runner: in.Runner, Kind: InvestigateKind})
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
