package ops

import (
	"context"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// agentWorkLine starts the ask of agent work queued by a redirect to an
// agent, and names the agent, so a second redirect to the same agent finds it.
const agentWorkLine = "Agent: "

// queueRedirectWork queues agent work for each agent a redirect hands the
// obligation to, as task_delegate does for a task: a request linked to the
// obligation with the reason, done_when, and the standing instruction. An
// open request for the same obligation and agent is not doubled. It returns
// the request ids made. Under a person's receipt, undoing the change archives
// the request.
func queueRedirectWork(ctx context.Context, d Deps, doc model.Document, o *model.Obligation, owers []model.Link, reason string) ([]string, error) {
	var made []string
	for _, l := range owers {
		if !model.IsAgentOwer(l) {
			continue
		}
		agent := string(l)
		reqs, err := loadRequests(ctx, d.Store)
		if err != nil {
			return made, err
		}
		if slices.ContainsFunc(reqs, func(rd model.Document) bool {
			r := rd.Meta.(*model.Request)
			return openWork(rd.Title, r) && slices.ContainsFunc(r.Links, func(k model.Link) bool { return k.Target() == doc.ID }) &&
				strings.Contains("\n"+rd.Body, "\n"+agentWorkLine+agent+"\n")
		}) {
			continue
		}
		body := agentWorkLine + agent + "\n\nWork on " + doc.ID + ": it was handed to you.\n\nRead the obligation. Do the work with your own tools. " +
			"Take no external act without an approved draft: file the draft (io_add) and name it in your result. " +
			"Report with a resolution and sources: end the work with request_done and say what you did, with the sources."
		if reason != "" {
			body += "\n\n## Why it was handed to you\n\n" + reason
		}
		if o.DoneWhen != "" {
			body += "\n\n## Done when\n\n" + o.DoneWhen
		}
		r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: workPrefix + doc.Title, Ask: body, Links: []string{doc.ID}, Kind: DelegateKind})
		if err != nil {
			return made, err
		}
		made = append(made, r.ID)
	}
	return made, nil
}
