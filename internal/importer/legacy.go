package importer

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

// lgIndex finds units by name for the legacy adapter: projects, areas,
// people, policies.
type lgIndex struct {
	projects, areas, people, policies, byImportedID map[string]*cand
	items                                           map[string]*cand // item id -> unit
}

func newLgIndex() *lgIndex {
	return &lgIndex{projects: map[string]*cand{}, areas: map[string]*cand{}, people: map[string]*cand{},
		policies: map[string]*cand{}, byImportedID: map[string]*cand{}, items: map[string]*cand{}}
}

func (ix *lgIndex) reg(m map[string]*cand, c *cand, names ...string) {
	for _, n := range names {
		if s := slug(n); s != "" {
			if _, dup := m[s]; !dup {
				m[s] = c
			}
		}
	}
}

// legacyKind says what a legacy markdown file is: its type, folder, tags and
// hints, from where it sits and from its frontmatter type.
type legacyKind struct {
	typ     model.Type
	folder  model.Folder
	tags    []string
	hints   map[string]string
	special string // incident, meeting, clarification
}

func parts(rel string) []string { return strings.Split(rel, "/") }

func legacyClassify(rel string, md *mdFile) (legacyKind, bool, string) {
	ps := parts(rel)
	k := legacyKind{typ: model.TypeNote, folder: model.FolderResources, hints: map[string]string{}}
	top := ps[0]
	if len(ps) >= 2 && ps[0] == "Journal" && ps[1] == "Rollup" {
		top = "Journal/Rollup"
	}
	archive := false
	switch top {
	case "00-Inbox":
		if len(ps) > 2 && ps[1] == "Outbox" {
			k.tags = []string{"draft"}
			if md.scalar("status") != "draft-not-sent" {
				k.tags = []string{"outbox"}
			}
			return k, true, ""
		}
		k.typ, k.folder = model.TypeInbox, model.FolderInbox
		return k, true, ""
	case "01-Projects":
		k.folder = model.FolderProjects
	case "02-Areas":
		k.folder = model.FolderAreas
	case "03-Resources":
	case "04-Archive":
		archive = true
		k.folder = model.FolderArchive
	case "Journal/Rollup":
		k.typ, k.folder = model.TypeJournal, model.FolderJournal
	default:
		return k, false, "outside the legacy folders (00-Inbox 01-Projects 02-Areas 03-Resources 04-Archive Journal/Rollup)"
	}
	// Location default.
	stem := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	switch top {
	case "01-Projects":
		if len(ps) == 3 && strings.EqualFold(ps[1], stem) {
			k.typ = model.TypeProject
		} else {
			k.folder = model.FolderResources
		}
	case "02-Areas":
		if len(ps) == 2 || (len(ps) == 3 && strings.EqualFold(ps[1], stem)) {
			k.typ = model.TypeArea
		} else {
			k.folder = model.FolderResources
		}
	}
	// The frontmatter type refines it.
	ft := strings.ToLower(md.scalar("type"))
	switch ft {
	case "":
	case "project":
		k.typ, k.folder = model.TypeProject, model.FolderProjects
	case "area":
		k.typ, k.folder = model.TypeArea, model.FolderAreas
	case "person":
		k.typ, k.folder = model.TypePerson, model.FolderOrg
	case "daily":
		k.typ, k.folder, k.hints["scope"] = model.TypeJournal, model.FolderJournal, "day"
	case "weekly":
		k.typ, k.folder, k.hints["scope"] = model.TypeJournal, model.FolderJournal, "week"
	case "journal":
		k.typ, k.folder = model.TypeJournal, model.FolderJournal
	case "incident":
		k.special, k.folder = "incident", model.FolderIncidents
	case "meeting":
		k.special, k.folder = "meeting", model.FolderMeetings
	case "clarification":
		k.special, k.folder = "clarification", model.FolderPolicies
	case "change-case", "policy", "evidence":
		k.typ, k.folder = model.TypeNote, model.FolderResources
		k.tags = append(k.tags, ft)
	default:
		k.typ, k.folder = model.TypeNote, model.FolderResources
		k.tags = append(k.tags, slug(ft))
	}
	if archive {
		k.folder = model.FolderArchive
		if k.typ == model.TypeProject {
			k.hints["status"] = "done"
		}
	}
	return k, true, ""
}

