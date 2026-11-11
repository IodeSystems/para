// mkmanifest writes a release's para-manifest.json (make release): one entry
// per archive in -dist (para_<os>_<arch>.tar.gz, sha256 and size computed
// here, URL -url/<archive>), product para, and a summary: -summary, or the
// first sentence of the People section of -changes (changes/<X.Y.Z>.md). It
// signs nothing and never writes key_next: make release runs the minisign CLI
// on its output.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iodesystems/para/changes"
	"github.com/iodesystems/para/internal/update"
)

const product = "para"

func main() {
	version := flag.String("version", "", "the release version, vX.Y.Z")
	dist := flag.String("dist", "", "the directory holding para_<os>_<arch>.tar.gz")
	base := flag.String("url", "", "the release's download URL; each archive is <url>/<archive>")
	notes := flag.String("changes", "", "the release notes, changes/<X.Y.Z>.md, for the summary")
	summary := flag.String("summary", "", "one line on what changed; default the first sentence of the notes' People section")
	out := flag.String("o", "", "where to write the manifest")
	flag.Parse()
	if err := run(*version, *dist, *base, *notes, *summary, *out, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "mkmanifest:", err)
		os.Exit(1)
	}
}

func run(version, dist, base, notes, summary, out string, now time.Time) error {
	if _, err := update.ParseVersion(version); err != nil {
		return err
	}
	if dist == "" || base == "" || out == "" {
		return errors.New("-dist, -url and -o are required")
	}
	if summary == "" {
		s, err := notesSummary(version, notes)
		if err != nil {
			return err
		}
		summary = s
	}
	m := update.Manifest{
		Product: product, Version: version, Summary: summary,
		Published: now.UTC().Format(time.RFC3339), Platforms: map[string]update.File{},
	}
	archives, _ := filepath.Glob(filepath.Join(dist, product+"_*_*.tar.gz"))
	if len(archives) == 0 {
		return fmt.Errorf("no %s_<os>_<arch>.tar.gz in %s", product, dist)
	}
	for _, a := range archives {
		name := filepath.Base(a)
		plat := strings.TrimSuffix(strings.TrimPrefix(name, product+"_"), ".tar.gz")
		sum, size, err := hashFile(a)
		if err != nil {
			return err
		}
		m.Platforms[plat] = update.File{URL: strings.TrimSuffix(base, "/") + "/" + name, SHA256: sum, Size: size}
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if _, err := update.ParseManifest(body); err != nil { // what clients will read
		return err
	}
	return os.WriteFile(out, append(body, '\n'), 0o644)
}

// notesSummary is the first sentence of the first People bullet of the notes
// of version (vX.Y.Z reads X.Y.Z's notes).
func notesSummary(version, notes string) (string, error) {
	if notes == "" {
		return "", errors.New("-changes or -summary is required")
	}
	b, err := os.ReadFile(notes)
	if err != nil {
		return "", err
	}
	core, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), "-")
	r, err := changes.Parse(core, b)
	if err != nil {
		return "", err
	}
	if len(r.People) == 0 {
		return "", fmt.Errorf("%s: no People bullet for the summary (or set -summary)", notes)
	}
	return firstSentence(r.People[0]), nil
}

// firstSentence is s up to the first ". " (or all of s).
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
