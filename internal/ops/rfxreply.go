package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// ReplyOutcome is how a target answered.
type ReplyOutcome string

const (
	ReplyAnswered ReplyOutcome = "answered"
	ReplyNo       ReplyOutcome = "no"
	ReplyPartial  ReplyOutcome = "partial"
)

// ReplyOutcomes lists every ReplyOutcome.
var ReplyOutcomes = []ReplyOutcome{ReplyAnswered, ReplyNo, ReplyPartial}

// Enum implements the MCP enum hook.
func (ReplyOutcome) Enum() []string {
	out := make([]string, len(ReplyOutcomes))
	for i, o := range ReplyOutcomes {
		out[i] = string(o)
	}
	return out
}

// ReplyHeard is where the owner heard a reply that has no link.
type ReplyHeard string

const (
	HeardInPerson ReplyHeard = "in_person"
	HeardCall     ReplyHeard = "call"
	HeardMessage  ReplyHeard = "message"
	HeardOther    ReplyHeard = "other"
)

// ReplyHeards lists every ReplyHeard.
var ReplyHeards = []ReplyHeard{HeardInPerson, HeardCall, HeardMessage, HeardOther}

// Enum implements the MCP enum hook.
func (ReplyHeard) Enum() []string {
	out := make([]string, len(ReplyHeards))
	for i, o := range ReplyHeards {
		out[i] = string(o)
	}
	return out
}

var heardWords = map[ReplyHeard]string{
	HeardInPerson: "heard in person", HeardCall: "heard on a call",
	HeardMessage: "heard in a message", HeardOther: "heard elsewhere",
}

const (
	repliesHeading = "## Replies"
	// reviewDefaultDays is when a "no" is asked again if the caller gives no date.
	reviewDefaultDays = 180
)

// RFXReplyInput is the input of rfx_reply.
type RFXReplyInput struct {
	ID        string       `json:"id"`
	From      string       `json:"from"`                 // person or group id of the target that replied
	Via       model.Medium `json:"via,omitempty"`        // needed when the target has several mediums
	Outcome   ReplyOutcome `json:"outcome"`              // answered | no | partial
	Text      string       `json:"text"`                 // the reply, or your summary of it; first line is the title
	URL       string       `json:"url,omitempty"`        // link to the original reply; required for no unless the target has a ref URL
	Heard     ReplyHeard   `json:"heard,omitempty"`      // where the owner heard it: in_person | call | message | other. With no url, the owner's no is accepted and cites this as its source; an agent still needs a url
	Date      string       `json:"date,omitempty"`       // YYYY-MM-DD; default the local date
	Why       string       `json:"why,omitempty"`        // outcome no: the reason given, one line; on another outcome it is only the write's reason (an agent's why)
	Says      string       `json:"says,omitempty"`       // outcome no: the no itself, one line; default "No: <request title>"
	AppliesIn []string     `json:"applies_in,omitempty"` // outcome no: scopes where it holds
	NotIn     []string     `json:"not_in,omitempty"`     // outcome no: scopes where it does not
	ReviewBy  string       `json:"review_by,omitempty"`  // outcome no: YYYY-MM-DD; default 180 days out
	// Sources is required from an agent when the request asks a question: the
	// answer cites them: each a URL, a vault document id, or an absolute path
	// (or ~/path) to a file on this machine. With none, a url is cited.
	Sources []model.AnswerSource `json:"sources,omitempty"`
}

// RFXReplyOutput is the output of rfx_reply.
type RFXReplyOutput struct {
	ID          string            `json:"id"`
	From        string            `json:"from"`
	Outcome     ReplyOutcome      `json:"outcome"`
	State       model.TargetState `json:"state"`
	Interaction string            `json:"interaction"`
	Constraint  string            `json:"constraint"`         // empty unless outcome no
	Question    string            `json:"question,omitempty"` // the question this reply answered
	Warnings    []string          `json:"warnings"`
	Rev         string            `json:"rev"`
}

