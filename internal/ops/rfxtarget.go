package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// Targets record who an RFX goes to and by what medium. Para sends nothing;
// the user sends, then records the state.

// buildTargets validates each input and appends it to have. A target is new
// when no existing one has the same person or group, medium, and value.
func buildTargets(ctx context.Context, d Deps, have []model.Target, in []TargetInput) ([]model.Target, error) {
	out := slices.Clone(have)
	for i, t := range in {
		to, err := linkTo(ctx, d, fmt.Sprintf("targets[%d].to", i), t.To, model.TypePerson, model.TypeGroup)
		if err != nil {
			return nil, err
		}
		if to == "" {
			return nil, fmt.Errorf("targets[%d]: to is empty", i)
		}
		if t.Medium == "" {
			return nil, fmt.Errorf("targets[%d]: medium is empty: say how it goes (%v)", i, model.Mediums)
		}
		if err := checkMedium(t.Medium); err != nil {
			return nil, fmt.Errorf("targets[%d]: %w", i, err)
		}
		value := strings.Join(strings.Fields(t.Value), " ")
		if value == "" {
			doc, err := d.Store.Get(ctx, to.Target())
			if err != nil {
				return nil, err
			}
			if p, ok := doc.Meta.(*model.Person); ok {
				value = preferredValue(p, t.Medium)
			}
		}
		if slices.ContainsFunc(out, func(x model.Target) bool {
			return x.To == to && x.Medium == t.Medium && x.Value == value
		}) {
			return nil, fmt.Errorf("targets[%d]: %s by %s is already a target", i, to.Target(), t.Medium)
		}
		out = append(out, model.Target{To: to, Medium: t.Medium, Value: value, State: model.TargetDrafted})
	}
	return out, nil
}

// RFXTargetAddInput is the input of rfx_target_add.
type RFXTargetAddInput struct {
	ID     string       `json:"id"`
	To     string       `json:"to"`
	Medium model.Medium `json:"medium"`
	Value  string       `json:"value,omitempty"`
}

// RFXTargetOutput is the output of the target ops.
type RFXTargetOutput struct {
	ID      string         `json:"id"`
	Targets []model.Target `json:"targets"`
	Waiting bool           `json:"waiting"`
	Rev     string         `json:"rev"`
}

// RFXTargetAdd adds a target in state drafted.
var RFXTargetAdd = define("rfx_target_add", "Add a person or group, and a medium, to a request", func(ctx context.Context, d Deps, in RFXTargetAddInput) (RFXTargetOutput, error) {
	return changeTargets(ctx, d, in.ID, func(m *model.RFX) error {
		tg, err := buildTargets(ctx, d, m.Targets, []TargetInput{{To: in.To, Medium: in.Medium, Value: in.Value}})
		m.Targets = tg
		return err
	})
})

// RFXTargetStateInput is the input of rfx_target_state.
type RFXTargetStateInput struct {
	ID     string            `json:"id"`
	To     string            `json:"to"`
	Medium model.Medium      `json:"medium,omitempty"` // needed when the same target has several mediums
	State  model.TargetState `json:"state"`
	Ref    string            `json:"ref,omitempty"`  // thread or ticket URL or id
	Date   string            `json:"date,omitempty"` // YYYY-MM-DD, the day it was sent; empty means the local date
}

// RFXTargetState sets the state of one target. Moving to sent records the
// date.
var RFXTargetState = define("rfx_target_state", "Record that a request to one target was sent, answered, or left unanswered", func(ctx context.Context, d Deps, in RFXTargetStateInput) (RFXTargetOutput, error) {
	if !slices.Contains(model.TargetStates, in.State) {
		return RFXTargetOutput{}, fmt.Errorf("state %q: want one of %v", in.State, model.TargetStates)
	}
	date, err := dateOrToday(in.Date)
	if err != nil {
		return RFXTargetOutput{}, err
	}
	to := normID(in.To)
	return changeTargets(ctx, d, in.ID, func(m *model.RFX) error {
		var hit []int
		for i, t := range m.Targets {
			if t.To.Target() == to && (in.Medium == "" || t.Medium == in.Medium) {
				hit = append(hit, i)
			}
		}
		switch {
		case len(hit) == 0:
			return fmt.Errorf("no target %s %s: add it with rfx target add", to, in.Medium)
		case len(hit) > 1:
			return fmt.Errorf("%s has %d targets: pass medium", to, len(hit))
		}
		t := &m.Targets[hit[0]]
		if (in.State == model.TargetRequested || in.State == model.TargetScheduled) && t.Medium != model.MediumMeeting {
			return fmt.Errorf("state %s applies to a meeting target; this target is %s", in.State, t.Medium)
		}
		if in.State == model.TargetSent && t.State != model.TargetSent {
			t.Sent = model.Date(date)
		}
		t.State = in.State
		if ref := strings.TrimSpace(in.Ref); ref != "" {
			t.Ref = ref
		}
		return nil
	})
})

func changeTargets(ctx context.Context, d Deps, id string, change func(*model.RFX) error) (RFXTargetOutput, error) {
	doc, m, err := getRFX(ctx, d, normID(id))
	if err != nil {
		return RFXTargetOutput{}, err
	}
	if err := change(m); err != nil {
		return RFXTargetOutput{}, err
	}
	if m.Status == model.RFXClosed {
		return RFXTargetOutput{}, errors.New("request is closed")
	}
	doc, err = d.Store.Update(ctx, doc)
	if err != nil {
		return RFXTargetOutput{}, err
	}
	return RFXTargetOutput{ID: doc.ID, Targets: append([]model.Target{}, m.Targets...), Waiting: isWaiting(m), Rev: doc.Rev}, nil
}
