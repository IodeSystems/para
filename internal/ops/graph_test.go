package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/model"
)

func TestMermaid(t *testing.T) {
	cases := []struct {
		name string
		in   GraphOutput
		want []string
		not  []string
	}{
		{"empty", GraphOutput{}, []string{"flowchart TD\n"}, []string{"-->", "class n"}},
		{"edge runs dependency to dependent",
			GraphOutput{
				Nodes: []GraphNode{
					{ID: "task-a", Type: model.TypeTask, Title: "design", State: StateDone},
					{ID: "rfx-b", Type: model.TypeRFX, Title: "ask", State: StateWaiting},
				},
				Edges: []GraphEdge{{From: "rfx-b", To: "task-a"}},
			},
			[]string{`n0["design"]`, `n1(["ask"])`, "n0 --> n1", "class n0 done", "class n1 waiting"}, nil},
		{"quotes in titles cannot break labels",
			GraphOutput{Nodes: []GraphNode{{ID: "task-a", Type: model.TypeTask, Title: `say "hi"` + "\nnow", State: StateReady}}},
			[]string{`n0["say 'hi' now"]`}, []string{`"hi"`}},
		{"missing node keeps its id",
			GraphOutput{Nodes: []GraphNode{{ID: "task-x", Type: "missing", Title: "task-x", State: StateMissing}}},
			[]string{`n0["task-x"]`, "class n0 missing"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Mermaid(c.in)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("lacks %q:\n%s", w, got)
				}
			}
			for _, w := range c.not {
				if strings.Contains(got, w) {
					t.Errorf("has %q:\n%s", w, got)
				}
			}
		})
	}
}

func TestResolved(t *testing.T) {
	task := func(s model.TaskStatus) model.Document { return model.Document{Meta: &model.Task{Status: s}} }
	rfx := func(s model.RFXStatus) model.Document { return model.Document{Meta: &model.RFX{Status: s}} }
	cases := []struct {
		name string
		doc  model.Document
		want bool
	}{
		{"todo", task(model.TaskTodo), false},
		{"doing", task(model.TaskDoing), false},
		{"blocked", task(model.TaskBlocked), false},
		{"done", task(model.TaskDone), true},
		{"dropped", task(model.TaskDropped), true},
		{"rfx open", rfx(model.RFXOpen), false},
		{"rfx answered", rfx(model.RFXAnswered), true},
		{"rfx closed", rfx(model.RFXClosed), true},
		{"rfx declined", rfx(model.RFXDeclined), false},
		{"other type", model.Document{Meta: &model.Inbox{}}, false},
	}
	for _, c := range cases {
		if got := resolved(c.doc); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
