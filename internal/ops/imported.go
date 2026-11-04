package ops

import (
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Import provenance. A create op takes imported_from {path, hash}: the file a
// new document comes from, as the importer names it, and the sha256 of its
// bytes. Para stores both and reads neither: over MCP it opens nothing outside
// the vault. import_verify compares a list of files the caller hashed with
// what the vault holds, so an owner can see what an old vault still has
// before deleting it.

// importedFromField is the Input field a create op takes.
const importedFromField = "ImportedFrom"

var importedFromType = reflect.TypeFor[*model.ImportedFrom]()

// importedFrom returns the imported_from of an op's Input, or nil.
func importedFrom(in any) *model.ImportedFrom {
	v := reflect.ValueOf(in)
	if v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName(importedFromField)
	if !f.IsValid() || f.Type() != importedFromType || f.IsNil() {
		return nil
	}
	return f.Interface().(*model.ImportedFrom)
}

// CheckImportedFrom validates a source file and returns it as stored: the path
// trimmed, the hash as sha256:<64 lowercase hex>. A bare hex hash is accepted.
func CheckImportedFrom(src model.ImportedFrom) (model.ImportedFrom, error) {
	p := strings.TrimSpace(src.Path)
	if p == "" {
		return src, errors.New("imported_from: path is empty")
	}
	if strings.ContainsAny(p, "\r\n") {
		return src, errors.New("imported_from: path must be one line")
	}
	h := strings.ToLower(strings.TrimSpace(src.Hash))
	h = strings.TrimPrefix(h, "sha256:")
	if b, err := hex.DecodeString(h); err != nil || len(b) != 32 {
		return src, fmt.Errorf("imported_from %s: hash must be the sha256 of the file, as 64 hex characters or sha256:<64 hex>", p)
	}
	return model.ImportedFrom{Path: p, Hash: "sha256:" + h}, nil
}

// importStamp is a store that sets imported_from on each document a create op
// makes, except receipts: they record the write, not the import.
type importStamp struct {
	store.Store
	src model.ImportedFrom
}

func (s importStamp) Create(ctx context.Context, d model.Document) (model.Document, error) {
	if d.Meta != nil {
		if h := d.Meta.Header(); h.Type != model.TypeReceipt && h.ImportedFrom == nil {
			src := s.src
			h.ImportedFrom = &src
		}
	}
	return s.Store.Create(ctx, d)
}

// withImport returns d with a store that stamps the input's imported_from on
// what the op creates. An input without one leaves d as it is.
func withImport(d Deps, in any) (Deps, error) {
	src := importedFrom(in)
	if src == nil {
		return d, nil
	}
	clean, err := CheckImportedFrom(*src)
	if err != nil {
		return d, err
	}
	d.Store = importStamp{Store: d.Store, src: clean}
	return d, nil
}

// ImportFile is one source file: its path and the sha256 of its bytes.
type ImportFile struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// ImportVerifyInput is the input of import_verify.
type ImportVerifyInput struct {
	Files []ImportFile `json:"files"`
	// Root, when given, also lists documents imported from a path under it
	// that is not among files: the source file is gone or was not listed.
	Root string `json:"root,omitempty"`
}

// Import results of one file.
const (
	ImportImported = "imported"
	ImportMissing  = "missing"
	ImportChanged  = "changed"
)

// ImportFileResult is what the vault holds for one file.
type ImportFileResult struct {
	Path   string `json:"path"`
	Status string `json:"status"` // imported, missing, or changed
	// Docs are the documents from this file: on changed, the ones whose
	// stored hash differs.
	Docs []string `json:"docs"`
	// MatchedBy is path, or hash when no document names this path but one
	// was imported from a file with the same bytes.
	MatchedBy string `json:"matched_by,omitempty"`
	// StoredHash is the hash the documents hold, on changed.
	StoredHash string `json:"stored_hash,omitempty"`
}

// ImportedDoc is a document with its source file.
type ImportedDoc struct {
	ID     string       `json:"id"`
	Type   model.Type   `json:"type"`
	Folder model.Folder `json:"folder"`
	Title  string       `json:"title"`
	Path   string       `json:"path"`
	Hash   string       `json:"hash"`
}

// ImportVerifyOutput is the output of import_verify. Files lists missing,
// then changed, then imported, each by path.
type ImportVerifyOutput struct {
	Checked  int                `json:"checked"`
	Imported int                `json:"imported"`
	Missing  int                `json:"missing"`
	Changed  int                `json:"changed"`
	Files    []ImportFileResult `json:"files"`
	// Extra are documents imported from under root whose path is not in
	// files. Empty without root.
	Extra []ImportedDoc `json:"extra"`
}

// importedDocs lists every document that has imported_from, by id.
func importedDocs(ctx context.Context, s store.Store) ([]ImportedDoc, error) {
	refs, err := s.List(ctx, store.Filter{})
	if err != nil {
		return nil, err
	}
	out := []ImportedDoc{}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		src := doc.Meta.Header().ImportedFrom
		if src == nil {
			continue
		}
		out = append(out, ImportedDoc{ID: doc.ID, Type: doc.Meta.Header().Type, Folder: doc.Folder, Title: doc.Title,
			Path: src.Path, Hash: strings.ToLower(src.Hash)})
	}
	return out, nil
}

