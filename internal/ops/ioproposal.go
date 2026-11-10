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

// An outgoing draft is a proposal: a preview of an outward act. An agent
// says why, the impact, and its assessments; a missing one is shown as "not
// stated" and never refused. Group is a batch key the owner reviews
// together. VariantOf joins drafts into a variation set, keyed by its first
// draft (the root): approving or sending one closes the others of the set,
// the root too, as superseded.

// proposalInput is the proposal fields of io_add and io_edit.
type proposalInput struct {
	why, impact string
	assessments []model.IOAssessment
	group       string
	variantOf   string
}

// setProposal checks the proposal fields and sets the given ones on m. self
// is the draft's id ("" for a new one). Empty fields stay as they are;
// assessments, when given, replace the list.
//
// why is also the name of every agent write's receipt reason, and a surface
// passes it to an op that has the field. So on an incoming message it is
// only that reason, and on an edit (self set) it fills a draft that states
// none and never replaces one: an edit's reason is not the proposal's.
func setProposal(ctx context.Context, d Deps, self string, m *model.IO, in proposalInput) error {
	if m.Direction != model.IOOut {
		if in.impact != "" || in.assessments != nil || in.group != "" || in.variantOf != "" {
			return errors.New("impact, assessments, group, and variant_of are for an outgoing draft")
		}
		return nil
	}
	if self != "" && m.Why != "" {
		in.why = ""
	}
	given := in.why != "" || in.impact != "" || in.assessments != nil || in.group != "" || in.variantOf != ""
	if !given {
		return nil
	}
	for _, f := range []struct{ name, text string }{{"why", in.why}, {"impact", in.impact}} {
		if err := lintText(f.name, f.text); err != nil {
			return err
		}
	}
	if v := ioLine(in.why); v != "" {
		m.Why = v
	}
	if v := ioLine(in.impact); v != "" {
		m.Impact = v
	}
	if in.assessments != nil {
		var list []model.IOAssessment
		for i, a := range in.assessments {
			a.Name, a.Basis = strings.ToLower(ioLine(a.Name)), ioLine(a.Basis)
			switch {
			case a.Name == "":
				return fmt.Errorf("assessments[%d]: name is empty: confidence, recommend, risk, or your own", i)
			case a.Percent < 0 || a.Percent > 100:
				return fmt.Errorf("assessments[%d] %s: percent %d: want 0 to 100", i, a.Name, a.Percent)
			case slices.ContainsFunc(list, func(x model.IOAssessment) bool { return x.Name == a.Name }):
				return fmt.Errorf("assessments[%d]: %s is given twice", i, a.Name)
			}
			if err := lintText("assessments basis", a.Basis); err != nil {
				return err
			}
			list = append(list, a)
		}
		m.Assessments = list
	}
	if g := ioLine(in.group); g != "" {
		m.Group = g
	}
	if in.variantOf != "" {
		root, err := variantRoot(ctx, d, self, in.variantOf)
		if err != nil {
			return err
		}
		m.VariantOf = root
	}
	return nil
}

// variantRoot is the root of the variation set the draft id names: the
// draft itself, or the root it is a variation of.
func variantRoot(ctx context.Context, d Deps, self, id string) (string, error) {
	id = normID(id)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return "", linkErr("variant_of", id, err)
	}
	vm, ok := doc.Meta.(*model.IO)
	if !ok || vm.Direction != model.IOOut {
		return "", fmt.Errorf("variant_of: %s is not an outgoing draft", doc.ID)
	}
	root := variantSet(doc.ID, vm)
	if root == self {
		return "", fmt.Errorf("variant_of: %s is this draft's own set", doc.ID)
	}
	return root, nil
}

// variantSet is the key of the variation set of a draft: its root.
func variantSet(id string, m *model.IO) string { return firstOf(normID(m.VariantOf), id) }

// supersedeVariants closes the other open drafts of the variation set of id
// as superseded by it. It returns their ids.
func supersedeVariants(ctx context.Context, d Deps, id string, m *model.IO) ([]string, error) {
	set := variantSet(id, m)
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeIO})
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range refs {
		if r.ID == id {
			continue
		}
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return out, err
		}
		sm, ok := doc.Meta.(*model.IO)
		if !ok || sm.Direction != model.IOOut || !sm.State.Open() || (doc.ID != set && normID(sm.VariantOf) != set) {
			continue
		}
		sm.State, sm.Reason = model.IOSuperseded, "superseded by "+id
		sm.ApprovedHash, sm.CheckedAt = "", ""
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return out, err
		}
		out = append(out, doc.ID)
	}
	return out, nil
}

// IOAttemptInput is the input of io_attempt.
type IOAttemptInput struct {
	ID     string              `json:"id"`
	Result model.AttemptResult `json:"result"`          // failed, held, or other; a send is io_send
	Tool   string              `json:"tool,omitempty"`  // what you tried it with
	URL    string              `json:"url,omitempty"`   // where the try shows, if anywhere
	Error  string              `json:"error,omitempty"` // what went wrong; required for failed
}

