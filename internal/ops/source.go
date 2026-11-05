package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/recur"
	"github.com/iodesystems/para/internal/store"
)

// A source is one place work comes in or goes out: a mailbox, a calendar, a
// chat, a tracker, a folder. Sources are recommended, never required. Para
// fetches nothing and cannot assume it reaches a source: the connector may
// exist only inside an agent's harness. A source says how an agent reaches
// it and what to scan; agents declare the sources they reach at session start
// (agent_sources, kept on this machine); a sweep (sweep_plan, sweep_record)
// is the agent reading them and writing what it found with the usual ops.

// getSource reads a source document.
func getSource(ctx context.Context, s store.Store, id string) (model.Document, *model.SourceDoc, error) {
	id = normID(id)
	if id == "" {
		return model.Document{}, nil, errors.New("source id is empty")
	}
	doc, err := s.Get(ctx, id)
	if err != nil {
		return doc, nil, err
	}
	m, ok := doc.Meta.(*model.SourceDoc)
	if !ok {
		return doc, nil, fmt.Errorf("%s is %s, not a source", id, doc.Meta.Header().Type)
	}
	return doc, m, nil
}

// loadSources returns every source document, sorted by id.
func loadSources(ctx context.Context, s store.Store) ([]model.Document, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeSource})
	if err != nil {
		return nil, err
	}
	out := make([]model.Document, 0, len(refs))
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if _, ok := doc.Meta.(*model.SourceDoc); ok {
			out = append(out, doc)
		}
	}
	slices.SortFunc(out, func(a, b model.Document) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// sourceResolver resolves via.source text to a source document: its id or a
// link to it, else the one source of that system.
type sourceResolver struct {
	byID     map[string]model.Document
	bySystem map[string][]string
}

func newSourceResolver(sources []model.Document) sourceResolver {
	r := sourceResolver{byID: map[string]model.Document{}, bySystem: map[string][]string{}}
	for _, d := range sources {
		r.byID[strings.ToLower(d.ID)] = d
		sys := string(d.Meta.(*model.SourceDoc).System)
		r.bySystem[sys] = append(r.bySystem[sys], d.ID)
	}
	return r
}

// resolve returns the source id that name names, or "".
func (r sourceResolver) resolve(name string) string {
	key := strings.ToLower(normID(name))
	if key == "" {
		return ""
	}
	if d, ok := r.byID[key]; ok {
		return d.ID
	}
	if ids := r.bySystem[key]; len(ids) == 1 {
		return ids[0]
	}
	return ""
}

// sourceDue returns the date the next sweep of m is due, "" when it has no
// cadence a sweep can follow. A source never swept is due from its created
// date.
func sourceDue(m *model.SourceDoc) string {
	rule, err := recur.Parse(m.Cadence)
	if m.Cadence == "" || err != nil {
		return ""
	}
	if m.LastScan == "" {
		return dayOf(string(m.Created))
	}
	t, err := time.Parse(time.RFC3339Nano, m.LastScan)
	if err != nil {
		return ""
	}
	return rule.After(t.Local()).Format(recur.Layout)
}

// sourceOverdue reports whether a sweep of m was due before today: a sweep
// is a day or more late. A source never swept is overdue one cadence after it
// was made.
func sourceOverdue(m *model.SourceDoc, today string) bool {
	if m.Status != model.SourceActive {
		return false
	}
	due := sourceDue(m)
	if m.LastScan == "" && due != "" {
		rule, _ := recur.Parse(m.Cadence)
		if t, err := time.Parse(recur.Layout, due); err == nil {
			due = rule.After(t).Format(recur.Layout)
		}
	}
	return due != "" && due < today
}

// SourceRow is one source in a list.
type SourceRow struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	System    model.SourceSystem `json:"system"`
	Status    model.SourceStatus `json:"status"`
	Via       model.SourceVia    `json:"via"`
	Server    string             `json:"server,omitempty"`
	RefURL    string             `json:"ref_url,omitempty"`
	Cadence   string             `json:"cadence,omitempty"`
	LastScan  string             `json:"last_scan,omitempty"`
	ReachedBy []string           `json:"reached_by"`
	// Declared lists the agents that declared the source on this machine
	// (agent_sources), with when; Fresh is within sources.reach_days.
	Declared []SourceDeclared `json:"declared"`
	// Due is the date the next sweep is due, empty with no cadence.
	Due     string `json:"due,omitempty"`
	Overdue bool   `json:"overdue"`
	// Request is the open sweep request of the source, if any.
	Request string `json:"request,omitempty"`
	// LastSweep is the line the last sweep_record left: when, by whom,
	// counts, and errors. LastFailed: that sweep did not finish.
	LastSweep  string `json:"last_sweep,omitempty"`
	LastFailed bool   `json:"last_failed"`
}