// RFXReply records a reply to an outbound request: the target becomes
// replied, an inbound interaction keeps the text, and the request body gets a
// one-line summary that links it. A "no" must carry its reason and a source
// URL, and also makes a constraint, so the no is documented and findable.
var RFXReply = defineSafe("rfx_reply", "Record a reply to a request: target replied, interaction logged; a no also records a constraint with its reason", func(ctx context.Context, d Deps, in RFXReplyInput) (RFXReplyOutput, error) {
	doc, m, err := getRFX(ctx, d, normID(in.ID))
	if err != nil {
		return RFXReplyOutput{}, err
	}
	switch {
	case m.Direction != model.RFXOut:
		return RFXReplyOutput{}, errors.New("only an outbound request takes a reply")
	case m.Status == model.RFXClosed:
		return RFXReplyOutput{}, errors.New("request is closed")
	case !slices.Contains(ReplyOutcomes, in.Outcome):
		return RFXReplyOutput{}, fmt.Errorf("outcome %q: want one of %v", in.Outcome, ReplyOutcomes)
	case in.Heard != "" && !slices.Contains(ReplyHeards, in.Heard):
		return RFXReplyOutput{}, fmt.Errorf("heard %q: want one of %v", in.Heard, ReplyHeards)
	}
	title, _, err := splitCapture(in.Text)
	if err != nil {
		return RFXReplyOutput{}, fmt.Errorf("text: %w", err)
	}
	date, err := dateOrToday(in.Date)
	if err != nil {
		return RFXReplyOutput{}, err
	}
	if strings.TrimSpace(in.URL) == "" {
		in.URL = sourceLink(ctx, d, in.Sources)
	}
	from, hit, err := replyTarget(ctx, d, doc.ID, m, in)
	if err != nil {
		return RFXReplyOutput{}, err
	}
	tgt := &m.Targets[hit]
	if tgt.State == model.TargetReplied {
		// The same reply recorded again (same text, link, or io) is done.
		if prev, ok := sameReply(ctx, d, doc.Body, from, title, in); ok {
			return RFXReplyOutput{ID: doc.ID, From: from, Outcome: in.Outcome, State: model.TargetReplied, Interaction: prev,
				Warnings: []string{"this reply is already recorded: nothing was written"}, Rev: doc.Rev}, nil
		}
		return RFXReplyOutput{}, fmt.Errorf("%s already replied to %s (see its ## Replies): nothing more to record; to end the Ask: close {id: %s, outcome: closed}", from, doc.ID, doc.ID)
	}
	medium := tgt.Medium
	if in.Via != "" {
		medium = in.Via
	}
	// why is not refused here: over MCP it is also the reason every agent write
	// must carry, so an agent always sends it.
	if in.Outcome != ReplyNo && (in.Says != "" || in.ReviewBy != "" || len(in.AppliesIn) > 0 || len(in.NotIn) > 0) {
		return RFXReplyOutput{}, errors.New("says, applies_in, not_in, and review_by apply to outcome no only")
	}
	qsources, err := replySources(ctx, d, m, in)
	if err != nil {
		return RFXReplyOutput{}, err
	}
	// Check a "no" in full before any write.
	var con ConstraintAddInput
	if in.Outcome == ReplyNo {
		verify := ""
		who, err := d.Store.Get(ctx, from)
		if err != nil {
			return RFXReplyOutput{}, fmt.Errorf("from %s: %w", from, err)
		}
		source := strings.TrimSpace(in.URL)
		if source == "" {
			source = tgt.Ref
		}
		if source == "" && in.Heard != "" && !agentSurface(d) {
			// The owner heard it with no link: the constraint cites where and when.
			verify = fmt.Sprintf("owner: %s, %s", heardWords[in.Heard], date)
		} else if source == "" {
			return RFXReplyOutput{}, errors.New("a no needs its source: pass url (the reply's link)")
		}
		review := in.ReviewBy
		if review == "" {
			review = time.Now().AddDate(0, 0, reviewDefaultDays).Format(dateLayout)
		}
		says := strings.TrimSpace(in.Says)
		if says == "" {
			says = "No: " + doc.Title
		}
		con = ConstraintAddInput{
			Says: says, Why: in.Why, By: who.Title, Source: source, Verify: verify,
			AppliesIn: in.AppliesIn, NotIn: in.NotIn, ReviewBy: review,
		}
		if _, err := oneLine("why", con.Why); err != nil {
			return RFXReplyOutput{}, fmt.Errorf("a no needs its reason: %w", err)
		}
		if _, err := oneLine("says", con.Says); err != nil {
			return RFXReplyOutput{}, err
		}
		if verify == "" {
			if _, err := checkURL(con.Source); err != nil {
				return RFXReplyOutput{}, fmt.Errorf("source: %w", err)
			}
		}
		if err := checkDate("review_by", con.ReviewBy); err != nil {
			return RFXReplyOutput{}, err
		}
	}
	ia, err := InteractionAdd.Run(ctx, d, InteractionAddInput{
		Summary: in.Text, With: []string{from}, Medium: medium,
		Direction: model.InteractionIn, URL: in.URL, Date: date,
	})
	if err != nil {
		return RFXReplyOutput{}, err
	}
	out := RFXReplyOutput{ID: doc.ID, From: from, Outcome: in.Outcome, State: model.TargetReplied, Interaction: ia.ID, Warnings: ia.Warnings}
	line := fmt.Sprintf("- %s: [[%s]] %s: %s ([[%s]]", date, from, in.Outcome, title, ia.ID)
	if in.Heard != "" {
		line = fmt.Sprintf("- %s: [[%s]] %s (%s): %s ([[%s]]", date, from, in.Outcome, heardWords[in.Heard], title, ia.ID)
	}
	if in.Outcome == ReplyNo {
		con.Text = fmt.Sprintf("Reply to [[%s]], logged in [[%s]].", doc.ID, ia.ID)
		c, err := ConstraintAdd.Run(ctx, d, con)
		if err != nil {
			return out, fmt.Errorf("interaction %s was made but the constraint was not: %w", ia.ID, err)
		}
		out.Constraint = c.ID
		line += ", [[" + c.ID + "]]"
	}
	tgt.State = model.TargetReplied
	doc.Body = appendToSection(doc.Body, repliesHeading, line+")")
	doc, err = d.Store.Update(ctx, doc)
	if err != nil {
		return out, fmt.Errorf("interaction %s was made but the request was not updated: %w", ia.ID, err)
	}
	out.Rev = doc.Rev
	if qid := m.Question.Target(); qid != "" {
		answered, err := answerFromReply(ctx, d, qid, from, in, qsources)
		if err != nil {
			return out, fmt.Errorf("the reply was recorded but question %s was not answered: %w", qid, err)
		}
		out.Question = answered
	}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	return out, nil
})