// IOAttemptOutput is the output of io_attempt.
type IOAttemptOutput struct {
	ID       string        `json:"id"`
	State    model.IOState `json:"state"`
	Attempts int           `json:"attempts"`
	Rev      string        `json:"rev"`
}

// IOAttempt records a try to carry out an outgoing draft that did not send
// it. The draft keeps its state.
var IOAttempt = defineSafe("io_attempt", "Record a try to carry out an outgoing draft that did not send it: result failed (with error: what went wrong), held (something stopped it before it went), or other; tool is what you tried with, url where the try shows. The draft keeps its state and approval: tell the owner when it failed. A send is io_send, which records its own attempt", func(ctx context.Context, d Deps, in IOAttemptInput) (IOAttemptOutput, error) {
	switch {
	case in.Result == model.AttemptSent:
		return IOAttemptOutput{}, errors.New("result sent: record the send with io_send")
	case !slices.Contains(model.AttemptResults, in.Result):
		return IOAttemptOutput{}, fmt.Errorf("result %q: want failed, held, or other", in.Result)
	}
	msg := cleanText(in.Error)
	if in.Result == model.AttemptFailed && msg == "" {
		return IOAttemptOutput{}, errors.New("error is empty: say what went wrong")
	}
	if err := lintText("error", msg); err != nil {
		return IOAttemptOutput{}, err
	}
	link := strings.TrimSpace(in.URL)
	if link != "" {
		var err error
		if link, err = checkURL(link); err != nil {
			return IOAttemptOutput{}, err
		}
	}
	doc, m, err := getOpenIO(ctx, d, in.ID)
	if err != nil {
		return IOAttemptOutput{}, err
	}
	if m.Direction != model.IOOut {
		return IOAttemptOutput{}, fmt.Errorf("%s is incoming: an attempt is a try to send an outgoing draft", doc.ID)
	}
	m.Attempts = append(m.Attempts, model.IOAttempt{At: nowString(d.Store), Actor: d.Store.Info().Actor, Tool: ioLine(in.Tool),
		Medium: m.Medium, Result: in.Result, URL: link, Error: msg})
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return IOAttemptOutput{}, err
	}
	return IOAttemptOutput{ID: saved.ID, State: m.State, Attempts: len(m.Attempts), Rev: saved.Rev}, nil
})

// IOReviewInput is the input of io_review: ids, a group, or a thread key
// name the io items; together they add up.
type IOReviewInput struct {
	IDs   []string `json:"ids,omitempty"`
	Group string   `json:"group,omitempty"` // every io item with this group
	Key   string   `json:"key,omitempty"`   // every io item of this thread (an item id, #tag, or channel:<source>/<thread>)
}

// IOReviewOutput is the output of io_review.
type IOReviewOutput struct {
	Reviewed []string `json:"reviewed"`
	At       string   `json:"at"`
}

// IOReview marks io items reviewed by the owner. It changes no state and
// nothing that needs the owner: it records that they looked.
var IOReview = withOneReceipt(definePerson("io_review", "Mark io items, in or out, reviewed by the owner now: ids, every item of a group, or every io item of a thread key. It changes no state and not what needs the owner. One receipt undoes it. People only", func(ctx context.Context, d Deps, in IOReviewInput) (IOReviewOutput, error) {
	out := IOReviewOutput{Reviewed: []string{}, At: nowString(d.Store)}
	var ids []string
	add := func(id string) {
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, id := range in.IDs {
		add(normID(id))
	}
	if g := ioLine(in.Group); g != "" {
		refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeIO})
		if err != nil {
			return out, err
		}
		n := len(ids)
		for _, r := range refs {
			doc, err := d.Store.Get(ctx, r.ID)
			if err != nil {
				return out, err
			}
			if m, ok := doc.Meta.(*model.IO); ok && m.Group == g {
				add(doc.ID)
			}
		}
		if len(ids) == n {
			return out, fmt.Errorf("group %q: no io item has it", g)
		}
	}
	if strings.TrimSpace(in.Key) != "" {
		th, err := Thread.Run(ctx, d, ThreadInput{Key: in.Key})
		if err != nil {
			return out, err
		}
		n := len(ids)
		if th.Item != "" && ioID(ctx, d, th.Item) != "" {
			add(th.Item)
		}
		for _, t := range th.Turns {
			add(t.IO)
		}
		if len(ids) == n {
			return out, fmt.Errorf("key %s: no io item is in that thread", th.Key)
		}
	}
	if len(ids) == 0 {
		return out, errors.New("give ids, group, or key")
	}
	// Read all before any write: an id that is not io refuses the whole mark.
	docs := make([]model.Document, 0, len(ids))
	for _, id := range ids {
		doc, _, err := getIO(ctx, d, id)
		if err != nil {
			return out, err
		}
		docs = append(docs, doc)
	}
	for _, doc := range docs {
		doc.Meta.(*model.IO).Reviewed = out.At
		saved, err := d.Store.Update(ctx, doc)
		if err != nil {
			return out, err
		}
		out.Reviewed = append(out.Reviewed, saved.ID)
	}
	return out, nil
}))
