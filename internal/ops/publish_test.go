package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// publish decides what leaves the vault: the owner sets it, and no agent
// write changes it, by publish_set, doc_edit, or any other op.
func TestPublishFieldIsTheOwners(t *testing.T) {
	f := newAgentFix(t)
	w := writeCtx(t)
	if !PublishSet.PersonOnly() || slices.ContainsFunc(ForAgents(), func(e Entry) bool { return e.Name() == "publish_set" }) {
		t.Fatal("publish_set is listed for agents")
	}
	if _, err := PublishSet.Run(w, f.agent, PublishSetInput{ID: f.proj, Sets: []string{"design"}}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent publish_set = %v", err)
	}
	_, err := DocEdit.Run(w, f.agent, DocEditInput{ID: f.proj, Fields: map[string]any{"publish": []any{"design"}}})
	if err == nil || !strings.Contains(err.Error(), "publish_set") {
		t.Fatalf("agent doc_edit publish = %v", err)
	}
	out := mustRun(t, PublishSet, f.owner, PublishSetInput{ID: f.proj, Sets: []string{"runbooks", "design", "design"}})
	if !slices.Equal(out.Publish, []string{"design", "runbooks"}) {
		t.Fatalf("publish = %v", out.Publish)
	}
	if _, err := PublishSet.Run(t.Context(), f.owner, PublishSetInput{ID: f.proj, Sets: []string{"Bad Name"}}); err == nil {
		t.Fatal("bad set name accepted")
	}
	if _, err := PublishSet.Run(t.Context(), f.owner, PublishSetInput{ID: f.proj, Sets: []string{"preview"}}); err == nil {
		t.Fatal("reserved set name accepted")
	}

	// An agent's other writes keep the field as it is.
	body := "Edited by the agent."
	mustRun(t, DocEdit, f.agent, DocEditInput{ID: f.proj, Body: &body})
	doc, err := f.owner.Store.Get(t.Context(), f.proj)
	if err != nil || !slices.Equal(doc.Meta.Header().Publish, []string{"design", "runbooks"}) || doc.Body != body {
		t.Fatalf("after agent edit: publish %v body %q err %v", doc.Meta.Header().Publish, doc.Body, err)
	}

	// Any agent write that changes it is refused where every agent write
	// passes: the recorder.
	rec := newRecorder(f.agent.Store)
	doc.Meta.Header().Publish = nil
	if _, err := rec.Update(t.Context(), doc); !errors.Is(err, ErrPublishOwnerOnly) {
		t.Fatalf("agent update that clears publish = %v", err)
	}
	doc.Meta.Header().Publish = []string{"design", "runbooks", "leak"}
	if _, err := rec.Update(t.Context(), doc); !errors.Is(err, ErrPublishOwnerOnly) {
		t.Fatalf("agent update that adds a set = %v", err)
	}
	doc.ID, doc.Rev = "project-copy", ""
	if _, err := rec.Create(t.Context(), doc); !errors.Is(err, ErrPublishOwnerOnly) {
		t.Fatalf("agent create with publish = %v", err)
	}

	// A set's marking is the owner's setting; the key is a template.
	if _, err := VaultConfigSet.Run(w, f.agent, VaultConfigSetInput{Key: "publish.design.marking", Value: "X"}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent sets a marking = %v", err)
	}
	mustRun(t, VaultConfigSet, f.owner, VaultConfigSetInput{Key: "publish.design.marking", Value: "ACME SHARED"})
	list := mustRun(t, VaultConfigList, f.owner, VaultConfigListInput{})
	if !slices.ContainsFunc(list.Settings, func(c ConfigSetting) bool {
		return c.Key == "publish.design.marking" && c.Known && c.Value == "ACME SHARED"
	}) {
		t.Fatalf("settings = %+v", list.Settings)
	}
	if _, err := VaultConfigSet.Run(t.Context(), f.owner, VaultConfigSetInput{Key: "publish.<set>.marking", Value: "X"}); err == nil {
		t.Fatal("the template key was set")
	}
	pv := mustRun(t, PublishPreview, f.agent, PublishPreviewInput{Set: "design"})
	if pv.Marking != "ACME SHARED" || len(pv.Files) != 2 || pv.Files[1].Name != "index.md" {
		t.Fatalf("preview = %+v", pv)
	}

	// Clearing: no sets takes it out of every set.
	if out := mustRun(t, PublishSet, f.owner, PublishSetInput{ID: f.proj}); len(out.Publish) != 0 {
		t.Fatalf("cleared publish = %v", out.Publish)
	}
}

// The batch paths write whole documents too. An agent store refuses a batch
// rewrite that changes publish, in the rewritten or in a made document, and
// an undo, which restores old bytes.
func TestPublishFieldBatchPaths(t *testing.T) {
	f := newAgentFix(t)
	ctx := t.Context()
	mustRun(t, PublishSet, f.owner, PublishSetInput{ID: f.proj, Sets: []string{"design"}})
	rec := newRecorder(f.agent.Store)

	set := func(sets []string) model.RewriteFunc {
		return func(d model.Document) (model.Document, bool, error) {
			if d.ID != f.proj {
				return d, false, nil
			}
			d.Meta.Header().Publish = sets
			return d, true, nil
		}
	}
	if _, err := rec.Rewrite(ctx, "agent-leak", false, set([]string{"design", "leak"})); !errors.Is(err, ErrPublishOwnerOnly) {
		t.Fatalf("agent Rewrite that adds a set = %v", err)
	}
	if _, err := rec.RewriteExpand(ctx, "agent-leak2", false, func(d model.Document) (model.Document, []model.Document, bool, error) {
		if d.ID != f.proj {
			return d, nil, false, nil
		}
		n := d
		n.ID, n.Rev = "project-leak", ""
		return d, []model.Document{n}, true, nil
	}); !errors.Is(err, ErrPublishOwnerOnly) {
		t.Fatalf("agent RewriteExpand that makes a published document = %v", err)
	}
	// A rewrite that keeps the field passes.
	if _, err := rec.Rewrite(ctx, "agent-ok", false, func(d model.Document) (model.Document, bool, error) {
		if d.ID != f.proj {
			return d, false, nil
		}
		d.Body += "\nMore."
		return d, true, nil
	}); err != nil {
		t.Fatalf("agent Rewrite that keeps publish = %v", err)
	}
	if _, err := rec.UndoBatch(ctx, "agent-ok"); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent UndoBatch = %v", err)
	}

	// The owner's store is not wrapped: an upgrade step may change the field.
	if _, err := f.owner.Store.Rewrite(ctx, "owner-ok", false, set([]string{"design", "runbooks"})); err != nil {
		t.Fatalf("owner Rewrite = %v", err)
	}
}
