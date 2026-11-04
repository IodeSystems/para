package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
)

// A tracked document is a remote document the owner follows: a wiki page, a
// ticket, a pull request, a comment. Para fetches nothing. An agent reads the
// document with its own tools and passes the text: doc_track stores it as
// version 1 (the original), and doc_version_add stores each later text that
// differs as the next version. Every version stays in the vault as a
// transcript named <id>-v<n>.source; events hold its hash, never its text.
// The owner marks versions seen (doc_seen); doc_diff shows what changed
// since. Comments people make on ranges of the document are kept with their
// quote and become Asks, questions, or commitments.
//
// This is the policy transcript and policy_refresh mechanism made general:
// the same normalized text and hash, a refresh that changes nothing when the
// hash is the same, and a flag on what the change may affect (here, comment
// anchors). A policy keeps one transcript and its summary; it is not moved
// onto this.

// versionID is the id of version n of a tracked document.
func versionID(doc string, n int) string { return fmt.Sprintf("%s-v%d.source", doc, n) }

// mediumOf is the medium of a comment made in a system.
func mediumOf(s model.DocSystem) model.Medium {
	switch s {
	case model.SystemConfluence:
		return model.MediumConfluence
	case model.SystemJira:
		return model.MediumJira
	case model.SystemGitHub:
		return model.MediumGithub
	}
	return model.MediumOther
}

// getTracked reads a tracked document.
func getTracked(ctx context.Context, d Deps, id string) (model.Document, *model.TrackedDoc, error) {
	id = normID(id)
	if id == "" {
		return model.Document{}, nil, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return doc, nil, err
	}
	m, ok := doc.Meta.(*model.TrackedDoc)
	if !ok {
		return doc, nil, fmt.Errorf("%s is %s, not a tracked document", id, doc.Meta.Header().Type)
	}
	return doc, m, nil
}

// docVersion is one stored version.
type docVersion struct {
	doc  model.Document
	meta *model.Transcript
	text string
}

// readVersion reads version n of a tracked document. Version 0 is the empty
// text: nothing seen.
func readVersion(ctx context.Context, d Deps, id string, n int) (docVersion, error) {
	if n == 0 {
		return docVersion{}, nil
	}
	doc, err := d.Store.Get(ctx, versionID(id, n))
	if err != nil {
		return docVersion{}, fmt.Errorf("version %d of %s: %w", n, id, err)
	}
	t, ok := doc.Meta.(*model.Transcript)
	if !ok {
		return docVersion{}, fmt.Errorf("%s is %s, not a transcript", doc.ID, doc.Meta.Header().Type)
	}
	return docVersion{doc: doc, meta: t, text: versionText(doc.Body)}, nil
}

// versionText is the text of a version body: what follows the Source line.
func versionText(body string) string {
	if strings.HasPrefix(body, "Source: ") {
		_, rest, _ := strings.Cut(body, "\n")
		return strings.TrimLeft(rest, "\n")
	}
	return body
}

// versionDoc builds version n of a tracked document.
func versionDoc(id, title string, n int, src model.Source, by, norm string) model.Document {
	return model.Document{
		ID:     versionID(id, n),
		Folder: model.FolderTracked,
		Title:  fmt.Sprintf("%s (version %d)", title, n),
		Body:   "Source: " + src.URL + "\n\n" + norm,
		Meta: &model.Transcript{Common: model.Common{Type: model.TypeTranscript}, Of: model.LinkTo(id), Source: src,
			Version: n, FetchedBy: by},
	}
}

func checkFidelity(f model.Fidelity) (model.Fidelity, error) {
	switch f {
	case "":
		return model.FidelityExact, nil
	case model.FidelityExact, model.FidelityPartial, model.FidelityOCR:
		return f, nil
	}
	return "", fmt.Errorf("fidelity %q: want exact, partial, or ocr (a tracked document keeps its text)", f)
}

func fetchedAt(d Deps, s string) (string, error) {
	t, err := ioTime(s)
	if err != nil {
		return "", fmt.Errorf("fetched_at: %w", err)
	}
	if t == "" {
		t = d.Store.Now().UTC().Format(time.RFC3339)
	}
	return t, nil
}

