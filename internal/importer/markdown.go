package importer

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/iodesystems/parasolve/internal/model"
)

// mdFile is a parsed source markdown file.
type mdFile struct {
	fm            map[string]yaml.Node
	body          string // after the frontmatter and the title line, LF only
	title         string
	titleFromName bool
	notes         []string
	err           string // why the file cannot be converted at all
}

var (
	h1Re       = regexp.MustCompile(`^# +(\S.*)$`)
	indentH1Re = regexp.MustCompile(`^ {0,3}# +\S`)
	wikilinkRe = regexp.MustCompile(`(!?)\[\[([^\[\]\n]*)\]\]`)
	dateHeadRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
)

// parseMD reads markdown bytes. It normalizes line endings and the byte order
// mark, splits frontmatter, and finds the title. It never drops content.
func parseMD(raw []byte, stem string) *mdFile {
	f := &mdFile{fm: map[string]yaml.Node{}}
	if !utf8.Valid(raw) {
		f.err = "not valid UTF-8"
		return f
	}
	if b, ok := bytes.CutPrefix(raw, []byte("\xef\xbb\xbf")); ok {
		raw = b
		f.notes = append(f.notes, "bom: byte order mark removed")
	}
	if bytes.Contains(raw, []byte("\r")) {
		raw = bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
		f.notes = append(f.notes, "line-ending: CR removed")
	}
	text := string(raw)
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if end := frontmatterEnd(rest); end >= 0 {
			yamlText := rest[:end]
			text = strings.TrimPrefix(rest[end:], "---")
			text = strings.TrimPrefix(text, "\n")
			var m map[string]yaml.Node
			if err := yaml.Unmarshal([]byte(yamlText), &m); err != nil {
				f.err = "invalid frontmatter YAML: " + firstLine(err.Error())
				return f
			}
			if m != nil {
				f.fm = m
			}
		}
	}
	lines := strings.Split(text, "\n")
	// Title: the first non-blank line, when it is "# Title".
	first := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			first = i
			break
		}
	}
	if first >= 0 {
		if m := h1Re.FindStringSubmatch(lines[first]); m != nil {
			f.title = strings.TrimSpace(m[1])
			lines = lines[first+1:]
		}
	}
	if f.title == "" {
		f.title = titleFromName(stem)
		f.titleFromName = true
		f.notes = append(f.notes, "title: no '# Title'; taken from the file name")
	}
	// Other level-1 headings are demoted: a document has one.
	fence := ""
	demoted := 0
	for i, l := range lines {
		if fence != "" {
			if closesFence(l, fence) {
				fence = ""
			}
			continue
		}
		if fn := opensFence(l); fn != "" {
			fence = fn
			continue
		}
		if indentH1Re.MatchString(l) {
			lines[i] = "#" + l
			demoted++
		}
	}
	if demoted > 0 {
		f.notes = append(f.notes, fmt.Sprintf("heading: %d extra level-1 heading(s) demoted to level 2", demoted))
	}
	f.body = strings.Trim(strings.Join(lines, "\n"), "\n")
	return f
}

