package ops

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// The checks request_done makes on a reply: the work of a turn goes on the
// item, not only in the words to the owner.

// partlyWord finds a reply that says the work is only partly done.
var partlyWord = regexp.MustCompile(`(?i)\b(partly|partially|half)\b`)

// replyID finds a vault id in a reply: a type word, a dash, and a slug. It is
// wider than idWord: it also finds io, goal, and obligation ids.
var replyID = regexp.MustCompile(`\b(?:io|goal|obligation|project|task|rfx|question|meeting|incident|note|area|request|proposal|journal|inbox|interaction|constraint|policy|convention|exception)-[a-z0-9][a-z0-9.-]*`)

// replyIgnored are the types a reply may name without a link to the item:
// people and the records of writes are not work the item waits on.
var replyIgnored = []model.Type{model.TypePerson, model.TypeGroup, model.TypeReceipt}

// checkReply refuses a reply that leaves the item behind: a task said to be
// partly done that no write under the request changed, and vault ids the
// item does not link. A request with no item, or no reply, is not checked.
func checkReply(ctx context.Context, d Deps, req model.Document, r *model.Request, res model.RequestResolution, reply string) error {
	reply = strings.TrimSpace(reply)
	if reply == "" || len(r.Links) == 0 {
		return nil
	}
	item, err := d.Store.Get(ctx, r.Links[0].Target())
	if err != nil {
		return nil
	}
	if _, ok := item.Meta.(*model.Task); ok && (res == model.ResolutionDone || res == model.ResolutionPartial) && partlyWord.MatchString(reply) {
		touched, err := requestTouched(ctx, d.Store, req.ID, item.ID)
		if err != nil {
			return err
		}
		if !touched {
			return fmt.Errorf("%s is partly done but unchanged: split it with task_split {id: %s, done, remaining}, or edit it to say what remains", item.ID, item.ID)
		}
	}
	var calls []string
	for _, id := range namedIDs(ctx, d.Store, reply) {
		if id == item.ID || id == req.ID {
			continue
		}
		doc, err := d.Store.Get(ctx, id)
		if err != nil || slices.Contains(replyIgnored, doc.Meta.Header().Type) || linksEither(item, doc) {
			continue
		}
		calls = append(calls, fmt.Sprintf("%s: link {from: %s, to: %s, rel: depends_on} if it blocks %s, else name [[%s]] in the body or notes of %s (edit)", id, item.ID, id, item.ID, id, item.ID))
	}
	if len(calls) > 0 {
		return fmt.Errorf("the reply names ids that %s does not link; link each first: %s", item.ID, strings.Join(calls, "; "))
	}
	return nil
}

// namedIDs are the ids of vault documents a text names, in order, once each.
// A sentence end after an id is not part of it.
func namedIDs(ctx context.Context, s store.Store, text string) []string {
	var out []string
	for _, m := range replyID.FindAllString(text, -1) {
		id := strings.TrimRight(m, ".-")
		if slices.Contains(out, id) {
			continue
		}
		if _, err := s.Get(ctx, id); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// linksEither reports whether a or b names the other in its file: a link
// field, depends_on, a wikilink in the body, any rel.
func linksEither(a, b model.Document) bool {
	return strings.Contains(renderText(a), b.ID) || strings.Contains(renderText(b), a.ID)
}

// requestTouched reports whether a write under the request changed the
// document id, by the receipts not undone.
func requestTouched(ctx context.Context, s store.Store, req, id string) (bool, error) {
	refs, err := s.List(ctx, store.Filter{Folder: model.FolderReceipts, Type: model.TypeReceipt})
	if err != nil {
		return false, err
	}
	for _, ref := range refs {
		doc, r, err := getReceipt(ctx, s, ref.ID)
		if err != nil {
			return false, err
		}
		if r.Request != req || r.Undone != "" {
			continue
		}
		if r.Doc == id {
			return true, nil
		}
		c, err := model.ParseReceiptBody(doc.Body)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(c.Also, func(p model.ReceiptPart) bool { return p.Doc == id }) {
			return true, nil
		}
	}
	return false, nil
}
