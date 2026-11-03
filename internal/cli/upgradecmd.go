package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/changes"
	"github.com/iodesystems/parasolve/internal/mcpserver"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/upgrade"
)

// goInstallLine is what to run when para was not built from a checkout.
const goInstallLine = "go install github.com/iodesystems/parasolve/cmd/para@latest"

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

// newVaultUpgradeCmd is `para vault upgrade`. --status reads; the rest is the
// person-only op.
func newVaultUpgradeCmd(l *loader) *cobra.Command {
	var status bool
	var in ops.VaultUpgradeInput
	cmd := &cobra.Command{
		Use:   "upgrade [--dry-run | --status | --file | --undo <batch>]",
		Short: "Bring this vault to the current format: run pending automated steps as one undoable batch each",
		Long: `Bring this vault to the format of this para. format_version in
.para/vault.properties says which steps are done.

An automated step rewrites documents through the vault: one batch, one event per
document. Use --dry-run first. --undo <batch> restores the files on the machine
that ran the batch. A judgement step needs an agent: --file queues each pending
one as agent work and changes no document. --status lists what is pending.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			deps, err := l.deps()
			if err != nil {
				return err
			}
			if status {
				if in.DryRun || in.File || in.Undo != "" {
					return errors.New("vault upgrade: --status takes no other flag")
				}
				out, err := ops.VaultUpgradeStatus.Run(cmd.Context(), deps, ops.VaultUpgradeStatusInput{})
				if err != nil {
					return err
				}
				return emit(cmd, out, func(w io.Writer, s upgrade.Status) error { return writeUpgradeStatus(w, s) })
			}
			out, err := ops.VaultUpgrade.Run(cmd.Context(), deps, in)
			if err != nil {
				return err
			}
			return emit(cmd, out, writeVaultUpgrade)
		},
	}
	addJSONFlag(cmd)
	cmd.Flags().BoolVar(&status, "status", false, "list the pending steps and change nothing")
	cmd.Flags().BoolVar(&in.DryRun, "dry-run", false, "list the documents each automated step would rewrite and change nothing")
	cmd.Flags().BoolVar(&in.File, "file", false, "queue each pending judgement step as agent work; runs no automated step")
	cmd.Flags().StringVar(&in.Undo, "undo", "", "restore the documents of a batch id printed by an earlier run")
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
		how := "judgement: agent work (para vault upgrade --file)"
		if p.Auto {
			how = "automated: para vault upgrade --dry-run, then para vault upgrade"
		}
		if _, err := fmt.Fprintf(w, "  %s\t%d documents\t%s\n", p.ID, p.Count, how); err != nil {
			return err
		}
	}
	return nil
}

func writeVaultUpgrade(w io.Writer, o ops.VaultUpgradeOutput) error {
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
	return writeUpgradeStatus(w, o.Status)
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
	_, err := fmt.Fprintf(w, "This para was not built from a git checkout. Run:\n  %s\nThat build has no version stamp. Then restart the service (para service restart) and any MCP server.\n", goInstallLine)
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
