package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestSourcesAndEvidenceTakeLocalFiles(t *testing.T) {
	d, proj, _ := questionVault(t)
	agent := agentOf(d)
	dir := t.TempDir()
	file := filepath.Join(dir, "notes", "window.md")
	home := filepath.Join(dir, "home")
	for _, p := range []string{filepath.Dir(file), home} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{file, filepath.Join(home, "plan.md")} {
		if err := os.WriteFile(p, []byte("Window is Saturday.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)

	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Pick the window", Project: proj}).ID
	for name, ref := range map[string]string{
		"relative":  "notes/window.md",
		"missing":   filepath.Join(dir, "nope.md"),
		"directory": filepath.Join(dir, "notes"),
		"home dir":  "~/",
	} {
		_, err := QuestionAnswer.Run(ctx, agent, QuestionAnswerInput{ID: q, Text: "Sat", Sources: []model.AnswerSource{{Ref: ref}}})
		if err == nil {
			t.Errorf("%s: %s accepted", name, ref)
		}
	}
	out := must(t, QuestionAnswer, agent, QuestionAnswerInput{ID: q, Text: "Sat",
		Sources: []model.AnswerSource{{Ref: file, Quote: "Window is Saturday."}, {Ref: "~/plan.md"}}})
	if out.Status != model.QuestionAnswered {
		t.Fatalf("out = %+v", out)
	}
	sh := must(t, QuestionShow, d, QuestionShowInput{ID: q})
	for _, want := range []string{"- Source: " + file + `: "Window is Saturday."`, "- Source: ~/plan.md"} {
		if !strings.Contains(sh.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, sh.Body)
		}
	}
	mustLintClean(t, d)

	for name, loc := range map[string]string{"missing": filepath.Join(dir, "nope.md"), "directory": dir, "relative": "notes/window.md"} {
		if err := checkEvidence(ctx, d, model.Evidence{Locator: loc}); err == nil {
			t.Errorf("evidence %s: %s accepted", name, loc)
		}
	}
	for _, loc := range []string{file, "~/plan.md"} {
		if err := checkEvidence(ctx, d, model.Evidence{Locator: loc}); err != nil {
			t.Errorf("evidence %s: %v", loc, err)
		}
	}
}

func TestCloseTakesLocalFileEvidence(t *testing.T) {
	v := newVerbVault(t)
	id := v.task(t, "Write the notes")
	file := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(file, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Close.Run(writeCtx(t), v.agent, CloseInput{ID: id, Evidence: &model.Evidence{Locator: file + ".gone"}}); err == nil {
		t.Fatal("a missing file accepted as evidence")
	}
	out, err := Close.Run(writeCtx(t), v.agent, CloseInput{ID: id, Evidence: &model.Evidence{Locator: file}})
	if err != nil || out.Op != "task_done" {
		t.Fatalf("%+v %v", out, err)
	}
}
