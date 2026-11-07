package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// A reply owed is an obligation. An incoming message whose sender waits for
// the owner's reply (expects response) gets one obligation, "Reply to
// <sender>: <subject>", owed to the sender when the sender is a person or a
// group, due io.reply_days after the message came. The message links it
// (reply) and it links the message (reply_to). While the obligation is open
// the message does not need the owner on its own: the obligation holds it, so
// it shows once.
//
// syncReply keeps the two in step from the message's state, so every path
// (io_add, io_edit, io_file, io_drop, io_send of a threaded reply, a receipt
// undo, a hand edit the tick finds) reaches the same result:
//
//	open, expects response, no obligation  -> make it
//	open, expects response, closed by para -> reopen it
//	answered by a sent reply               -> close it done
//	filed or dropped with no reply         -> close it dropped
//	open, no reply expected any more       -> close it dropped
//
// Para closes it with a reason that starts with replyReasonPrefix; only such a
// close is reopened, so a close by the owner stays.

// ReplyDaysKey is the vault setting of how many days after a message its
// reply is due.
const ReplyDaysKey = "io.reply_days"

// DefaultReplyDays is the reply window when the vault sets none.
const DefaultReplyDays = 2

// replyBackfillMax caps the reply obligations one tick makes, so a vault with
// a long backlog fills over several passes.
const replyBackfillMax = 50

// replyReasonPrefix starts the reason of a reply obligation para closed.
const replyReasonPrefix = "io: "

// replyWanted reports whether an incoming message holds a reply obligation:
// open or not, it expects a response from the owner and is not the owner's
// own capture or a logged contact.
func replyWanted(m *model.IO) bool {
	return m.Direction == model.IOIn && m.Expects == model.ExpectsResponse && !m.Logged() &&
		!slices.Contains(model.OwnerMediums, m.Medium)
}

// syncReply brings the reply obligation of the io item id in step with it.
// It returns the obligation id it made, or "". An item that is not io, or a
// vault below the obligation format, is left alone.
func syncReply(ctx context.Context, d Deps, id string) (string, error) {
	if !obligationsOn(d) {
		return "", nil
	}
	doc, err := d.Store.Get(ctx, normID(id))
	if err != nil {
		return "", err
	}
	m, ok := doc.Meta.(*model.IO)
	if !ok || m.Direction != model.IOIn {
		return "", nil
	}
	var odoc model.Document
	var o *model.Obligation
	if t := m.Reply.Target(); t != "" {
		if od, err := d.Store.Get(ctx, t); err == nil && od.Folder != model.FolderArchive {
			if ob, ok := od.Meta.(*model.Obligation); ok {
				odoc, o = od, ob
			}
		} else if err != nil && !errors.Is(err, model.ErrNotFound) {
			return "", err
		}
	}
	gone := doc.Folder == model.FolderArchive // the message's own write was undone
	open := m.State.Open() && !gone
	switch {
	case open && replyWanted(m) && o == nil:
		return makeReply(ctx, d, doc, m)
	case open && replyWanted(m) && !o.Open() && strings.HasPrefix(o.Reason, replyReasonPrefix):
		o.State, o.Outcome, o.Reason, o.Closed = model.StateOpen, "", "", ""
		_, err := d.Store.Update(ctx, odoc)
		return "", err
	case o == nil || !o.Open():
		return "", nil
	case open && replyWanted(m):
		return "", nil
	}
	outcome, why := model.OutcomeDropped, replyReasonPrefix+"no reply expected any more"
	switch {
	case gone:
		why = replyReasonPrefix + "the message was undone"
	case m.State == model.IOFiled && repliedBy(ctx, d, m.Became.Target()):
		outcome, why = model.OutcomeDone, replyReasonPrefix+"replied in "+string(m.Became)
	case m.State == model.IOFiled:
		why = replyReasonPrefix + "filed into " + string(m.Became) + " without a reply"
	case m.State == model.IODropped:
		why = replyReasonPrefix + "message dropped: " + m.Reason
	}
	o.State, o.Outcome, o.Reason, o.Closed = model.StateClosed, outcome, flatLine(why), model.Date(d.Store.Now().Format(dateLayout))
	if _, err := d.Store.Update(ctx, odoc); err != nil {
		return "", err
	}
	return "", recordClose(ctx, d, odoc.ID, string(outcome))
}

// repliedBy reports whether id is an outgoing io item that was sent: the
// reply a threaded io_send filed the message into.
func repliedBy(ctx context.Context, d Deps, id string) bool {
	if id == "" {
		return false
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return false
	}
	m, ok := doc.Meta.(*model.IO)
	return ok && m.Direction == model.IOOut && m.State == model.IOSent
}

