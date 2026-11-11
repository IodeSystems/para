package cli

import (
	"bytes"
	"testing"
)

func TestVersionPrintsStamp(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var out bytes.Buffer
		root := newRoot(&loader{version: "v1.2.3"})
		root.SetOut(&out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if got := out.String(); got != "v1.2.3\n" {
			t.Fatalf("%v: got %q", args, got)
		}
	}
}
