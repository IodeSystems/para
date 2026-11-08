package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
)

func convert(in []paramd.Finding) []model.Finding {
	out := make([]model.Finding, 0, len(in))
	for _, f := range in {
		out = append(out, model.Finding{Path: f.Path, Line: f.Line, Code: f.Code, Severity: string(f.Severity), Message: f.Msg})
	}
	return out
}

// lintFile is paramd.Lint plus the dossier blocks of a proposal body
// (model.LintDossier), which paramd cannot import. A closed or superseded
// proposal is a record no one acts on: its blocks are not linted.
func lintFile(f *paramd.File) []paramd.Finding {
	out := paramd.Lint(f)
	if f.Type() != string(model.TypeProposal) || !model.IsDossier(f.Body) {
		return out
	}
	if s, _ := f.Frontmatter["status"].(string); s == string(model.ProposalClosed) || s == string(model.ProposalSuperseded) {
		return out
	}
	for _, d := range model.LintDossier(f.Body) {
		sev := paramd.Error
		if d.Code == model.CodeDossierEvidence {
			sev = paramd.Warn
		}
		out = append(out, paramd.Finding{Path: f.Path, Line: f.BodyLine + d.Line - 1, Code: d.Code, Severity: sev, Msg: d.Msg})
	}
	return out
}

// renderChecked renders d (onto oldFrontmatter, as model.RenderOnto) and refuses it if Lint reports an error. Warnings
// do not refuse a write.
func renderChecked(rel string, d model.Document, oldFrontmatter []byte) ([]byte, error) {
	data, err := model.RenderOnto(d, oldFrontmatter)
	if err != nil {
		return nil, err
	}
	var bad []model.Finding
	for _, f := range convert(lintFile(paramd.Parse(rel, data))) {
		if f.Severity == model.SeverityError {
			bad = append(bad, f)
		}
	}
	if len(bad) > 0 {
		return nil, &model.LintError{Findings: bad}
	}
	return data, nil
}

// lintFiles runs Lint and CheckVault over rels, which are relative to root.
func lintFiles(root string, rels []string) ([]model.Finding, error) {
	var files []*paramd.File
	var out []paramd.Finding
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		f := paramd.Parse(rel, data)
		files = append(files, f)
		out = append(out, lintFile(f)...)
	}
	out = append(out, paramd.CheckVault(files)...)
	return convert(out), nil
}

// LintPath lints a file or directory outside any vault selection. A directory
// that holds .para/ is treated as a vault, so templates/ and events/ are not
// read. A single file gets per-file lint only. Paths in the result are
// relative to the directory, or the path as given for a file.
func LintPath(p string) ([]model.Finding, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("vault: lint path: %w", err)
	}
	if !st.IsDir() {
		f, err := paramd.ParseFile(p)
		if err != nil {
			return nil, fmt.Errorf("vault: lint path: %w", err)
		}
		return convert(lintFile(f)), nil
	}
	dirs := []string{"."}
	if _, err := os.Stat(filepath.Join(p, dirState)); err == nil {
		dirs = dirs[:0]
		for _, f := range model.Folders {
			dirs = append(dirs, string(f))
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("vault: lint path: %w", err)
	}
	var rels []string
	for _, d := range dirs {
		found, err := mdFiles(p, d)
		if err != nil {
			return nil, fmt.Errorf("vault: lint path: %w", err)
		}
		rels = append(rels, found...)
	}
	out, err := lintFiles(p, rels)
	if err != nil {
		return nil, fmt.Errorf("vault: lint path: %w", err)
	}
	conflicts, err := conflictCopies(p, dirs)
	if err != nil {
		return nil, fmt.Errorf("vault: lint path: %w", err)
	}
	return append(out, conflictFindings(conflicts)...), nil
}