// frontmatterEnd returns the offset in s (the text after the opening ---) of
// the closing --- line, or -1.
func frontmatterEnd(s string) int {
	off := 0
	for _, l := range strings.SplitAfter(s, "\n") {
		if strings.TrimRight(l, "\n") == "---" {
			return off
		}
		off += len(l)
	}
	return -1
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

func opensFence(l string) string {
	t := strings.TrimLeft(l, " ")
	if len(l)-len(t) > 3 {
		return ""
	}
	for _, c := range []string{"`", "~"} {
		n := len(t) - len(strings.TrimLeft(t, c))
		if n >= 3 {
			return strings.Repeat(c, n)
		}
	}
	return ""
}

func closesFence(l, fence string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == ""
}

func titleFromName(stem string) string {
	var b strings.Builder
	for _, r := range stem {
		switch {
		case r == '_':
			b.WriteByte(' ')
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	t := strings.Join(strings.Fields(b.String()), " ")
	if t == "" {
		return "Untitled"
	}
	return t
}

// scalar returns the string value of a scalar frontmatter key.
func (f *mdFile) scalar(key string) string {
	n, ok := f.fm[key]
	if !ok || n.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(n.Value)
}

// list returns a scalar or a list of scalars as strings.
func (f *mdFile) list(key string) []string {
	n, ok := f.fm[key]
	if !ok {
		return nil
	}
	var out []string
	switch n.Kind {
	case yaml.ScalarNode:
		for _, p := range strings.FieldsFunc(n.Value, func(r rune) bool { return r == ',' || r == ' ' }) {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && strings.TrimSpace(c.Value) != "" {
				out = append(out, strings.TrimSpace(c.Value))
			}
		}
	}
	return out
}

// date returns the YYYY-MM-DD form of a date or timestamp value.
func dateOf(s string) (model.Date, bool) {
	if !dateHeadRe.MatchString(s) {
		return "", false
	}
	if _, err := time.Parse("2006-01-02", s[:10]); err != nil {
		return "", false
	}
	return model.Date(s[:10]), true
}

// yamlKeys returns the frontmatter keys the meta type declares.
func yamlKeys(meta model.Meta) map[string]bool {
	out := map[string]bool{}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		for i := range t.NumField() {
			sf := t.Field(i)
			tag := sf.Tag.Get("yaml")
			name, opts, _ := strings.Cut(tag, ",")
			if strings.Contains(opts, "inline") {
				walk(sf.Type)
				continue
			}
			if name == "" || name == "-" {
				continue
			}
			out[name] = true
		}
	}
	walk(reflect.TypeOf(meta))
	return out
}

// codeSeg is a piece of a line, in or out of an inline code span.
type codeSeg struct {
	text string
	code bool
}

// splitCode splits a line at inline code spans.
func splitCode(line string) []codeSeg {
	var out []codeSeg
	start := 0
	i := 0
	for i < len(line) {
		if line[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		n := j - i
		// find the next run of exactly n backticks
		k := j
		closeAt := -1
		for k < len(line) {
			if line[k] != '`' {
				k++
				continue
			}
			e := k
			for e < len(line) && line[e] == '`' {
				e++
			}
			if e-k == n {
				closeAt = e
				break
			}
			k = e
		}
		if closeAt < 0 {
			i = j
			continue
		}
		if i > start {
			out = append(out, codeSeg{line[start:i], false})
		}
		out = append(out, codeSeg{line[i:closeAt], true})
		start, i = closeAt, closeAt
	}
	if start < len(line) {
		out = append(out, codeSeg{line[start:], false})
	}
	return out
}

// rewriteLinks maps Obsidian wikilinks in body to the ids of the imported
// documents. Code is left alone. An embed becomes a link. A link to nothing
// imported becomes its plain text, and a note says so: no link is dropped
// without a report.
func (b *bctx) rewriteLinks(body string, self *cand) (string, []string) {
	var notes []string
	counts := map[string]int{}
	lines := strings.Split(body, "\n")
	fence := ""
	for li, l := range lines {
		if fence != "" {
			if closesFence(l, fence) {
				fence = ""
			}
			continue
		}
		if fn := opensFence(l); fn != "" {
			fence = fn
			continue
		}
		if !strings.Contains(l, "[[") {
			continue
		}
		segs := splitCode(l)
		for si, s := range segs {
			if s.code {
				continue
			}
			segs[si].text = wikilinkRe.ReplaceAllStringFunc(s.text, func(m string) string {
				sub := wikilinkRe.FindStringSubmatch(m)
				embed, inner := sub[1] == "!", sub[2]
				alias := ""
				if i := strings.Index(inner, "|"); i >= 0 {
					inner, alias = inner[:i], strings.TrimSpace(inner[i+1:])
				}
				target, frag := inner, ""
				if i := strings.Index(inner, "#"); i >= 0 {
					target, frag = inner[:i], inner[i+1:]
				}
				target = strings.TrimSpace(target)
				var to *cand
				if target == "" && frag != "" {
					to = self
				} else if target != "" {
					to = b.lookup(target)
				}
				id := ""
				if to != nil {
					id = b.idOf(to)
				}
				if id == "" {
					counts["link: unresolved link kept as plain text"]++
					switch {
					case alias != "":
						return alias
					case frag != "" && target != "":
						return target + " > " + frag
					case target != "":
						return target
					}
					return inner
				}
				if embed {
					counts["embed: ![[...]] turned into a link"]++
				} else {
					counts["link: wikilink rewritten to the new id"]++
				}
				out := "[[" + id
				if strings.HasPrefix(frag, "^") {
					if blockOK.MatchString(frag[1:]) {
						out += "#" + frag
					}
				} else if frag != "" && !strings.ContainsAny(frag, "[]|#^") {
					out += "#" + strings.TrimSpace(frag)
				}
				if alias != "" && !strings.ContainsAny(alias, "[]") {
					out += "|" + alias
				}
				return out + "]]"
			})
		}
		var sb strings.Builder
		for _, s := range segs {
			sb.WriteString(s.text)
		}
		lines[li] = sb.String()
	}
	for _, k := range sortedKeys(counts) {
		notes = append(notes, fmt.Sprintf("%s (%d)", k, counts[k]))
	}
	return strings.Join(lines, "\n"), notes
}

var blockOK = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// fileOK reports whether a file name can be a source file at all.
func isMarkdownName(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".md") }

// walkResult is what walkDir finds under a directory.
type walkResult struct {
	files    []walkFile
	unmapped []Issue
}

type walkFile struct {
	rel   string // slash path
	abs   string
	size  int64
	mtime time.Time
}

// walkDir lists the regular files under dir. It does not follow symlinks (a
// link in a hostile folder could point at any local file) and it skips hidden
// directories; both are reported.
func walkDir(dir string, skipDir func(rel string) bool) (*walkResult, error) {
	res := &walkResult{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			res.unmapped = append(res.unmapped, Issue{rel, "symbolic link not followed"})
			return nil
		case d.IsDir():
			if strings.HasPrefix(d.Name(), ".") {
				res.unmapped = append(res.unmapped, Issue{rel + "/", "hidden directory not read"})
				return filepath.SkipDir
			}
			if skipDir != nil && skipDir(rel) {
				res.unmapped = append(res.unmapped, Issue{rel + "/", "directory skipped (templates)"})
				return filepath.SkipDir
			}
			return nil
		case !d.Type().IsRegular():
			res.unmapped = append(res.unmapped, Issue{rel, "not a regular file"})
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		res.files = append(res.files, walkFile{rel: rel, abs: p, size: info.Size(), mtime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("importer: walk %s: %w", dir, err)
	}
	sort.Slice(res.files, func(i, j int) bool { return res.files[i].rel < res.files[j].rel })
	return res, nil
}

// readSource reads a source file with a size cap.
func readSource(f walkFile) ([]byte, error) {
	if f.size > maxFile {
		return nil, fmt.Errorf("larger than %d MiB", maxFile>>20)
	}
	return os.ReadFile(f.abs)
}

// fileCand makes the unit for one markdown file. Type and folder are the
// adapter's defaults.
func fileCand(f walkFile, raw []byte, typ model.Type, folder model.Folder) *cand {
	stem := strings.TrimSuffix(path.Base(f.rel), path.Ext(f.rel))
	noExt := strings.TrimSuffix(f.rel, path.Ext(f.rel))
	md := parseMD(raw, stem)
	c := &cand{
		key: f.rel, srcRel: f.rel, hash: sha(raw), title: md.title,
		typ: typ, folder: folder, idBase: slug(stem),
		names: []string{strings.ToLower(stem), strings.ToLower(noExt)},
		md:    md, mtimeDate: f.mtime.UTC().Format("2006-01-02"),
		notes: append([]string(nil), md.notes...),
	}
	if c.idBase == "" {
		c.idBase = string(typ) + "-" + strings.TrimPrefix(sha([]byte(f.rel)), "sha256:")[:8]
		c.notes = append(c.notes, "id: file name has no letters or digits; id made from a hash of the path")
	}
	c.build = buildFileDoc
	return c
}

// prepared is what every file-derived document shares.
type prepared struct {
	common model.Common
	taken  map[string]bool // source keys the builder used as typed fields
	notes  []string
}

// prep fills the common fields from the source file and the plan entry.
func prep(c *cand, e Entry) (prepared, error) {
	md := c.md
	if md.err != "" {
		return prepared{}, fmt.Errorf("%s", md.err)
	}
	p := prepared{notes: append([]string(nil), md.notes...), taken: map[string]bool{"type": true, "created": true, "updated": true, "tags": true}}
	created := model.Date("")
	for _, k := range []string{"created", "date"} {
		if d, ok := dateOf(md.scalar(k)); ok {
			created = d
			break
		}
	}
	if created == "" {
		created = model.Date(c.mtimeDate)
		p.notes = append(p.notes, "created: no usable date in the file; file modification date used")
	}
	p.common = model.Common{Type: model.Type(e.Type), Created: created, Tags: mergeTags(e.Tags, tagsFrom(md))}
	if d, ok := dateOf(md.scalar("updated")); ok {
		p.common.Updated = d
	}
	return p, nil
}

// finish rewrites links, sets provenance and the undeclared keys, and returns
// the document.
func finish(b *bctx, c *cand, e Entry, p prepared, meta model.Meta) (model.Document, []string, error) {
	md := c.md
	body, linkNotes := b.rewriteLinks(md.body, c)
	notes := append(p.notes, linkNotes...)
	imp := md.scalar("id")
	if imp != "" {
		body = strings.TrimRight(body, "\n") + "\n\nImported id: " + imp
		body = strings.TrimLeft(body, "\n")
	}
	doc := model.Document{ID: b.idOf(c), Folder: model.Folder(e.Folder), Title: c.title, Body: body, Meta: meta}
	if imp != "" {
		if err := doc.SetExtra("imported_id", imp); err != nil {
			return doc, notes, err
		}
	}
	err := b.addExtras(&doc, c, md, p.taken)
	return doc, notes, err
}

// buildFileDoc converts a markdown unit into a document of the planned type.
func buildFileDoc(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
	p, err := prep(c, e)
	if err != nil {
		return model.Document{}, nil, err
	}
	md, taken, common := c.md, p.taken, p.common
	linkOf := func(id string) model.Link {
		if id == "" {
			return ""
		}
		if r := b.refID(id); r != "" {
			return model.LinkTo(r)
		}
		p.notes = append(p.notes, "link: area or project "+safe(id)+" is not in the vault or the plan; link dropped")
		return ""
	}
	var meta model.Meta
	switch model.Type(e.Type) {
	case model.TypeNote:
		meta = &model.Note{Common: common, Area: linkOf(e.Area), Project: linkOf(e.Project)}
	case model.TypeArea:
		st := model.AreaActive
		if md.scalar("status") == "paused" {
			st = model.AreaPaused
		}
		taken["status"] = md.scalar("status") == "active" || md.scalar("status") == "paused"
		meta = &model.Area{Common: common, Status: st}
	case model.TypeProject:
		st := model.ProjectActive
		if c.hint("status") != "" {
			st = model.ProjectStatus(c.hint("status"))
		}
		for _, ps := range model.ProjectStatuses {
			if md.scalar("status") == string(ps) {
				st = ps
				taken["status"] = true
			}
		}
		owner := "self"
		if o := md.scalar("owner"); o != "" {
			owner, taken["owner"] = o, true
		}
		meta = &model.Project{Common: common, Status: st, Owner: owner, Area: linkOf(e.Area)}
		if md.scalar("area") != "" && e.Area != "" {
			taken["area"] = true
		}
	case model.TypeInbox:
		meta = &model.Inbox{Common: common}
	case model.TypeJournal:
		date, scope, ok := journalDate(c, md)
		if !ok {
			return model.Document{}, nil, fmt.Errorf("journal: no date in the front matter or the file name")
		}
		taken["date"], taken["scope"] = true, true
		meta = &model.Journal{Common: common, Date: date, Scope: scope}
	case model.TypePerson:
		org := "unknown"
		if o := md.scalar("org"); o != "" {
			org, taken["org"] = o, true
		}
		pe := &model.Person{Common: common, Org: org}
		if al := md.list("aliases"); len(al) > 0 {
			pe.Aliases, taken["aliases"] = al, true
		}
		meta = pe
	default:
		return model.Document{}, nil, fmt.Errorf("type %q cannot be built from a markdown file", e.Type)
	}
	return finish(b, c, e, p, meta)
}

// journalDate finds a journal's date and scope from the front matter or the
// file name (YYYY-MM-DD, or YYYY-Www for a week).
func journalDate(c *cand, md *mdFile) (model.Date, model.JournalScope, bool) {
	scope := model.JournalScope(c.hint("scope"))
	if scope == "" {
		scope = model.JournalDay
	}
	if s := md.scalar("scope"); s != "" {
		for _, x := range []model.JournalScope{model.JournalDay, model.JournalWeek, model.JournalMonth, model.JournalProject} {
			if string(x) == s {
				scope = x
			}
		}
	}
	if d, ok := dateOf(md.scalar("date")); ok {
		return d, scope, true
	}
	stem := strings.TrimSuffix(path.Base(c.srcRel), path.Ext(c.srcRel))
	if d, ok := dateOf(stem); ok {
		return d, scope, true
	}
	if m := weekRe.FindStringSubmatch(stem); m != nil {
		y, _ := strconv.Atoi(m[1])
		w, _ := strconv.Atoi(m[2])
		if w >= 1 && w <= 53 {
			// Monday of ISO week w: week 1 holds Jan 4.
			jan4 := time.Date(y, 1, 4, 0, 0, 0, 0, time.UTC)
			monday := jan4.AddDate(0, 0, -int((jan4.Weekday()+6)%7)+(w-1)*7)
			if scope == model.JournalDay {
				scope = model.JournalWeek
			}
			return model.Date(monday.Format("2006-01-02")), scope, true
		}
	}
	return "", "", false
}

var weekRe = regexp.MustCompile(`^(\d{4})-?[Ww](\d{1,2})`)

func tagsFrom(md *mdFile) []string {
	var out []string
	for _, t := range md.list("tags") {
		t = strings.TrimPrefix(t, "#")
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func mergeTags(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range append(append([]string(nil), a...), b...) {
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// addExtras sets provenance and the source's undeclared frontmatter on doc.
// A source key the meta type declares but this builder did not use is kept as
// imported_<key>, so nothing is lost and nothing is misread as a typed field.
func (b *bctx) addExtras(doc *model.Document, c *cand, md *mdFile, taken map[string]bool) error {
	declared := yamlKeys(doc.Meta)
	if err := doc.SetExtra("source", b.provenance(c)); err != nil {
		return err
	}
	if md == nil {
		return nil
	}
	for _, k := range sortedKeys(md.fm) {
		if taken[k] {
			continue
		}
		name := k
		if declared[k] || reservedExtra[k] {
			name = "imported_" + k
		}
		if k == "imported_id" {
			name = "imported_imported_id"
		}
		n := cloneNode(md.fm[k])
		b.rewriteNodeLinks(&n, c)
		doc.SetExtraNode(name, n)
	}
	return nil
}

// reservedExtra are keys para itself writes as extras.
var reservedExtra = map[string]bool{"source": true, "imported_id": true}

func cloneNode(n yaml.Node) yaml.Node {
	out := n
	out.Content = make([]*yaml.Node, len(n.Content))
	for i, c := range n.Content {
		cc := cloneNode(*c)
		out.Content[i] = &cc
	}
	return out
}

// rewriteNodeLinks maps wikilinks inside frontmatter values like links in the
// body: a link in a kept key must resolve, or lint would refuse the file.
func (b *bctx) rewriteNodeLinks(n *yaml.Node, self *cand) {
	if n.Kind == yaml.ScalarNode && strings.Contains(n.Value, "[[") {
		n.Value, _ = b.rewriteLinks(n.Value, self)
		if strings.Contains(n.Value, "[[") && n.Style == 0 {
			n.Style = yaml.DoubleQuotedStyle
		}
		return
	}
	for _, c := range n.Content {
		b.rewriteNodeLinks(c, self)
	}
}