// DocTrackInput is the input of doc_track. Text is the document as the agent
// read it with its own tools: it becomes version 1, the original.
type DocTrackInput struct {
	URL       string          `json:"url"` // a public link a person can open
	Text      string          `json:"text"`
	Title     string          `json:"title,omitempty"` // empty: the first heading of the text, else host and path
	System    model.DocSystem `json:"system,omitempty"`
	Via       *model.IOVia    `json:"via,omitempty"` // private connector reference: {source, ref}
	Owner     string          `json:"owner,omitempty"`
	Watchers  []string        `json:"watchers,omitempty"` // person ids
	Area      string          `json:"area,omitempty"`
	Project   string          `json:"project,omitempty"`
	Route     string          `json:"route,omitempty"` // domain an Ask from a comment goes to
	Note      string          `json:"note,omitempty"`  // what the document is to the owner; the body
	Fidelity  model.Fidelity  `json:"fidelity,omitempty"`
	FetchedAt string          `json:"fetched_at,omitempty"` // RFC 3339; default now
	ID        string          `json:"id,omitempty"`         // empty means generated
}

// DocTrackOutput is the output of doc_track.
type DocTrackOutput struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Hash    string `json:"hash"`
	Rev     string `json:"rev"`
}

// DocTrack starts tracking a remote document with the text the agent read.
var DocTrack = defineSafe("doc_track", "Start tracking a remote document (a wiki page, ticket, pull request, or comment): url, the text you read with your own tools (version 1, the original), system, and optional via, owner, watchers, area, project. Para fetches nothing", func(ctx context.Context, d Deps, in DocTrackInput) (DocTrackOutput, error) {
	u, err := checkURL(in.URL)
	if err != nil {
		return DocTrackOutput{}, err
	}
	if in.ID != "" && (!paramd.ValidStem(in.ID) || strings.HasSuffix(in.ID, ".source")) {
		return DocTrackOutput{}, fmt.Errorf("id %q is not a valid document id", in.ID)
	}
	norm, err := Normalize(in.Text)
	if err != nil {
		return DocTrackOutput{}, err
	}
	fid, err := checkFidelity(in.Fidelity)
	if err != nil {
		return DocTrackOutput{}, err
	}
	at, err := fetchedAt(d, in.FetchedAt)
	if err != nil {
		return DocTrackOutput{}, err
	}
	m := &model.TrackedDoc{Common: model.Common{Type: model.TypeTrackedDoc}, URL: u, System: in.System, Status: model.DocWatching,
		Route: ioLine(in.Route), Latest: 1, Checked: at, Via: in.Via}
	if m.System == "" {
		m.System = model.SystemOther
	}
	if !slices.Contains(model.DocSystems, m.System) {
		return DocTrackOutput{}, fmt.Errorf("system %q: want one of %v", m.System, model.DocSystems)
	}
	if v := m.Via; v != nil {
		v.Source, v.Ref = ioLine(v.Source), strings.TrimSpace(v.Ref)
		if v.Source == "" || v.Ref == "" {
			return DocTrackOutput{}, errors.New("via: give source and ref, or leave via out")
		}
	}
	if m.Owner, err = linkTo(ctx, d, "owner", in.Owner, model.TypePerson, model.TypeGroup, model.TypeTeam); err != nil {
		return DocTrackOutput{}, err
	}
	for _, w := range in.Watchers {
		l, err := linkTo(ctx, d, "watchers", w, model.TypePerson, model.TypeGroup, model.TypeTeam)
		if err != nil {
			return DocTrackOutput{}, err
		}
		if l != "" && !slices.Contains(m.Watchers, l) {
			m.Watchers = append(m.Watchers, l)
		}
	}
	if m.Area, err = linkTo(ctx, d, "area", in.Area, model.TypeArea); err != nil {
		return DocTrackOutput{}, err
	}
	if m.Project, err = linkTo(ctx, d, "project", in.Project, model.TypeProject); err != nil {
		return DocTrackOutput{}, err
	}
	title := ioLine(in.Title)
	if title == "" {
		title = defaultTitle(norm, u)
	}
	src := model.Source{URL: u, Retrieved: at, Hash: hashText(norm), Fidelity: fid, Untrusted: ptr(true)}
	by := d.Store.Info().Actor
	// Lint the version before anything is written, so a bad text leaves no
	// tracked document without its original.
	if err := precheck(versionDoc("tracked-pending", title, 1, src, by, norm)); err != nil {
		return DocTrackOutput{}, err
	}
	doc, err := d.Store.Create(ctx, model.Document{ID: in.ID, Folder: model.FolderTracked, Title: title,
		Body: firstOf(cleanText(in.Note), "Tracked from "+u+"."), Meta: m})
	if err != nil {
		return DocTrackOutput{}, err
	}
	if _, err := d.Store.Create(ctx, versionDoc(doc.ID, title, 1, src, by, norm)); err != nil {
		return DocTrackOutput{}, fmt.Errorf("%s created, version 1 not written: %w", doc.ID, err)
	}
	if err := d.Store.Record(ctx, "doc_version_add", doc.ID, "0", "1", src.Hash); err != nil {
		return DocTrackOutput{}, err
	}
	return DocTrackOutput{ID: doc.ID, Version: 1, Hash: src.Hash, Rev: doc.Rev}, nil
})

