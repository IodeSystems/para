// Package syncname recognises the file names that file-sync tools and merge
// tools give to a losing copy of a file. Para has no sync feature; it only
// refuses to treat such a copy as a document and reports it.
package syncname

import (
	"regexp"
)

// Code is the lint finding code for a conflict copy.
const Code = "sync-conflict"

// patterns is the whole table. Each matches the base name (no directory).
var patterns = []*regexp.Regexp{
	regexp.MustCompile(` \(\d+\)\.[^.]+$`),        // "note (1).md"            browser/OS copies, Drive
	regexp.MustCompile(` \([^)]*conflicted copy`), // "note (conflicted copy", "(Ann's conflicted copy 2026-11-01)"  Dropbox
	regexp.MustCompile(`\.conflict\d+(\.|$)`),     // "note.conflict1.md", "note.md.conflict2"
	regexp.MustCompile(`\.sync-conflict-`),        // "note.sync-conflict-20261101-101500-ABC.md"  Syncthing
	regexp.MustCompile(`\.orig(\.|$)`),            // "note.md.orig"           merge tools
}

// IsConflict reports whether base (a file name, not a path) is a conflict
// copy by the table.
func IsConflict(base string) bool {
	for _, p := range patterns {
		if p.MatchString(base) {
			return true
		}
	}
	return false
}
