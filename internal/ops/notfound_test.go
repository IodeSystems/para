package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestMissingLinkArgumentsSayWhatToDo(t *testing.T) {
	f := newPropFix(t)
	ctx := t.Context()
	tk, err := TaskAdd.Run(ctx, f.owner, TaskAddInput{Project: f.proj, Title: "Real", DoneWhen: "Done."})
	if err != nil {
		t.Fatal(err)
	}
	want := func(name string, err error, part string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), part) || strings.Contains(err.Error(), "vault: get") {
			t.Errorf("%s: err = %v, want it to hold %q", name, err, part)
		}
		if !errors.Is(err, model.ErrNotFound) {
			t.Errorf("%s: err does not unwrap to ErrNotFound", name)
		}
	}
	proj := `project "project-nope" not found; list goals with project_list or search with similar`
	_, err = TaskAdd.Run(ctx, f.owner, TaskAddInput{Project: "project-nope", Title: "X", DoneWhen: "Done."})
	want("task_add project", err, proj)
	_, err = TaskAdd.Run(ctx, f.owner, TaskAddInput{Area: "area-nope", Title: "X", DoneWhen: "Done."})
	want("task_add area", err, `area "area-nope" not found; list areas with area_list`)
	_, err = TaskAdd.Run(ctx, f.owner, TaskAddInput{Project: f.proj, Parent: "task-nope", Title: "X", DoneWhen: "Done."})
	want("task_add parent", err, `parent "task-nope" not found; list tasks with task_list`)
	_, err = QuestionAdd.Run(ctx, f.owner, QuestionAddInput{Text: "Why?", Project: "project-nope"})
	want("question_add project", err, proj)
	_, err = RFXAdd.Run(ctx, f.owner, RFXAddInput{Direction: model.RFXOut, Ask: "Please", Route: "x", Project: "project-nope"})
	want("rfx_add project", err, `project "project-nope" not found`)
	_, err = TaskMove.Run(ctx, f.owner, TaskMoveInput{ID: tk.ID, Project: "project-nope"})
	want("task_move project", err, proj)
}
