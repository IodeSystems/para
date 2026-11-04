package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// expiringDefault is the default window of exception_expiring, in days.
const expiringDefault = 30

// ExceptionView is a policy exception as list and show report it.
type ExceptionView struct {
	ID         string                `json:"id"`
	Title      string                `json:"title"`
	Policy     string                `json:"policy"`
	PolicyText string                `json:"policy_text,omitempty"` // the policy in words, when it is not a vault document
	Rule       string                `json:"rule"`                  // rule id such as R4, or empty
	Kind       model.ExceptionKind   `json:"kind"`
	Status     model.ExceptionStatus `json:"status"`
	By         string                `json:"by"`
	With       string                `json:"with"` // document id, or empty
	ReviewBy   model.Date            `json:"review_by"`
}

func exceptionView(doc model.Document, e *model.PolicyException) ExceptionView {
	v := ExceptionView{
		ID: doc.ID, Title: doc.Title, Policy: e.Policy.Target(), PolicyText: e.PolicyText, Kind: e.Kind,
		Status: e.Status, By: e.By, With: e.With.Target(), ReviewBy: e.ReviewBy,
	}
	if _, id, ok := strings.Cut(strings.TrimSuffix(string(e.Rule), "]]"), "#^"); ok {
		v.Rule = id
	}
	return v
}

func getException(ctx context.Context, d Deps, id string) (model.Document, *model.PolicyException, error) {
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return doc, nil, err
	}
	e, ok := doc.Meta.(*model.PolicyException)
	if !ok {
		return doc, nil, fmt.Errorf("%s is a %s, not a policy exception", id, doc.Meta.Header().Type)
	}
	return doc, e, nil
}

func allExceptions(ctx context.Context, d Deps) ([]ExceptionView, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypePolicyException})
	if err != nil {
		return nil, err
	}
	out := []ExceptionView{}
	for _, r := range refs {
		doc, e, err := getException(ctx, d, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, exceptionView(doc, e))
	}
	return out, nil
}

// ExceptionAddInput is the input of exception_add.
type ExceptionAddInput struct {
	Policy     string              `json:"policy,omitempty"`      // required unless policy_text is given
	PolicyText string              `json:"policy_text,omitempty"` // the policy in words, for one not in the vault
	Rule       string              `json:"rule,omitempty"`        // optional rule id such as R4
	Kind       model.ExceptionKind `json:"kind"`
	By         string              `json:"by"`                  // who raised or decided it
	With       string              `json:"with,omitempty"`      // document id; required for conflict_with
	ReviewBy   string              `json:"review_by,omitempty"` // optional YYYY-MM-DD
	Title      string              `json:"title,omitempty"`     // empty: "Exception: <kind>"
	Text       string              `json:"text,omitempty"`      // body: what is unclear or wrong
}

// ExceptionAddOutput is the output of exception_add.
type ExceptionAddOutput struct {
	ID  string `json:"id"`
	Rev string `json:"rev"`
}

// ExceptionAdd records a policy exception. Status starts open.
var ExceptionAdd = define("exception_add", "Record a policy exception", func(ctx context.Context, d Deps, in ExceptionAddInput) (ExceptionAddOutput, error) {
	kind := in.Kind
	if !slices.Contains(model.ExceptionKinds, kind) {
		return ExceptionAddOutput{}, fmt.Errorf("kind %q must be one of %v", in.Kind, model.ExceptionKinds)
	}
	by, err := oneLine("by", in.By)
	if err != nil {
		return ExceptionAddOutput{}, err
	}
	e := &model.PolicyException{
		Common: model.Common{Type: model.TypePolicyException}, Kind: kind, Status: model.ExceptionOpen, By: by,
	}
	var pol model.Document
	switch text := strings.TrimSpace(in.PolicyText); {
	case in.Policy != "":
		var err error
		if pol, _, err = getPolicy(ctx, d, in.Policy); err != nil {
			return ExceptionAddOutput{}, err
		}
		e.Policy, e.PolicyText = model.LinkTo(pol.ID), text
	case text != "":
		if strings.ContainsAny(text, "\r\n") {
			return ExceptionAddOutput{}, errors.New("policy_text must be one line")
		}
		e.PolicyText = text
	default:
		return ExceptionAddOutput{}, errors.New("give policy (a policy id) or policy_text (the policy in words)")
	}
	if in.Rule != "" && pol.ID == "" {
		return ExceptionAddOutput{}, errors.New("rule needs policy: a rule belongs to a policy document")
	}
	if in.Rule != "" {
		if !ruleIDRe.MatchString(in.Rule) || !slices.ContainsFunc(parseRules(pol.ID, pol.Body), func(r Rule) bool { return r.ID == in.Rule }) {
			return ExceptionAddOutput{}, fmt.Errorf("%s has no rule %q", pol.ID, in.Rule)
		}
		e.Rule = model.RuleLink(pol.ID, in.Rule)
	}
	switch {
	case in.With != "":
		if _, err := d.Store.Get(ctx, in.With); err != nil {
			return ExceptionAddOutput{}, fmt.Errorf("with: %w", err)
		}
		e.With = model.LinkTo(in.With)
	case kind == model.ExceptionConflictWith:
		return ExceptionAddOutput{}, errors.New("kind conflict_with needs with: the document it conflicts with")
	}
	if in.ReviewBy != "" {
		if err := checkDate("review_by", in.ReviewBy); err != nil {
			return ExceptionAddOutput{}, err
		}
		e.ReviewBy = model.Date(in.ReviewBy)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Exception: " + strings.ReplaceAll(string(in.Kind), "_", " ")
	}
	if strings.ContainsAny(title, "\r\n") {
		return ExceptionAddOutput{}, errors.New("title must be one line")
	}
	doc, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderPolicies, Title: title, Body: strings.TrimSpace(in.Text), Meta: e,
	})
	if err != nil {
		return ExceptionAddOutput{}, err
	}
	return ExceptionAddOutput{ID: doc.ID, Rev: doc.Rev}, nil
})

