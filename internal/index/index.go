// Package index is the full-text search cache of one vault: SQLite with FTS5
// in the vault's state dir, outside the vault tree (file sync corrupts SQLite).
// It is a cache. Markdown is the source of truth, so
// the file can be deleted at any time and Rebuild recreates it. Every Search
// first compares the vault with the index and re-reads only documents whose
// content hash changed, so an answer is never older than the files.
package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/iodesystems/para/internal/model"
)

// schemaVersion changes when the tables change. A file with another version
// is dropped and rebuilt.
const schemaVersion = "2"

const schema = `
CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS docs (
	id TEXT PRIMARY KEY,
	path TEXT NOT NULL,
	title TEXT NOT NULL,
	kind TEXT NOT NULL,
	source_url TEXT NOT NULL,
	unverified INTEGER NOT NULL,
	rev TEXT NOT NULL,
	type TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	area TEXT NOT NULL DEFAULT '',
	project TEXT NOT NULL DEFAULT '',
	updated TEXT NOT NULL DEFAULT ''
);
CREATE VIRTUAL TABLE IF NOT EXISTS fts USING fts5(title, body, tokenize='porter unicode61');
`

// Kinds a hit can carry. Other documents use their model.Type.
const (
	KindTranscript = "transcript"
	KindSummary    = "summary" // a policy summary
)

// Hit is one search result.
type Hit struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Path       string  `json:"path"`
	Kind       string  `json:"kind"` // transcript, summary, or the document type
	Passage    string  `json:"passage"`
	SourceURL  string  `json:"source_url"`
	Unverified bool    `json:"unverified"` // a summary no person reviewed
	Score      float64 `json:"score"`      // lower is better (bm25)
	// Match says how the hit was found: "exact" (words, FTS5) or "meaning"
	// (a raglit daemon). Set by ops.Search.
	Match string `json:"match,omitempty"`
	// Via names the document the text came from when it is not the hit itself:
	// a meaning hit on a transcript is shown under its summary.
	Via string `json:"via,omitempty"`
	// Caveat says why a meaning hit's text is not the document's own words.
	Caveat string `json:"caveat,omitempty"`
	// Snippet is Passage for a reader: no match markers, links written as the
	// titles they point at. Marks are the matched words in it, counted in runes
	// (Unicode code points) from the start of Snippet. Set by ops.Search.
	Snippet string `json:"snippet"`
	Marks   []Span `json:"marks,omitempty"`
}

// Span is a half-open range [Start, End) of runes in a snippet.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// The markers the index puts around matched words in Passage. ops.Search
// strips them; nothing else should show them.
const (
	MarkOpen  = "<<"
	MarkClose = ">>"
)

// Stats says what a Rebuild or sync did.
type Stats struct {
	Documents int `json:"documents"` // documents in the index now
	Added     int `json:"added"`
	Updated   int `json:"updated"`
	Removed   int `json:"removed"`
}

// Source is the vault as the index reads it.
type Source interface {
	// Refs lists every document.
	Refs(ctx context.Context) ([]model.Ref, error)
	Get(ctx context.Context, id string) (model.Document, error)
}

// Index is an open index file.
type Index struct {
	db   *sql.DB
	path string
}

// Open opens the index at path. An unreadable file or one of another schema
// version is removed and recreated empty.
func Open(path string) (*Index, error) {
	ix, err := open(path)
	if err == nil {
		return ix, nil
	}
	if rerr := removeFiles(path); rerr != nil {
		return nil, errors.Join(err, rerr)
	}
	return open(path)
}

func open(path string) (*Index, error) {
	// Pragmas ride the DSN: a pragma run after Open reaches one pooled
	// connection only (raglit store.go:Open).
	dsn := "file:" + path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("index: open: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("index: open: %w", err)
	}
	var v string
	switch err := db.QueryRow(`SELECT v FROM meta WHERE k='version'`).Scan(&v); {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := db.Exec(`INSERT INTO meta(k, v) VALUES('version', ?)`, schemaVersion); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("index: open: %w", err)
		}
	case err != nil:
		_ = db.Close()
		return nil, fmt.Errorf("index: open: %w", err)
	case v != schemaVersion:
		_ = db.Close()
		return nil, fmt.Errorf("index: open: schema version %q, want %q", v, schemaVersion)
	}
	return &Index{db: db, path: path}, nil
}

func removeFiles(path string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("index: remove: %w", err)
		}
	}
	return nil
}

// Close releases the file.
func (ix *Index) Close() error { return ix.db.Close() }

