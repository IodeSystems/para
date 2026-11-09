package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/iodesystems/para/changes"
	"github.com/iodesystems/para/internal/model"
)

// VaultCheckKind is the kind of the one agent work request that holds the
// vault's lint findings. An open one is updated, never added again.
const VaultCheckKind = "vault-check"

// vaultCheckPrefix starts the title of that request.
const vaultCheckPrefix = "Vault check: "

// vaultCheckFindings is the section of the request that lists the findings.
const vaultCheckFindings = "## Findings"

// VaultCheckInput has no fields.
type VaultCheckInput struct{}

// VaultCheckOutput is the lint of the vault and the request that holds its
// findings. Request is empty when lint found nothing. Updated is true when an
// open vault-check request was updated, not added.
type VaultCheckOutput struct {
	Errors   int             `json:"errors"`
	Warnings int             `json:"warnings"`
	Findings []model.Finding `json:"findings"`
	Request  string          `json:"request"`
	Updated  bool            `json:"updated"`
}

// Failed implements Failing.
func (o VaultCheckOutput) Failed() error {
	if o.Errors > 0 {
		return fmt.Errorf("lint: %d errors", o.Errors)
	}
	return nil
}

// VaultCheck lints the vault and files the findings as agent work.
var VaultCheck = definePerson("vault_check", "Lint every document in the vault and file the findings as one agent work request of kind vault-check (an open one is updated, not added again). The agent fixes the data with ops, or reports a para bug for the owner to forward", func(ctx context.Context, d Deps, _ VaultCheckInput) (VaultCheckOutput, error) {
	findings, err := d.Store.Lint(ctx)
	if err != nil {
		return VaultCheckOutput{}, err
	}
	lo := NewLintOutput(findings)
	out := VaultCheckOutput{Errors: lo.Errors, Warnings: lo.Warnings, Findings: lo.Findings}
	if out.Request, out.Updated, err = fileVaultCheck(ctx, d, lo, ""); err != nil {
		return out, err
	}
	return out, nil
})

// fileVaultCheck files lint findings as the one vault-check request, or
// updates the open one. step is the upgrade step that stopped on them, or
// empty. Nothing is filed for no findings.
func fileVaultCheck(ctx context.Context, d Deps, lo LintOutput, step string) (id string, updated bool, err error) {
	if len(lo.Findings) == 0 {
		return "", false, nil
	}
	title := fmt.Sprintf("%s%d errors, %d warnings", vaultCheckPrefix, lo.Errors, lo.Warnings)
	var lines strings.Builder
	for _, f := range lo.Findings {
		lines.WriteString(f.String() + "\n")
	}
	findings := fence("text", lines.String())
	docs, err := loadRequests(ctx, d.Store)
	if err != nil {
		return "", false, fmt.Errorf("vault_check: %w", err)
	}
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if r.Kind != VaultCheckKind || (r.Status != model.RequestProposed && r.Status != model.RequestQueued && r.Status != model.RequestClaimed) {
			continue
		}
		doc.Title = title
		doc.Body = setSection(doc.Body, "## Source", vaultCheckSource(step))
		doc.Body = setSection(doc.Body, vaultCheckFindings, findings)
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return "", false, fmt.Errorf("vault_check: update %s: %w", doc.ID, err)
		}
		return doc.ID, true, nil
	}
	ask := VaultCheckInstructions + "\n\n## Source\n\n" + vaultCheckSource(step) + "\n\n" + vaultCheckFindings + "\n\n" + findings
	r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: title, Ask: ask, Kind: VaultCheckKind})
	if err != nil {
		return "", false, fmt.Errorf("vault_check: %w", err)
	}
	return r.ID, false, nil
}

// vaultCheckSource says what found the findings: the para release, and the
// upgrade step when one stopped on them.
func vaultCheckSource(step string) string {
	s := "Para release " + changes.Latest() + ". "
	if step != "" {
		return s + "para vault upgrade stopped at step " + step + ": the findings are of the documents that step would write."
	}
	return s + "para lint of the whole vault."
}

// VaultCheckInstructions is what the agent does with a vault-check request.
const VaultCheckInstructions = "Para lint found the problems below in this vault. Each line is a path, a line, the severity, the lint code, and para's message. " +
	"For each finding, decide which is wrong, the data or para.\n\n" +
	"- The data is wrong (a field, a link target, a heading, a value the rule rightly refuses): fix it with para's own write ops (doc_edit, task_edit, and the rest), with this request as request. Never edit the files by hand or with a script.\n" +
	"- Para is wrong (the lint rule refuses what para's own guide allows, or an upgrade step wrote what lint refuses): change nothing for that finding.\n" +
	"- Para cannot read the file at all (codes such as frontmatter or title on a file no op can open): no op fixes it. Change nothing, and say in the result which path the owner must fix or remove.\n\n" +
	"End with request_done. Resolution done when you fixed every finding. Otherwise resolution blocked, and the result is a bug report the owner can paste to para's developer: the para version (the release below, and `para version` when you can run it), the lint code, the upgrade step if any, the path, what para expected, and why you think para is wrong. Paths, codes, and para's messages only: never copy document content into it. " +
	"Lint runs again on the next para lint --file or para vault upgrade, and updates this request while it is open."
