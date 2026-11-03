// Package upgrade holds the ordered steps that bring a vault to the format
// this binary expects. A vault records format_version in .para/vault.properties:
// every step with a lower or equal version is done.
//
// A step is automated or judgement. An automated step is a deterministic
// rewrite through internal/vault: dry run first, one batch, one event per
// document, undoable on the machine that ran it. A judgement step is a change
// no program can make; it has instructions for an agent, a selector for the
// documents it concerns, and a done test, which is the selector finding nothing.
//
// Every step has a selector, so "pending" always means "a document still
// matches". A second machine that sees the new format_version, or finds no
// match, does nothing. Old documents stay readable until a step clears them.
package upgrade

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// ConfigKey is the vault.properties key that records the format version.
const ConfigKey = "format_version"

// Step is one vault upgrade step.
type Step struct {
	ID      string // lowercase words and '-': it is part of a batch id
	Version int    // the format_version once this step and every earlier one is done
	Title   string
	// Auto is true for an automated step, which has Rewrite.
	Auto bool
	// Types are the document types Select can match; a status check lists only these.
	Types []model.Type
	// Select reports whether the document still needs the step.
	Select func(model.Document) bool
	// Rewrite is the automated change to one document selected by Select. It is
	// deterministic and keeps content and links.
	Rewrite func(model.Document) (model.Document, error)
	// Instructions tell an agent what to do for a judgement step. Done says the
	// done test in words.
	Instructions string
	Done         string
}

// Steps returns every step in order.
func Steps() []Step { return steps() }

// Latest is the format version of this binary.
func Latest() int {
	n := 0
	for _, s := range steps() {
		n = max(n, s.Version)
	}
	return n
}

// Pending is a step that still has documents to change.
type Pending struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Auto  bool     `json:"auto"`
	Docs  []string `json:"docs"`
	Count int      `json:"count"`
}

// Status is where a vault stands against this binary's steps.
type Status struct {
	FormatVersion int       `json:"format_version"`
	Latest        int       `json:"latest"`
	Pending       []Pending `json:"pending"`
}

// maxListed bounds the document ids a Pending names.
const maxListed = 20

// Reader is the part of a store a status check needs.
type Reader interface {
	Config(key string) (string, error)
	List(ctx context.Context, f store.Filter) ([]model.Ref, error)
	Get(ctx context.Context, id string) (model.Document, error)
}

// Current reads format_version; a vault without it is at 0.
func Current(r Reader) (int, error) {
	v, err := r.Config(ConfigKey)
	if err != nil {
		return 0, fmt.Errorf("upgrade: current: %w", err)
	}
	if strings.TrimSpace(v) == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("upgrade: current: %s=%q is not a number", ConfigKey, v)
	}
	return n, nil
}

// StatusOf lists the steps that still have work. It reads nothing when
// format_version is current.
func StatusOf(ctx context.Context, r Reader) (Status, error) {
	cur, err := Current(r)
	if err != nil {
		return Status{}, err
	}
	st := Status{FormatVersion: cur, Latest: Latest(), Pending: []Pending{}}
	for _, s := range steps() {
		if s.Version <= cur {
			continue
		}
		docs, n, err := matching(ctx, r, s)
		if err != nil {
			return st, err
		}
		if n > 0 {
			st.Pending = append(st.Pending, Pending{ID: s.ID, Title: s.Title, Auto: s.Auto, Docs: docs, Count: n})
		}
	}
	return st, nil
}

// matching returns up to maxListed ids of the documents the step selects, and the count.
func matching(ctx context.Context, r Reader, s Step) ([]string, int, error) {
	var ids []string
	n := 0
	for _, t := range s.Types {
		refs, err := r.List(ctx, store.Filter{Type: t})
		if err != nil {
			return nil, 0, fmt.Errorf("upgrade: %s: %w", s.ID, err)
		}
		for _, ref := range refs {
			d, err := r.Get(ctx, ref.ID)
			if err != nil {
				continue
			}
			if s.Select(d) {
				n++
				if len(ids) < maxListed {
					ids = append(ids, d.ID)
				}
			}
		}
	}
	return ids, n, nil
}

