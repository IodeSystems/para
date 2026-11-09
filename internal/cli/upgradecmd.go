package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/changes"
	"github.com/iodesystems/para/internal/mcpserver"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/upgrade"
)

// goInstallLine is what to run when para was not built from a checkout.
const goInstallLine = "go install github.com/iodesystems/para/cmd/para@latest"

func newChangesCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Changes, binding[ops.ChangesInput, ops.ChangesOutput]{
		Use:  "changes [--since <version>]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.ChangesInput) {
			fs.StringVar(&in.Since, "since", "", "show the notes of every version after this one; default the newest only")
		},
		Text: func(w io.Writer, out ops.ChangesOutput) error {
			_, err := io.WriteString(w, out.Text)
			return err
		},
	})
}

// newVaultUpgradeCmd is `para vault upgrade`: the person-only op, with
// `status` as the read-only subcommand.
func newVaultUpgradeCmd(l *loader) *cobra.Command {
	cmd := opCommand(l, ops.VaultUpgrade, binding[ops.VaultUpgradeInput, ops.VaultUpgradeOutput]{
		Use:  "upgrade [--dry-run | --file | --undo <batch>] [--force]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.VaultUpgradeInput) {
			fs.BoolVar(&in.DryRun, "dry-run", false, "list the documents each automated step would rewrite and change nothing")
			fs.BoolVar(&in.File, "file", false, "queue each pending judgement step as agent work; runs no automated step")
			fs.StringVar(&in.Undo, "undo", "", "restore the documents of a batch id printed by an earlier run")
			fs.BoolVar(&in.Force, "force", false, "run while another install seen in 14 days runs an older para; that machine then refuses the vault until it is upgraded")
		},
		Text: func(w io.Writer, o ops.VaultUpgradeOutput) error { return writeVaultUpgrade(w, l.vaultName(), o) },
	})
	cmd.Short = "Bring this vault to the current format: run pending automated steps as one undoable batch each"
	cmd.Long = `Bring this vault to the format of this para. format_version in
.para/vault.properties says which steps are done.

An automated step rewrites documents through the vault: one batch, one event per
document. Use --dry-run first. --undo <batch> restores the files on the machine
that ran the batch. Upgrade para on every machine of the vault first: a run
refuses while an install seen in 14 days runs an older para (--force runs it). A judgement step needs an agent: --file queues each pending
one as agent work and changes no document. "upgrade status" lists what is pending.

The last line is the verdict: the format before and after, the batch ids, lint
errors and warnings, and the judgement steps still pending. A run that stops at
a step, or lint errors after a run, exit non-zero. After a run, lint findings
are filed as one agent work request (kind vault-check, as para lint --file);
run /para loop to work it.`
	cmd.AddCommand(opCommand(l, ops.VaultUpgradeStatus, binding[ops.VaultUpgradeStatusInput, upgrade.Status]{
		Use:  "status",
		Args: cobra.NoArgs,
		Text: func(w io.Writer, s upgrade.Status) error {
			if err := writeUpgradeStatus(w, s); err != nil {
				return err
			}
			return statusVerdict(w, l.vaultName(), s)
		},
	}))
	return cmd
}

// newDossierCmd is `para dossier refile`: the judgement part of vault
// upgrade step dossiers.
func newDossierCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "dossier", Short: "Old dossiers: record what became of a staged change (vault upgrade step dossiers)", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.DossierRefile, binding[ops.DossierRefileInput, ops.DossierRefileOutput]{
		Use:  "refile <dossier> <item> (--result <id> | --dropped <reason>)",
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.DossierRefileInput) {
			fs.StringVar(&in.Result, "result", "", "the id the direct write made or changed, or its receipt")
			fs.StringVar(&in.Dropped, "dropped", "", "why the staged change is not needed")
		},
		Bind: func(args []string, in *ops.DossierRefileInput) error {
			in.Proposal, in.Item = args[0], args[1]
			return nil
		},
		Text: func(w io.Writer, out ops.DossierRefileOutput) error {
			_, err := fmt.Fprintf(w, "%s %s: %s; dossier %s, %d staged left\n", out.Proposal, out.Item, out.State, out.Status, out.Left)
			return err
		},
	}))
	return cmd
}

