package ops

import (
	"context"
	"fmt"

	"github.com/iodesystems/paradize/internal/model"
)

// LintInput is the input of lint.
type LintInput struct{}

// LintOutput is the output of lint.
type LintOutput struct {
	Errors   int             `json:"errors"`
	Warnings int             `json:"warnings"`
	Findings []model.Finding `json:"findings"`
}

// NewLintOutput counts findings by severity.
func NewLintOutput(findings []model.Finding) LintOutput {
	out := LintOutput{Findings: findings}
	if out.Findings == nil {
		out.Findings = []model.Finding{}
	}
	for _, f := range findings {
		if f.Severity == model.SeverityError {
			out.Errors++
		} else {
			out.Warnings++
		}
	}
	return out
}

// Failed implements Failing.
func (o LintOutput) Failed() error {
	if o.Errors > 0 {
		return fmt.Errorf("lint: %d errors", o.Errors)
	}
	return nil
}

// Lint checks every document in the vault.
var Lint = define("lint", "Lint every document in the vault", func(ctx context.Context, d Deps, _ LintInput) (LintOutput, error) {
	findings, err := d.Store.Lint(ctx)
	if err != nil {
		return LintOutput{}, err
	}
	return NewLintOutput(findings), nil
})

// ReconcileInput is the input of reconcile.
type ReconcileInput struct{}

// Reconcile logs files changed outside the vault write path.
var Reconcile = define("reconcile", "Detect and log files changed outside para", func(ctx context.Context, d Deps, _ ReconcileInput) (model.ReconcileResult, error) {
	return d.Store.Reconcile(ctx)
})

const eventsTailDefault = 20

// EventsTailInput is the input of events_tail.
type EventsTailInput struct {
	Limit int `json:"limit,omitempty"` // 0 means 20
}

// EventsTailOutput is the output of events_tail.
type EventsTailOutput struct {
	Events []model.Event `json:"events"`
}

// EventsTail returns the newest events, oldest first.
var EventsTail = define("events_tail", "Show the newest events", func(ctx context.Context, d Deps, in EventsTailInput) (EventsTailOutput, error) {
	n := in.Limit
	if n <= 0 {
		n = eventsTailDefault
	}
	recs, err := d.Store.Events(ctx, n)
	if err != nil {
		return EventsTailOutput{}, err
	}
	if recs == nil {
		recs = []model.Event{}
	}
	return EventsTailOutput{Events: recs}, nil
})