// Version add results.
const (
	VersionUnchanged = "unchanged"
	VersionAdded     = "added"
)

// DocVersionAddInput is the input of doc_version_add. Text is the document as
// the agent read it again. Proposal and item name the approved doc edit whose
// result this text is, when there is one.
type DocVersionAddInput struct {
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	Fidelity  model.Fidelity `json:"fidelity,omitempty"`
	FetchedAt string         `json:"fetched_at,omitempty"` // RFC 3339; default now
	Proposal  string         `json:"proposal,omitempty"`
	Item      string         `json:"item,omitempty"`
}

// DocVersionAddOutput is the output of doc_version_add.
type DocVersionAddOutput struct {
	ID       string `json:"id"`
	Result   string `json:"result"`   // unchanged or added
	Version  int    `json:"version"`  // the latest version after the call
	Previous int    `json:"previous"` // the latest version before the call
	Hash     string `json:"hash"`
	Unseen   int    `json:"unseen"` // versions after last_seen
	// Added and Removed count changed lines against the previous version.
	Added   int `json:"added"`
	Removed int `json:"removed"`
	// AnchorsLost lists open comments whose quote is not in the new version.
	AnchorsLost []string `json:"anchors_lost"`
	Rev         string   `json:"rev"`
}

// DocVersionAdd stores a newly read text as the next version, unless it is the
// same as the latest.
var DocVersionAdd = defineSafe("doc_version_add", "Pass the text of a tracked document as you read it again with your own tools. Same as the latest version (by hash of the normalized text): nothing is stored but the check time. Else it becomes the next version, and comment quotes are looked for in it", func(ctx context.Context, d Deps, in DocVersionAddInput) (DocVersionAddOutput, error) {
	return addVersion(ctx, d, in)
})