// Rebuild drops every row and reads the whole vault again.
func (ix *Index) Rebuild(ctx context.Context, src Source) (Stats, error) {
	if _, err := ix.db.ExecContext(ctx, `DELETE FROM docs; DELETE FROM fts`); err != nil {
		return Stats{}, fmt.Errorf("index: rebuild: %w", err)
	}
	st, err := ix.Sync(ctx, src)
	if err != nil {
		return st, fmt.Errorf("index: rebuild: %w", err)
	}
	return st, nil
}

// Sync brings the index up to date: it adds new documents, re-reads changed
// ones (by content hash), and drops deleted ones. Unchanged documents are not
// read.
func (ix *Index) Sync(ctx context.Context, src Source) (Stats, error) {
	var st Stats
	refs, err := src.Refs(ctx)
	if err != nil {
		return st, fmt.Errorf("index: sync: %w", err)
	}
	have, err := ix.revs(ctx)
	if err != nil {
		return st, err
	}
	var todo []model.Ref
	for _, r := range refs {
		old, ok := have[r.ID]
		delete(have, r.ID)
		switch {
		case !ok:
			st.Added++
			todo = append(todo, r)
		case old != r.Rev:
			st.Updated++
			todo = append(todo, r)
		}
	}
	st.Removed = len(have)
	st.Documents = len(refs)
	if len(todo) == 0 && len(have) == 0 {
		return st, nil
	}
	rows := make([]row, 0, len(todo))
	for _, r := range todo {
		d, err := src.Get(ctx, r.ID)
		if err != nil {
			return st, fmt.Errorf("index: sync: %w", err)
		}
		rows = append(rows, newRow(d))
	}
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return st, fmt.Errorf("index: sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for id := range have {
		if err := deleteDoc(ctx, tx, id); err != nil {
			return st, err
		}
	}
	for _, r := range rows {
		if err := deleteDoc(ctx, tx, r.id); err != nil {
			return st, err
		}
		if err := insertDoc(ctx, tx, r); err != nil {
			return st, err
		}
	}
	if err := tx.Commit(); err != nil {
		return st, fmt.Errorf("index: sync: %w", err)
	}
	return st, nil
}

func (ix *Index) revs(ctx context.Context) (map[string]string, error) {
	rs, err := ix.db.QueryContext(ctx, `SELECT id, rev FROM docs`)
	if err != nil {
		return nil, fmt.Errorf("index: revs: %w", err)
	}
	defer func() { _ = rs.Close() }()
	out := map[string]string{}
	for rs.Next() {
		var id, rev string
		if err := rs.Scan(&id, &rev); err != nil {
			return nil, fmt.Errorf("index: revs: %w", err)
		}
		out[id] = rev
	}
	return out, rs.Err()
}

type row struct {
	id, path, title, kind, url, rev, body string
	typ, status, area, project, updated   string
	unverified                            bool
}

func newRow(d model.Document) row {
	r := row{
		id: d.ID, path: string(d.Folder) + "/" + d.ID + ".md", title: d.Title,
		kind: string(d.Meta.Header().Type), rev: d.Rev, body: d.Body,
	}
	h := d.Meta.Header()
	r.typ, r.updated = string(h.Type), string(h.Updated)
	if r.updated == "" {
		r.updated = string(h.Created)
	}
	r.status = metaString(d.Meta, "Status")
	r.area = linkTarget(metaString(d.Meta, "Area"))
	r.project = linkTarget(metaString(d.Meta, "Project"))
	switch m := d.Meta.(type) {
	case *model.Policy:
		r.kind, r.url, r.unverified = KindSummary, m.Source.URL, !m.Verified
	case *model.Transcript:
		r.kind, r.url = KindTranscript, m.Source.URL
	case *model.Constraint:
		r.url = m.Source.URL
	}
	return r
}

// HitFor builds the hit of a document found outside the index, with the given
// passage.
func HitFor(d model.Document, passage string) Hit {
	r := newRow(d)
	return Hit{ID: r.id, Title: r.title, Path: r.path, Kind: r.kind, Passage: passage, SourceURL: r.url, Unverified: r.unverified}
}

// Match reports whether a document passes the filters of q, as the index
// applies them.
func (q Query) Match(d model.Document) bool {
	r := newRow(d)
	switch {
	case q.Type != "" && r.typ != q.Type,
		q.Area != "" && r.area != model.Link(q.Area).Target(),
		q.Project != "" && r.project != model.Link(q.Project).Target(),
		q.Since != "" && r.updated < q.Since:
		return false
	}
	switch q.Status {
	case "":
	case StatusOpen:
		return r.status != "" && !slices.Contains(finalStatuses, r.status)
	default:
		return r.status == q.Status
	}
	return true
}

// metaString reads a string-kinded field of a document's typed frontmatter,
// or "" when the type has none.
func metaString(m model.Meta, name string) string {
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return ""
	}
	f := v.Elem().FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}

func linkTarget(s string) string { return model.Link(s).Target() }

