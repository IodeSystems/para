package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// The composer is the box at the foot of every item's page. The owner writes
// what happened or what to do about the item, may attach files, and sends it
// to an agent: agent_send queues agent work that carries the owner's words
// and points the agent at the prompt sort_update. agent_unsend takes it back
// while no agent has it.

// composerPrefix starts the title of composer work.
const composerPrefix = "From the owner: "

// linkTypes are the item types a request may link (request_add links);
// the composer names any other item in context.
var linkTypes = []model.Type{model.TypeTask, model.TypeProject, model.TypeProposal, model.TypeJournal, model.TypeQuestion,
	model.TypeMeeting, model.TypeRFX, model.TypeRequest}

// AgentSendInput is what the owner wrote about an item. Text or attachments
// is required. Attachments are vault paths (attachment_add). Done asks the
// agent to close the item when it has processed the text.
type AgentSendInput struct {
	ID          string   `json:"id"`
	Text        string   `json:"text,omitempty"`
	Attachments []string `json:"attachments,omitempty"`
	Done        bool     `json:"done,omitempty"`
}

// AgentSendOutput is the queued work. Note says why nothing may run it yet.
type AgentSendOutput struct {
	Request string              `json:"request"`
	Status  model.RequestStatus `json:"status"`
	Title   string              `json:"title"`
	Note    string              `json:"note,omitempty"`
}

// composerAsk is the body of composer work: where it came from, what to do,
// the owner's words, and the attachments as embeds.
func composerAsk(id string, typ model.Type, title, text string, attachments []string, done bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The owner wrote this about [[%s]] (%s: %s) in the composer of its page.\n\n", id, typ, firstOf(title, id))
	b.WriteString("Process it with the prompt sort_update, as an update on this item: read the item; format the owner's words into its notes " +
		"(notes_read, then notes_edit with the hash: keep the notes there, add to them, and embed each attachment as written below, ![description](path)); " +
		"update the items it touches (tasks done, asks answered, new tasks and questions, meeting_hold for a meeting with the notes); " +
		"cite the owner's words as the source of each write; and name the receipts in your result. " +
		"Read an attachment with attachment_read.\n\n")
	if done {
		b.WriteString("Mark done: yes. The owner asked you to close this item after you process the words, with the op that fits its type " +
			"(task_done for a task, meeting_hold for a meeting, question_answer for a question, rfx_close for an ask).\n\n")
	} else {
		b.WriteString("Mark done: no. If the words clearly say the item is done (for example \"met with Sam, decided to ship\"), close it anyway and say so in your result.\n\n")
	}
	b.WriteString("## Owner's words\n\n")
	if t := strings.TrimSpace(text); t != "" {
		b.WriteString(flattenHeadings(t))
	} else {
		b.WriteString("(none: see the attachments)")
	}
	if len(attachments) > 0 {
		b.WriteString("\n\n## Attachments\n")
		for _, a := range attachments {
			b.WriteString("\n- " + model.AttachmentEmbed(a))
		}
	}
	return b.String()
}

// AgentSend queues agent work about an item with the owner's words.
var AgentSend = definePerson("agent_send", "Send what the owner wrote about an item to an agent: queues agent work that carries the words and attachments, linked to the item, to process as an update (prompt sort_update). done asks the agent to close the item. People only", func(ctx context.Context, d Deps, in AgentSendInput) (AgentSendOutput, error) {
	id := normID(in.ID)
	if id == "" {
		return AgentSendOutput{}, errors.New("id is empty")
	}
	text := strings.TrimSpace(strings.ReplaceAll(in.Text, "\r\n", "\n"))
	if text == "" && len(in.Attachments) == 0 {
		return AgentSendOutput{}, errors.New("write what to send, or attach a file")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return AgentSendOutput{}, linkErr("id", id, err)
	}
	var atts []string
	for _, p := range in.Attachments {
		a, err := d.Store.AttachmentStat(ctx, strings.TrimSpace(p))
		if err != nil {
			return AgentSendOutput{}, fmt.Errorf("attachment %q: %w", p, err)
		}
		if !slices.Contains(atts, a.Path) {
			atts = append(atts, a.Path)
		}
	}
	typ := doc.Meta.Header().Type
	add := RequestAddInput{Title: composerPrefix + firstOf(doc.Title, id), Ask: composerAsk(id, typ, doc.Title, text, atts, in.Done)}
	if slices.Contains(linkTypes, typ) {
		add.Links = []string{id}
	} else {
		add.Context = []string{id}
	}
	r, err := RequestAdd.Run(ctx, d, add)
	if err != nil {
		return AgentSendOutput{}, err
	}
	out := AgentSendOutput{Request: r.ID, Status: r.Status, Title: add.Title}
	runner, err := d.Store.Config(runnerConfig)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(runner) == "" {
		out.Note = "Queued. No agent runner is set (" + runnerConfig + "), so it waits until an agent on this machine claims it."
	}
	return out, nil
})

// AgentUnsendInput names composer work to take back.
type AgentUnsendInput struct {
	Request string `json:"request"`
}

// AgentUnsend cancels agent work the owner sent while no agent has claimed it.
var AgentUnsend = definePerson("agent_unsend", "Take back agent work the owner sent (agent_send) while no agent has claimed it. People only", func(ctx context.Context, d Deps, in AgentUnsendInput) (RequestChangeOutput, error) {
	doc, r, err := getRequest(ctx, d.Store, in.Request)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	if r.Status != model.RequestQueued && r.Status != model.RequestProposed {
		return RequestChangeOutput{}, fmt.Errorf("%s is %s: an agent has it or it ended, so it cannot be taken back here", doc.ID, r.Status)
	}
	return RequestCancel.Run(ctx, d, RequestCancelInput{ID: doc.ID})
})

// ItemCloseInput names an item the owner marks done from its page.
type ItemCloseInput struct {
	ID string `json:"id"`
}

// ItemCloseOutput says what closed the item: task_done or meeting_hold.
type ItemCloseOutput struct {
	ID  string `json:"id"`
	By  string `json:"by"`
	Rev string `json:"rev"`
}

// ItemClose is the composer's Mark done when the owner saves notes: a task
// is done (task_done), a meeting (a meeting, or a task of kind meeting) is
// held (meeting_hold, with the
// notes already on it). Any other item is closed by an agent (agent_send
// with done), so it is refused here.
var ItemClose = definePerson("item_close", "Mark a task done or a scheduled meeting held from its page, after the owner saved notes on it. Other items are refused: send them to an agent with done. People only", func(ctx context.Context, d Deps, in ItemCloseInput) (ItemCloseOutput, error) {
	id := normID(in.ID)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return ItemCloseOutput{}, linkErr("id", id, err)
	}
	isMeeting := doc.Meta.Header().Type == model.TypeMeeting
	if t, ok := doc.Meta.(*model.Task); ok && t.Kind == model.KindMeeting {
		isMeeting = true
	}
	switch {
	case isMeeting:
		out, err := MeetingHold.Run(ctx, d, MeetingHoldInput{ID: id})
		return ItemCloseOutput{ID: id, By: MeetingHold.Name(), Rev: out.Rev}, err
	case doc.Meta.Header().Type == model.TypeTask:
		out, err := TaskDone.Run(ctx, d, TaskDoneInput{ID: id})
		return ItemCloseOutput{ID: id, By: TaskDone.Name(), Rev: out.Rev}, err
	}
	return ItemCloseOutput{}, fmt.Errorf("%s is a %s: send it to an agent with Mark done to close it", id, doc.Meta.Header().Type)
})
