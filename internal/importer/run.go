package importer

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	ashid "github.com/IodeSystems/ashid/go"
	"gopkg.in/yaml.v3"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/paramd"
	"github.com/iodesystems/parasolve/internal/vault"
)

// Writer is the vault side of an import. internal/store/local.Store satisfies
// it. It is not part of store.Store: only the CLI import command uses it.
type Writer interface {
	CreateBatch(ctx context.Context, batch string, docs []model.Document) ([]vault.BatchResult, error)
	ImportedDocs(ctx context.Context) ([]vault.ImportedDoc, map[string]bool, error)
	RemoveBatch(ctx context.Context, batch string) (vault.RemoveResult, error)
}

// RunOptions configures Run.
type RunOptions struct {
	DryRun bool
	Now    func() time.Time // nil means time.Now
}

// RunResult is the outcome of a run. It holds ids, counts, and paths, no
// source text.
type RunResult struct {
	Batch         string         `json:"batch"`
	DryRun        bool           `json:"dry_run"`
	Entries       int            `json:"entries"`
	Created       int            `json:"created"`
	Already       int            `json:"already_imported"`
	SourceChanged int            `json:"source_changed"`
	Skipped       int            `json:"skipped"`
	Duplicates    int            `json:"duplicates"`
	Refused       int            `json:"refused"`
	ByType        map[string]int `json:"by_type"`
	Normalized    map[string]int `json:"normalized"`
	Issues        []Issue        `json:"issues"`
	Millis        int64          `json:"millis"`
}

// LoadPlan reads a plan file.
func LoadPlan(p string) (*Plan, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("importer: plan: %w", err)
	}
	var plan Plan
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&plan); err != nil {
		return nil, fmt.Errorf("importer: plan: %s: %w", p, err)
	}
	if plan.Version != 1 {
		return nil, fmt.Errorf("importer: plan: version %d is not supported", plan.Version)
	}
	return &plan, nil
}

const planHeader = `# para import plan. Edit the entries, then: para import run <this file> --dry-run
#
# Each entry plans one document. path identifies the source and is not edited.
# You may change: type, folder, id, area, project, tags, skip.
#   type     note, area, project, inbox, journal, person (events-derived entries keep their type)
#   folder   a vault folder: inbox projects areas resources archive journal interactions questions rfx ...
#   id       the file name stem in the vault: lower-case a-z 0-9 and '-'
#   area, project   a vault document id, or the id of another entry in this plan
#   skip     any text keeps the entry out of the run
# duplicate_of: the entry has the same bytes as the named one; run skips it. Delete the line to import it.
# refuse: the converted document fails lint (the reason is printed). Fix the source and scan again.
# notes: what scan changed to make the file conform. Read-only.
# hash: the source hash at scan. A source that changed since is not imported.
`

