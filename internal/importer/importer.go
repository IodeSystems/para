// Package importer copies a folder of markdown, or the owner's legacy PARA
// vault, into a para vault. Bytes move by program: an agent never retypes
// source content. The package reads files and writes through vault.CreateBatch;
// it never runs a command found in the source and has no path an MCP caller
// can reach (the CLI is the only entry).
//
// Three steps: Scan reads a directory and returns a Report and a Plan (writes
// nothing); Run reads the plan and writes one batch; Undo removes a batch.
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/iodesystems/parasolve/internal/model"
)

// Adapter names.
const (
	AdapterMarkdown = "markdown"
	AdapterLegacy   = "legacy"
)

// maxFile is the largest source file read. A larger one is reported, not read.
const maxFile = 8 << 20

// Issue is one report line about one source path.
type Issue struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// FoldReport counts the folded state of the legacy events.jsonl, so it can be
// compared with the source machine's own count.
type FoldReport struct {
	Lines             int            `json:"lines"`
	Items             int            `json:"items"`
	ItemsByType       map[string]int `json:"items_by_type"`
	TasksByStatus     map[string]int `json:"tasks_by_status"`     // type T
	DeadlinesByStatus map[string]int `json:"deadlines_by_status"` // type D
	QuestionsByStatus map[string]int `json:"questions_by_status"` // type Q
	OpenQuestions     int            `json:"open_questions"`
	Contacts          int            `json:"contacts"`
	ContactsByState   map[string]int `json:"contacts_by_state"`
	DependsEdges      int            `json:"depends_edges"`
	Checks            int            `json:"checks"`
	Risks             int            `json:"risks"`
	BadLines          []Issue        `json:"bad_lines,omitempty"`
}

// Report is the inventory of a scan. It holds counts and paths, no content.
type Report struct {
	Adapter         string         `json:"adapter"`
	Detected        string         `json:"detected"`
	Dir             string         `json:"dir"`
	Files           int            `json:"files"`
	Documents       int            `json:"documents"`
	ByType          map[string]int `json:"by_type"`
	ByFolder        map[string]int `json:"by_folder"`
	SourceFolders   map[string]int `json:"source_folders"`
	FrontmatterKeys map[string]int `json:"frontmatter_keys"`
	Normalized      map[string]int `json:"normalized"`
	Duplicates      [][]string     `json:"duplicates"`
	Unmapped        []Issue        `json:"unmapped"`
	Refused         []Issue        `json:"refused"`
	Fold            *FoldReport    `json:"fold,omitempty"`
}

// Plan is the YAML a person or agent edits between scan and run.
type Plan struct {
	Version int     `yaml:"version"`
	Adapter string  `yaml:"adapter"`
	Source  string  `yaml:"source"`
	Entries []Entry `yaml:"entries"`
}

// Entry plans one document. Path identifies the source unit and is not
// edited. Type, Folder, ID, Area, Project, Tags, Skip are the decisions: Area
// and Project are document ids, either already in the vault or the ID of
// another entry. A non-empty Skip keeps the entry out of the run. Refuse is
// set by scan when the converted document fails lint; it blocks the entry
// until the source is fixed and scanned again. Hash is the source hash at scan;
// a source that changed since is refused.
type Entry struct {
	Path        string   `yaml:"path"`
	Type        string   `yaml:"type"`
	Folder      string   `yaml:"folder"`
	ID          string   `yaml:"id"`
	Area        string   `yaml:"area,omitempty"`
	Project     string   `yaml:"project,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Skip        string   `yaml:"skip,omitempty"`
	DuplicateOf string   `yaml:"duplicate_of,omitempty"`
	Refuse      string   `yaml:"refuse,omitempty"`
	Title       string   `yaml:"title,omitempty"`
	Hash        string   `yaml:"hash"`
	Notes       []string `yaml:"notes,omitempty"`
}

// cand is one source unit that becomes one document.
type cand struct {
	key    string // Entry.Path
	srcRel string // file the unit came from, relative to the scanned dir
	frag   string // fragment of the source url, for a unit inside a file
	hash   string
	title  string

	typ    model.Type
	folder model.Folder
	// fixedType is true when the plan may not change the type: the unit is not
	// a plain markdown document.
	fixedType bool
	idBase    string
	names     []string // lower-case link names that resolve to this unit
	area      *cand
	project   *cand
	tags      []string
	notes     []string
	mtimeDate string

	hints     map[string]string
	withCands []*cand // meeting attendees
	policy    *cand   // policy a clarification is about
	md        *mdFile // parsed markdown, for file units
	build     func(b *bctx, c *cand, e Entry) (model.Document, []string, error)
}

// corpus is the result of loading a directory.
type corpus struct {
	adapter  string
	files    int
	cands    []*cand
	srcDirs  map[string]int
	keys     map[string]int
	unmapped []Issue
	fold     *FoldReport
}

var kebabRe = func() func(string) string {
	return func(s string) string {
		var b strings.Builder
		dash := true
		for _, r := range strings.ToLower(s) {
			if r < utf8.RuneSelf && (unicode.IsLower(r) || unicode.IsDigit(r)) {
				b.WriteRune(r)
				dash = false
			} else if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
		return strings.Trim(b.String(), "-")
	}
}()

// slug turns text into a file stem: lower-case [a-z0-9] groups joined by '-',
// at most 60 characters. Empty when nothing usable remains.
func slug(s string) string {
	out := kebabRe(s)
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
		if i := strings.LastIndexByte(out, '-'); i > 30 {
			out = out[:i]
		}
	}
	return out
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// safe makes text safe to print to a terminal or a report: control characters,
// including escape and newline, are written as \x escapes.
func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == utf8.RuneError, unicode.IsControl(r), r == ' ', r == ' ', unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// fileURL builds the file:// url of an absolute path, percent-encoded, with an
// optional fragment.
func fileURL(abs, frag string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if frag != "" {
		u.Fragment = frag
	}
	return u.String()
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c *cand) hint(k string) string { return c.hints[k] }
