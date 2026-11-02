package cli

import (
	"strings"
	"testing"
)

func TestReviewCommandShape(t *testing.T) {
	root := newRoot(&loader{})
	cmd, rest, err := root.Find([]string{"review", "proposal-x"})
	if err != nil || cmd.Name() != "review" {
		t.Fatalf("find: %v %v", cmd, err)
	}
	if err := cmd.ValidateArgs(rest); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	e.init("acme")
	if _, _, err := e.run("review"); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("review with no terminal: %v", err)
	}
}
