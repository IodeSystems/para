package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
)

// PolicyView is a policy as list and show report it.
type PolicyView struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	URL           string         `json:"url"`
	Fidelity      model.Fidelity `json:"fidelity"`
	Verified      bool           `json:"verified"`
	ReviewedBy    string         `json:"reviewed_by"`
	SourceStatus  string         `json:"source_status"` // current | changed | none (no transcript by design) | missing
	SourceChecked string         `json:"source_checked"`
	Rules         int            `json:"rules"`
}

func policyView(ctx context.Context, d Deps, doc model.Document, p *model.Followed) (PolicyView, error) {
	v := PolicyView{
		ID: doc.ID, Title: doc.Title, URL: p.Source.URL, Fidelity: p.Source.Fidelity,
		Verified: p.Verified, ReviewedBy: p.ReviewedBy, Rules: len(parseRules(doc.ID, doc.Body)),
	}
	switch tdoc, t, err := getTranscript(ctx, d, doc.ID, p); {
	case errors.Is(err, model.ErrNotFound):
		v.SourceStatus = "missing"
	case err != nil:
		return v, err
	case tdoc == nil:
		v.SourceStatus = "none"
	default:
		v.SourceStatus, v.SourceChecked = t.Source.Status, t.Source.Checked
		if v.SourceStatus == "" {
			v.SourceStatus = sourceCurrent
		}
	}
	return v, nil
}

// PolicyListInput is the input of policy_list.
type PolicyListInput struct{}

// PolicyListOutput is the output of policy_list.
type PolicyListOutput struct {
	Policies []PolicyView `json:"policies"`
}

// PolicyList lists policies, and the areas that follow a source.
var PolicyList = defineRead("policy_list", "List policies (areas that follow a source) with review and source state", func(ctx context.Context, d Deps, _ PolicyListInput) (PolicyListOutput, error) {
	ids, err := followedSummaries(ctx, d)
	if err != nil {
		return PolicyListOutput{}, err
	}
	out := PolicyListOutput{Policies: []PolicyView{}}
	for _, id := range ids {
		doc, p, err := getPolicy(ctx, d, id)
		if err != nil {
			return PolicyListOutput{}, err
		}
		v, err := policyView(ctx, d, doc, p)
		if err != nil {
			return PolicyListOutput{}, err
		}
		out.Policies = append(out.Policies, v)
	}
	return out, nil
})

// ExceptionRef names an exception of a policy.
type ExceptionRef struct {
	ID     string                `json:"id"`
	Kind   model.ExceptionKind   `json:"kind"`
	Status model.ExceptionStatus `json:"status"`
	Rule   model.Link            `json:"rule"`
}

// PolicyShowInput is the input of policy_show.
type PolicyShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// PolicyShowOutput is the output of policy_show.
type PolicyShowOutput struct {
	PolicyView   `json:",inline"`
	TranscriptID string         `json:"transcript_id"` // empty when fidelity is none
	Body         string         `json:"body"`
	RuleList     []Rule         `json:"rule_list"`
	Exceptions   []ExceptionRef `json:"exceptions"`
	Rev          string         `json:"rev"`
}

// PolicyShow shows a policy summary, its rules, and its exceptions.
var PolicyShow = defineRead("policy_show", "Show a policy summary, rules, and exceptions", func(ctx context.Context, d Deps, in PolicyShowInput) (PolicyShowOutput, error) {
	doc, p, err := getPolicy(ctx, d, in.ID)
	if err != nil {
		return PolicyShowOutput{}, err
	}
	v, err := policyView(ctx, d, doc, p)
	if err != nil {
		return PolicyShowOutput{}, err
	}
	out := PolicyShowOutput{PolicyView: v, Body: doc.Body, Rev: doc.Rev, RuleList: parseRules(doc.ID, doc.Body)}
	if p.Source.Fidelity != model.FidelityNone {
		out.TranscriptID = transcriptID(doc.ID)
	}
	if out.Exceptions, err = exceptionsOf(ctx, d, doc.ID); err != nil {
		return PolicyShowOutput{}, err
	}
	return out, nil
})