// underRoot reports whether p is root or a path inside it.
func underRoot(p, root string) bool {
	root = strings.TrimSuffix(root, "/")
	return p == root || strings.HasPrefix(p, root+"/")
}

var importRank = map[string]int{ImportMissing: 0, ImportChanged: 1, ImportImported: 2}

// ImportVerify compares source files with the vault. The caller lists and
// hashes the files: para reads nothing outside the vault here.
var ImportVerify = defineRead("import_verify", "Check an import is complete before the old files are deleted: give every source file as {path, hash} (sha256 of its bytes, which you compute); each comes back imported (with its document ids), missing, or changed (imported, but the file's hash differs). With root, also lists documents imported from under root whose file you did not list", func(ctx context.Context, d Deps, in ImportVerifyInput) (ImportVerifyOutput, error) {
	out := ImportVerifyOutput{Files: []ImportFileResult{}, Extra: []ImportedDoc{}}
	files := make([]model.ImportedFrom, 0, len(in.Files))
	seen := map[string]bool{}
	for _, f := range in.Files {
		c, err := CheckImportedFrom(model.ImportedFrom{Path: f.Path, Hash: f.Hash})
		if err != nil {
			return out, err
		}
		if seen[c.Path] {
			return out, fmt.Errorf("file %s is listed twice", c.Path)
		}
		seen[c.Path] = true
		files = append(files, c)
	}
	root := strings.TrimSpace(in.Root)
	if len(files) == 0 && root == "" {
		return out, errors.New("files is empty: list each source file with its path and sha256, or give root")
	}
	docs, err := importedDocs(ctx, d.Store)
	if err != nil {
		return out, err
	}
	byPath, byHash := map[string][]ImportedDoc{}, map[string][]ImportedDoc{}
	for _, doc := range docs {
		byPath[doc.Path] = append(byPath[doc.Path], doc)
		byHash[doc.Hash] = append(byHash[doc.Hash], doc)
	}
	ids := func(list []ImportedDoc) []string {
		out := make([]string, 0, len(list))
		for _, x := range list {
			out = append(out, x.ID)
		}
		return out
	}
	hashMatched := map[string]bool{} // doc ids a file claimed by hash
	for _, f := range files {
		r := ImportFileResult{Path: f.Path, Docs: []string{}}
		same := slices.DeleteFunc(slices.Clone(byPath[f.Path]), func(x ImportedDoc) bool { return x.Hash != f.Hash })
		switch {
		case len(same) > 0:
			r.Status, r.MatchedBy, r.Docs = ImportImported, "path", ids(same)
		case len(byPath[f.Path]) > 0:
			r.Status, r.MatchedBy, r.Docs = ImportChanged, "path", ids(byPath[f.Path])
			r.StoredHash = byPath[f.Path][0].Hash
		case len(byHash[f.Hash]) > 0:
			r.Status, r.MatchedBy, r.Docs = ImportImported, "hash", ids(byHash[f.Hash])
			for _, x := range byHash[f.Hash] {
				hashMatched[x.ID] = true
			}
		default:
			r.Status = ImportMissing
		}
		switch r.Status {
		case ImportImported:
			out.Imported++
		case ImportChanged:
			out.Changed++
		default:
			out.Missing++
		}
		out.Files = append(out.Files, r)
	}
	out.Checked = len(files)
	slices.SortStableFunc(out.Files, func(a, b ImportFileResult) int {
		return cmp.Or(cmp.Compare(importRank[a.Status], importRank[b.Status]), cmp.Compare(a.Path, b.Path))
	})
	if root != "" {
		root = path.Clean(root)
		for _, doc := range docs {
			if underRoot(doc.Path, root) && !seen[doc.Path] && !hashMatched[doc.ID] {
				out.Extra = append(out.Extra, doc)
			}
		}
		slices.SortStableFunc(out.Extra, func(a, b ImportedDoc) int { return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.ID, b.ID)) })
	}
	return out, nil
})

// ImportListInput is the input of import_list.
type ImportListInput struct {
	Root string `json:"root,omitempty" query:"root" doc:"only documents imported from this path or under it"`
}

// ImportListOutput is the output of import_list.
type ImportListOutput struct {
	Docs []ImportedDoc `json:"docs"`
}

// ImportList lists the documents that carry imported_from, by source path.
var ImportList = defineRead("import_list", "List documents imported from files, with the source path and hash each holds; root narrows to one folder of the source", func(ctx context.Context, d Deps, in ImportListInput) (ImportListOutput, error) {
	docs, err := importedDocs(ctx, d.Store)
	if err != nil {
		return ImportListOutput{}, err
	}
	if root := strings.TrimSpace(in.Root); root != "" {
		root = path.Clean(root)
		docs = slices.DeleteFunc(docs, func(x ImportedDoc) bool { return !underRoot(x.Path, root) })
	}
	slices.SortStableFunc(docs, func(a, b ImportedDoc) int { return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.ID, b.ID)) })
	return ImportListOutput{Docs: docs}, nil
})
