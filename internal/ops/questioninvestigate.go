package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// findEvidencePrefix starts the title of agent work made by question_investigate.
const findEvidencePrefix = "Find evidence: "

// investigating returns the ids of open questions that queued or claimed
// agent work looks into.
func investigating(ctx context.Context, s store.Store) (map[string]bool, error) {
	docs, err := loadRequests(ctx, s)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if (r.Status != model.RequestQueued && r.Status != model.RequestClaimed) || !strings.HasPrefix(doc.Title, findEvidencePrefix) {
			continue
		}
		for _, l := range r.Links {
			out[l.Target()] = true
		}
	}
	return out, nil
}

// QuestionInvestigateInput is the input of question_investigate.
type QuestionInvestigateInput struct {
	ID     string `json:"id"`
	How    string `json:"how"`              // where to look, in words: "check the wiki", "search #platform"
	Runner string `json:"runner,omitempty"` // install id that may claim; default as request_add
}

// QuestionInvestigateOutput is the output of question_investigate.
type QuestionInvestigateOutput struct {
	ID      string              `json:"id"`
	Request string              `json:"request"`
	Status  model.RequestStatus `json:"status"`
	Runner  string              `json:"runner"`
}

// QuestionInvestigate queues agent work that looks for evidence for a question.
// The agent proposes an answer with sources; the owner reviews it as always.
var QuestionInvestigate = definePerson("question_investigate", "Send an open question to an agent to find evidence: queues agent work linked to the question; the agent's result is a proposed answer with sources for the owner to review, and the question stays open", func(ctx context.Context, d Deps, in QuestionInvestigateInput) (QuestionInvestigateOutput, error) {
	doc, q, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionInvestigateOutput{}, err
	}
	if q.Status != model.QuestionOpen {
		return QuestionInvestigateOutput{}, fmt.Errorf("%s is already %s", doc.ID, q.Status)
	}
	how := strings.TrimSpace(strings.ReplaceAll(in.How, "\r\n", "\n"))
	if how == "" {
		return QuestionInvestigateOutput{}, errors.New("how is empty: say where to look, such as \"check the wiki\"")
	}
	if err := lintText("how", how); err != nil {
		return QuestionInvestigateOutput{}, err
	}
	looking, err := investigating(ctx, d.Store)
	if err != nil {
		return QuestionInvestigateOutput{}, err
	}
	if looking[doc.ID] {
		return QuestionInvestigateOutput{}, fmt.Errorf("%s already has agent work looking for evidence", doc.ID)
	}
	ask := "Find evidence for the question " + doc.ID + ".\n\nWhere to look: " + how +
		"\n\nPropose an answer with question_answer and at least one source (a URL or a document id, with a quote). " +
		"Do not decide for the owner. If you find nothing, end the work and say so."
	r, err := RequestAdd.Run(ctx, d, RequestAddInput{
		Title: findEvidencePrefix + doc.Title, Ask: ask, Links: []string{doc.ID}, Runner: in.Runner,
	})
	if err != nil {
		return QuestionInvestigateOutput{}, err
	}
	return QuestionInvestigateOutput{ID: doc.ID, Request: r.ID, Status: r.Status, Runner: r.Runner}, nil
})
