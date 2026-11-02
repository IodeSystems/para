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