// ExceptionListInput is the input of exception_list. Empty filters match all.
type ExceptionListInput struct {
	Policy string                `json:"policy,omitempty" query:"policy"`
	Status model.ExceptionStatus `json:"status,omitempty" query:"status"`
	Kind   model.ExceptionKind   `json:"kind,omitempty" query:"kind"`
}

// ExceptionListOutput is the output of exception_list.
type ExceptionListOutput struct {
	Exceptions []ExceptionView `json:"exceptions"`
}

// ExceptionList lists exceptions.
var ExceptionList = defineRead("exception_list", "List policy exceptions", func(ctx context.Context, d Deps, in ExceptionListInput) (ExceptionListOutput, error) {
	all, err := allExceptions(ctx, d)
	if err != nil {
		return ExceptionListOutput{}, err
	}
	out := ExceptionListOutput{Exceptions: []ExceptionView{}}
	for _, v := range all {
		if (in.Policy == "" || v.Policy == in.Policy) && (in.Status == "" || v.Status == in.Status) && (in.Kind == "" || v.Kind == in.Kind) {
			out.Exceptions = append(out.Exceptions, v)
		}
	}
	return out, nil
})

// ExceptionShowInput is the input of exception_show.
type ExceptionShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// ExceptionShowOutput is the output of exception_show.
type ExceptionShowOutput struct {
	ExceptionView `json:",inline"`
	Body          string `json:"body"`
	Rev           string `json:"rev"`
}

// ExceptionShow shows one exception.
var ExceptionShow = defineRead("exception_show", "Show one policy exception", func(ctx context.Context, d Deps, in ExceptionShowInput) (ExceptionShowOutput, error) {
	doc, e, err := getException(ctx, d, in.ID)
	if err != nil {
		return ExceptionShowOutput{}, err
	}
	return ExceptionShowOutput{ExceptionView: exceptionView(doc, e), Body: doc.Body, Rev: doc.Rev}, nil
})

// ExpiringException is an exception whose review date is near or past.
type ExpiringException struct {
	ExceptionView `json:",inline"`
	DaysLeft      int `json:"days_left"` // negative: overdue
}

// ExceptionExpiringInput is the input of exception_expiring.
type ExceptionExpiringInput struct {
	Days int `json:"days,omitempty" query:"days"` // window from today; 0 means 30
}

// ExceptionExpiringOutput is the output of exception_expiring.
type ExceptionExpiringOutput struct {
	Exceptions []ExpiringException `json:"exceptions"`
}

// ExceptionExpiring lists exceptions that are not resolved and whose
// review_by is overdue or within the window, soonest first.
var ExceptionExpiring = defineRead("exception_expiring", "List unresolved exceptions due for review", func(ctx context.Context, d Deps, in ExceptionExpiringInput) (ExceptionExpiringOutput, error) {
	days := in.Days
	if days <= 0 {
		days = expiringDefault
	}
	all, err := allExceptions(ctx, d)
	if err != nil {
		return ExceptionExpiringOutput{}, err
	}
	base, _ := time.Parse("2006-01-02", string(today()))
	out := ExceptionExpiringOutput{Exceptions: []ExpiringException{}}
	for _, v := range all {
		if v.Status == model.ExceptionResolved || v.ReviewBy == "" {
			continue
		}
		due, err := time.Parse("2006-01-02", string(v.ReviewBy))
		if err != nil {
			continue
		}
		if left := int(due.Sub(base).Hours() / 24); left <= days {
			out.Exceptions = append(out.Exceptions, ExpiringException{ExceptionView: v, DaysLeft: left})
		}
	}
	sort.SliceStable(out.Exceptions, func(i, j int) bool { return out.Exceptions[i].ReviewBy < out.Exceptions[j].ReviewBy })
	return out, nil
})