// WritePlan writes the plan to p. It refuses to overwrite a file unless force.
func WritePlan(p string, plan *Plan, force bool) error {
	data, err := yaml.Marshal(plan)
	if err != nil {
		return fmt.Errorf("importer: write plan: %w", err)
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(p, flags, 0o600)
	if err != nil {
		return fmt.Errorf("importer: write plan: %w (use --force to replace a plan)", err)
	}
	if _, err := f.WriteString(planHeader + string(data)); err != nil {
		_ = f.Close()
		return fmt.Errorf("importer: write plan: %w", err)
	}
	return f.Close()
}

// Run imports the plan as one batch.
func Run(ctx context.Context, w Writer, plan *Plan, opts RunOptions) (*RunResult, error) {
	start := time.Now()
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	if plan.Adapter != AdapterMarkdown && plan.Adapter != AdapterLegacy {
		return nil, fmt.Errorf("importer: run: plan adapter %q is not markdown or legacy", plan.Adapter)
	}
	co, err := load(plan.Source, plan.Adapter)
	if err != nil {
		return nil, fmt.Errorf("importer: run: %w", err)
	}
	byKey := map[string]*cand{}
	for _, c := range co.cands {
		byKey[c.key] = c
	}
	entries := map[*cand]Entry{}
	for _, e := range plan.Entries {
		c := byKey[e.Path]
		if c == nil {
			return nil, fmt.Errorf("importer: run: plan entry %q is not in the source: scan again", safe(e.Path))
		}
		if _, dup := entries[c]; dup {
			return nil, fmt.Errorf("importer: run: plan lists %q twice", safe(e.Path))
		}
		entries[c] = e
	}
	for _, c := range co.cands {
		if _, ok := entries[c]; !ok {
			return nil, fmt.Errorf("importer: run: %q is in the source but not in the plan: scan again", safe(c.key))
		}
	}
	sort.Slice(co.cands, func(i, j int) bool { return co.cands[i].key < co.cands[j].key })

	imported, vaultIDs, err := w.ImportedDocs(ctx)
	if err != nil {
		return nil, fmt.Errorf("importer: run: %w", err)
	}
	byURL := map[string]vault.ImportedDoc{}
	for _, d := range imported {
		if d.SourceURL != "" {
			byURL[d.SourceURL] = d
		}
	}

	res := &RunResult{Batch: "imp-" + strings.ToLower(ashid.New()), DryRun: opts.DryRun, Entries: len(plan.Entries),
		ByType: map[string]int{}, Normalized: map[string]int{}, Issues: []Issue{}}
	refuse := func(c *cand, why string) {
		res.Refused++
		res.Issues = append(res.Issues, Issue{safe(c.key), safe(why)})
	}
	b := newBctx(plan.Source, co.cands, entries)
	b.now = now().UTC().Format(time.RFC3339)
	for id := range vaultIDs {
		b.vaultID[id] = true
	}

	// Decide each entry before building: links need every final id.
	var todo []*cand
	planIDs := map[string]*cand{}
	for _, c := range co.cands {
		e := entries[c]
		switch {
		case e.Skip != "":
			res.Skipped++
			continue
		case e.DuplicateOf != "":
			if o := byKey[e.DuplicateOf]; o == nil {
				refuse(c, "duplicate_of names no entry")
			} else {
				res.Duplicates++
			}
			continue
		case e.Refuse != "":
			refuse(c, "refused at scan: "+e.Refuse)
			continue
		case e.Hash != c.hash:
			refuse(c, "source changed since scan: scan again")
			continue
		}
		if !paramd.ValidStem(e.ID) || strings.HasSuffix(e.ID, ".source") {
			refuse(c, fmt.Sprintf("id %q is not a valid file name", safe(e.ID)))
			continue
		}
		if err := checkEntry(c, e); err != nil {
			refuse(c, err.Error())
			continue
		}
		url := b.provenance(c)["url"].(string)
		if d, ok := byURL[url]; ok {
			b.final[c] = d.ID
			if d.SourceHash == c.hash {
				res.Already++
			} else {
				res.SourceChanged++
				res.Issues = append(res.Issues, Issue{safe(c.key), "source changed since import as " + d.ID + "; not updated"})
			}
			continue
		}
		if o := planIDs[strings.ToLower(e.ID)]; o != nil {
			refuse(c, fmt.Sprintf("id %q is also used by %q", e.ID, safe(o.key)))
			continue
		}
		if vaultIDs[strings.ToLower(e.ID)] {
			refuse(c, fmt.Sprintf("id %q is already in the vault", e.ID))
			continue
		}
		planIDs[strings.ToLower(e.ID)] = c
		b.final[c] = e.ID
		todo = append(todo, c)
	}

	// Build until stable: a refused document leaves a hole that earlier
	// documents may have linked to, so those are built again without it.
	var docs []model.Document
	var from []*cand
	var noteSets [][]string
	for pass := 0; pass < 8; pass++ {
		docs, from, noteSets = nil, nil, nil
		again := false
		for _, c := range todo {
			if _, ok := b.final[c]; !ok {
				continue
			}
			doc, notes, err := c.build(b, c, entries[c])
			if err == nil {
				var msg string
				if msg, err = lintDoc(doc); err == nil && msg != "" {
					err = fmt.Errorf("%s", msg)
				}
			}
			if err != nil {
				delete(b.final, c)
				refuse(c, err.Error())
				again = true
				continue
			}
			docs = append(docs, doc)
			from = append(from, c)
			noteSets = append(noteSets, dedupe(append(append([]string(nil), c.notes...), notes...)))
		}
		if !again {
			break
		}
	}
	for _, ns := range noteSets {
		for _, n := range ns {
			k, _, _ := strings.Cut(n, ":")
			res.Normalized[k]++
		}
	}
	if opts.DryRun {
		res.Created = len(docs)
		for _, d := range docs {
			res.ByType[string(d.Meta.Header().Type)]++
		}
		res.Millis = time.Since(start).Milliseconds()
		return res, nil
	}
	if len(docs) > 0 {
		results, err := w.CreateBatch(ctx, res.Batch, docs)
		if err != nil {
			return nil, fmt.Errorf("importer: run: %w", err)
		}
		for i, r := range results {
			if r.Err != nil {
				refuse(from[i], r.Err.Error())
				continue
			}
			res.Created++
			res.ByType[string(docs[i].Meta.Header().Type)]++
		}
	}
	res.Millis = time.Since(start).Milliseconds()
	return res, nil
}

// checkEntry validates the decisions a person made in the plan.
func checkEntry(c *cand, e Entry) error {
	if !model.Folder(e.Folder).Valid() {
		return fmt.Errorf("folder %q is not a vault folder", safe(e.Folder))
	}
	if c.fixedType {
		if e.Type != string(c.typ) {
			return fmt.Errorf("type %q cannot be changed for this entry (it is %s)", safe(e.Type), c.typ)
		}
		return nil
	}
	switch model.Type(e.Type) {
	case model.TypeNote, model.TypeArea, model.TypeProject, model.TypeInbox, model.TypeJournal, model.TypePerson:
		return nil
	}
	return fmt.Errorf("type %q is not one a markdown file can become (note area project inbox journal person)", safe(e.Type))
}

// Undo removes the documents a batch created, unless one changed since.
func Undo(ctx context.Context, w Writer, batch string) (vault.RemoveResult, error) {
	res, err := w.RemoveBatch(ctx, batch)
	if err != nil {
		return res, fmt.Errorf("importer: undo: %w", err)
	}
	return res, nil
}
