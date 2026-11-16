package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/guide"
)

// guide returns the text of the MCP prompt of that name; an unknown topic is
// refused and lists the topics.
func TestGuideTopics(t *testing.T) {
	out := must(t, Guide, Deps{}, GuideInput{Topic: "loop"})
	if out.Topic != "loop" || out.Text != guide.Loop() {
		t.Fatalf("loop = %q", out.Text[:40])
	}
	for _, name := range guide.Topics {
		if got := must(t, Guide, Deps{}, GuideInput{Topic: name}); got.Text == "" {
			t.Errorf("topic %s is empty", name)
		}
	}
	if got := must(t, Guide, Deps{}, GuideInput{}); got.Text != guide.Extended() {
		t.Error("no topic is not the use cases")
	}
	_, err := Guide.Run(ctx, Deps{}, GuideInput{Topic: "nope"})
	if err == nil || !strings.Contains(err.Error(), "grill_goal") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown topic error = %v", err)
	}
}
