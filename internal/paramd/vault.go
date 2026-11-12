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
	former := FormerIDs(files)
	for id, f := range former {
		if g, ok := byName[id]; ok {
			out = append(out, Finding{Path: g.Path, Code: CodeFormerTaken, Severity: Error,
				Msg: fmt.Sprintf("%s is a former id of %s: the file is an old copy (a sync that came in late?), or a new document took a reserved id", id, f.Path)})
		}
	}
	for _, f := range files {
		for _, lk := range f.Links {
			if lk.Target == "" {
				continue
			}
			dst, ok := byName[strings.ToLower(lk.Target)]
			if g, was := former[strings.ToLower(lk.Target)]; !ok && was {
				out = append(out, Finding{Path: f.Path, Line: lk.Line, Code: CodeLinkFormer, Severity: Warn,
					Msg: fmt.Sprintf("%s names a former id of %s: it resolves; relink to fix", lk.Raw, g.Name)})
				continue
			}
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
		out = append(out, checkAreaKinds(f, byName)...)
		out = append(out, checkTypedLinks(f, byName)...)
		out = append(out, checkParent(f, byName)...)
		out = append(out, checkRefines(f, byName)...)
		if f.Type() == "tracked_doc" {
			if n, ok := f.Frontmatter["latest"].(int); ok {
				for v := 1; v <= n; v++ {
					if _, ok := byName[strings.ToLower(fmt.Sprintf("%s-v%d.source", f.Name, v))]; !ok {
						out = append(out, Finding{Path: f.Path, Code: CodeNoTranscript, Severity: Error,
							Msg: fmt.Sprintf("no version %d: %s-v%d.source.md is missing; a tracked document keeps every version", v, f.Name, v)})
					}
				}
			}
		}
		if _, ok := f.Frontmatter["version"].(int); ok && f.Type() == "transcript" {
			if m := linkValRe.FindStringSubmatch(str(f.Frontmatter["of"])); m != nil {
				if dst, ok := byName[strings.ToLower(m[1])]; ok && dst.Type() != "tracked_doc" && dst.Type() != "area" {
					out = append(out, Finding{Path: f.Path, Code: CodeFieldValue, Severity: Error,
						Msg: fmt.Sprintf("a transcript with version is a version of a tracked document or an area; %s is a %s", m[1], dst.Type())})
				}
			}
		}
		src, _ := f.Frontmatter["source"].(map[string]any)
		if fl, ok := f.Frontmatter["follows"].(map[string]any); ok && f.Type() == "area" {
			src, _ = fl["source"].(map[string]any)
		}
		if f.Type() == "policy" || f.Type() == "area" {
			if src != nil && src["fidelity"] != "none" {
				if _, ok := byName[strings.ToLower(f.Name+".source")]; !ok {
					out = append(out, Finding{Path: f.Path, Code: CodeNoTranscript, Severity: Error,
						Msg: fmt.Sprintf("no transcript %s.source.md; set source.fidelity to none if the document must not be copied", f.Name)})
				}
			}
		}
	}
	out = append(out, checkViaSources(files, byName)...)
	return out
}

// checkScope: a question is scoped to a goal, a project, a task, or an area, or to
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
	case dst.Type() != "goal" && dst.Type() != "project" && dst.Type() != "task" && dst.Type() != "area":
		return []Finding{{Path: f.Path, Code: CodeScope, Severity: Error,
			Msg: fmt.Sprintf("scope %s is a %s: want a goal, a project, a task, an area, or root", scope, dst.Type())}}
	}
	return nil
}

// checkParent: the parent of a task, an obligation, or an rfx is a task, an
// obligation, or an older meeting.
func checkParent(f *File, byName map[string]*File) []Finding {
	switch f.Type() {
	case "task", "obligation", "rfx":
	default:
		return nil
	}
	v := f.Frontmatter["parent"]
	dst := linkTarget(v, byName)
	if dst == nil {
		return nil
	}
	switch dst.Type() {
	case "task", "obligation", "meeting":
		return nil
	}
	return []Finding{{Path: f.Path, Code: CodeFieldValue, Severity: Error,
		Msg: fmt.Sprintf("parent %s is a %s: want a task or an obligation", str(v), dst.Type())}}
}

