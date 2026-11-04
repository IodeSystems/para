package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newImportCmd is `para import`: reads about an import. The import itself is
// agent work (`/para import`); these commands check it. The CLI may read local
// files: it hashes them here and passes path and hash to the op, which reads
// nothing outside the vault.
func newImportCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "import", Short: "Check an import from files: what was imported, what is missing or changed", Args: cobra.NoArgs}
	var exts []string
	cmd.AddCommand(
		opCommand(l, ops.ImportVerify, binding[ops.ImportVerifyInput, ops.ImportVerifyOutput]{
			Use:  "verify <dir> [--ext md]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, _ *ops.ImportVerifyInput) {
				fs.StringSliceVar(&exts, "ext", []string{"md"}, "file extensions to check (repeat or comma-separate); \"*\" checks every file")
			},
			Bind: func(args []string, in *ops.ImportVerifyInput) error {
				root, files, err := hashTree(args[0], exts)
				if err != nil {
					return err
				}
				in.Root, in.Files = root, files
				return nil
			},
			Text: func(w io.Writer, out ops.ImportVerifyOutput) error {
				p := &printer{w: w}
				p.f("checked %d: %d imported, %d missing, %d changed, %d extra\n", out.Checked, out.Imported, out.Missing, out.Changed, len(out.Extra))
				for _, f := range out.Files {
					switch f.Status {
					case ops.ImportImported:
						p.f("%s\t%s\t%s\n", f.Status, f.Path, strings.Join(f.Docs, ","))
					case ops.ImportChanged:
						p.f("%s\t%s\t%s\twas %s\n", f.Status, f.Path, strings.Join(f.Docs, ","), f.StoredHash)
					default:
						p.f("%s\t%s\n", f.Status, f.Path)
					}
					if f.Status == ops.ImportImported && f.MatchedBy == "hash" {
						p.f("  matched by hash, not path\n")
					}
				}
				for _, x := range out.Extra {
					p.f("extra\t%s\t%s\t%s\n", x.Path, x.ID, x.Title)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ImportList, binding[ops.ImportListInput, ops.ImportListOutput]{
			Use:  "list [--root <path>]",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ImportListInput) {
				fs.StringVar(&in.Root, "root", "", "only documents imported from this path or under it")
			},
			Text: func(w io.Writer, out ops.ImportListOutput) error {
				p := &printer{w: w}
				for _, x := range out.Docs {
					p.f("%s\t%s\t%s\t%s\n", x.Path, x.ID, x.Type, x.Title)
				}
				return p.err
			},
		}),
	)
	return cmd
}

// hashTree lists the files under dir with one of exts, as absolute paths with
// the sha256 of each. Hidden files and folders (.git, .obsidian) are skipped.
func hashTree(dir string, exts []string) (string, []ops.ImportFile, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, fmt.Errorf("import: verify: %w", err)
	}
	st, err := os.Stat(root)
	if err != nil {
		return "", nil, fmt.Errorf("import: verify: %w", err)
	}
	if !st.IsDir() {
		return "", nil, fmt.Errorf("import: verify: %s is not a folder", dir)
	}
	all := slices.Contains(exts, "*")
	want := map[string]bool{}
	for _, e := range exts {
		want["."+strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))] = true
	}
	var files []ops.ImportFile
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if !all && !want[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		files = append(files, ops.ImportFile{Path: filepath.ToSlash(p), Hash: "sha256:" + hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("import: verify: %w", err)
	}
	if len(files) == 0 {
		return "", nil, errors.New("import: verify: no files with those extensions under " + dir)
	}
	return filepath.ToSlash(root), files, nil
}