func addVersion(ctx context.Context, d Deps, in DocVersionAddInput) (DocVersionAddOutput, error) {
	doc, m, err := getTracked(ctx, d, in.ID)
	if err != nil {
		return DocVersionAddOutput{}, err
	}
	norm, err := Normalize(in.Text)
	if err != nil {
		return DocVersionAddOutput{}, err
	}
	fid, err := checkFidelity(in.Fidelity)
	if err != nil {
		return DocVersionAddOutput{}, err
	}
	at, err := fetchedAt(d, in.FetchedAt)
	if err != nil {
		return DocVersionAddOutput{}, err
	}
	prev, err := readVersion(ctx, d, doc.ID, m.Latest)
	if err != nil {
		return DocVersionAddOutput{}, err
	}
	hash := hashText(norm)
	out := DocVersionAddOutput{ID: doc.ID, Result: VersionUnchanged, Version: m.Latest, Previous: m.Latest, Hash: hash, AnchorsLost: []string{}}
	m.Checked = at
	if prev.meta != nil && prev.meta.Source.Hash == hash {
		if doc, err = d.Store.Update(ctx, doc); err != nil {
			return out, err
		}
		out.Unseen, out.Rev = max(0, m.Latest-m.LastSeen), doc.Rev
		return out, nil
	}
	n := m.Latest + 1
	src := model.Source{URL: m.URL, Retrieved: at, Hash: hash, Fidelity: fid, Untrusted: ptr(true)}
	v := versionDoc(doc.ID, doc.Title, n, src, d.Store.Info().Actor, norm)
	if in.Proposal != "" {
		t := v.Meta.(*model.Transcript)
		t.Proposal, t.Item = model.LinkTo(normID(in.Proposal)), ioLine(in.Item)
	}
	if _, err := d.Store.Create(ctx, v); err != nil {
		return out, err
	}
	for _, p := range diffUnits(splitUnits(prev.text, UnitLine), splitUnits(norm, UnitLine)) {
		switch p.Op {
		case DiffAdd:
			out.Added++
		case DiffDel:
			out.Removed++
		}
	}
	m.Latest = n
	out.AnchorsLost = relocate(m, n, norm)
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return out, fmt.Errorf("version %d of %s written, latest not set: %w", n, doc.ID, err)
	}
	if err := d.Store.Record(ctx, "doc_version_add", doc.ID, fmt.Sprint(n-1), fmt.Sprint(n), hash); err != nil {
		return out, err
	}
	out.Result, out.Version, out.Unseen, out.Rev = VersionAdded, n, max(0, n-m.LastSeen), doc.Rev
	return out, nil
}

// relocate looks for the quote of each open comment in version n and returns
// the ids of those not found there. A comment found again is no longer lost.
func relocate(m *model.TrackedDoc, n int, text string) []string {
	lost := []string{}
	for i := range m.Comments {
		c := &m.Comments[i]
		if !c.Open() || strings.TrimSpace(c.Quote) == "" {
			continue
		}
		hint := 0
		switch {
		case c.Anchor != nil && !c.Anchor.Lost:
			hint = c.Anchor.Start
		case c.Start != nil:
			hint = *c.Start
		}
		s, e, ok := locateQuote(text, c.Quote, hint)
		if !ok {
			c.Anchor = &model.DocAnchor{Version: n, Lost: true}
			lost = append(lost, c.ID)
			continue
		}
		c.Anchor = &model.DocAnchor{Version: n, Start: s, End: e}
	}
	return lost
}

// TrackedView is a tracked document in a list.
type TrackedView struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	URL      string          `json:"url"`
	System   model.DocSystem `json:"system"`
	Status   model.DocStatus `json:"status"`
	Owner    string          `json:"owner,omitempty"` // id
	Area     string          `json:"area,omitempty"`
	Project  string          `json:"project,omitempty"`
	Latest   int             `json:"latest"`
	LastSeen int             `json:"last_seen"`
	Unseen   int             `json:"unseen"` // versions after last_seen
	// OpenComments counts comments with no resolution; LostAnchors those of
	// them whose quote is not in the latest version.
	OpenComments int        `json:"open_comments"`
	LostAnchors  int        `json:"lost_anchors"`
	Checked      string     `json:"checked,omitempty"`
	SeenAt       string     `json:"seen_at,omitempty"`
	Created      model.Date `json:"created"`
}

func trackedView(doc model.Document, m *model.TrackedDoc) TrackedView {
	v := TrackedView{ID: doc.ID, Title: doc.Title, URL: m.URL, System: m.System, Status: m.Status, Owner: m.Owner.Target(),
		Area: m.Area.Target(), Project: m.Project.Target(), Latest: m.Latest, LastSeen: m.LastSeen, Unseen: max(0, m.Latest-m.LastSeen),
		Checked: m.Checked, SeenAt: m.SeenAt, Created: m.Created}
	for _, c := range m.Comments {
		if c.Open() {
			v.OpenComments++
			if c.Anchor != nil && c.Anchor.Lost {
				v.LostAnchors++
			}
		}
	}
	return v
}

