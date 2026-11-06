package ops

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/upgrade"
)

// IngestInput is the input of ingest. Text is the source as the manual
// backend read it: pasted or piped by the user.
type IngestInput struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`   // empty: first heading of the text, else the URL host and path
	Text    string `json:"text,omitempty"`    // source text; must be empty with no_copy
	NoCopy  bool   `json:"no_copy,omitempty"` // keep summary + URL only, fidelity none
	Backend string `json:"backend,omitempty"` // empty means manual
	ID      string `json:"id,omitempty"`      // empty means generated
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
}

// IngestOutput is the output of ingest.
type IngestOutput struct {
	ID           string         `json:"id"`
	TranscriptID string         `json:"transcript_id"` // empty with no_copy
	Fidelity     model.Fidelity `json:"fidelity"`
	Hash         string         `json:"hash"` // empty with no_copy
	Verified     bool           `json:"verified"`
}

// Ingest stores a source document as a policy: a summary stub marked
// unverified, and a transcript. At the policy docs format the summary is an
// area of kind concept that follows the source.
var Ingest = defineSafe("ingest", "Store a source document as a policy summary stub and transcript", ingest)

func ingest(ctx context.Context, d Deps, in IngestInput) (IngestOutput, error) {
	u, err := checkURL(in.URL)
	if err != nil {
		return IngestOutput{}, err
	}
	if in.ID != "" && (!paramd.ValidStem(in.ID) || strings.HasSuffix(in.ID, ".source")) {
		return IngestOutput{}, fmt.Errorf("id %q is not a valid policy id", in.ID)
	}
	if in.NoCopy && strings.TrimSpace(in.Text) != "" {
		return IngestOutput{}, errors.New("text given with no_copy: nothing would be stored")
	}
	backend := in.Backend
	if backend == "" {
		backend = backendManual
	}
	src := model.Source{URL: u, Retrieved: timestamp(), Fidelity: model.FidelityNone, Backend: backend}
	var norm string
	if !in.NoCopy {
		if norm, err = Normalize(in.Text); err != nil {
			return IngestOutput{}, err
		}
		src.Fidelity, src.Hash = model.FidelityExact, hashText(norm)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = defaultTitle(norm, u)
	}
	if strings.ContainsAny(title, "\r\n") {
		return IngestOutput{}, errors.New("title must be one line")
	}

	// Lint the transcript before anything is written, so a bad text leaves no
	// summary without its transcript.
	var tr model.Document
	if !in.NoCopy {
		tr = transcriptDoc("policy-pending", title, src, norm)
		if err := precheck(tr); err != nil {
			return IngestOutput{}, err
		}
	}

	sum := model.Document{
		ID:     in.ID,
		Folder: model.FolderPolicies,
		Title:  title,
		Body:   stubBody(),
		Meta:   &model.Policy{Common: model.Common{Type: model.TypePolicy}, Followed: model.Followed{Source: src}},
	}
	if policyDocsOn(d) {
		// At the policy docs format a policy is an area that follows its source.
		if sum, err = upgrade.AsFollowedArea(sum); err != nil {
			return IngestOutput{}, err
		}
		sum = sum.DropExtra()
		sum.Meta.(*model.Area).FoldedFrom = ""
	}
	pol, err := d.Store.Create(ctx, sum)
	if err != nil {
		return IngestOutput{}, err
	}
	out := IngestOutput{ID: pol.ID, Fidelity: src.Fidelity, Hash: src.Hash}
	if in.NoCopy {
		return out, nil
	}
	tr = transcriptDoc(pol.ID, title, src, norm)
	tr.Meta.(*model.Transcript).Source.Untrusted = ptr(true)
	if _, err := d.Store.Create(ctx, tr); err != nil {
		return out, fmt.Errorf("policy %s created, transcript not written: %w", pol.ID, err)
	}
	out.TranscriptID = tr.ID
	return out, nil
}

func ptr[T any](v T) *T { return &v }

// transcriptDoc builds the .source.md document of a policy.
func transcriptDoc(policy, title string, src model.Source, norm string) model.Document {
	src.Status, src.Checked = sourceCurrent, src.Retrieved
	return model.Document{
		ID:     transcriptID(policy),
		Folder: model.FolderPolicies,
		Title:  title + " (transcript)",
		Body:   "Source: " + src.URL + "\n\n" + norm,
		Meta:   &model.Transcript{Common: model.Common{Type: model.TypeTranscript}, Of: model.LinkTo(policy), Source: src},
	}
}

// stubBody is the summary of a policy that was just stored. It is for a person:
// how an agent fills it in is in the orient prompt.
func stubBody() string {
	return "Summary not written yet. Read the source.\n\n" + rulesHead
}

// checkURL requires an absolute http(s) URL. A signed or credential query
// string is refused by lint when the document is written; this check names
// the cause before any text is processed.
func checkURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("url %q must be an absolute http(s) URL", raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("url must not carry credentials")
	}
	if findings := paramd.Lint(paramd.Parse("policies/x.md", []byte("---\ntype: inbox\ncreated: 2026-01-01\n---\n\n# X\n\n"+raw+"\n"))); len(findings) > 0 {
		for _, f := range findings {
			if f.Code == paramd.CodeURL {
				return "", fmt.Errorf("url refused: %s", f.Msg)
			}
		}
	}
	return raw, nil
}

// defaultTitle is the first heading of the text, else host and path.
func defaultTitle(norm, rawURL string) string {
	for _, line := range strings.Split(norm, "\n") {
		if t := strings.TrimSpace(strings.TrimLeft(line, "#")); strings.HasPrefix(line, "#") && t != "" {
			return t
		}
	}
	u, _ := url.Parse(rawURL)
	return strings.TrimRight(path.Join(u.Host, u.Path), "/")
}