// makeReply makes the reply obligation of an incoming message and links it
// from the message.
func makeReply(ctx context.Context, d Deps, doc model.Document, m *model.IO) (string, error) {
	days, err := configInt(d.Store, ReplyDaysKey, DefaultReplyDays)
	if err != nil {
		return "", err
	}
	came := d.Store.Now()
	if t, err := time.Parse(time.RFC3339Nano, m.At); err == nil {
		came = t
	}
	due := model.Date(came.In(d.Store.Now().Location()).AddDate(0, 0, days).Format(dateLayout))
	// owed_to is the sender when it is a person or a group (io_add resolved
	// it as io_file resolves requested_by); free text stays in the title and
	// the body, never as a broken link.
	var owedTo []model.Link
	for _, id := range ioLinks([]string{m.From}) {
		if _, err := d.Store.Get(ctx, id); err == nil {
			owedTo = append(owedTo, model.LinkTo(id))
		}
	}
	who := senderName(ctx, d, m.From)
	title := ioTitle("Reply to "+who+": "+firstOf(m.Subject, doc.Title), "")
	body := "Reply to [[" + doc.ID + "]]."
	if len(owedTo) == 0 && strings.TrimSpace(m.From) != "" {
		body += "\n\nFrom: " + m.From
	}
	o := &model.Obligation{Common: model.Common{Type: model.TypeObligation}, Kind: model.KindCommitment, State: model.StateOpen,
		Schedule: model.ScheduleOnce, OwedTo: owedTo, Due: due, Effort: m.Effort, ReplyTo: model.LinkTo(doc.ID)}
	saved, err := d.Store.Create(ctx, model.Document{Folder: model.FolderObligations, Title: title, Body: body, Meta: o})
	if err != nil {
		return "", err
	}
	cur, err := d.Store.Get(ctx, doc.ID)
	if err != nil {
		return saved.ID, fmt.Errorf("%s was made, but %s does not link it: %w", saved.ID, doc.ID, err)
	}
	if cm, ok := cur.Meta.(*model.IO); ok {
		cm.Reply = model.LinkTo(saved.ID)
		if _, err := d.Store.Update(ctx, cur); err != nil {
			return saved.ID, fmt.Errorf("%s was made, but %s does not link it: %w", saved.ID, doc.ID, err)
		}
	}
	return saved.ID, nil
}

// senderName is how a reply's title names the sender: the person's or the
// group's title, else the text, else "the sender".
func senderName(ctx context.Context, d Deps, from string) string {
	if ids := ioLinks([]string{from}); len(ids) > 0 {
		if doc, err := d.Store.Get(ctx, ids[0]); err == nil {
			return doc.Title
		}
		return ids[0]
	}
	return firstOf(ioLine(from), "the sender")
}

// replyThread is the thread a draft names: a reply obligation stands for the
// message it replies to, so a draft threaded to one answers that message.
func replyThread(ctx context.Context, d Deps, id string) string {
	if strings.TrimSpace(id) == "" {
		return id
	}
	if doc, err := d.Store.Get(ctx, normID(id)); err == nil {
		if o, ok := doc.Meta.(*model.Obligation); ok && o.ReplyTo != "" {
			return o.ReplyTo.Target()
		}
	}
	return id
}

// syncReplyWarn is syncReply for a caller whose own write already succeeded:
// a failure is a warning, and the tick tries again.
func syncReplyWarn(ctx context.Context, d Deps, id string) []string {
	if _, err := syncReply(ctx, d, id); err != nil {
		return []string{fmt.Sprintf("reply obligation of %s not updated (the tick tries again): %v", normID(id), err)}
	}
	return nil
}

// tickReplies brings every incoming message's reply obligation in step: it
// makes the missing ones for open messages that expect a reply (at most
// replyBackfillMax a pass) and closes or reopens the ones whose message
// changed by hand.
func tickReplies(ctx context.Context, d Deps, out *TickOutput) error {
	if !obligationsOn(d) {
		return nil
	}
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeIO})
	if err != nil {
		return err
	}
	made := 0
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return err
		}
		m, ok := doc.Meta.(*model.IO)
		if !ok || m.Direction != model.IOIn || (m.Reply == "" && !(m.State.Open() && replyWanted(m))) {
			continue
		}
		if m.Reply == "" && made >= replyBackfillMax {
			continue
		}
		id, err := syncReply(ctx, d, doc.ID)
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("reply of %s: %v", doc.ID, err))
			continue
		}
		if id != "" {
			made++
			out.Replies = append(out.Replies, id)
		}
	}
	return nil
}