// DocTrackedInput is the input of doc_tracked. With no status, archived
// documents are left out.
type DocTrackedInput struct {
	Status model.DocStatus `json:"status,omitempty" query:"status"`
	Unseen bool            `json:"unseen,omitempty" query:"unseen"` // only documents with versions not seen
}

// DocTrackedOutput is the output of doc_tracked.
type DocTrackedOutput struct {
	Docs []TrackedView `json:"docs"`
	// Unseen counts documents with versions not seen; OpenComments counts open
	// comments, both over every document that is not archived.
	Unseen       int `json:"unseen"`
	OpenComments int `json:"open_comments"`
}

// DocTracked lists tracked documents, unseen changes first.
var DocTracked = defineRead("doc_tracked", "List tracked documents with versions not seen and open comments, unseen first; archived ones only with status archived", func(ctx context.Context, d Deps, in DocTrackedInput) (DocTrackedOutput, error) {
	if in.Status != "" && !slices.Contains(model.DocStatuses, in.Status) {
		return DocTrackedOutput{}, fmt.Errorf("status %q: want one of %v", in.Status, model.DocStatuses)
	}
	all, err := loadTracked(ctx, d)
	if err != nil {
		return DocTrackedOutput{}, err
	}
	out := DocTrackedOutput{Docs: []TrackedView{}}
	for _, v := range all {
		if v.Status != model.DocArchived {
			if v.Unseen > 0 {
				out.Unseen++
			}
			out.OpenComments += v.OpenComments
		}
		switch {
		case in.Status == "" && v.Status == model.DocArchived,
			in.Status != "" && v.Status != in.Status,
			in.Unseen && v.Unseen == 0:
			continue
		}
		out.Docs = append(out.Docs, v)
	}
	slices.SortStableFunc(out.Docs, func(a, b TrackedView) int {
		if (a.Unseen > 0) != (b.Unseen > 0) {
			if a.Unseen > 0 {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Title, b.Title)
	})
	return out, nil
})

func loadTracked(ctx context.Context, d Deps) ([]TrackedView, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeTrackedDoc})
	if err != nil {
		return nil, err
	}
	out := make([]TrackedView, 0, len(refs))
	for _, r := range refs {
		doc, m, err := getTracked(ctx, d, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, trackedView(doc, m))
	}
	return out, nil
}

// VersionView is one version of a tracked document.
type VersionView struct {
	Version   int            `json:"version"`
	ID        string         `json:"id"`
	FetchedAt string         `json:"fetched_at"`
	FetchedBy string         `json:"fetched_by,omitempty"`
	Hash      string         `json:"hash"`
	Fidelity  model.Fidelity `json:"fidelity"`
	Proposal  string         `json:"proposal,omitempty"` // the dossier of the doc edit that made it
	Item      string         `json:"item,omitempty"`
	Seen      bool           `json:"seen"`
}

// DocTrackedShowInput is the input of doc_tracked_show. Version picks the
// text to return; 0 is the latest.
type DocTrackedShowInput struct {
	ID      string `json:"id" query:"id" required:"true"`
	Version int    `json:"version,omitempty" query:"version"`
}

// DocTrackedShowOutput is the output of doc_tracked_show.
type DocTrackedShowOutput struct {
	Doc      TrackedView        `json:"doc"`
	Note     string             `json:"note"` // the body: what the document is to the owner
	Via      *model.IOVia       `json:"via,omitempty"`
	Watchers []string           `json:"watchers"` // ids
	Route    string             `json:"route,omitempty"`
	Versions []VersionView      `json:"versions"`
	Version  int                `json:"version"` // the version Text is
	Text     string             `json:"text"`
	Comments []model.DocComment `json:"comments"`
	Rev      string             `json:"rev"`
}