func writeUpgradeStatus(w io.Writer, s upgrade.Status) error {
	if len(s.Pending) == 0 {
		_, err := fmt.Fprintf(w, "format %d of %d: nothing pending\n", s.FormatVersion, s.Latest)
		return err
	}
	if _, err := fmt.Fprintf(w, "format %d of %d: %d pending\n", s.FormatVersion, s.Latest, len(s.Pending)); err != nil {
		return err
	}
	for _, p := range s.Pending {
		how := "judgement: agent work (para vault upgrade --file) titled \"Vault upgrade: " + p.Title + "\""
		if p.Auto {
			how = "automated: para vault upgrade --dry-run, then para vault upgrade"
		}
		if _, err := fmt.Fprintf(w, "  %s\t%d documents\t%s\n", p.ID, p.Count, how); err != nil {
			return err
		}
	}
	return nil
}

func writeVaultUpgrade(w io.Writer, vault string, o ops.VaultUpgradeOutput) error {
	switch {
	case o.Undo != nil:
		if _, err := fmt.Fprintf(w, "restored %d documents from %s\n", len(o.Undo.Docs), o.Undo.Batch); err != nil {
			return err
		}
		for _, id := range o.Undo.Skipped {
			if _, err := fmt.Fprintf(w, "skipped %s: edited since the batch\n", id); err != nil {
				return err
			}
		}
	case o.Run != nil:
		for _, s := range o.Run.Stale {
			what := "a para that logs no version"
			if s.Format > 0 {
				what = fmt.Sprintf("para %s (format %d)", s.Version, s.Format)
			}
			if _, err := fmt.Fprintf(w, "install %s (seen %s) runs %s: upgrade para there first\n", s.Install, s.LastSeen, what); err != nil {
				return err
			}
		}
		verb := "rewrote"
		if o.Run.DryRun {
			verb = "would rewrite"
		}
		for _, s := range o.Run.Steps {
			if _, err := fmt.Fprintf(w, "%s: %s %d documents\n", s.ID, verb, s.Count); err != nil {
				return err
			}
			for _, id := range s.Docs {
				if _, err := fmt.Fprintf(w, "  %s\n", id); err != nil {
					return err
				}
			}
			for _, p := range s.Plan {
				if _, err := fmt.Fprintf(w, "  plan %s:\n", p.Doc); err != nil {
					return err
				}
				for _, ln := range p.Lines {
					if _, err := fmt.Fprintf(w, "    %s\n", ln); err != nil {
						return err
					}
				}
			}
			if s.Batch != "" {
				if _, err := fmt.Fprintf(w, "  undo: para vault upgrade --undo %s\n", s.Batch); err != nil {
					return err
				}
			}
		}
		if len(o.Run.Steps) == 0 {
			if _, err := fmt.Fprintln(w, "no automated step is pending"); err != nil {
				return err
			}
		}
	}
	for _, id := range o.Filed {
		if _, err := fmt.Fprintf(w, "filed agent work %s\n", id); err != nil {
			return err
		}
	}
	if err := writeUpgradeStatus(w, o.Status); err != nil {
		return err
	}
	return upgradeVerdict(w, vault, o)
}

// vaultName is the id of the selected vault, for a verdict line.
func (l *loader) vaultName() string {
	if sel, err := l.selectVault(); err == nil {
		return sel.ID
	}
	return "vault"
}

// pendingParts says which steps are still pending and the command for each
// kind. queue is false when the judgement steps were just filed.
func pendingParts(s upgrade.Status, queue bool) []string {
	var auto, judge []string
	for _, p := range s.Pending {
		list := &judge
		if p.Auto {
			list = &auto
		}
		if !slices.Contains(*list, p.ID) {
			*list = append(*list, p.ID)
		}
	}
	var parts []string
	if len(auto) > 0 {
		parts = append(parts, "automated steps pending: "+strings.Join(auto, ", ")+" (para vault upgrade --dry-run, then para vault upgrade)")
	}
	if len(judge) > 0 {
		how := ""
		if queue {
			how = " (queue them: para vault upgrade --file)"
		}
		parts = append(parts, "judgement steps pending: "+strings.Join(judge, ", ")+how)
	}
	return parts
}

