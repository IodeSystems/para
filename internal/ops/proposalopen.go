package ops

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// The owner reads a proposal, or any document, in the web app and decides in
// the chat. proposal_open opens the page on the machine para runs on, and always
// returns the URL, so an agent can print it when nothing opens.

// ErrNoServer is a viewer URL asked for when no para serve runs on this machine.
var ErrNoServer = errors.New("no para serve runs on this machine: start it with `para serve`, then open the page again")

// proposalPath is the viewer path of a proposal; docPath of any other document.
func proposalPath(id string) string { return "/proposals/" + url.PathEscape(id) }
func docPath(id string) string      { return "/doc/" + url.PathEscape(id) }

// viewerBase is the web app's base URL for this vault.
func viewerBase(d Deps) (string, error) {
	if d.ViewerBase == nil {
		return "", ErrNoServer
	}
	base, err := d.ViewerBase()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(base, "/"), nil
}

// ProposalOpenInput is the input of proposal_open.
type ProposalOpenInput struct {
	// ID is a proposal id, any document id, or a URL of the web app.
	ID string `json:"id" query:"id" required:"true"`
}

// ProposalOpenOutput is the output of proposal_open.
type ProposalOpenOutput struct {
	URL     string `json:"url"`               // the page; empty only when no server is known
	Opened  bool   `json:"opened"`            // a browser was started on this machine
	Command string `json:"command,omitempty"` // what was run to open it
	Note    string `json:"note,omitempty"`    // why nothing opened, or what to do next
}

// ProposalOpen opens the web app's page of a proposal or a document. It writes
// nothing. A URL must be one of this vault's web app.
var ProposalOpen = defineRead("proposal_open", "Open the web page of a proposal or any document (an id, or a URL of the web app) in a browser on the machine para runs on, for the owner to read. Always returns the URL: print it when opened is false", func(ctx context.Context, d Deps, in ProposalOpenInput) (ProposalOpenOutput, error) {
	raw := strings.TrimSpace(in.ID)
	if raw == "" {
		return ProposalOpenOutput{}, errors.New("id is empty: give a proposal id, a document id, or a URL of the web app")
	}
	base, err := d.Store.Config(reviewBaseConfig)
	if err != nil {
		return ProposalOpenOutput{}, err
	}
	var baseErr error
	if base == "" {
		base, baseErr = viewerBase(d)
	}
	base = strings.TrimRight(base, "/")
	var page string
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := checkURL(raw)
		if err != nil {
			return ProposalOpenOutput{}, err
		}
		if baseErr != nil {
			return ProposalOpenOutput{URL: u, Note: baseErr.Error()}, nil
		}
		if !sameOrigin(u, base) {
			return ProposalOpenOutput{}, fmt.Errorf("%s is not a page of this vault's web app (%s): para opens only its own pages", u, base)
		}
		page = u
	} else {
		doc, err := d.Store.Get(ctx, normID(raw))
		if err != nil {
			return ProposalOpenOutput{}, err
		}
		path := docPath(doc.ID)
		if doc.Meta.Header().Type == model.TypeProposal {
			path = proposalPath(doc.ID)
		}
		if baseErr != nil {
			return ProposalOpenOutput{Note: baseErr.Error()}, nil
		}
		page = base + path
	}
	out := ProposalOpenOutput{URL: page}
	if d.OpenURL == nil {
		out.Note = "nothing can open a browser from here: give the owner the URL"
		return out, nil
	}
	cmd, err := d.OpenURL(page)
	out.Command = cmd
	if err != nil {
		out.Note = "could not open a browser (" + err.Error() + "): give the owner the URL"
		return out, nil
	}
	out.Opened = true
	return out, nil
})

// sameOrigin reports whether u is on the scheme, host, and port of base.
func sameOrigin(u, base string) bool {
	a, err := url.Parse(u)
	if err != nil {
		return false
	}
	b, err := url.Parse(base)
	if err != nil {
		return false
	}
	return a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host)
}
