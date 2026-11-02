package paramd

import (
	"fmt"
	"strings"
)

// CheckVault runs the rules that need more than one file: unique names,
// resolvable links and block ids, and a transcript for every policy that
// allows copying.
func CheckVault(files []*File) []Finding {
	var out []Finding
	byName := map[string]*File{}
	for _, f := range files {
		key := strings.ToLower(f.Name)
		if prev, dup := byName[key]; dup {
			out = append(out, Finding{Path: f.Path, Code: CodeDupFilename, Severity: Error,
				Msg: fmt.Sprintf("file name %q already used by %s; names are unique in a vault", f.Name, prev.Path)})
			continue
		}
		byName[key] = f
	}
	for _, f := range files {
		for _, lk := range f.Links {
			if lk.Target == "" {
				continue
			}
			dst, ok := byName[strings.ToLower(lk.Target)]
			if !ok {
				out = append(out, Finding{Path: f.Path, Line: lk.Line, Code: CodeLinkBroken, Severity: Error,
					Msg: fmt.Sprintf("%s: no file named %q", lk.Raw, lk.Target)})
				continue
			}
			if lk.Block == "" {
				continue
			}
			found := false
			for _, b := range dst.Blocks {
				if b.ID == lk.Block {
					found = true
					break
				}
			}
			if !found {
				out = append(out, Finding{Path: f.Path, Line: lk.Line, Code: CodeBlockBroken, Severity: Error,
					Msg: fmt.Sprintf("%s: %s has no block ^%s", lk.Raw, dst.Path, lk.Block)})
			}
		}
		if f.Type() == "question" {
			out = append(out, checkScope(f, byName)...)
		}
		if f.Type() == "policy" {
			if src, ok := f.Frontmatter["source"].(map[string]any); ok && src["fidelity"] != "none" {
				if _, ok := byName[strings.ToLower(f.Name+".source")]; !ok {
					out = append(out, Finding{Path: f.Path, Code: CodeNoTranscript, Severity: Error,
						Msg: fmt.Sprintf("no transcript %s.source.md; set source.fidelity to none if the document must not be copied", f.Name)})
				}
			}
		}
	}
	return out
}

// checkScope: a question is scoped to a project, a task, or an area, or to
// root. Lint checks the shape of the field; only the vault can check the type of
// the target. A target that does not exist is a link-unresolved finding.
func checkScope(f *File, byName map[string]*File) []Finding {
	scope, _ := f.Frontmatter["scope"].(string)
	m := linkValRe.FindStringSubmatch(scope)
	if m == nil { // root, or a shape error that Lint reports
		return nil
	}
	dst, ok := byName[strings.ToLower(m[1])]
	switch {
	case !ok: // link-unresolved reports it
	case dst.Type() != "project" && dst.Type() != "task" && dst.Type() != "area":
		return []Finding{{Path: f.Path, Code: CodeScope, Severity: Error,
			Msg: fmt.Sprintf("scope %s is a %s: want a project, a task, an area, or root", scope, dst.Type())}}
	}
	return nil
}