// DocTrackedShow shows a tracked document: its versions, one version's text,
// and its comments.
var DocTrackedShow = defineRead("doc_tracked_show", "Show a tracked document: versions (with who fetched each and the doc edit that made it), the text of one version (default latest), and comments with their anchors", func(ctx context.Context, d Deps, in DocTrackedShowInput) (DocTrackedShowOutput, error) {
	doc, m, err := getTracked(ctx, d, in.ID)
	if err != nil {
		return DocTrackedShowOutput{}, err
	}
	want := in.Version
	if want == 0 {
		want = m.Latest
	}
	if want < 1 || want > m.Latest {
		return DocTrackedShowOutput{}, fmt.Errorf("version %d: %s has versions 1 to %d", want, doc.ID, m.Latest)
	}
	out := DocTrackedShowOutput{Doc: trackedView(doc, m), Note: doc.Body, Via: m.Via, Watchers: []string{}, Route: m.Route,
		Versions: []VersionView{}, Version: want, Comments: append([]model.DocComment{}, m.Comments...), Rev: doc.Rev}
	for _, w := range m.Watchers {
		out.Watchers = append(out.Watchers, w.Target())
	}
	for n := 1; n <= m.Latest; n++ {
		v, err := readVersion(ctx, d, doc.ID, n)
		if err != nil {
			return out, err
		}
		out.Versions = append(out.Versions, VersionView{Version: n, ID: v.doc.ID, FetchedAt: v.meta.Source.Retrieved, FetchedBy: v.meta.FetchedBy,
			Hash: v.meta.Source.Hash, Fidelity: v.meta.Source.Fidelity, Proposal: v.meta.Proposal.Target(), Item: v.meta.Item, Seen: n <= m.LastSeen})
		if n == want {
			out.Text = v.text
		}
	}
	return out, nil
})

// DocTrackedStatusInput is the input of doc_tracked_status.
type DocTrackedStatusInput struct {
	ID     string          `json:"id"`
	Status model.DocStatus `json:"status"` // watching, paused, or archived
}

// DocTrackedStatusOutput is the output of doc_tracked_status.
type DocTrackedStatusOutput struct {
	ID     string          `json:"id"`
	Status model.DocStatus `json:"status"`
	Rev    string          `json:"rev"`
}

// DocTrackedStatus watches, pauses, or archives a tracked document. Its
// versions stay.
var DocTrackedStatus = define("doc_tracked_status", "Set a tracked document watching, paused (not checked), or archived (history); its versions and comments stay", func(ctx context.Context, d Deps, in DocTrackedStatusInput) (DocTrackedStatusOutput, error) {
	if !slices.Contains(model.DocStatuses, in.Status) {
		return DocTrackedStatusOutput{}, fmt.Errorf("status %q: want one of %v", in.Status, model.DocStatuses)
	}
	doc, m, err := getTracked(ctx, d, in.ID)
	if err != nil {
		return DocTrackedStatusOutput{}, err
	}
	m.Status = in.Status
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return DocTrackedStatusOutput{}, err
	}
	return DocTrackedStatusOutput{ID: doc.ID, Status: m.Status, Rev: doc.Rev}, nil
})

// DocSeenInput is the input of doc_seen. Version 0 is the latest.
type DocSeenInput struct {
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"`
}

// DocSeenOutput is the output of doc_seen.
type DocSeenOutput struct {
	ID       string `json:"id"`
	LastSeen int    `json:"last_seen"`
	Unseen   int    `json:"unseen"`
	Rev      string `json:"rev"`
}

// DocSeen marks a tracked document reviewed up to a version. Only a person
// can: it says the owner read the change.
var DocSeen = definePerson("doc_seen", "Mark a tracked document reviewed up to a version (default the latest). People only", func(ctx context.Context, d Deps, in DocSeenInput) (DocSeenOutput, error) {
	return markSeen(ctx, d, in)
})

