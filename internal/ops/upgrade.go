package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/changes"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// upgradeKind is the Kind of agent work filed for a judgement step.
const upgradeKind = "vault_upgrade"

// upgradePrefix starts the title of that agent work.
const upgradePrefix = "Vault upgrade: "

// ChangesInput is the input of changes. Since is a version; empty means the
// versions after the one this session last saw, else the newest only.
type ChangesInput struct {
	Since string `json:"since,omitempty" query:"since"`
}

// ChangesOutput is the output of changes. Current is the newest version with
// notes in this binary.
type ChangesOutput struct {
	Current  string            `json:"current"`
	Since    string            `json:"since,omitempty"`
	Releases []changes.Release `json:"releases"`
	Text     string            `json:"text"`
}

// Changes returns the release notes embedded in this binary.
var Changes = defineRead("changes", "Release notes of this para: what changed for the owner and for agents (new tools, changed rules, breaking changes), for each version after since; with no since, the versions after the one this session last saw", func(_ context.Context, d Deps, in ChangesInput) (ChangesOutput, error) {
	since := strings.TrimSpace(in.Since)
	if since == "" {
		since = d.ChangesSince
	}
	rs, err := changes.Since(since)
	if err != nil {
		return ChangesOutput{}, err
	}
	out := ChangesOutput{Current: changes.Latest(), Since: since, Releases: rs}
	if out.Releases == nil {
		out.Releases = []changes.Release{}
	}
	out.Text = changes.Render(rs, "")
	if len(rs) == 0 {
		out.Text = "No changes after " + since + ".\n"
	}
	return out, nil
})

// VaultUpgradeStatusInput has no fields.
type VaultUpgradeStatusInput struct{}

// VaultUpgradeStatus lists the upgrade steps this vault still needs.
var VaultUpgradeStatus = defineRead("vault_upgrade_status", "List the format upgrade steps this vault still needs, each with the full list of its documents (docs holds all of them; count is its length): automated steps the owner runs with `para vault upgrade`, and judgement steps that arrive as agent work titled \"Vault upgrade: ...\"", func(ctx context.Context, d Deps, _ VaultUpgradeStatusInput) (upgrade.Status, error) {
	return upgrade.StatusOf(ctx, d.Store)
})

// VaultUpgradeInput is the input of vault_upgrade. Undo is a batch id from an
// earlier run. File only files each pending judgement step as agent work, and
// runs no automated step. Force runs while another install seen in the last
// 14 days runs an older para: that machine then refuses the vault.
type VaultUpgradeInput struct {
	DryRun bool   `json:"dry_run,omitempty"`
	Force  bool   `json:"force,omitempty"`
	File   bool   `json:"file,omitempty"`
	Undo   string `json:"undo,omitempty"`
}

// VaultUpgradeOutput is the output of vault_upgrade.
type VaultUpgradeOutput struct {
	Run    *upgrade.RunResult `json:"run,omitempty"`
	Undo   *model.BatchResult `json:"undo,omitempty"`
	Filed  []string           `json:"filed"`
	Status upgrade.Status     `json:"status"`
	// From is format_version before the call; Status.FormatVersion is after.
	From int `json:"from"`
	// Stopped is set when a run stopped at a step or was refused. The steps
	// before it keep their batches (Run.Steps).
	Stopped *UpgradeStop `json:"stopped,omitempty"`
	// Lint counts what lint finds after a run; Check is the vault-check agent
	// work that holds the findings.
	Lint  *LintCount `json:"lint,omitempty"`
	Check string     `json:"check,omitempty"`
}

// UpgradeStop is why a run stopped. Step is the step it stopped at, empty
// when the run was refused before any step. Errors counts the lint errors of
// the documents that step would write.
type UpgradeStop struct {
	Step   string `json:"step,omitempty"`
	Error  string `json:"error"`
	Errors int    `json:"errors,omitempty"`
}

// LintCount is the error and warning count of a lint.
type LintCount struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

// Failed implements Failing: a stopped run, or lint errors after a run.
func (o VaultUpgradeOutput) Failed() error {
	switch {
	case o.Stopped != nil:
		return errors.New(o.Stopped.Error)
	case o.Lint != nil && o.Lint.Errors > 0:
		return fmt.Errorf("vault_upgrade: lint: %d errors", o.Lint.Errors)
	}
	return nil
}