// SourceDeclared is one agent's declaration of a source.
type SourceDeclared struct {
	Actor string `json:"actor"`
	At    string `json:"at"`
	Fresh bool   `json:"fresh"`
}

// sourceView builds the view of one source. decl is every declaration on
// this machine; open maps a source id to its open sweep request.
func sourceView(doc model.Document, decl []Declaration, cut time.Time, open map[string]string, today string) SourceRow {
	m := doc.Meta.(*model.SourceDoc)
	v := SourceRow{ID: doc.ID, Title: doc.Title, System: m.System, Status: m.Status, Via: m.Via, Server: m.Server,
		Cadence: m.Cadence, LastScan: m.LastScan, ReachedBy: append([]string{}, m.ReachedBy...), Declared: []SourceDeclared{},
		Due: sourceDue(m), Overdue: sourceOverdue(m, today), Request: open[doc.ID]}
	_, _, v.LastSweep = splitSourceBody(doc.Body)
	v.LastFailed = failedSweep.MatchString(v.LastSweep)
	for _, d := range decl {
		if slices.Contains(d.Sources, doc.ID) {
			v.Declared = append(v.Declared, SourceDeclared{Actor: d.Actor, At: d.At, Fresh: d.Fresh(cut)})
		}
	}
	return v
}

// failedSweep matches the line of a sweep that did not finish (sweepLine).
var failedSweep = regexp.MustCompile(`^\S+ by \S+, failed`)

// openSweeps maps each source id to its open sweep request: proposed,
// queued, or claimed.
func openSweeps(ctx context.Context, s store.Store) (map[string]string, error) {
	reqs, err := loadRequests(ctx, s)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, doc := range reqs {
		r := doc.Meta.(*model.Request)
		if r.RequiresSource == "" {
			continue
		}
		switch r.Status {
		case model.RequestProposed, model.RequestQueued, model.RequestClaimed:
			out[r.RequiresSource.Target()] = doc.ID
		}
	}
	return out, nil
}

// SourceListInput is the input of source_list.
type SourceListInput struct {
	Status model.SourceStatus `json:"status,omitempty" query:"status"` // empty: all
}

// SourceListOutput is the output of source_list.
type SourceListOutput struct {
	Sources []SourceRow `json:"sources"`
	// ReachDays is sources.reach_days: how long a declaration lasts.
	ReachDays int `json:"reach_days"`
}

// SourceList lists the vault's sources with who reaches them and when each
// is due.
var SourceList = defineRead("source_list", "List the vault's sources (mailboxes, calendars, chats, trackers, folders) with status, cadence, last scan, who reaches them, and whether a sweep is overdue", func(ctx context.Context, d Deps, in SourceListInput) (SourceListOutput, error) {
	if in.Status != "" && !slices.Contains(model.SourceStatuses, in.Status) {
		return SourceListOutput{}, fmt.Errorf("status %q: want one of %v", in.Status, model.SourceStatuses)
	}
	docs, err := loadSources(ctx, d.Store)
	if err != nil {
		return SourceListOutput{}, err
	}
	days, err := reachDays(d.Store)
	if err != nil {
		return SourceListOutput{}, err
	}
	decl, err := readDeclarations(d.Store.Info().StateDir)
	if err != nil {
		return SourceListOutput{}, err
	}
	open, err := openSweeps(ctx, d.Store)
	if err != nil {
		return SourceListOutput{}, err
	}
	now := d.Store.Now()
	today := now.Local().Format(recur.Layout)
	out := SourceListOutput{Sources: []SourceRow{}, ReachDays: days}
	for _, doc := range docs {
		if in.Status != "" && doc.Meta.(*model.SourceDoc).Status != in.Status {
			continue
		}
		out.Sources = append(out.Sources, sourceView(doc, decl, reachCut(now, days), open, today))
	}
	return out, nil
})