// loadLegacy loads the owner's legacy vault: files by folder and events.jsonl.
func loadLegacy(dir string) (*corpus, error) {
	w, err := walkDir(dir, func(rel string) bool { return rel == "_Templates" || strings.HasPrefix(rel, "_Templates/") })
	if err != nil {
		return nil, err
	}
	co := &corpus{adapter: AdapterLegacy, srcDirs: map[string]int{}, keys: map[string]int{}, unmapped: w.unmapped}
	ix := newLgIndex()
	var files []*cand
	var eventsFile *walkFile
	for i := range w.files {
		f := w.files[i]
		co.files++
		switch {
		case f.rel == "events.jsonl":
			eventsFile = &w.files[i]
			continue
		case !isMarkdownName(f.rel):
			co.unmapped = append(co.unmapped, Issue{f.rel, "not markdown; not copied"})
			continue
		}
		raw, err := readSource(f)
		if err != nil {
			co.unmapped = append(co.unmapped, Issue{f.rel, "not read: " + err.Error()})
			continue
		}
		stem := strings.TrimSuffix(path.Base(f.rel), path.Ext(f.rel))
		md := parseMD(raw, stem)
		k, ok, why := legacyClassify(f.rel, md)
		if !ok {
			co.unmapped = append(co.unmapped, Issue{f.rel, why})
			continue
		}
		c := fileCand(f, raw, k.typ, k.folder)
		c.tags, c.hints, c.mtimeDate = mergeTags(k.tags, nil), k.hints, f.mtime.UTC().Format("2006-01-02")
		if id := md.scalar("id"); id != "" {
			c.names = append(c.names, strings.ToLower(id))
			ix.byImportedID[strings.ToLower(id)] = c
		}
		if k.special != "" {
			c.hints["special"] = k.special
		}
		files = append(files, c)
	}
	// Names for lookups.
	for _, c := range files {
		stem := strings.TrimSuffix(path.Base(c.srcRel), path.Ext(c.srcRel))
		names := []string{stem, c.title}
		if c.md != nil {
			names = append(names, c.md.list("aliases")...)
		}
		switch c.typ {
		case model.TypeProject:
			// 01-Projects/<name>/<name>.md is the project <name>.
			ix.reg(ix.projects, c, names...)
		case model.TypeArea:
			ix.reg(ix.areas, c, names...)
		case model.TypePerson:
			ix.reg(ix.people, c, names...)
		}
		if strings.EqualFold(c.md.scalar("type"), "policy") {
			ix.reg(ix.policies, c, names...)
		}
	}
	// Project and area links, and the specials that need targets.
	var roots []*cand
	for _, c := range files {
		ps := parts(c.srcRel)
		switch {
		case ps[0] == "01-Projects" && c.typ == model.TypeNote && len(ps) >= 3:
			c.project = ix.projects[slug(ps[1])]
		case ps[0] == "02-Areas" && c.typ == model.TypeNote && len(ps) >= 3:
			c.area = ix.areas[slug(ps[1])]
		}
		if c.typ == model.TypeProject {
			if a := c.md.scalar("area"); a != "" {
				c.area = ix.areas[slug(strings.Trim(a, "[]"))]
			}
			if c.area == nil {
				roots = append(roots, areaQuestion(c))
			}
		}
		finalizeSpecial(c, ix)
	}
	for _, c := range files {
		co.add(c)
	}
	for _, q := range roots {
		co.add(q)
	}
	if eventsFile != nil {
		fr, err := foldEvents(eventsFile.abs)
		if err != nil {
			co.unmapped = append(co.unmapped, Issue{"events.jsonl", "not read: " + err.Error()})
		} else {
			co.fold = fr.report
			for _, c := range eventCands(fr, ix, *eventsFile) {
				co.add(c)
			}
		}
	} else {
		co.unmapped = append(co.unmapped, Issue{"events.jsonl", "not found; no task state imported"})
	}
	sort.Slice(co.cands, func(i, j int) bool { return co.cands[i].key < co.cands[j].key })
	return co, nil
}

// areaQuestion is the root question for a project with no known area: the
// importer does not guess a place for it.
func areaQuestion(p *cand) *cand {
	key := p.key + "#area"
	c := &cand{
		key: key, srcRel: p.srcRel, frag: "area", hash: sha([]byte(key)),
		title: fmt.Sprintf("Which area does the project %q belong to?", p.title),
		typ:   model.TypeQuestion, folder: model.FolderQuestions, fixedType: true,
		idBase: "question-area-" + slug(p.title), names: nil, project: p,
	}
	if c.idBase == "question-area-" {
		c.idBase = "question-area-" + strings.TrimPrefix(c.hash, "sha256:")[:8]
	}
	c.build = func(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
		q := &model.Question{Common: model.Common{Type: model.TypeQuestion, Created: model.Date(b.now[:10]), Tags: e.Tags},
			Status: model.QuestionOpen, Scope: model.ScopeRoot}
		body := "The project imported from the legacy vault has no area in its source.\n\nProject: " + linkText(b, p)
		doc := model.Document{ID: b.idOf(c), Folder: model.Folder(e.Folder), Title: c.title, Body: body, Meta: q}
		return doc, nil, doc.SetExtra("source", b.provenance(c))
	}
	return c
}

