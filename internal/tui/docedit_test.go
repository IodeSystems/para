package tui

import (
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

func TestDocBufferRoundTrip(t *testing.T) {
	d, err := model.Decode("task-x", model.FolderProjects, []byte("type: task\ncreated: 2026-11-01\nstatus: todo\nproject: \"[[project-a]]\"\nblocked_by:\n  - legal said no\n"), "Fix it", "Body.")
	if err != nil {
		t.Fatal(err)
	}
	d.Rev = "r1"
	buf, err := docBuffer(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf, "status:") || !strings.Contains(buf, "project: [[project-a]]") {
		t.Fatalf("buffer = %q", buf)
	}
	in, err := parseDocBuffer(d, buf)
	if err != nil || in.Fields != nil || *in.Title != "Fix it" || *in.Body != "Body." || in.Rev != "r1" {
		t.Fatalf("unchanged: %+v %v", in, err)
	}
	edited := strings.Replace(buf, "title: Fix it", "title: Fix it now", 1)
	edited = strings.Replace(edited, "project: [[project-a]]\n", "project: [[project-a]]\ndue: 2026-12-01\n", 1)
	edited = strings.Replace(edited, "blocked_by: [\"legal said no\"]\n", "", 1)
	in, err = parseDocBuffer(d, edited)
	if err != nil || *in.Title != "Fix it now" || in.Fields["due"] != "2026-12-01" {
		t.Fatalf("edited: %+v %v", in, err)
	}
	if v, ok := in.Fields["blocked_by"]; !ok || v != nil {
		t.Fatalf("a removed line must remove the field: %+v", in.Fields)
	}
	if _, err := parseDocBuffer(d, strings.Replace(buf, "project:", "status: done\nproject:", 1)); err == nil {
		t.Fatal("status was accepted")
	}
}
