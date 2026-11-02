package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/guide"
	"github.com/iodesystems/parasolve/internal/importer"
	"github.com/iodesystems/parasolve/internal/vault"
)

// importLines caps the report lines printed as text; --json has them all.
const importLines = 40

// newImportCmd is `para import scan|run|undo`. It is not an op: the commands
// take a filesystem path, and an op reaches agent surfaces, where a path would
// let hostile text read any local file into a vault.
func newImportCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a folder of markdown, or the legacy PARA vault (scan, run, undo)",
		Long:  guide.ImportCorpus(),
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newImportScanCmd(), newImportRunCmd(l), newImportUndoCmd(l))
	return cmd
}

// scanOutput is `import scan --json`: the report, then the plan file path.
type scanOutput struct {
	*importer.Report
	PlanFile string `json:"plan_file"`
}

func newImportScanCmd() *cobra.Command {
	var adapter, planPath string
	var force bool
	cmd := &cobra.Command{
		Use:   "scan <dir>",
		Short: "Inventory a folder and write a plan; writes nothing to the vault",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().StringVar(&adapter, "adapter", "", "markdown or legacy (default: detect)")
	cmd.Flags().StringVar(&planPath, "plan", "import-plan.yaml", "where to write the plan")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing plan file")
	addJSONFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		rep, plan, err := importer.Scan(args[0], adapter)
		if err != nil {
			return err
		}
		if err := importer.WritePlan(planPath, plan, force); err != nil {
			return err
		}
		return emit(cmd, scanOutput{rep, planPath}, func(w io.Writer, o scanOutput) error { return printScan(w, o) })
	}
	return cmd
}

func printScan(w io.Writer, o scanOutput) error {
	p := &printer{w: w}
	r := o.Report
	p.f("adapter\t%s (%s)\nsource\t%s\n", r.Adapter, r.Detected, r.Dir)
	p.f("files\t%d\ndocuments\t%d\n", r.Files, r.Documents)
	printCounts(p, "types", r.ByType)
	printCounts(p, "destination folders", r.ByFolder)
	printCounts(p, "source folders", r.SourceFolders)
	printCounts(p, "frontmatter keys", r.FrontmatterKeys)
	printCounts(p, "normalized", r.Normalized)
	if f := r.Fold; f != nil {
		p.f("fold\t%d lines, %d items\n", f.Lines, f.Items)
		printCounts(p, "  items by type", f.ItemsByType)
		printCounts(p, "  tasks by status", f.TasksByStatus)
		printCounts(p, "  deadlines by status", f.DeadlinesByStatus)
		printCounts(p, "  questions by status", f.QuestionsByStatus)
		p.f("  open questions\t%d\n  contacts\t%d\n  depends edges\t%d\n", f.OpenQuestions, f.Contacts, f.DependsEdges)
		printIssues(p, "  bad lines", f.BadLines)
	}
	p.f("duplicates\t%d group(s)\n", len(r.Duplicates))
	for i, g := range r.Duplicates {
		if i == importLines {
			p.f("  ... %d more (--json)\n", len(r.Duplicates)-i)
			break
		}
		p.f("  %v\n", g)
	}
	printIssues(p, "cannot map", r.Unmapped)
	printIssues(p, "refused (fails lint)", r.Refused)
	p.f("plan\t%s\n", o.PlanFile)
	return p.err
}

func printCounts(p *printer, name string, m map[string]int) {
	if len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	p.f("%s\n", name)
	for _, k := range keys {
		p.f("  %-28s %d\n", k, m[k])
	}
}

func printIssues(p *printer, name string, is []importer.Issue) {
	if len(is) == 0 {
		return
	}
	p.f("%s\t%d\n", name, len(is))
	for i, x := range is {
		if i == importLines {
			p.f("  ... %d more (--json)\n", len(is)-i)
			break
		}
		p.f("  %s\t%s\n", x.Path, x.Reason)
	}
}

func newImportRunCmd(l *loader) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "run <plan>",
		Short: "Import the plan as one batch (--dry-run changes nothing)",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().BoolVar(&dry, "dry-run", false, "build and check every document; write nothing")
	addJSONFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := l.load(); err != nil {
			return err
		}
		plan, err := importer.LoadPlan(args[0])
		if err != nil {
			return err
		}
		w, err := l.importWriter()
		if err != nil {
			return err
		}
		res, err := importer.Run(cmd.Context(), w, plan, importer.RunOptions{DryRun: dry})
		if err != nil {
			return err
		}
		return emit(cmd, res, printRun)
	}
	return cmd
}

func (l *loader) importWriter() (importer.Writer, error) {
	deps, err := l.deps()
	if err != nil {
		return nil, err
	}
	w, ok := deps.Store.(importer.Writer)
	if !ok {
		return nil, errors.New("import: this vault store cannot take a batch")
	}
	return w, nil
}

func printRun(w io.Writer, r *importer.RunResult) error {
	p := &printer{w: w}
	mode := "batch"
	if r.DryRun {
		mode = "dry run (nothing written), batch id would be"
	}
	p.f("%s\t%s\n", mode, r.Batch)
	p.f("entries\t%d\ncreated\t%d\nalready imported\t%d\nsource changed since import\t%d\nskipped\t%d\nduplicates\t%d\nrefused\t%d\nmilliseconds\t%d\n",
		r.Entries, r.Created, r.Already, r.SourceChanged, r.Skipped, r.Duplicates, r.Refused, r.Millis)
	printCounts(p, "created by type", r.ByType)
	printCounts(p, "normalized", r.Normalized)
	printIssues(p, "report", r.Issues)
	return p.err
}

func newImportUndoCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "undo <batch>",
		Short: "Remove the documents of an import batch, unless one changed since",
		Args:  cobra.ExactArgs(1),
	}
	addJSONFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := l.load(); err != nil {
			return err
		}
		w, err := l.importWriter()
		if err != nil {
			return err
		}
		res, err := importer.Undo(cmd.Context(), w, args[0])
		if err != nil && !errors.Is(err, vault.ErrBatchChanged) {
			return err
		}
		if jerr := emit(cmd, res, func(w io.Writer, r vault.RemoveResult) error {
			_, e := fmt.Fprintf(w, "removed\t%d\nmissing\t%d\nchanged\t%d\n", len(r.Removed), len(r.Missing), len(r.Changed))
			for _, id := range r.Changed {
				if e == nil {
					_, e = fmt.Fprintf(w, "  changed since import: %s\n", id)
				}
			}
			return e
		}); jerr != nil {
			return jerr
		}
		if err != nil {
			return fmt.Errorf("import undo: %w: nothing was removed", vault.ErrBatchChanged)
		}
		return nil
	}
	return cmd
}
