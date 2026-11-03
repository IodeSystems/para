// Package changes holds the release notes embedded in the binary: one file per
// version, changes/<version>.md, with two sections. People lists what changed
// for the owner. Agents lists new tools, changed rules, and breaking changes.
// Both are written in plain words and committed with the release.
package changes

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

//go:embed *.md
var files embed.FS

// Release is the notes of one version.
type Release struct {
	Version string   `json:"version"`
	People  []string `json:"people"`
	Agents  []string `json:"agents"`
}

var versionRe = regexp.MustCompile(`^\d+(\.\d+){1,2}$`)

// Compare orders two dotted versions: -1, 0, or 1. A missing part is 0.
func Compare(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := part(pa, i), part(pb, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func part(p []string, i int) int {
	if i >= len(p) {
		return 0
	}
	n, _ := strconv.Atoi(p[i])
	return n
}

// Parse reads the notes of one version. It wants "## People" and "## Agents"
// sections of "- " bullets.
func Parse(version string, data []byte) (Release, error) {
	if !versionRe.MatchString(version) {
		return Release{}, fmt.Errorf("changes: parse: %q is not a version like 0.1.0", version)
	}
	r := Release{Version: version, People: []string{}, Agents: []string{}}
	var sect *[]string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "## "):
			switch strings.TrimSpace(t[3:]) {
			case "People":
				sect, seen["People"] = &r.People, true
			case "Agents":
				sect, seen["Agents"] = &r.Agents, true
			default:
				return r, fmt.Errorf("changes: parse: %s: section %q: want People or Agents", version, t[3:])
			}
		case strings.HasPrefix(t, "- "):
			if sect == nil {
				return r, fmt.Errorf("changes: parse: %s: a bullet before a section", version)
			}
			*sect = append(*sect, strings.TrimSpace(t[2:]))
		case t != "" && !strings.HasPrefix(t, "#") && sect != nil && len(*sect) > 0:
			// A continued line belongs to the last bullet.
			(*sect)[len(*sect)-1] += " " + t
		}
	}
	if !seen["People"] || !seen["Agents"] {
		return r, fmt.Errorf("changes: parse: %s: want both a People and an Agents section", version)
	}
	return r, nil
}

// All returns every release, oldest first.
func All() ([]Release, error) {
	ents, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("changes: all: %w", err)
	}
	var out []Release
	for _, e := range ents {
		data, err := fs.ReadFile(files, e.Name())
		if err != nil {
			return nil, fmt.Errorf("changes: all: %w", err)
		}
		r, err := Parse(strings.TrimSuffix(e.Name(), ".md"), data)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Release) int { return Compare(a.Version, b.Version) })
	return out, nil
}

// Latest is the newest version with notes, or "" when there are none.
func Latest() string {
	all, err := All()
	if err != nil || len(all) == 0 {
		return ""
	}
	return all[len(all)-1].Version
}

// Since returns the releases newer than version, oldest first. An empty
// version returns only the newest release.
func Since(version string) ([]Release, error) {
	all, err := All()
	if err != nil {
		return nil, err
	}
	if version == "" {
		if len(all) > 0 {
			all = all[len(all)-1:]
		}
		return all, nil
	}
	if !versionRe.MatchString(version) {
		return nil, fmt.Errorf("changes: since: %q is not a version like 0.1.0", version)
	}
	var out []Release
	for _, r := range all {
		if Compare(r.Version, version) > 0 {
			out = append(out, r)
		}
	}
	return out, nil
}

// Render prints releases as text. audience is "people", "agents", or "" for both.
func Render(rs []Release, audience string) string {
	var b strings.Builder
	for _, r := range rs {
		fmt.Fprintf(&b, "%s\n", r.Version)
		if audience != "agents" {
			section(&b, "People", r.People)
		}
		if audience != "people" {
			section(&b, "Agents", r.Agents)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func section(b *strings.Builder, name string, items []string) {
	fmt.Fprintf(b, "  %s\n", name)
	if len(items) == 0 {
		b.WriteString("    nothing\n")
	}
	for _, it := range items {
		fmt.Fprintf(b, "    - %s\n", it)
	}
}
