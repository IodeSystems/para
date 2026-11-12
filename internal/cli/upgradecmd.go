package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/changes"
	"github.com/iodesystems/para/internal/mcpserver"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/update"
	"github.com/iodesystems/para/internal/upgrade"
	"github.com/iodesystems/para/release"
)

// newChangesCmd is `para changes`. It reads the notes embedded in this
// binary and no vault, so it runs before the first para init and right after
// para upgrade on a machine with no vault.
func newChangesCmd(l *loader) *cobra.Command {
	var in ops.ChangesInput
	text := func(w io.Writer, out ops.ChangesOutput) error {
		_, err := io.WriteString(w, out.Text)
		return err
	}
	cmd := opCommand(l, ops.Changes, binding[ops.ChangesInput, ops.ChangesOutput]{
		Use:  "changes [--since <version>]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, _ *ops.ChangesInput) {
			fs.StringVar(&in.Since, "since", "", "show the notes of every version after this one; default the newest only")
		},
		Text: text,
	})
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		out, err := ops.Changes.Run(cmd.Context(), ops.Deps{}, in)
		if err != nil {
			return err
		}
		return emit(cmd, out, text)
	}
	return cmd
}

// newVaultUpgradeCmd is `para vault upgrade`: the person-only op, with
// `status` as the read-only subcommand.
func newVaultUpgradeCmd(l *loader) *cobra.Command {
	cmd := opCommand(l, ops.VaultUpgrade, binding[ops.VaultUpgradeInput, ops.VaultUpgradeOutput]{
		Use:  "upgrade [--dry-run | --file | --undo <batch>] [--force]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.VaultUpgradeInput) {
			fs.BoolVar(&in.DryRun, "dry-run", false, "list the documents each automated step would rewrite and change nothing")
			fs.BoolVar(&in.File, "file", false, "queue each pending judgement step as agent work, and one item asking the owner to run the automated steps; runs no automated step")
			fs.StringVar(&in.Undo, "undo", "", "restore the documents of a batch id printed by an earlier run")
			fs.BoolVar(&in.Force, "force", false, "run while another install seen in 14 days runs an older para; that machine then refuses the vault until it is upgraded")
			fs.BoolVar(&in.Unattended, "unattended", false, "a run nobody asked for now (after an auto-install): leave the steps only the owner runs pending")
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
one as agent work, and one item that asks the owner to run the pending
automated steps; it changes no document. "upgrade status" lists what is pending.

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

// writeCounts prints a step's counts, known names first in their order.
func writeCounts(w io.Writer, counts map[string]int) error {
	if len(counts) == 0 {
		return nil
	}
	names := slices.Clone(upgrade.CountOrder)
	for k := range counts {
		if !slices.Contains(names, k) {
			names = append(names, k)
		}
	}
	for _, k := range names {
		if n, ok := counts[k]; ok {
			if _, err := fmt.Fprintf(w, "  %s: %d\n", k, n); err != nil {
				return err
			}
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
			if s.Skipped != "" {
				if _, err := fmt.Fprintf(w, "%s: not run: %s\n", s.ID, s.Skipped); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(w, "%s: %s %d documents\n", s.ID, verb, s.Count); err != nil {
				return err
			}
			if err := writeCounts(w, s.Counts); err != nil {
				return err
			}
			if s.Snapshot != "" {
				if _, err := fmt.Fprintf(w, "  snapshot: %s (restore by hand: stop para on every machine, untar over the vault)\n", s.Snapshot); err != nil {
					return err
				}
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
// checkout, the programs, the release source, and the new binary.
type upgradeOptions struct {
	Src        string // the checkout; empty means not built from one
	Git, Make  string // program names or paths
	NewBin     string // the binary make install produced; empty means this one
	Version    string // the running binary's version
	OldRelease string // the newest release notes in the running binary
	Home       string // install home passed to the new binary
	VaultIDs   []string
	Skipped    []string // vaults this machine does not hold, with why

	Unattended bool // vault steps run with --unattended: an auto-install, nobody asked now

	Release bool // install the newest release even with a checkout
	Force   bool // install a release that is not newer than this one
	Updater update.Upgrader
	// Egress logs the fetch in each vault before it goes out; nil logs
	// nothing (the auto-install logged it already).
	Egress func(ctx context.Context, dest string) error

	NoVaultSteps bool                // run no automated vault step
	Yes          bool                // apply automated vault steps without asking
	Ask          func(q string) bool // nil: no terminal to ask on
}

// runUpgrade upgrades para — from the checkout (git pull --ff-only, make
// install) or from the newest signed release — and then reports what changed
// and brings each vault along. It never commits and never pushes, and it
// refuses a dirty checkout.
func runUpgrade(ctx context.Context, w io.Writer, o upgradeOptions) error {
	var (
		newVersion, bin string
		err             error
	)
	if o.Release || o.Src == "" || !isCheckout(o.Src) {
		o.Src = "" // the new binary runs in the current directory
		newVersion, bin, err = upgradeFromRelease(ctx, w, o)
	} else {
		newVersion, bin, err = upgradeFromCheckout(ctx, w, o)
	}
	if err != nil || newVersion == "" {
		return err
	}
	if _, err := fmt.Fprintf(w, "\nupgraded para: %s -> %s\n", o.Version, newVersion); err != nil {
		return err
	}
	if bin == "" {
		return nil
	}
	notes, err := runIn(ctx, o.Src, bin, "changes", "--since", o.OldRelease)
	if err != nil {
		_, _ = fmt.Fprintf(w, "could not read the changes from %s: %v\n", bin, err)
	} else if _, err := fmt.Fprintf(w, "\nchanges since %s:\n%s", o.OldRelease, notes); err != nil {
		return err
	}
	for _, s := range o.Skipped {
		if _, err := fmt.Fprintf(w, "\nskipped %s\n", s); err != nil {
			return err
		}
	}
	for _, id := range o.VaultIDs {
		if err := vaultSteps(ctx, w, o, bin, id); err != nil {
			return err
		}
	}
	refreshSkill(ctx, w, o.Src, bin)
	return nil
}

// refreshSkill reinstalls the agent skill with the new binary, so the skill
// never names an op the new para removed. Only a skill that para skill
// install wrote (it has a version marker) is touched; a hand-written or
// absent skill is left alone.
func refreshSkill(ctx context.Context, w io.Writer, dir, bin string) {
	have, ok := installedSkill("")
	if !ok || have == "" {
		return
	}
	if _, err := runIn(ctx, dir, bin, "skill", "install"); err != nil {
		_, _ = fmt.Fprintf(w, "could not update the para skill (%s): %v; run para skill install\n", have, err)
		return
	}
	_, _ = fmt.Fprintf(w, "updated the para skill (was %s)\n", have)
}

func isCheckout(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// upgradeFromCheckout pulls and installs the checkout. An empty version means
// para is current.
func upgradeFromCheckout(ctx context.Context, w io.Writer, o upgradeOptions) (version, bin string, err error) {
	git := or(o.Git, "git")
	out, err := runIn(ctx, o.Src, git, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", "", fmt.Errorf("upgrade: git status: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		return "", "", fmt.Errorf("upgrade: %s has uncommitted changes to tracked files: commit or stash them, then run para upgrade again", o.Src)
	}
	if err := stream(ctx, w, o.Src, git, "pull", "--ff-only"); err != nil {
		return "", "", fmt.Errorf("upgrade: git pull --ff-only: %w", err)
	}
	desc, err := runIn(ctx, o.Src, git, "describe", "--always", "--dirty", "--tags")
	if err != nil {
		return "", "", fmt.Errorf("upgrade: git describe: %w", err)
	}
	version = strings.TrimSpace(desc)
	if version == o.Version {
		_, err := fmt.Fprintf(w, "para is current: %s\n", o.Version)
		return "", "", err
	}
	if err := stream(ctx, w, o.Src, or(o.Make, "make"), "install"); err != nil {
		return "", "", fmt.Errorf("upgrade: make install: %w", err)
	}
	bin = o.NewBin
	if bin == "" {
		bin = mcpserver.ExecutablePath()
	}
	return version, bin, nil
}

// upgradeFromRelease installs the newest release over o.Updater.Exe after
// verifying its signed manifest, then restarts the service when one runs. A
// build that is not a release version (a checkout's git describe) counts as
// the release of its newest notes. An empty version means para is current.
func upgradeFromRelease(ctx context.Context, w io.Writer, o upgradeOptions) (version, bin string, err error) {
	res, err := installRelease(ctx, w, o)
	if err != nil || !res.Installed {
		return "", "", err
	}
	restartService(ctx, w, o.Updater.Exe, o.Home)
	return res.To, o.Updater.Exe, nil
}

// releaseCurrent is the release a build counts as: its version when that is
// a release version, else v<newest release notes>.
func releaseCurrent(version, oldRelease string) string {
	if _, err := update.ParseVersion(version); err == nil {
		return version
	}
	return "v" + or(oldRelease, "0.0.0")
}

// installRelease is the verify-and-swap half of upgradeFromRelease; the auto
// install runs it too.
func installRelease(ctx context.Context, w io.Writer, o upgradeOptions) (update.Result, error) {
	u := o.Updater
	u.Product, u.Force = "para", o.Force
	u.Current = releaseCurrent(o.Version, o.OldRelease)
	if _, err := fmt.Fprintf(w, "checking %s\n", u.ManifestURL); err != nil {
		return update.Result{}, err
	}
	if o.Egress != nil {
		if err := o.Egress(ctx, update.DestRelease); err != nil {
			return update.Result{}, fmt.Errorf("upgrade: %w", err)
		}
	}
	res, err := u.Run(ctx)
	if err != nil {
		return res, fmt.Errorf("upgrade: release: %w", err)
	}
	if !res.Installed {
		_, err := fmt.Fprintf(w, "para is current: %s (newest release %s)\n", o.Version, res.To)
		return res, err
	}
	// The hold of a pending auto-install ends with any install.
	if u.StatePath != "" {
		_ = update.ClearPending(u.StatePath)
	}
	if _, err := fmt.Fprintf(w, "installed para %s at %s (signature and sha256 checked)\n", res.To, u.Exe); err != nil {
		return res, err
	}
	if res.Summary != "" {
		_, _ = fmt.Fprintf(w, "%s\n", res.Summary)
	}
	return res, nil
}

// restartService restarts the para service with exe when one runs.
func restartService(ctx context.Context, w io.Writer, exe, home string) {
	if _, err := runIn(ctx, "", exe, "--home", home, "service", "status"); err != nil {
		_, _ = fmt.Fprintln(w, "no para service runs: restart para serve to run the new binary")
		return
	}
	if err := stream(ctx, w, "", exe, "--home", home, "service", "restart"); err != nil {
		_, _ = fmt.Fprintf(w, "could not restart the para service: %v\n", err)
	} else {
		_, _ = fmt.Fprintln(w, "restarted the para service")
	}
}

// vaultSteps brings one vault to the new format: the automated steps (a dry
// run first, then a run when asked, or with --yes), then the judgement steps
// queued as agent work.
func vaultSteps(ctx context.Context, w io.Writer, o upgradeOptions, bin, id string) error {
	base := []string{"--home", o.Home, "--vault", id, "vault", "upgrade"}
	run := base
	if o.Unattended {
		run = append(slices.Clone(base), "--unattended")
	}
	if _, err := fmt.Fprintf(w, "\nvault %s:\n", id); err != nil {
		return err
	}
	if !o.NoVaultSteps && autoPending(ctx, w, o.Src, bin, base) {
		if err := stream(ctx, w, o.Src, bin, append(base, "--dry-run")...); err != nil {
			_, _ = fmt.Fprintf(w, "the dry run failed, so no step runs: %v\n", err)
		} else {
			cmd := "para --vault " + id + " vault upgrade"
			switch {
			case o.Yes || (o.Ask != nil && o.Ask("run the automated steps above on vault "+id+"?")):
				if err := stream(ctx, w, o.Src, bin, run...); err != nil {
					_, _ = fmt.Fprintf(w, "the automated steps failed: %v\n", err)
				}
			case o.Ask != nil:
				_, _ = fmt.Fprintf(w, "not run; later: %s\n", cmd)
			default:
				_, _ = fmt.Fprintf(w, "no terminal to ask on, so no step ran: run %s (or para upgrade --yes)\n", cmd)
			}
		}
	}
	if err := stream(ctx, w, o.Src, bin, append(base, "--file")...); err != nil {
		_, _ = fmt.Fprintf(w, "could not file the judgement steps: %v\n", err)
	}
	return nil
}

// autoPending reports whether the vault has an automated step pending, by
// the new binary's vault upgrade status.
func autoPending(ctx context.Context, w io.Writer, dir, bin string, base []string) bool {
	out, err := runIn(ctx, dir, bin, append(base, "status", "--json")...)
	var st upgrade.Status
	if err == nil {
		err = json.Unmarshal([]byte(out), &st)
	}
	if err != nil {
		_, _ = fmt.Fprintf(w, "could not read the vault's upgrade status: %v\n", err)
		return false
	}
	for _, p := range st.Pending {
		if p.Auto {
			return true
		}
	}
	return false
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

// The release source; tests replace them with a test server and a test key.
var (
	upgradeManifestURL = release.ManifestURL
	upgradeKeys        = func() ([]update.PublicKey, error) { return update.ParseKeys(release.PublicKey) }
	upgradeFetcher     = &update.Fetcher{}
)

// machineVaults lists the vaults this machine holds (machine.domains), and
// the others with why they are skipped.
func machineVaults(l *loader) (ids, skipped []string) {
	vs, err := l.install.List()
	if err != nil {
		return nil, nil
	}
	for _, v := range vs {
		if err := l.install.CheckVault(v.ID, v.Path); err != nil {
			skipped = append(skipped, v.ID+": "+err.Error())
			continue
		}
		ids = append(ids, v.ID)
	}
	return ids, skipped
}

// cliEgress logs one egress record for dest in each vault this machine holds.
// A vault that does not open here (one newer than this para, which is what an
// upgrade fixes) is skipped with a note, so it cannot block the upgrade.
func cliEgress(ctx context.Context, w io.Writer, l *loader, dest string) error {
	vs, err := l.install.List()
	if err != nil {
		return err
	}
	for _, v := range vs {
		d, err := l.depsFor(v.ID, v.Path)
		if err != nil {
			_, _ = fmt.Fprintf(w, "egress not logged in vault %s: %v\n", v.ID, err)
			continue
		}
		if _, err := d.Store.Egress(ctx, "", dest); err != nil {
			return fmt.Errorf("vault %s: %w", v.ID, err)
		}
	}
	return nil
}

// releaseUpdater is the Upgrader of this install: the release source, the
// key built in, this binary, and the state in the install home.
func releaseUpdater(l *loader) (update.Upgrader, error) {
	keys, err := upgradeKeys()
	if err != nil {
		return update.Upgrader{}, fmt.Errorf("upgrade: the release key: %w", err)
	}
	return update.Upgrader{
		Product: "para", ManifestURL: upgradeManifestURL, Keys: keys, Exe: upgradeTarget(),
		StatePath: update.StateFile(l.install.Home), Platform: update.Platform(runtime.GOOS, runtime.GOARCH),
		Fetch: upgradeFetcher,
	}, nil
}

// upgradeTarget is the binary a release replaces: this one, or
// ~/.local/bin/para when this one cannot be found.
func upgradeTarget() string {
	if p := mcpserver.ExecutablePath(); p != "" {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "para")
}

func newUpgradeCmd(l *loader) *cobra.Command {
	var (
		src string
		o   upgradeOptions
	)
	cmd := &cobra.Command{
		Use:   "upgrade [--src <checkout>] [--release] [--force] [--yes] [--no-vault-steps]",
		Short: "Install the newest para (from a git checkout or a signed release), show what changed, and upgrade each vault",
		Long: `From a git checkout (the one this binary was built from, or --src): git pull
--ff-only, then make install, which also restarts the para service. It refuses
when tracked files have uncommitted changes, and it never commits or pushes.

Without a checkout, or with --release: the newest release from
github.com/IodeSystems/para. Its manifest's minisign signature is checked with
the key built into para, and the download's size and sha256 with the manifest;
then the binary replaces this one (a temp file beside it, renamed over it), and
the para service is restarted when it runs. An older release than this para is
refused (--force installs it), and so is a manifest older than one seen before.

After the install it prints the changes between the old and new version. Then,
for each vault this machine holds (machine.domains): when an automated vault
step is pending, it prints the dry run and asks to run it (--yes runs it
without asking; with no terminal and no --yes it prints the command instead;
--no-vault-steps skips this), and it queues the pending judgement steps as
agent work.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			if src == "" {
				src = l.srcDir
			}
			o.Src, o.Version, o.OldRelease, o.Home = src, l.version, changes.Latest(), l.install.Home
			o.VaultIDs, o.Skipped = machineVaults(l)
			u, err := releaseUpdater(l)
			if err != nil {
				return err
			}
			o.Updater = u
			o.Egress = func(ctx context.Context, dest string) error { return cliEgress(ctx, cmd.ErrOrStderr(), l, dest) }
			if in := cmd.InOrStdin(); l.stdinIsTTY(in) {
				o.Ask = askYesNo(cmd.OutOrStdout(), bufio.NewReader(in))
			}
			return runUpgrade(cmd.Context(), cmd.OutOrStdout(), o)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&src, "src", "", "the git checkout to upgrade from; default the one this binary was built from")
	fs.BoolVar(&o.Release, "release", false, "install the newest signed release, even when this para was built from a checkout")
	fs.BoolVar(&o.Force, "force", false, "install the newest release even when it is not newer than this para")
	fs.BoolVar(&o.Yes, "yes", false, "run each vault's pending automated steps without asking (the dry run still prints first)")
	fs.BoolVar(&o.NoVaultSteps, "no-vault-steps", false, "run no automated vault step; judgement steps are still queued")
	return cmd
}

// askYesNo asks q on w and reads y or yes from r.
func askYesNo(w io.Writer, r *bufio.Reader) func(string) bool {
	return func(q string) bool {
		if _, err := fmt.Fprintf(w, "%s [y/N] ", q); err != nil {
			return false
		}
		line, _ := r.ReadString('\n')
		a := strings.ToLower(strings.TrimSpace(line))
		return a == "y" || a == "yes"
	}
}
