package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/guide"
)

// RunLoopHint tells the owner how to start an agent that works the queue. It
// is the one wording of every "no agent" hint.
const RunLoopHint = "run /loop 30m /para (or the loop prompt)"

// GuideInput names a guide topic: the name of an MCP prompt. Empty is the
// use cases of `para --help-extended`.
type GuideInput struct {
	Topic string `json:"topic,omitempty" query:"topic"`
}

// GuideOutput is the guide text of the topic.
type GuideOutput struct {
	Topic string `json:"topic"`
	Text  string `json:"text"`
}

// Guide returns the text of an MCP prompt, so an agent without prompt support
// reads the same copy.
var Guide = defineRead("guide", "Get the guide text of a topic, the same text as the MCP prompt of that name: "+strings.Join(guide.Topics, ", ")+". No topic: use cases with real commands. Start with topic loop", func(_ context.Context, _ Deps, in GuideInput) (GuideOutput, error) {
	if in.Topic == "" {
		return GuideOutput{Text: guide.Extended()}, nil
	}
	text, ok := guide.Topic(in.Topic)
	if !ok {
		return GuideOutput{}, fmt.Errorf("guide: unknown topic %q: use one of %s", in.Topic, strings.Join(guide.Topics, ", "))
	}
	return GuideOutput{Topic: in.Topic, Text: text}, nil
})