// VaultUpgrade runs the automated steps of a vault upgrade as one batch per
// step, undoes a batch, or files the judgement steps as agent work.
var VaultUpgrade = withNoUndo(definePerson("vault_upgrade", "Bring this vault to the format of this para: run each pending automated step as one undoable batch (dry_run lists the documents and changes nothing), undo a batch, or (file) only queue each pending judgement step as agent work. It refuses while another install seen in 14 days runs an older para (upgrade para there first), unless force. Never runs from an agent", func(ctx context.Context, d Deps, in VaultUpgradeInput) (VaultUpgradeOutput, error) {
	out := VaultUpgradeOutput{Filed: []string{}}
	from, err := upgrade.Current(d.Store)
	if err != nil {
		return out, err
	}
	out.From = from
	var stepLint *model.LintError
	switch {
	case in.File && (in.Undo != "" || in.DryRun):
		return out, errors.New("file takes no other input")
	case in.File:
		// Filing changes no document: it only queues agent work.
	case in.Undo != "":
		if in.DryRun {
			return out, errors.New("undo takes no other input")
		}
		r, err := upgrade.Undo(ctx, d.Store, in.Undo)
		if err != nil {
			return out, err
		}
		out.Undo = &r
	default:
		run := upgrade.Run
		if in.Force {
			run = upgrade.RunForced
		}
		r, err := run(ctx, d.Store, in.DryRun)
		var se *upgrade.StepError
		switch {
		case err == nil:
			if !in.DryRun {
				if _, err := upgrade.Settle(ctx, d.Store); err != nil {
					return out, err
				}
			}
		case errors.As(err, &se):
			out.Stopped = &UpgradeStop{Step: se.Step, Error: err.Error()}
			if errors.As(err, &stepLint) {
				out.Stopped.Errors = NewLintOutput(stepLint.Findings).Errors
			}
		case len(r.Stale) > 0:
			out.Stopped = &UpgradeStop{Error: err.Error()}
		default:
			return out, err
		}
		out.Run = &r
	}
	st, err := upgrade.StatusOf(ctx, d.Store)
	if err != nil {
		return out, err
	}
	out.Status = st
	if in.File {
		if out.Filed, err = fileUpgradeWork(ctx, d, st); err != nil {
			return out, err
		}
	}
	if out.Run != nil && !in.DryRun {
		if out.Lint, out.Check, err = checkAfterUpgrade(ctx, d, out.Stopped, stepLint); err != nil {
			return out, err
		}
	}
	return out, nil
}))

// checkAfterUpgrade lints after a run and files what it finds as the
// vault-check agent work. A step that stopped on lint files the findings of
// its documents, with the step named.
func checkAfterUpgrade(ctx context.Context, d Deps, stop *UpgradeStop, stepLint *model.LintError) (*LintCount, string, error) {
	var lo LintOutput
	step := ""
	if stop != nil && stepLint != nil {
		lo, step = NewLintOutput(stepLint.Findings), stop.Step
	} else {
		findings, err := d.Store.Lint(ctx)
		if err != nil {
			return nil, "", err
		}
		lo = NewLintOutput(findings)
	}
	id, _, err := fileVaultCheck(ctx, d, lo, step)
	return &LintCount{Errors: lo.Errors, Warnings: lo.Warnings}, id, err
}

// fileUpgradeWork queues agent work for each pending judgement step that has
// none open, and returns the request ids.
func fileUpgradeWork(ctx context.Context, d Deps, st upgrade.Status) ([]string, error) {
	filed := []string{}
	open, err := loadRequests(ctx, d.Store)
	if err != nil {
		return filed, err
	}
	have := map[string]bool{}
	for _, doc := range open {
		r := doc.Meta.(*model.Request)
		if r.Kind == upgradeKind && (r.Status == model.RequestQueued || r.Status == model.RequestClaimed) {
			have[doc.Title] = true
		}
	}
	for _, p := range st.Pending {
		step := stepByID(p.ID)
		if p.Auto || step.Instructions == "" || have[upgradePrefix+p.Title] {
			continue
		}
		ask := step.Instructions + "\n\n## Documents\n\n" + upgradeDocs(p) + "\n\n## Done when\n\n" + step.Done +
			"\n\nThe step clears by itself when its done test finds nothing. Make the edits with ops, never with a script."
		r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: upgradePrefix + p.Title, Ask: ask, Links: p.Docs, Kind: upgradeKind})
		if err != nil {
			return filed, fmt.Errorf("vault_upgrade: file %s: %w", p.ID, err)
		}
		filed = append(filed, r.ID)
	}
	return filed, nil
}

func upgradeDocs(p upgrade.Pending) string {
	var b strings.Builder
	for _, id := range p.Docs {
		b.WriteString("- " + id + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func stepByID(id string) upgrade.Step {
	for _, s := range upgrade.Steps() {
		if s.ID == id {
			return s
		}
	}
	return upgrade.Step{}
}