func exceptionsOf(ctx context.Context, d Deps, policy string) ([]ExceptionRef, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypePolicyException})
	if err != nil {
		return nil, err
	}
	out := []ExceptionRef{}
	// An area's exceptions are annotations in its body.
	if doc, err := d.Store.Get(ctx, policy); err == nil {
		for _, c := range paramd.Callouts(doc.Body) {
			if c.Kind != "exception" || c.Broken != "" {
				continue
			}
			x := ExceptionRef{ID: c.ID, Kind: model.ExceptionKind(c.Field("kind")), Status: model.ExceptionStatus(cmp.Or(c.Field("status"), string(model.ExceptionOpen)))}
			if b, ok := strings.CutPrefix(c.Field("anchor"), "^"); ok {
				x.Rule = model.RuleLink(doc.ID, b)
			}
			out = append(out, x)
		}
	}
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if e, ok := doc.Meta.(*model.PolicyException); ok && e.Policy.Target() == policy {
			out = append(out, ExceptionRef{ID: doc.ID, Kind: e.Kind, Status: e.Status, Rule: e.Rule})
		}
	}
	return out, nil
}

// PolicyReviewInput is the input of policy_review.
type PolicyReviewInput struct {
	ID string `json:"id"`
	By string `json:"by"` // who compared the summary with the source
}

// PolicyReviewOutput is the output of policy_review.
type PolicyReviewOutput struct {
	ID         string `json:"id"`
	Verified   bool   `json:"verified"`
	ReviewedBy string `json:"reviewed_by"`
	Rev        string `json:"rev"`
}

// PolicyReview marks a summary as reviewed. It also clears a "changed"
// source state: the reviewer has now read the new transcript.
var PolicyReview = define("policy_review", "Mark a policy summary as reviewed by a person", func(ctx context.Context, d Deps, in PolicyReviewInput) (PolicyReviewOutput, error) {
	by, err := oneLine("by", in.By)
	if err != nil {
		return PolicyReviewOutput{}, err
	}
	doc, p, err := getPolicy(ctx, d, in.ID)
	if err != nil {
		return PolicyReviewOutput{}, err
	}
	p.Verified, p.ReviewedBy = true, by
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return PolicyReviewOutput{}, err
	}
	tdoc, t, err := getTranscript(ctx, d, in.ID, p)
	switch {
	case errors.Is(err, model.ErrNotFound), tdoc == nil && err == nil:
	case err != nil:
		return PolicyReviewOutput{}, err
	case t.Source.Status == sourceChanged:
		t.Source.Status = sourceCurrent
		if _, err := d.Store.Update(ctx, *tdoc); err != nil {
			return PolicyReviewOutput{}, fmt.Errorf("policy reviewed, transcript status not cleared: %w", err)
		}
	}
	return PolicyReviewOutput{ID: doc.ID, Verified: true, ReviewedBy: by, Rev: doc.Rev}, nil
})

// Refresh results.
const (
	RefreshUnchanged = "unchanged"
	RefreshChanged   = "changed"
	RefreshBaseline  = "baseline"
)

// PolicyRefreshInput is the input of policy_refresh. Text is the source as
// read again by some backend.
type PolicyRefreshInput struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Backend string `json:"backend,omitempty"` // empty means manual
}

// PolicyRefreshOutput is the output of policy_refresh.
type PolicyRefreshOutput struct {
	ID                 string         `json:"id"`
	Result             string         `json:"result"` // unchanged | changed | baseline
	Hash               string         `json:"hash"`
	PreviousHash       string         `json:"previous_hash"`
	Checked            string         `json:"checked"`
	AffectedRules      []Rule         `json:"affected_rules"`      // set when changed
	AffectedExceptions []ExceptionRef `json:"affected_exceptions"` // set when changed
	// Version is the version of the source the transcript now holds, for an
	// area; the text before a change is kept as <id>-v<n>.source.
	Version int `json:"version,omitempty"`
	// AnchorsLost are annotations whose quote is not in the new text.
	AnchorsLost []string `json:"anchors_lost"`
}

