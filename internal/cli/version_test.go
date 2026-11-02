package cli

import (
	"bytes"
	"testing"
)

func TestVersionPrintsStamp(t *testing.T) {
	var out bytes.Buffer
	root := newRoot(&loader{version: "v1.2.3"})
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "v1.2.3\n" {
		t.Fatalf("got %q", got)
	}
}