// markSeen is doc_seen. An approved upstream item runs it as the owner's
// decision, from any surface.
func markSeen(ctx context.Context, d Deps, in DocSeenInput) (DocSeenOutput, error) {
	doc, m, err := getTracked(ctx, d, in.ID)
	if err != nil {
		return DocSeenOutput{}, err
	}
	n := in.Version
	if n == 0 {
		n = m.Latest
	}
	if n < 1 || n > m.Latest {
		return DocSeenOutput{}, fmt.Errorf("version %d: %s has versions 1 to %d", n, doc.ID, m.Latest)
	}
	from := m.LastSeen
	if n > m.LastSeen {
		m.LastSeen, m.SeenAt = n, d.Store.Now().UTC().Format(time.RFC3339)
		if doc, err = d.Store.Update(ctx, doc); err != nil {
			return DocSeenOutput{}, err
		}
		if err := d.Store.Record(ctx, "doc_seen", doc.ID, fmt.Sprint(from), fmt.Sprint(n), ""); err != nil {
			return DocSeenOutput{}, err
		}
	}
	return DocSeenOutput{ID: doc.ID, LastSeen: m.LastSeen, Unseen: max(0, m.Latest-m.LastSeen), Rev: doc.Rev}, nil
}

// DocDiffInput is the input of doc_diff. With no from and no to it compares
// the last version seen with the latest (all of it when none is seen). From
// 0 is that default; whole compares with the empty text instead.
type DocDiffInput struct {
	ID      string `json:"id" query:"id" required:"true"`
	From    int    `json:"from,omitempty" query:"from"`
	To      int    `json:"to,omitempty" query:"to"`
	Whole   bool   `json:"whole,omitempty" query:"whole"`
	Unit    string `json:"unit,omitempty" query:"unit"`       // line (default) or paragraph
	Context int    `json:"context,omitempty" query:"context"` // unchanged units around a change; 0 is 2
}

// DocDiffOutput is the output of doc_diff.
type DocDiffOutput struct {
	ID      string     `json:"id"`
	From    int        `json:"from"`
	To      int        `json:"to"`
	Unit    string     `json:"unit"`
	Same    bool       `json:"same"`
	Added   int        `json:"added"`
	Removed int        `json:"removed"`
	Parts   []DiffPart `json:"parts"`
	Text    string     `json:"text"` // the parts as a unified text
}

// DocDiff compares two versions of a tracked document.
var DocDiff = defineRead("doc_diff", "Show what changed in a tracked document between two versions (default: the last version seen and the latest), by line or paragraph, with a few unchanged units around each change", func(ctx context.Context, d Deps, in DocDiffInput) (DocDiffOutput, error) {
	return docDiff(ctx, d, in)
})

func docDiff(ctx context.Context, d Deps, in DocDiffInput) (DocDiffOutput, error) {
	doc, m, err := getTracked(ctx, d, in.ID)
	if err != nil {
		return DocDiffOutput{}, err
	}
	unit := in.Unit
	if unit == "" {
		unit = UnitLine
	}
	if !slices.Contains(DiffUnits, unit) {
		return DocDiffOutput{}, fmt.Errorf("unit %q: want line or paragraph", unit)
	}
	to := in.To
	if to == 0 {
		to = m.Latest
	}
	from := in.From
	switch {
	case in.Whole && in.From != 0:
		return DocDiffOutput{}, errors.New("whole compares with the empty text: leave out from")
	case in.Whole:
	case from == 0 && m.LastSeen >= to:
		from = to - 1
	case from == 0:
		from = m.LastSeen
	}
	if to < 1 || to > m.Latest || from < 0 || from > to {
		return DocDiffOutput{}, fmt.Errorf("from %d to %d: %s has versions 1 to %d, and from comes first", from, to, doc.ID, m.Latest)
	}
	n := in.Context
	if n <= 0 {
		n = 2
	}
	a, err := readVersion(ctx, d, doc.ID, from)
	if err != nil {
		return DocDiffOutput{}, err
	}
	b, err := readVersion(ctx, d, doc.ID, to)
	if err != nil {
		return DocDiffOutput{}, err
	}
	all := diffUnits(splitUnits(a.text, unit), splitUnits(b.text, unit))
	out := DocDiffOutput{ID: doc.ID, From: from, To: to, Unit: unit, Parts: compactDiff(all, n)}
	for _, p := range all {
		switch p.Op {
		case DiffAdd:
			out.Added++
		case DiffDel:
			out.Removed++
		}
	}
	out.Same = out.Added == 0 && out.Removed == 0
	out.Text = renderDiff(out.Parts)
	return out, nil
}
