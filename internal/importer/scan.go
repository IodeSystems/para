package importer

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/paramd"
)

// bctx is the state of one build pass over a corpus: which unit has which
// final id, and how a link name resolves.
type bctx struct {
	now     string
	srcDir  string
	entries map[*cand]Entry
	byKey   map[string]*cand
	byStem  map[string][]*cand
	final   map[*cand]string // final vault id of a unit that is or will be in the vault
	planIDs map[string]*cand // plan id -> unit
	vaultID map[string]bool  // ids already in the vault (lower case)
}

func newBctx(srcDir string, cs []*cand, entries map[*cand]Entry) *bctx {
	b := &bctx{
		now: time.Now().UTC().Format(time.RFC3339), srcDir: srcDir, entries: entries,
		byKey: map[string]*cand{}, byStem: map[string][]*cand{}, final: map[*cand]string{},
		planIDs: map[string]*cand{}, vaultID: map[string]bool{},
	}
	for _, c := range cs {
		b.byKey[c.key] = c
		for _, n := range c.names {
			b.byStem[n] = append(b.byStem[n], c)
		}
		b.planIDs[entries[c].ID] = c
	}
	return b
}

func (b *bctx) idOf(c *cand) string {
	if e, ok := b.entries[c]; ok && e.DuplicateOf != "" {
		if o := b.byKey[e.DuplicateOf]; o != nil && o != c {
			return b.final[o]
		}
	}
	return b.final[c]
}

// refID maps a plan id (an Area or Project value) to the id in the vault: the
// final id of the unit with that plan id, or the id itself when the vault
// already holds it. Empty when neither.
func (b *bctx) refID(id string) string {
	if c, ok := b.planIDs[id]; ok {
		return b.idOf(c)
	}
	if b.vaultID[strings.ToLower(id)] {
		return id
	}
	return ""
}

// lookup resolves an Obsidian link target: a file name, or a path ending in
// one. Among several, the shortest path wins, as in Obsidian.
func (b *bctx) lookup(name string) *cand {
	name = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(name), ".md")))
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return nil
	}
	stem := path.Base(name)
	var best *cand
	for _, c := range b.byStem[stem] {
		if strings.Contains(name, "/") {
			ok := false
			for _, n := range c.names {
				if n == name || strings.HasSuffix(n, "/"+name) {
					ok = true
				}
			}
			if !ok {
				continue
			}
		}
		if best == nil || len(c.key) < len(best.key) || (len(c.key) == len(best.key) && c.key < best.key) {
			best = c
		}
	}
	return best
}

func (b *bctx) provenance(c *cand) map[string]any {
	fid := "exact"
	if c.md == nil {
		fid = "partial"
	}
	return map[string]any{
		"url":       fileURL(filepath.Join(b.srcDir, filepath.FromSlash(c.srcRel)), c.frag),
		"retrieved": b.now,
		"hash":      c.hash,
		"fidelity":  fid,
		"backend":   "import",
	}
}

// lintDoc renders d and returns the lint errors, one string, or "".
func lintDoc(d model.Document) (string, error) {
	data, err := model.Render(d)
	if err != nil {
		return "", err
	}
	rel := path.Join(string(d.Folder), d.ID+".md")
	var msgs []string
	for _, f := range paramd.Lint(paramd.Parse(rel, data)) {
		if f.Severity == paramd.Error {
			if f.Line > 0 {
				msgs = append(msgs, fmt.Sprintf("[%s] line %d: %s", f.Code, f.Line, f.Msg))
			} else {
				msgs = append(msgs, fmt.Sprintf("[%s] %s", f.Code, f.Msg))
			}
		}
	}
	return strings.Join(msgs, "; "), nil
}

// detect names the adapter that fits dir best, with the reason.
func detect(dir string) (adapter, why string, err error) {
	legacyDirs := []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "Journal/Rollup", "_Templates"}
	score := 0
	var hit []string
	if st, err := os.Lstat(filepath.Join(dir, "events.jsonl")); err == nil && st.Mode().IsRegular() {
		score += 3
		hit = append(hit, "events.jsonl")
	}
	for _, d := range legacyDirs {
		if st, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(d))); err == nil && st.IsDir() {
			score++
			hit = append(hit, d+"/")
		}
	}
	if score >= 3 {
		return AdapterLegacy, fmt.Sprintf("legacy contract score %d: %s", score, strings.Join(hit, " ")), nil
	}
	md := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && isMarkdownName(d.Name()) {
			md++
		}
		return nil
	})
	if md == 0 {
		return "", "", fmt.Errorf("importer: scan: no markdown files and no legacy layout in %s", safe(dir))
	}
	return AdapterMarkdown, fmt.Sprintf("%d markdown file(s); legacy contract score %d (needs 3)", md, score), nil
}

// loadMarkdown makes a unit of every .md file under dir.
func loadMarkdown(dir string) (*corpus, error) {
	w, err := walkDir(dir, nil)
	if err != nil {
		return nil, err
	}
	co := &corpus{adapter: AdapterMarkdown, srcDirs: map[string]int{}, keys: map[string]int{}, unmapped: w.unmapped}
	for _, f := range w.files {
		co.files++
		if !isMarkdownName(f.rel) {
			co.unmapped = append(co.unmapped, Issue{f.rel, "not markdown; not copied"})
			continue
		}
		raw, err := readSource(f)
		if err != nil {
			co.unmapped = append(co.unmapped, Issue{f.rel, "not read: " + err.Error()})
			continue
		}
		c := fileCand(f, raw, model.TypeNote, model.FolderResources)
		co.add(c)
	}
	return co, nil
}