// SourceShowInput is the input of source_show.
type SourceShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// SourceShowOutput is the output of source_show: the source, its steps, its
// body, and the io items and tracked documents that cite it.
type SourceShowOutput struct {
	Source  SourceRow         `json:"source"`
	Fetch   *model.SourceStep `json:"fetch,omitempty"`
	ScanIn  *model.SourceStep `json:"scan_in,omitempty"`
	ScanOut *model.SourceStep `json:"scan_out,omitempty"`
	State   *model.SourceStep `json:"state,omitempty"`
	Tools   []string          `json:"tools"`
	Body    string            `json:"body"`
	Cites   []SourceCite      `json:"cites"`
}

// SourceCite is one item whose via.source resolves to the source.
type SourceCite struct {
	ID    string     `json:"id"`
	Title string     `json:"title"`
	Type  model.Type `json:"type"`
	Ref   string     `json:"ref"`
}

// SourceShow shows one source.
var SourceShow = defineRead("source_show", "Show one source: how an agent reaches it, what to scan, who reaches it, and the io items and tracked documents that cite it", func(ctx context.Context, d Deps, in SourceShowInput) (SourceShowOutput, error) {
	doc, m, err := getSource(ctx, d.Store, in.ID)
	if err != nil {
		return SourceShowOutput{}, err
	}
	all, err := loadSources(ctx, d.Store)
	if err != nil {
		return SourceShowOutput{}, err
	}
	days, err := reachDays(d.Store)
	if err != nil {
		return SourceShowOutput{}, err
	}
	decl, err := readDeclarations(d.Store.Info().StateDir)
	if err != nil {
		return SourceShowOutput{}, err
	}
	open, err := openSweeps(ctx, d.Store)
	if err != nil {
		return SourceShowOutput{}, err
	}
	now := d.Store.Now()
	out := SourceShowOutput{Source: sourceView(doc, decl, reachCut(now, days), open, now.Local().Format(recur.Layout)),
		Fetch: m.Fetch, ScanIn: m.ScanIn, ScanOut: m.ScanOut, State: m.State, Tools: append([]string{}, m.Tools...), Body: doc.Body, Cites: []SourceCite{}}
	res := newSourceResolver(all)
	for _, t := range []model.Type{model.TypeIO, model.TypeTrackedDoc} {
		refs, err := d.Store.List(ctx, store.Filter{Type: t})
		if err != nil {
			return out, err
		}
		for _, r := range refs {
			c, err := d.Store.Get(ctx, r.ID)
			if err != nil {
				return out, err
			}
			if via := viaOf(c); via != nil && res.resolve(via.Source) == doc.ID {
				out.Cites = append(out.Cites, SourceCite{ID: c.ID, Title: c.Title, Type: t, Ref: via.Ref})
			}
		}
	}
	return out, nil
})

// viaOf returns the via of an io item or a tracked document.
func viaOf(doc model.Document) *model.IOVia {
	switch m := doc.Meta.(type) {
	case *model.IO:
		return m.Via
	case *model.TrackedDoc:
		return m.Via
	}
	return nil
}