// replyTarget finds the target of the Ask that replied: from is its id, or
// the name of a target (as rfx_show lists target_names); with no from, the
// Ask's only target. A reply is taken only from a target of the Ask. Via picks
// one of a target's mediums; a target with one medium may reply over another.
func replyTarget(ctx context.Context, d Deps, id string, m *model.RFX, in RFXReplyInput) (string, int, error) {
	var tos []string
	for _, t := range m.Targets {
		if to := t.To.Target(); !slices.Contains(tos, to) {
			tos = append(tos, to)
		}
	}
	from := normID(in.From)
	switch {
	case from == "" && len(tos) == 1:
		from = tos[0]
	case from == "":
		return "", 0, fmt.Errorf("from is empty: %s went to %s: pass from, one of those ids", id, strings.Join(tos, ", "))
	case !slices.Contains(tos, from):
		name := strings.Join(strings.Fields(in.From), " ")
		for _, to := range tos {
			if doc, err := d.Store.Get(ctx, to); err == nil && strings.EqualFold(doc.Title, name) {
				from = to
				break
			}
		}
	}
	var hit, all []int
	for i, t := range m.Targets {
		if t.To.Target() != from {
			continue
		}
		all = append(all, i)
		if in.Via == "" || t.Medium == in.Via {
			hit = append(hit, i)
		}
	}
	if len(hit) == 0 && len(all) == 1 {
		hit = all
	}
	switch {
	case len(hit) == 0 && len(all) > 1:
		return "", 0, fmt.Errorf("%s is not a target of %s by %s: pass via, one of its mediums on this Ask", from, id, in.Via)
	case len(hit) == 0:
		return "", 0, fmt.Errorf("no target %s on %s: its targets are %s: pass from, one of those ids. A reply from anyone else: rfx_target_add {id: %s, to, medium} first", from, id, strings.Join(tos, ", "), id)
	case len(hit) > 1:
		return "", 0, fmt.Errorf("%s has %d targets: pass via", from, len(hit))
	}
	return from, hit[0], nil
}

// sameReply reports whether from's reply on the Ask body is this one: the
// same first line, or the same link (url, a source URL, or an io a source
// names). It returns the id of the interaction that keeps it.
func sameReply(ctx context.Context, d Deps, body, from, title string, in RFXReplyInput) (string, bool) {
	var links, ids []string
	for _, s := range in.Sources {
		links = append(links, sourceLink(ctx, d, []model.AnswerSource{s}))
		ids = append(ids, normID(s.Ref))
	}
	if u := strings.TrimSpace(in.URL); u != "" {
		links = append(links, u)
	}
	for line := range strings.SplitSeq(body, "\n") {
		if !strings.Contains(line, "[["+from+"]] ") {
			continue
		}
		i := strings.LastIndex(line, "([[")
		if i < 0 {
			continue
		}
		prev, _, _ := strings.Cut(line[i+3:], "]]")
		if strings.HasSuffix(line[:i], ": "+title+" ") || slices.Contains(ids, prev) {
			return prev, true
		}
		if doc, err := d.Store.Get(ctx, prev); err == nil {
			if ia, ok := model.InteractionOf(doc); ok && ia.URL != "" && slices.Contains(links, ia.URL) {
				return prev, true
			}
		}
	}
	return "", false
}

// sourceLink is the first link a reply's sources give: a source URL, or the
// link of an io item a source names.
func sourceLink(ctx context.Context, d Deps, sources []model.AnswerSource) string {
	for _, s := range sources {
		ref := strings.TrimSpace(s.Ref)
		if model.IsWebURL(ref) {
			return ref
		}
		if doc, err := d.Store.Get(ctx, normID(ref)); err == nil {
			if io, ok := doc.Meta.(*model.IO); ok && model.IsWebURL(cmp.Or(io.Ref, io.URL)) {
				return cmp.Or(io.Ref, io.URL)
			}
		}
	}
	return ""
}