// add registers a unit and counts its folder and frontmatter keys.
func (co *corpus) add(c *cand) {
	co.cands = append(co.cands, c)
	top := "."
	if i := strings.Index(c.srcRel, "/"); i >= 0 {
		top = c.srcRel[:i]
	}
	co.srcDirs[top]++
	if c.md != nil {
		for k := range c.md.fm {
			co.keys[k]++
		}
	}
}

func load(dir, adapter string) (*corpus, error) {
	switch adapter {
	case AdapterMarkdown:
		return loadMarkdown(dir)
	case AdapterLegacy:
		return loadLegacy(dir)
	}
	return nil, fmt.Errorf("importer: unknown adapter %q (markdown or legacy)", adapter)
}

// assignIDs gives each unit a unique default id. A repeat gets -2, -3, ...
func assignIDs(cs []*cand) map[*cand]string {
	sort.Slice(cs, func(i, j int) bool { return cs[i].key < cs[j].key })
	used := map[string]bool{}
	out := make(map[*cand]string, len(cs))
	for _, c := range cs {
		id := c.idBase
		for n := 2; used[strings.ToLower(id)]; n++ {
			id = fmt.Sprintf("%s-%d", c.idBase, n)
		}
		used[strings.ToLower(id)] = true
		out[c] = id
	}
	return out
}

// defaultEntries makes the plan the scan proposes.
func defaultEntries(cs []*cand, ids map[*cand]string) map[*cand]Entry {
	byHash := map[string][]*cand{}
	for _, c := range cs {
		if c.md != nil {
			byHash[c.hash] = append(byHash[c.hash], c)
		}
	}
	out := make(map[*cand]Entry, len(cs))
	for _, c := range cs {
		e := Entry{Path: c.key, Type: string(c.typ), Folder: string(c.folder), ID: ids[c], Title: safe(c.title), Hash: c.hash, Tags: c.tags}
		if c.area != nil {
			e.Area = ids[c.area]
		}
		if c.project != nil {
			e.Project = ids[c.project]
		}
		if g := byHash[c.hash]; len(g) > 1 && g[0] != c {
			e.DuplicateOf = g[0].key
		}
		out[c] = e
	}
	return out
}

// Scan reads dir with the adapter (detected when empty), writes nothing, and
// returns the inventory and the plan it proposes.
func Scan(dir, adapter string) (*Report, *Plan, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("importer: scan: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return nil, nil, fmt.Errorf("importer: scan: %s is not a directory", safe(dir))
	}
	detected, why, derr := detect(abs)
	if adapter == "" {
		if derr != nil {
			return nil, nil, derr
		}
		adapter = detected
	}
	co, err := load(abs, adapter)
	if err != nil {
		return nil, nil, err
	}
	ids := assignIDs(co.cands)
	entries := defaultEntries(co.cands, ids)
	b := newBctx(abs, co.cands, entries)
	for _, c := range co.cands {
		if entries[c].DuplicateOf == "" {
			b.final[c] = entries[c].ID
		}
	}
	rep := &Report{
		Adapter: adapter, Detected: why, Dir: abs, Files: co.files, Documents: len(co.cands),
		ByType: map[string]int{}, ByFolder: map[string]int{}, SourceFolders: co.srcDirs, FrontmatterKeys: co.keys,
		Normalized: map[string]int{}, Duplicates: [][]string{}, Unmapped: co.unmapped, Refused: []Issue{}, Fold: co.fold,
	}
	if rep.Unmapped == nil {
		rep.Unmapped = []Issue{}
	}
	plan := &Plan{Version: 1, Adapter: adapter, Source: abs}
	dups := map[string][]string{}
	for _, c := range co.cands {
		e := entries[c]
		rep.ByType[e.Type]++
		rep.ByFolder[e.Folder]++
		doc, notes, berr := c.build(b, c, e)
		notes = append(append([]string(nil), c.notes...), notes...)
		if berr == nil {
			var lerr error
			if e.DuplicateOf == "" {
				if msg, err := lintDoc(doc); err != nil {
					lerr = err
				} else if msg != "" {
					lerr = fmt.Errorf("%s", msg)
				}
			}
			berr = lerr
		}
		if berr != nil {
			e.Refuse = safe(berr.Error())
			rep.Refused = append(rep.Refused, Issue{safe(c.key), e.Refuse})
		}
		notes = dedupe(notes)
		for _, n := range notes {
			k, _, _ := strings.Cut(n, ":")
			rep.Normalized[k]++
		}
		e.Notes = nil
		for _, n := range notes {
			e.Notes = append(e.Notes, safe(n))
		}
		if e.DuplicateOf != "" {
			dups[e.DuplicateOf] = append(dups[e.DuplicateOf], c.key)
		}
		entries[c] = e
	}
	for _, c := range co.cands {
		plan.Entries = append(plan.Entries, entries[c])
	}
	for _, first := range sortedKeys(dups) {
		g := append([]string{first}, dups[first]...)
		for i := range g {
			g[i] = safe(g[i])
		}
		rep.Duplicates = append(rep.Duplicates, g)
	}
	for i := range rep.Unmapped {
		rep.Unmapped[i].Path = safe(rep.Unmapped[i].Path)
	}
	return rep, plan, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