// RunResult is what Run did, step by step.
type RunResult struct {
	DryRun        bool          `json:"dry_run,omitempty"`
	FormatVersion int           `json:"format_version"`
	Steps         []StepOutcome `json:"steps"`
}

// StepOutcome is one step in a run. Batch is the id to undo an automated step.
type StepOutcome struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Auto  bool     `json:"auto"`
	Batch string   `json:"batch,omitempty"`
	Docs  []string `json:"docs"`
	Count int      `json:"count"`
}

var batchStamp = regexp.MustCompile(`-\d{14}$`)

// BatchID names the batch of a step run at t. UndoStep reads the step back.
func BatchID(stepID string, t time.Time) string {
	return "up-" + stepID + "-" + t.UTC().Format("20060102150405")
}

func stepOfBatch(batch string) (Step, bool) {
	id := batchStamp.ReplaceAllString(strings.TrimPrefix(batch, "up-"), "")
	for _, s := range steps() {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// Run applies each pending automated step in order as one batch, or lists what
// it would change when dry is set. It then raises format_version over the steps
// that are done. A judgement step is only counted here: Run changes nothing for it.
func Run(ctx context.Context, s store.Store, dry bool) (RunResult, error) {
	cur, err := Current(s)
	if err != nil {
		return RunResult{}, err
	}
	res := RunResult{DryRun: dry, FormatVersion: cur, Steps: []StepOutcome{}}
	for _, step := range steps() {
		if step.Version <= cur || !step.Auto {
			continue
		}
		batch := BatchID(step.ID, s.Now())
		out, err := s.Rewrite(ctx, batch, dry, func(d model.Document) (model.Document, bool, error) {
			if !step.Select(d) {
				return d, false, nil
			}
			nd, err := step.Rewrite(d)
			return nd, err == nil, err
		})
		if err != nil {
			return res, fmt.Errorf("upgrade: %s: %w", step.ID, err)
		}
		so := StepOutcome{ID: step.ID, Title: step.Title, Auto: true, Docs: out.Docs, Count: len(out.Docs)}
		if !dry && len(out.Docs) > 0 {
			so.Batch = batch
		}
		res.Steps = append(res.Steps, so)
	}
	if dry {
		return res, nil
	}
	res.FormatVersion, err = settle(ctx, s, cur)
	return res, err
}

// settle raises format_version to the last step whose predecessors are all done.
func settle(ctx context.Context, s store.Store, cur int) (int, error) {
	next := cur
	for _, step := range steps() {
		if step.Version <= cur {
			continue
		}
		if _, n, err := matching(ctx, s, step); err != nil {
			return cur, err
		} else if n > 0 {
			break
		}
		next = step.Version
	}
	if next != cur {
		if err := s.SetConfig(ctx, ConfigKey, strconv.Itoa(next)); err != nil {
			return cur, fmt.Errorf("upgrade: settle: %w", err)
		}
	}
	return next, nil
}

// Settle raises format_version after a judgement step clears. It is safe to call at any time.
func Settle(ctx context.Context, s store.Store) (int, error) {
	cur, err := Current(s)
	if err != nil {
		return 0, err
	}
	return settle(ctx, s, cur)
}

// Undo reverses an automated batch on this machine and lowers format_version
// below the step, so the step is pending again.
func Undo(ctx context.Context, s store.Store, batch string) (model.BatchResult, error) {
	step, ok := stepOfBatch(batch)
	if !ok {
		return model.BatchResult{}, fmt.Errorf("upgrade: undo: %s is not the id of an upgrade batch", batch)
	}
	res, err := s.UndoBatch(ctx, batch)
	if err != nil {
		return res, fmt.Errorf("upgrade: undo: %w", err)
	}
	cur, err := Current(s)
	if err != nil {
		return res, err
	}
	if cur >= step.Version {
		if err := s.SetConfig(ctx, ConfigKey, strconv.Itoa(step.Version-1)); err != nil {
			return res, fmt.Errorf("upgrade: undo: %w", err)
		}
	}
	return res, nil
}