// statusVerdict is the last line of vault upgrade status.
func statusVerdict(w io.Writer, vault string, s upgrade.Status) error {
	head := fmt.Sprintf("%s is at format %d of %d", vault, s.FormatVersion, s.Latest)
	parts := pendingParts(s, true)
	switch {
	case len(parts) == 0 && s.FormatVersion < s.Latest:
		return verdict(w, toneWarn, "%s, no document to change — run para vault upgrade to raise the format", head)
	case len(parts) == 0:
		return verdict(w, toneOK, "%s, nothing pending", head)
	}
	return verdict(w, toneWarn, "%s — %s", head, strings.Join(parts, "; "))
}

// upgradeVerdict is the last line of vault upgrade: what happened, the format
// before and after, the batches, lint after a run, and what is still pending.
// It is ✗ exactly when the command exits non-zero (ops.VaultUpgradeOutput.Failed).
func upgradeVerdict(w io.Writer, vault string, o ops.VaultUpgradeOutput) error {
	st := o.Status
	t := toneOK
	format := fmt.Sprintf("format %d of %d", st.FormatVersion, st.Latest)
	if o.From != st.FormatVersion {
		format = fmt.Sprintf("format %d -> %d of %d", o.From, st.FormatVersion, st.Latest)
	}
	parts := []string{format}
	var head string
	switch {
	case o.Stopped != nil && o.Stopped.Step != "":
		t = toneErr
		head = vault + " upgrade stopped at step " + o.Stopped.Step
		if o.Stopped.Errors > 0 {
			head += fmt.Sprintf(": %d errors", o.Stopped.Errors)
		}
	case o.Stopped != nil:
		t = toneErr
		head = vault + " upgrade refused: an install runs an older para (listed above); nothing changed"
	case o.Undo != nil:
		head = fmt.Sprintf("%s restored %d documents from %s", vault, len(o.Undo.Docs), o.Undo.Batch)
		if len(o.Undo.Skipped) > 0 {
			t = toneWarn
			head += fmt.Sprintf(", skipped %d edited since", len(o.Undo.Skipped))
		}
	case o.Run != nil && o.Run.DryRun:
		n := 0
		for _, s := range o.Run.Steps {
			n += s.Count
		}
		head = fmt.Sprintf("%s dry run: would rewrite %d documents in %d steps; nothing changed", vault, n, len(o.Run.Steps))
		if len(o.Run.Stale) > 0 {
			t = toneWarn
			parts = append(parts, "a run refuses until the installs above run this para")
		}
	case o.Run != nil:
		head = vault + " upgraded"
	default:
		head = vault + " filed no agent work: each pending judgement step already has open work, or none is pending"
		if len(o.Filed) > 0 {
			head = fmt.Sprintf("%s filed %d judgement steps as agent work (%s); run /para loop", vault, len(o.Filed), strings.Join(o.Filed, ", "))
		}
	}
	if o.Run != nil && !o.Run.DryRun {
		var batches []string
		for _, s := range o.Run.Steps {
			if s.Batch != "" {
				batches = append(batches, s.Batch)
			}
		}
		if len(batches) > 0 {
			parts = append(parts, "batches "+strings.Join(batches, ", "))
		} else if o.Stopped == nil {
			parts = append(parts, "no automated step ran")
		}
	}
	if o.Lint != nil {
		parts = append(parts, fmt.Sprintf("lint: %d errors, %d warnings", o.Lint.Errors, o.Lint.Warnings))
		t = max(t, countTone(o.Lint.Errors, o.Lint.Warnings))
	}
	if pend := pendingParts(st, o.Run != nil || o.Undo != nil); len(pend) > 0 {
		parts = append(parts, pend...)
		t = max(t, toneWarn)
	}
	if o.Stopped != nil && o.Stopped.Step != "" {
		parts = append(parts, "run para vault upgrade status")
	}
	if o.Check != "" {
		parts = append(parts, "findings filed as agent work "+o.Check+"; run /para loop")
	}
	return verdict(w, t, "%s — %s", head, strings.Join(parts, "; "))
}

// upgradeOptions are the pieces of `para upgrade` a test replaces: the
// checkout, the programs, and the new binary.
type upgradeOptions struct {
	Src        string // the checkout; empty means not built from one
	Git, Make  string // program names or paths
	NewBin     string // the binary make install produced; empty means this one
	Version    string // the running binary's version
	OldRelease string // the newest release notes in the running binary
	Home       string // install home passed to the new binary
	VaultIDs   []string
}