// PolicyRefresh compares new source text with the transcript by hash over
// normalized text. It replaces the transcript on a change and never edits the
// summary. It always records the check time.
var PolicyRefresh = define("policy_refresh", "Compare new source text with the transcript and flag a change", policyRefresh)

func policyRefresh(ctx context.Context, d Deps, in PolicyRefreshInput) (PolicyRefreshOutput, error) {
	doc, p, err := getPolicy(ctx, d, in.ID)
	if err != nil {
		return PolicyRefreshOutput{}, err
	}
	tdoc, t, err := getTranscript(ctx, d, in.ID, p)
	if err != nil {
		return PolicyRefreshOutput{}, err
	}
	if tdoc == nil {
		return PolicyRefreshOutput{}, fmt.Errorf("%s has fidelity none: no transcript to refresh", in.ID)
	}
	norm, err := Normalize(in.Text)
	if err != nil {
		return PolicyRefreshOutput{}, err
	}
	backend := in.Backend
	if backend == "" {
		backend = backendManual
	}
	out := PolicyRefreshOutput{
		ID: in.ID, Hash: hashText(norm), PreviousHash: t.Source.Hash, Checked: timestamp(),
		AffectedRules: []Rule{}, AffectedExceptions: []ExceptionRef{}, AnchorsLost: []string{},
	}
	_, isArea := doc.Meta.(*model.Area)
	prev := *tdoc
	t.Source.Checked = out.Checked
	switch {
	case t.Source.Backend != "" && t.Source.Backend != backend:
		out.Result = RefreshBaseline
	case out.Hash == t.Source.Hash:
		out.Result = RefreshUnchanged
	default:
		out.Result = RefreshChanged
		t.Source.Status = sourceChanged
	}
	if out.Result == RefreshChanged && isArea {
		// An area keeps each version: the text before this one as a version file.
		n, err := sourceVersion(ctx, d, doc.ID)
		if err != nil {
			return out, err
		}
		pt := *prev.Meta.(*model.Transcript)
		pt.Version, pt.Common.Updated = n, ""
		pt.Source.Status, pt.Source.Checked = "", ""
		v := model.Document{ID: fmt.Sprintf("%s-v%d.source", doc.ID, n), Folder: tdoc.Folder,
			Title: fmt.Sprintf("%s (version %d)", doc.Title, n), Body: prev.Body, Meta: &pt}
		pt.Common.Created = ""
		if _, err := d.Store.Create(ctx, v); err != nil {
			return out, fmt.Errorf("version %d of %s not kept: %w", n, doc.ID, err)
		}
		out.Version = n + 1
		for _, c := range paramd.Callouts(doc.Body) {
			if q := c.Field("quote"); q != "" && c.Broken == "" {
				if _, _, ok := locateQuote(norm, q, 0); !ok {
					out.AnchorsLost = append(out.AnchorsLost, c.ID)
				}
			}
		}
	}
	if out.Result != RefreshUnchanged {
		t.Source.Hash, t.Source.Retrieved, t.Source.Backend = out.Hash, out.Checked, backend
		tdoc.Body = "Source: " + t.Source.URL + "\n\n" + norm
	}
	if _, err := d.Store.Update(ctx, *tdoc); err != nil {
		return PolicyRefreshOutput{}, err
	}
	if out.Result == RefreshChanged {
		out.AffectedRules = parseRules(doc.ID, doc.Body)
		if out.AffectedExceptions, err = exceptionsOf(ctx, d, doc.ID); err != nil {
			return PolicyRefreshOutput{}, err
		}
	}
	return out, nil
}
