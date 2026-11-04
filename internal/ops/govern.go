package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
)

// Helpers shared by the policy, rule, exception, constraint, convention, and
// copy ops.

// now is the clock of these ops. Tests replace it.
var now = time.Now

func today() model.Date                 { return model.Date(now().Format("2006-01-02")) }
func timestamp() string                 { return now().UTC().Format(time.RFC3339) }
func transcriptID(policy string) string { return policy + ".source" }

const (
	sourceCurrent = "current"
	sourceChanged = "changed"
	backendManual = "manual"
)

var (
	fenceRe   = regexp.MustCompile("^(```|~~~)")
	h1Re      = regexp.MustCompile(`^# `)
	trailWS   = regexp.MustCompile(`[ \t]+$`)
	dateRe    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	ruleRe    = regexp.MustCompile(`^(?:- )?R(\d+) ([a-z0-9.-]+): (.*) \^R(\d+)$`)
	ruleIDRe  = regexp.MustCompile(`^R[1-9]\d*$`)
	rulesHead = "## Rules"
)

// Normalize turns pasted text into para-md body text: UTF-8 without a byte
// order mark, LF line endings, no trailing blanks, at most one blank line in
// a row, and no level-1 heading (the document title is the only one). Fenced
// code is kept as written except for line endings. The result is what the
// source hash covers, so a refresh from another copy of the same text
// matches.
func Normalize(text string) (string, error) {
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var out []string
	fenced, blank := false, 0
	for _, line := range strings.Split(text, "\n") {
		if fenceRe.MatchString(strings.TrimLeft(line, " ")) {
			fenced = !fenced
		}
		if !fenced {
			line = trailWS.ReplaceAllString(line, "")
			if h1Re.MatchString(line) {
				line = "#" + line
			}
		}
		if !fenced && line == "" {
			if blank++; blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	norm := strings.Trim(strings.Join(out, "\n"), "\n")
	if strings.TrimSpace(norm) == "" {
		return "", errors.New("text is empty")
	}
	return norm, nil
}

// hashText is the source hash of normalized text.
func hashText(norm string) string {
	sum := sha256.Sum256([]byte(norm))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// getPolicy reads a policy summary.
func getPolicy(ctx context.Context, d Deps, id string) (model.Document, *model.Policy, error) {
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return doc, nil, err
	}
	p, ok := doc.Meta.(*model.Policy)
	if !ok {
		return doc, nil, fmt.Errorf("%s is a %s, not a policy", id, doc.Meta.Header().Type)
	}
	return doc, p, nil
}

// getTranscript reads the transcript of a policy. It returns nil, nil when
// the policy has none by design (fidelity none).
func getTranscript(ctx context.Context, d Deps, policy string, p *model.Policy) (*model.Document, *model.Transcript, error) {
	if p.Source.Fidelity == model.FidelityNone {
		return nil, nil, nil
	}
	doc, err := d.Store.Get(ctx, transcriptID(policy))
	if err != nil {
		return nil, nil, err
	}
	t, ok := doc.Meta.(*model.Transcript)
	if !ok {
		return nil, nil, fmt.Errorf("%s is a %s, not a transcript", doc.ID, doc.Meta.Header().Type)
	}
	return &doc, t, nil
}

// precheck lints a document the way a store would, without writing it.
func precheck(doc model.Document) error {
	if h := doc.Meta.Header(); h.Created == "" {
		h.Created = today()
	}
	data, err := model.Render(doc)
	if err != nil {
		return err
	}
	var bad []model.Finding
	rel := path.Join(string(doc.Folder), doc.ID+".md")
	for _, f := range paramd.Lint(paramd.Parse(rel, data)) {
		if f.Severity == paramd.Error {
			bad = append(bad, model.Finding{Path: f.Path, Line: f.Line, Code: f.Code, Severity: string(f.Severity), Message: f.Msg})
		}
	}
	if len(bad) > 0 {
		return &model.LintError{Findings: bad}
	}
	return nil
}

// Rule is one rule line of a policy summary.
type Rule struct {
	Policy string     `json:"policy"`
	ID     string     `json:"id"`
	Text   string     `json:"text"`
	Link   model.Link `json:"link"`
	// PolicyName is the title of the policy, for people.
	PolicyName string `json:"policy_name,omitempty" nameof:"Policy"`
}

// parseRules reads the rule lines of a policy body.
func parseRules(policy, body string) []Rule {
	rules := []Rule{}
	fenced := false
	for _, line := range strings.Split(body, "\n") {
		if fenceRe.MatchString(strings.TrimLeft(line, " ")) {
			fenced = !fenced
		}
		m := ruleRe.FindStringSubmatch(line)
		if fenced || m == nil || m[1] != m[4] {
			continue
		}
		id := "R" + m[1]
		rules = append(rules, Rule{Policy: policy, ID: id, Text: m[3], Link: model.RuleLink(policy, id)})
	}
	return rules
}

// nextRuleNumber is one more than the highest rule number in the body.
func nextRuleNumber(rules []Rule) int {
	n := 0
	for _, r := range rules {
		if v, err := strconv.Atoi(strings.TrimPrefix(r.ID, "R")); err == nil && v > n {
			n = v
		}
	}
	return n + 1
}

// addRuleLine inserts line at the end of the "## Rules" section, or appends
// a new section.
func addRuleLine(body, line string) string {
	lines := strings.Split(strings.Trim(body, "\n"), "\n")
	if body == "" {
		lines = nil
	}
	head, end, fenced := -1, len(lines), false
	for i, l := range lines {
		if fenceRe.MatchString(strings.TrimLeft(l, " ")) {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if head < 0 && strings.TrimRight(l, " ") == rulesHead {
			head = i
		} else if head >= 0 && strings.HasPrefix(l, "#") {
			end = i
			break
		}
	}
	if head < 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		return strings.Join(append(lines, rulesHead, "", line), "\n")
	}
	// Place after the last non-blank line of the section.
	at := end
	for at > head+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	out := append([]string{}, lines[:at]...)
	if at == head+1 {
		out = append(out, "")
	}
	out = append(out, line)
	out = append(out, lines[at:]...)
	return strings.Join(out, "\n")
}

// checkDate validates a YYYY-MM-DD value.
func checkDate(name, v string) error {
	if !dateRe.MatchString(v) {
		return fmt.Errorf("%s %q must be YYYY-MM-DD", name, v)
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return fmt.Errorf("%s %q is not a date", name, v)
	}
	return nil
}

func splitList(in []string) []string {
	out := []string{}
	for _, s := range in {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func oneLine(name, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if strings.ContainsAny(v, "\r\n") {
		return "", fmt.Errorf("%s must be one line", name)
	}
	return v, nil
}