// runUpgrade pulls a checkout fast-forward only, installs it, and reports what
// changed. It never commits and never pushes, and it refuses a dirty tree.
func runUpgrade(ctx context.Context, w io.Writer, o upgradeOptions) error {
	if o.Src == "" {
		return printGoInstall(w)
	}
	if _, err := os.Stat(filepath.Join(o.Src, ".git")); err != nil {
		return printGoInstall(w)
	}
	git := or(o.Git, "git")
	out, err := runIn(ctx, o.Src, git, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return fmt.Errorf("upgrade: git status: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("upgrade: %s has uncommitted changes to tracked files: commit or stash them, then run para upgrade again", o.Src)
	}
	if err := stream(ctx, w, o.Src, git, "pull", "--ff-only"); err != nil {
		return fmt.Errorf("upgrade: git pull --ff-only: %w", err)
	}
	desc, err := runIn(ctx, o.Src, git, "describe", "--always", "--dirty", "--tags")
	if err != nil {
		return fmt.Errorf("upgrade: git describe: %w", err)
	}
	newVersion := strings.TrimSpace(desc)
	if newVersion == o.Version {
		_, err := fmt.Fprintf(w, "para is current: %s\n", o.Version)
		return err
	}
	if err := stream(ctx, w, o.Src, or(o.Make, "make"), "install"); err != nil {
		return fmt.Errorf("upgrade: make install: %w", err)
	}
	if _, err := fmt.Fprintf(w, "\nupgraded para: %s -> %s\n", o.Version, newVersion); err != nil {
		return err
	}
	bin := o.NewBin
	if bin == "" {
		if bin = mcpserver.ExecutablePath(); bin == "" {
			return nil
		}
	}
	notes, err := runIn(ctx, o.Src, bin, "changes", "--since", o.OldRelease)
	if err != nil {
		_, _ = fmt.Fprintf(w, "could not read the changes from %s: %v\n", bin, err)
	} else if _, err := fmt.Fprintf(w, "\nchanges since %s:\n%s", o.OldRelease, notes); err != nil {
		return err
	}
	for _, id := range o.VaultIDs {
		args := []string{"--home", o.Home, "--vault", id, "vault", "upgrade"}
		if _, err := fmt.Fprintf(w, "\nvault %s:\n", id); err != nil {
			return err
		}
		if err := stream(ctx, w, o.Src, bin, append(args, "--file")...); err != nil {
			_, _ = fmt.Fprintf(w, "could not file the judgement steps: %v\n", err)
		}
	}
	return nil
}

func printGoInstall(w io.Writer) error {
	_, err := fmt.Fprintf(w, "This para was not built from a git checkout. Run:\n  %s\nThat build has no version stamp. This repo is private: go install needs GOPRIVATE set and git access to it; otherwise use a checkout.\nThen restart the service (para service restart) and any MCP server.\n", goInstallLine)
	return err
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func runIn(ctx context.Context, dir, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	b, err := c.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return string(b), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
	}
	return string(b), err
}

func stream(ctx context.Context, w io.Writer, dir, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir, c.Stdout, c.Stderr = dir, w, w
	return c.Run()
}

func newUpgradeCmd(l *loader) *cobra.Command {
	var src string
	cmd := &cobra.Command{
		Use:   "upgrade [--src <checkout>]",
		Short: "Pull and install the newest para from a git checkout, and show what changed",
		Long: `From a git checkout (the one this binary was built from, or --src): git pull
--ff-only, then make install, which also restarts the para service. It refuses
when tracked files have uncommitted changes, and it never commits or pushes.
Without a checkout it prints the go install line to run.

After the install it prints the changes between the old and new version, and
queues each vault's pending judgement steps as agent work. Run para vault
upgrade --dry-run yourself for the automated steps.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			if src == "" {
				src = l.srcDir
			}
			o := upgradeOptions{Src: src, Version: l.version, OldRelease: changes.Latest(), Home: l.install.Home}
			if vs, err := l.install.List(); err == nil {
				for _, v := range vs {
					o.VaultIDs = append(o.VaultIDs, v.ID)
				}
			}
			return runUpgrade(cmd.Context(), cmd.OutOrStdout(), o)
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "the git checkout to upgrade from; default the one this binary was built from")
	return cmd
}
