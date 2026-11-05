package ops

import (
	"context"
	"fmt"
	"slices"

	"github.com/iodesystems/para/internal/model"
)

// Feedback about para itself is an incoming io item tagged para-feedback, in
// the vault the agent works in: the owner reads it in the inbox or under the
// topic, and carries it out of the vault by hand. Para sends nothing.

// FeedbackTag is the tag of every feedback item.
const FeedbackTag = "para-feedback"

// FeedbackKind is feedback_add's kind.
type FeedbackKind string

const (
	FeedbackBug      FeedbackKind = "bug"
	FeedbackFriction FeedbackKind = "friction"
	FeedbackIdea     FeedbackKind = "idea"
)

// FeedbackKinds lists every FeedbackKind.
var FeedbackKinds = []FeedbackKind{FeedbackBug, FeedbackFriction, FeedbackIdea}

// Enum lists the allowed values, for schema generation.
func (FeedbackKind) Enum() []string { return enumStrs(FeedbackKinds) }

// FeedbackAddInput is the input of feedback_add.
type FeedbackAddInput struct {
	Text string       `json:"text"`
	Kind FeedbackKind `json:"kind,omitempty"` // default friction
	Tool string       `json:"tool,omitempty"` // the op it is about, such as proposal_decide
}

// FeedbackAddOutput is the output of feedback_add.
type FeedbackAddOutput struct {
	ID   string   `json:"id"`
	Tags []string `json:"tags"`
	Rev  string   `json:"rev"`
}

// FeedbackAdd files feedback about para as a tagged incoming io item.
var FeedbackAdd = defineSafe("feedback_add", "File feedback about para itself (not about the owner's work): a bug, friction, or an idea, with the tool it is about. It becomes an incoming io item tagged "+FeedbackTag+" from you, in this vault; the owner reads it in the inbox or under that topic (tag_list). Rate it minor and obvious", func(ctx context.Context, d Deps, in FeedbackAddInput) (FeedbackAddOutput, error) {
	kind := in.Kind
	if kind == "" {
		kind = FeedbackFriction
	}
	if !slices.Contains(FeedbackKinds, kind) {
		return FeedbackAddOutput{}, fmt.Errorf("kind %q: want bug, friction, or idea", kind)
	}
	tool := ioLine(in.Tool)
	if tool != "" {
		if _, ok := registry[tool]; !ok {
			return FeedbackAddOutput{}, fmt.Errorf("tool %q: no such op", tool)
		}
	}
	text := cleanText(in.Text)
	if text == "" {
		return FeedbackAddOutput{}, fmt.Errorf("text is empty")
	}
	subject := "para " + string(kind)
	if tool != "" {
		subject += ": " + tool
	}
	added, err := IOAdd.Run(ctx, d, IOAddInput{Direction: model.IOIn, Text: text, Subject: subject, From: "agent " + d.Store.Info().Actor, Tags: []string{FeedbackTag}})
	if err != nil {
		return FeedbackAddOutput{}, err
	}
	return FeedbackAddOutput{ID: added.ID, Tags: []string{FeedbackTag}, Rev: added.Rev}, nil
})
