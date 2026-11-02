package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
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
	Date      string       `json:"date,omitempty"`       // YYYY-MM-DD; default the local date
	Why       string       `json:"why,omitempty"`        // outcome no: the reason given, one line
	Says      string       `json:"says,omitempty"`       // outcome no: the no itself, one line; default "No: <request title>"
	AppliesIn []string     `json:"applies_in,omitempty"` // outcome no: scopes where it holds
	NotIn     []string     `json:"not_in,omitempty"`     // outcome no: scopes where it does not
	ReviewBy  string       `json:"review_by,omitempty"`  // outcome no: YYYY-MM-DD; default 180 days out
}

// RFXReplyOutput is the output of rfx_reply.
type RFXReplyOutput struct {
	ID          string            `json:"id"`
	From        string            `json:"from"`
	Outcome     ReplyOutcome      `json:"outcome"`
	State       model.TargetState `json:"state"`
	Interaction string            `json:"interaction"`
	Constraint  string            `json:"constraint"` // empty unless outcome no
	Warnings    []string          `json:"warnings"`
	Rev         string            `json:"rev"`
}

// RFXReply records a reply to an outbound request: the target becomes
// replied, an inbound interaction keeps the text, and the request body gets a
// one-line summary that links it. A "no" must carry its reason and a source
// URL, and also makes a constraint, so the no is documented and findable.
var RFXReply = define("rfx_reply", "Record a reply to a request: target replied, interaction logged; a no also records a constraint with its reason", func(ctx context.Context, d Deps, in RFXReplyInput) (RFXReplyOutput, error) {
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
	}
	title, _, err := splitCapture(in.Text)
	if err != nil {
		return RFXReplyOutput{}, fmt.Errorf("text: %w", err)
	}
	date, err := dateOrToday(in.Date)
	if err != nil {
		return RFXReplyOutput{}, err
	}
	from := normID(in.From)
	var hit []int
	for i, t := range m.Targets {
		if t.To.Target() == from && (in.Via == "" || t.Medium == in.Via) {
			hit = append(hit, i)
		}
	}
	switch {
	case from == "":
		return RFXReplyOutput{}, errors.New("from is empty")
	case len(hit) == 0:
		return RFXReplyOutput{}, fmt.Errorf("no target %s %s: add it with rfx target add", from, in.Via)
	case len(hit) > 1:
		return RFXReplyOutput{}, fmt.Errorf("%s has %d targets: pass via", from, len(hit))
	}
	tgt := &m.Targets[hit[0]]
	if tgt.State == model.TargetReplied {
		return RFXReplyOutput{}, fmt.Errorf("%s already replied", from)
	}
	if in.Outcome != ReplyNo && (in.Why != "" || in.Says != "" || in.ReviewBy != "" || len(in.AppliesIn) > 0 || len(in.NotIn) > 0) {
		return RFXReplyOutput{}, errors.New("why, says, applies_in, not_in, and review_by apply to outcome no only")
	}
	// Check a "no" in full before any write.
	var con ConstraintAddInput
	if in.Outcome == ReplyNo {
		who, err := d.Store.Get(ctx, from)
		if err != nil {
			return RFXReplyOutput{}, fmt.Errorf("from %s: %w", from, err)
		}
		source := strings.TrimSpace(in.URL)
		if source == "" {
			source = tgt.Ref
		}
		if source == "" {
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
			Says: says, Why: in.Why, By: who.Title, Source: source,
			AppliesIn: in.AppliesIn, NotIn: in.NotIn, ReviewBy: review,
		}
		if _, err := oneLine("why", con.Why); err != nil {
			return RFXReplyOutput{}, fmt.Errorf("a no needs its reason: %w", err)
		}
		if _, err := oneLine("says", con.Says); err != nil {
			return RFXReplyOutput{}, err
		}
		if _, err := checkURL(con.Source); err != nil {
			return RFXReplyOutput{}, fmt.Errorf("source: %w", err)
		}
		if err := checkDate("review_by", con.ReviewBy); err != nil {
			return RFXReplyOutput{}, err
		}
	}
	ia, err := InteractionAdd.Run(ctx, d, InteractionAddInput{
		Summary: in.Text, With: []string{from}, Medium: tgt.Medium,
		Direction: model.InteractionIn, URL: in.URL, Date: date,
	})
	if err != nil {
		return RFXReplyOutput{}, err
	}
	out := RFXReplyOutput{ID: doc.ID, From: from, Outcome: in.Outcome, State: model.TargetReplied, Interaction: ia.ID, Warnings: ia.Warnings}
	line := fmt.Sprintf("- %s: [[%s]] %s: %s ([[%s]]", date, from, in.Outcome, title, ia.ID)
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
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	return out, nil
})