func deleteDoc(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM fts WHERE rowid IN (SELECT rowid FROM docs WHERE id=?)`, id); err != nil {
		return fmt.Errorf("index: delete: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM docs WHERE id=?`, id); err != nil {
		return fmt.Errorf("index: delete: %w", err)
	}
	return nil
}

func insertDoc(ctx context.Context, tx *sql.Tx, r row) error {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO docs(id, path, title, kind, source_url, unverified, rev, type, status, area, project, updated) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.id, r.path, r.title, r.kind, r.url, r.unverified, r.rev, r.typ, r.status, r.area, r.project, r.updated)
	if err != nil {
		return fmt.Errorf("index: insert: %w", err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("index: insert: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO fts(rowid, title, body) VALUES(?,?,?)`, rowid, r.title, r.body); err != nil {
		return fmt.Errorf("index: insert: %w", err)
	}
	return nil
}

const defaultLimit = 10

// Query is a search: text, plus optional filters. An empty filter matches all.
type Query struct {
	Text  string
	Limit int // 0 means 10
	// Type is a document type (task, project, note, policy ...).
	Type string
	// Status is the document's status. "open" also matches every status that
	// is not final (see finalStatuses).
	Status string
	// Area and Project are document ids; a document matches when its area or
	// project link points there.
	Area, Project string
	// Since is YYYY-MM-DD: documents updated (else created) on or after it.
	Since string
	// Prefix makes the last word match the start of a word: "repl" finds
	// "replication". It is for a search box that runs as a person types.
	Prefix bool
}

// StatusOpen is the status filter that means "not finished".
const StatusOpen = "open"

// finalStatuses end a document's life. They are what "open" excludes.
var finalStatuses = []string{"done", "dropped", "closed", "declined", "cancelled", "failed",
	"resolved", "retired", "superseded", "mitigated", "adopted", "accepted"}

// Search syncs with src, then returns up to limit hits, best first. A query
// with no word characters returns no hits.
func (ix *Index) Search(ctx context.Context, src Source, query string, limit int) ([]Hit, error) {
	return ix.Find(ctx, src, Query{Text: query, Limit: limit})
}

// Find is Search with filters.
func (ix *Index) Find(ctx context.Context, src Source, in Query) ([]Hit, error) {
	if _, err := ix.Sync(ctx, src); err != nil {
		return nil, err
	}
	q := ftsQuery(in.Text, in.Prefix)
	if q == "" {
		return []Hit{}, nil
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	where, args := []string{"fts MATCH ?"}, []any{q}
	eq := func(col, v string) {
		if v != "" {
			where, args = append(where, col+" = ?"), append(args, v)
		}
	}
	eq("d.type", in.Type)
	eq("d.area", model.Link(in.Area).Target())
	eq("d.project", model.Link(in.Project).Target())
	switch in.Status {
	case "":
	case StatusOpen:
		marks := strings.TrimSuffix(strings.Repeat("?,", len(finalStatuses)), ",")
		where = append(where, "d.status != '' AND d.status NOT IN ("+marks+")")
		for _, s := range finalStatuses {
			args = append(args, s)
		}
	default:
		eq("d.status", in.Status)
	}
	if in.Since != "" {
		where, args = append(where, "d.updated >= ?"), append(args, in.Since)
	}
	args = append(args, limit)
	rs, err := ix.db.QueryContext(ctx, `
SELECT d.id, d.title, d.path, d.kind, d.source_url, d.unverified,
       snippet(fts, -1, '`+MarkOpen+`', '`+MarkClose+`', ' ... ', 24), bm25(fts)
FROM fts JOIN docs d ON d.rowid = fts.rowid
WHERE `+strings.Join(where, " AND ")+`
ORDER BY bm25(fts), d.id
LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("index: search: %w", err)
	}
	defer func() { _ = rs.Close() }()
	out := []Hit{}
	for rs.Next() {
		var h Hit
		if err := rs.Scan(&h.ID, &h.Title, &h.Path, &h.Kind, &h.SourceURL, &h.Unverified, &h.Passage, &h.Score); err != nil {
			return nil, fmt.Errorf("index: search: %w", err)
		}
		h.Passage = strings.Join(strings.Fields(h.Passage), " ")
		out = append(out, h)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("index: search: %w", err)
	}
	return out, nil
}

// ftsQuery turns free text into an FTS5 query that cannot be a syntax error:
// each whitespace-separated word is a quoted string, and all must match.
func ftsQuery(q string, prefix bool) string {
	var parts []string
	toks := strings.Fields(q)
	for i, tok := range toks {
		part := `"` + strings.ReplaceAll(tok, `"`, `""`) + `"`
		if prefix && i == len(toks)-1 {
			part += "*"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}