// SourceAddInput is the input of source_add. Example starts from the
// example of a system (source_example); the other fields replace its values.
type SourceAddInput struct {
	ID        string             `json:"id,omitempty"` // default: the slug of the title
	Title     string             `json:"title,omitempty"`
	Example   model.SourceSystem `json:"example,omitempty"`
	System    model.SourceSystem `json:"system,omitempty"`
	Via       model.SourceVia    `json:"via,omitempty"`
	Server    string             `json:"server,omitempty"`
	RefURL    string             `json:"ref_url,omitempty"` // link template with {ref}: how a ref of this source opens in a browser
	Tools     []string           `json:"tools,omitempty"`
	Fetch     *model.SourceStep  `json:"fetch,omitempty"`
	ScanIn    *model.SourceStep  `json:"scan_in,omitempty"`
	ScanOut   *model.SourceStep  `json:"scan_out,omitempty"`
	State     *model.SourceStep  `json:"state,omitempty"`
	ReachedBy []string           `json:"reached_by,omitempty"` // agent identities
	Cadence   string             `json:"cadence,omitempty"`    // daily, weekly, monthly, quarterly, yearly, every <n> days
	Note      string             `json:"note,omitempty"`       // the body: what the source is to the owner
}

// SourceAddOutput is the output of source_add.
type SourceAddOutput struct {
	ID  string `json:"id"`
	Rev string `json:"rev"`
}

// SourceAdd makes a source document.
var SourceAdd = defineSafe("source_add", "Add a source: where work comes in or goes out (gmail, gdocs, gcal, slack, github, jira, confluence, folder, other), how an agent reaches it (via mcp with server and tools, cli with command templates, or none), and what to scan. Start from example <system> and change what differs. No secrets: the connector keeps its own", func(ctx context.Context, d Deps, in SourceAddInput) (SourceAddOutput, error) {
	doc := model.Document{Folder: model.FolderSources, Meta: &model.SourceDoc{Common: model.Common{Type: model.TypeSource}, Status: model.SourceActive}}
	if in.Example != "" {
		ex, err := exampleDoc(in.Example)
		if err != nil {
			return SourceAddOutput{}, err
		}
		doc.Title, doc.Body, doc.Meta = ex.Title, ex.Body, ex.Meta
		doc.Meta.Header().Created = ""
	}
	m := doc.Meta.(*model.SourceDoc)
	if t := strings.TrimSpace(in.Title); t != "" {
		title, err := oneLine("title", t)
		if err != nil {
			return SourceAddOutput{}, err
		}
		doc.Title = title
	}
	if doc.Title == "" {
		return SourceAddOutput{}, errors.New("title is empty: name the source, such as \"Work mailbox\"")
	}
	if strings.TrimSpace(in.Note) != "" {
		doc.Body = strings.TrimSpace(in.Note)
	}
	if in.System != "" {
		m.System = in.System
	}
	if in.Via != "" {
		m.Via = in.Via
		if in.Via != model.SourceViaMCP {
			m.Server, m.Tools = "", nil
		}
	}
	if in.Server != "" {
		m.Server = strings.TrimSpace(in.Server)
	}
	if in.Tools != nil {
		m.Tools = in.Tools
	}
	if u := strings.TrimSpace(in.RefURL); u != "" {
		if fillRefURL(u, "x") == "" {
			return SourceAddOutput{}, errors.New("ref_url: want an http(s) address with {ref} in it, such as https://mail.google.com/mail/u/0/#all/{ref}")
		}
		m.RefURL = u
	}
	for _, p := range []struct {
		in  *model.SourceStep
		out **model.SourceStep
	}{{in.Fetch, &m.Fetch}, {in.ScanIn, &m.ScanIn}, {in.ScanOut, &m.ScanOut}, {in.State, &m.State}} {
		if p.in != nil {
			*p.out = p.in
		}
	}
	if in.ReachedBy != nil {
		m.ReachedBy = in.ReachedBy
	}
	if in.Cadence != "" {
		m.Cadence = strings.TrimSpace(in.Cadence)
	}
	if !slices.Contains(model.SourceSystems, m.System) {
		return SourceAddOutput{}, fmt.Errorf("system %q: want one of %v", m.System, model.SourceSystems)
	}
	if m.Via == "" {
		m.Via = model.SourceViaNone
	}
	if !slices.Contains(model.SourceVias, m.Via) {
		return SourceAddOutput{}, fmt.Errorf("via %q: want one of %v", m.Via, model.SourceVias)
	}
	if m.Cadence != "" {
		if _, err := recur.Parse(m.Cadence); err != nil {
			return SourceAddOutput{}, fmt.Errorf("cadence: %w", err)
		}
	}
	doc.ID = normID(in.ID)
	if doc.ID == "" {
		doc.ID = slug(doc.Title)
	}
	got, err := d.Store.Create(ctx, doc)
	if err != nil {
		return SourceAddOutput{}, err
	}
	return SourceAddOutput{ID: got.ID, Rev: got.Rev}, nil
})