func linkText(b *bctx, c *cand) string {
	if id := b.idOf(c); id != "" {
		return "[[" + id + "]]"
	}
	return c.title
}

var (
	sevRe = regexp.MustCompile(`^sev[123]$`)
)

// finalizeSpecial decides, from the file's frontmatter, whether an incident,
// meeting, or clarification can be a real document of that type. If a required
// field is missing, it becomes a note tagged with its source type, and the
// scan says so: nothing is invented to fill a field.
func finalizeSpecial(c *cand, ix *lgIndex) {
	sp := c.hints["special"]
	if sp == "" {
		return
	}
	md := c.md
	fallback := func(why string) {
		c.typ, c.folder = model.TypeNote, model.FolderResources
		if c.hints["archive"] != "" {
			c.folder = model.FolderArchive
		}
		c.tags = mergeTags(c.tags, []string{sp})
		c.notes = append(c.notes, fmt.Sprintf("type: %s imported as a note tagged %s: %s", sp, sp, why))
		delete(c.hints, "special")
	}
	if strings.HasPrefix(c.srcRel, "04-Archive/") {
		c.hints["archive"] = "1"
	}
	switch sp {
	case "incident":
		st := map[string]string{"open": "open", "mitigated": "mitigated", "resolved": "resolved", "closed": "resolved", "done": "resolved"}[strings.ToLower(md.scalar("status"))]
		_, tsErr := time.Parse(time.RFC3339Nano, md.scalar("started"))
		switch {
		case !sevRe.MatchString(md.scalar("severity")):
			fallback("no severity sev1, sev2, or sev3")
		case tsErr != nil:
			fallback("no started time in RFC 3339")
		case st == "":
			fallback("no status open, mitigated, or resolved")
		default:
			c.typ, c.fixedType, c.build = model.TypeIncident, true, buildSpecial
		}
	case "meeting":
		var with []*cand
		for _, w := range md.list("with") {
			if p := ix.people[slug(strings.Trim(w, "[]"))]; p != nil {
				with = append(with, p)
			}
		}
		_, e1 := time.Parse(time.RFC3339Nano, md.scalar("start"))
		_, e2 := time.Parse(time.RFC3339Nano, md.scalar("end"))
		switch {
		case len(with) == 0 || len(with) != len(md.list("with")):
			fallback("attendees are missing or not all people in this vault")
		case e1 != nil || e2 != nil:
			fallback("no start and end in RFC 3339")
		default:
			c.typ, c.fixedType, c.build = model.TypeMeeting, true, buildSpecial
			c.withCands = with
		}
	case "clarification":
		kind := clarifyKind[strings.ToLower(md.scalar("kind"))]
		pol := ix.policies[slug(strings.Trim(md.scalar("policy"), "[]"))]
		switch {
		case kind == "":
			fallback("kind is not interpreted_as, wide_enough_for, established_example, or variance_unenforced")
		case pol == nil:
			fallback("policy is not a policy note in this vault")
		default:
			c.typ, c.fixedType, c.policy, c.build = model.TypePolicyException, true, pol, buildSpecial
			c.hints["kind"] = string(kind)
		}
	}
}

// clarifyKind maps the legacy clarification kinds to policy_exception.kind.
// wide_enough_for: the rule is wide enough to allow it, so overly_broad.
// established_example: a worked example fixes how the rule reads, so
// interpreted_as. variance_unenforced: the rule is not enforced, so
// unenforced.
var clarifyKind = map[string]model.ExceptionKind{
	"interpreted_as":      model.ExceptionInterpretedAs,
	"wide_enough_for":     model.ExceptionOverlyBroad,
	"established_example": model.ExceptionInterpretedAs,
	"variance_unenforced": model.ExceptionUnenforced,
}

// ClarificationKinds is the kind map, for help text and docs.
func ClarificationKinds() map[string]string {
	out := map[string]string{}
	for k, v := range clarifyKind {
		out[k] = string(v)
	}
	return out
}