// linkTarget returns the file a frontmatter link names, or nil when the value
// is not a link or names no file (link-unresolved reports that).
func linkTarget(v any, byName map[string]*File) *File {
	m := linkValRe.FindStringSubmatch(str(v))
	if m == nil {
		return nil
	}
	return byName[strings.ToLower(m[1])]
}

// areaKind is an area's kind; a missing kind reads as project.
func areaKind(f *File) string {
	if k := str(f.Frontmatter["kind"]); k != "" {
		return k
	}
	return "project"
}

// checkAreaKinds: a goal's area is an area of kind project, and every entry of
// concepts, on a goal, a project, or a project area, is an area of kind
// concept.
func checkAreaKinds(f *File, byName map[string]*File) []Finding {
	typ := f.Type()
	if typ != "goal" && typ != "project" && typ != "area" {
		return nil
	}
	var out []Finding
	bad := func(format string, a ...any) {
		out = append(out, Finding{Path: f.Path, Code: CodeFieldValue, Severity: Error, Msg: fmt.Sprintf(format, a...)})
	}
	if typ == "goal" {
		if v := f.Frontmatter["area"]; v != nil {
			switch dst := linkTarget(v, byName); {
			case dst == nil:
			case dst.Type() != "area":
				bad("area %s is a %s: want an area of kind project", str(v), dst.Type())
			case areaKind(dst) != "project":
				bad("area %s is a concept area: a goal's home is an area of kind project; list it in concepts", str(v))
			}
		}
	}
	list, _ := f.Frontmatter["concepts"].([]any)
	for _, v := range list {
		switch dst := linkTarget(v, byName); {
		case dst == nil:
		case dst.Type() != "area":
			bad("concepts: %s is a %s: want an area of kind concept", str(v), dst.Type())
		case areaKind(dst) != "concept":
			bad("concepts: %s is an area of kind %s: want kind concept", str(v), areaKind(dst))
		}
	}
	return out
}

// typedLinkTypes are the types whose goal: and project: keys link a document
// of that type.
var typedLinkTypes = map[string]bool{"task": true, "note": true, "obligation": true, "rfx": true, "tracked_doc": true, "goal": true}

// checkTypedLinks: goal: names a goal and project: names a project (format
// 11; below it the vault reads the old project: key as goal: before lint).
func checkTypedLinks(f *File, byName map[string]*File) []Finding {
	if !typedLinkTypes[f.Type()] {
		return nil
	}
	var out []Finding
	for _, key := range []string{"goal", "project"} {
		v := f.Frontmatter[key]
		if v == nil || key == "goal" && f.Type() == "goal" {
			continue
		}
		if dst := linkTarget(v, byName); dst != nil && dst.Type() != key {
			out = append(out, Finding{Path: f.Path, Code: CodeFieldValue, Severity: Error,
				Msg: fmt.Sprintf("%s %s is a %s: want a %s", key, str(v), dst.Type(), key)})
		}
	}
	return out
}

// checkRefines: a convention refines a convention, a constraint refines a
// constraint, and the refines chain has no cycle.
func checkRefines(f *File, byName map[string]*File) []Finding {
	typ := f.Type()
	if (typ != "convention" && typ != "constraint") || f.Frontmatter["refines"] == nil {
		return nil
	}
	v := f.Frontmatter["refines"]
	dst := linkTarget(v, byName)
	if dst == nil {
		return nil
	}
	if dst.Type() != typ {
		return []Finding{{Path: f.Path, Code: CodeFieldValue, Severity: Error,
			Msg: fmt.Sprintf("refines %s is a %s: want a %s", str(v), dst.Type(), typ)}}
	}
	seen := map[*File]bool{f: true}
	for cur := dst; cur != nil; cur = linkTarget(cur.Frontmatter["refines"], byName) {
		if seen[cur] {
			return []Finding{{Path: f.Path, Code: CodeFieldValue, Severity: Error,
				Msg: fmt.Sprintf("refines %s: the refines chain is a cycle", str(v))}}
		}
		seen[cur] = true
	}
	return nil
}

// FormerIDs maps each former id (lower case) that a file lists in former_ids
// to that file. The first file wins when two list the same id.
func FormerIDs(files []*File) map[string]*File {
	out := map[string]*File{}
	for _, f := range files {
		list, _ := f.Frontmatter["former_ids"].([]any)
		for _, v := range list {
			id := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
			if _, dup := out[id]; id != "" && !dup {
				out[id] = f
			}
		}
	}
	return out
}