// SourceStatusInput is the input of source_status.
type SourceStatusInput struct {
	ID     string             `json:"id"`
	Status model.SourceStatus `json:"status"` // active or paused
}

// SourceStatusOutput is the output of source_status.
type SourceStatusOutput struct {
	ID     string             `json:"id"`
	Status model.SourceStatus `json:"status"`
	Rev    string             `json:"rev"`
}

// SourceStatus pauses or resumes a source. A paused source is not swept or
// scheduled.
var SourceStatus = defineSafe("source_status", "Pause or resume a source: a paused source is not swept or scheduled", func(ctx context.Context, d Deps, in SourceStatusInput) (SourceStatusOutput, error) {
	if !slices.Contains(model.SourceStatuses, in.Status) {
		return SourceStatusOutput{}, fmt.Errorf("status %q: want active or paused", in.Status)
	}
	doc, m, err := getSource(ctx, d.Store, in.ID)
	if err != nil {
		return SourceStatusOutput{}, err
	}
	if m.Status != in.Status {
		m.Status = in.Status
		if doc, err = d.Store.Update(ctx, doc); err != nil {
			return SourceStatusOutput{}, err
		}
	}
	return SourceStatusOutput{ID: doc.ID, Status: m.Status, Rev: doc.Rev}, nil
})

// SourceExampleInput is the input of source_example.
type SourceExampleInput struct {
	System model.SourceSystem `json:"system" query:"system" required:"true"`
}

// SourceExampleOutput is the output of source_example: the example document
// as markdown, to copy and adapt.
type SourceExampleOutput struct {
	System  model.SourceSystem `json:"system"`
	Text    string             `json:"text"`
	Systems []string           `json:"systems"` // every system with an example
}

// SourceExample prints the example source of a system.
var SourceExample = defineRead("source_example", "Print the example source document of a system (gmail, gdocs, gcal, slack, github, jira, confluence, folder) to copy and adapt; source_add with example makes one from it", func(_ context.Context, _ Deps, in SourceExampleInput) (SourceExampleOutput, error) {
	raw, err := exampleText(in.System)
	if err != nil {
		return SourceExampleOutput{}, err
	}
	return SourceExampleOutput{System: in.System, Text: raw, Systems: exampleSystems()}, nil
})

// exampleSystems lists the systems with an example.
func exampleSystems() []string {
	entries, _ := fs.ReadDir(docs.Sources, "sources")
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	return out
}

func exampleText(system model.SourceSystem) (string, error) {
	b, err := fs.ReadFile(docs.Sources, "sources/"+string(system)+".md")
	if err != nil {
		return "", fmt.Errorf("no example for system %q: want one of %v", system, exampleSystems())
	}
	return string(b), nil
}

// exampleDoc decodes the example of a system.
func exampleDoc(system model.SourceSystem) (model.Document, error) {
	raw, err := exampleText(system)
	if err != nil {
		return model.Document{}, err
	}
	rest, ok := strings.CutPrefix(raw, "---\n")
	fm, body, ok2 := strings.Cut(rest, "\n---\n")
	if !ok || !ok2 {
		return model.Document{}, fmt.Errorf("example %s: no frontmatter", system)
	}
	body = strings.TrimLeft(body, "\n")
	title := ""
	if h, after, found := strings.Cut(body, "\n"); strings.HasPrefix(h, "# ") {
		title, body = strings.TrimPrefix(h, "# "), strings.TrimLeft(after, "\n")
	} else if !found && strings.HasPrefix(h, "# ") {
		title, body = strings.TrimPrefix(h, "# "), ""
	}
	return model.Decode(string(system), model.FolderSources, []byte(fm+"\n"), title, strings.TrimSpace(body))
}
